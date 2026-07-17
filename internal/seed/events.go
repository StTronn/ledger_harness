package seed

import (
	"fmt"

	"github.com/razorpay/ledger-flow/internal/money"
)

// This file holds the per-event constructors (one Razorpay-shaped object each)
// and the per-event truth-GL entry builders. Each constructor draws its random
// fields from the seeded RNG in a fixed order; each GL builder appends one
// balanced entry whose lines mirror the matching playbook entry type, with all
// amounts in integer paise.

// makePayment mints one captured payment at the given timestamp, drawing a gross
// in the configured range and a GST rate from the catalogue. Fee and tax follow
// from gross via the fee rate and the Razorpay GST rate.
func (g *generator) makePayment(ts int64) Payment {
	gross := money.FromPaise(g.rng.Int64Range(grossLoPaise, grossHiPaise))
	rate := g.rng.IntRange(0, len(gstRatePercents)-1)
	gstRate := gstRatePercents[rate]
	sku := g.rng.Pick(skuOptions)
	method := g.rng.Pick(methodOptions)

	fee := feeForGross(gross, feeBps)
	tax := gstOnFee(fee, razorpayGSTRate)

	return Payment{
		Entity:    "payment",
		ID:        g.ids.payment(),
		Amount:    gross,
		Currency:  "INR",
		Status:    "captured",
		OrderID:   g.ids.order(),
		Method:    method,
		Captured:  true,
		Fee:       fee,
		Tax:       tax,
		CreatedAt: ts,
		Notes:     Notes{SKU: sku, GSTRate: fmt.Sprintf("%d", gstRate)},
	}
}

// orderLeadSeconds is how far BEFORE its payment an order is created. A real
// order is created when the customer initiates checkout, a short while before
// the payment is captured; a small fixed lead keeps that ordering without a new
// RNG draw, so the clean stream is unchanged.
const orderLeadSeconds = 300

// makeOrder mints the authoritative order a payment was captured against (SPEC
// §2). It copies the payment's TRUE notes (sku + gst_rate) and gross amount, and
// reuses the payment's already-minted OrderID — so no extra RNG draw is taken and
// the order is the legitimate place a downstream consumer recovers the GST rate
// from when a payment's own notes have been stripped of it. The order is NOT
// truth: it is another snapshotted agent-input fixture.
func (g *generator) makeOrder(pay Payment) Order {
	o := Order{
		Entity:    "order",
		ID:        pay.OrderID,
		Amount:    pay.Amount,
		Currency:  "INR",
		Status:    "paid",
		Receipt:   receiptFor(pay.OrderID),
		CreatedAt: pay.CreatedAt - orderLeadSeconds,
		Notes:     pay.Notes,
	}
	// Partial-refunds world: the order carries its two-line-item breakdown (the
	// matching substrate for the refund-intent judgment, partial.go). Amounts are
	// derived arithmetically from the order total — no RNG draw — so the clean
	// stream is untouched; both items carry the order's (single) rate.
	if g.partial {
		i0, i1 := orderItemSplit(pay.Amount)
		o.Items = []OrderItem{
			{SKU: pay.Notes.SKU, Amount: i0, GSTRate: pay.Notes.GSTRate},
			{SKU: pay.Notes.SKU + "-ADDON", Amount: i1, GSTRate: pay.Notes.GSTRate},
		}
	}
	return o
}

// receiptFor derives a deterministic merchant receipt number from an order id,
// reusing the order id's token so no RNG draw is consumed. Real receipts are
// merchant-assigned opaque strings; a stable "rcpt_"+token mirrors that shape.
func receiptFor(orderID string) string {
	return "rcpt_" + orderID[len(prefixOrder):]
}

// makeRefund mints a full refund of the given payment at the settlement
// timestamp (refunds in v1 are full-amount and netted in the same batch). The
// notes (sku + gst_rate) are copied from the payment so a downstream consumer can
// split the refund's GST without re-fetching the order.
func (g *generator) makeRefund(pay Payment, ts int64) Refund {
	return Refund{
		Entity:    "refund",
		ID:        g.ids.refund(),
		Amount:    pay.Amount,
		Currency:  "INR",
		PaymentID: pay.ID,
		Status:    "processed",
		CreatedAt: ts,
		Notes:     pay.Notes,
	}
}

// makeDispute mints a LOST dispute against the given payment. The dispute lands
// disputeLagDays after the payment's capture day; its amount is the full gross.
func (g *generator) makeDispute(pay Payment, captureDayOffset int) Dispute {
	ts := g.cal.epochForDayOffset(captureDayOffset + disputeLagDays)
	return Dispute{
		Entity:    "dispute",
		ID:        g.ids.dispute(),
		Amount:    pay.Amount,
		Currency:  "INR",
		PaymentID: pay.ID,
		Status:    "lost",
		Reason:    "chargeback_fraud",
		CreatedAt: ts,
		Notes:     pay.Notes,
	}
}

// makeSettlement mints the settlement payout for a batch. net_deposit, fee, and
// tax are the batch aggregates computed by the caller; UTR is a deterministic
// bank reference the bank-feed credit will match on.
func (g *generator) makeSettlement(netDeposit, fee, tax money.Money, ts int64, paymentIDs, refundIDs []string) Settlement {
	id := g.ids.settlement()
	return Settlement{
		Entity:     "settlement",
		ID:         id,
		Amount:     netDeposit,
		Currency:   "INR",
		Status:     "processed",
		Fee:        fee,
		Tax:        tax,
		UTR:        utrFor(id),
		CreatedAt:  ts,
		PaymentIDs: paymentIDs,
		RefundIDs:  refundIDs,
	}
}

// utrFor derives a deterministic bank UTR (unique transaction reference) from a
// settlement id, so the settlement and its matching bank-feed credit share a
// stable ref without an extra RNG draw. Real UTRs are opaque alphanumerics; we
// reuse the settlement id's token to keep them tied.
func utrFor(settlementID string) string {
	// settlementID is "setl_" + token; reuse the token as the UTR body.
	return "UTR" + settlementID[len(prefixSettlement):]
}

// gstRatePercentOf reads back the integer GST rate stamped in a payment's notes.
// Refunds and disputes copy the payment's notes, so this is the one place the
// rate string is turned back into an int for the GST split — float-free.
func gstRatePercentOf(pay Payment) int {
	s := pay.Notes.GSTRate
	n := 0
	for i := 0; i < len(s); i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// addSaleEntry binds and posts the dtc_sale truth-GL entry for a payment. The
// playbook template expands to:
//
//	Dr assets/razorpay-settlement-receivable {gross}
//	Cr income/product-sales                  {net}
//	Cr liabilities/gst-output-payable        {gst}
//
// net + gst == gross by construction (splitGSTInclusive), so the ledger accepts
// the post; the payment id is the event id and the tx id stamped on the entry.
func (g *generator) addSaleEntry(pay Payment, net, gst money.Money) error {
	params := map[string]money.Money{
		"gross":      pay.Amount,
		"net":        net,
		"gst":        gst,
		"payment_id": txIDParam(),
	}
	return g.binder.bind("dtc_sale", "sale:"+pay.ID, pay.ID, pay.ID, pay.CreatedAt, params)
}

// addRefundEntry binds and posts the refund_reversal truth-GL entry for a refund.
// The playbook template expands to:
//
//	Dr income/sales-returns                  {net}
//	Dr liabilities/gst-output-payable        {gst}
//	Cr assets/razorpay-settlement-receivable {net+gst}
func (g *generator) addRefundEntry(rf Refund, net, gst money.Money) error {
	return g.addRefundEntryAs("refund_reversal", rf, net, gst)
}

// addRefundEntryAs binds and posts a refund-shaped truth-GL entry under the given
// entry type. refund_reversal and price_adjustment share the same params
// ({net, gst, refund_id}) and the same Cr-receivable shape, differing only in the
// contra-revenue account their templates name — the partial-refunds world books
// R2/R3 (goodwill / unexplained) as price_adjustment while R1 stays a
// refund_reversal. The IK keeps the canonical "refund:"+id scheme either way (one
// refund, one entry, whichever treatment truth assigns it).
func (g *generator) addRefundEntryAs(entryType string, rf Refund, net, gst money.Money) error {
	params := map[string]money.Money{
		"net":       net,
		"gst":       gst,
		"refund_id": txIDParam(),
	}
	return g.binder.bind(entryType, "refund:"+rf.ID, rf.ID, rf.ID, rf.CreatedAt, params)
}

// addSettlementEntry binds and posts the razorpay_settlement truth-GL entry for a
// settlement, crediting the receivable by the batch's remaining gross. The
// playbook template expands to:
//
//	Dr assets/bank                            {net_deposit}
//	Dr expense/processor-fees                 {fee}
//	Dr expense/gst-input                      {gst_on_fee}
//	Cr assets/razorpay-settlement-receivable  {gross}
//
// By construction net_deposit + fee + gst_on_fee == gross, so the ledger accepts
// the post. The settlement UTR is the tx id; the settlement id is the event id.
func (g *generator) addSettlementEntry(setl Settlement, gross money.Money) error {
	params := map[string]money.Money{
		"net_deposit": setl.Amount,
		"fee":         setl.Fee,
		"gst_on_fee":  setl.Tax,
		"gross":       gross,
		"bank_tx_id":  txIDParam(),
	}
	return g.binder.bind("razorpay_settlement", "settle:"+setl.ID, setl.ID, setl.UTR, setl.CreatedAt, params)
}

// addDisputeEntry binds and posts the chargeback_loss truth-GL entry for a lost
// dispute. The playbook template expands to:
//
//	Dr expense/chargeback-loss        {net}
//	Dr liabilities/gst-output-payable {gst}
//	Cr assets/bank                    {net+gst}
func (g *generator) addDisputeEntry(disp Dispute, net, gst money.Money) error {
	params := map[string]money.Money{
		"net":        net,
		"gst":        gst,
		"dispute_id": txIDParam(),
	}
	return g.binder.bind("chargeback_loss", "dispute:"+disp.ID, disp.ID, disp.ID, disp.CreatedAt, params)
}

// addBankCreditForSettlement appends the independent bank-feed credit for a
// settlement deposit, matched on the settlement UTR (SPEC §7 check #1).
func (g *generator) addBankCreditForSettlement(setl Settlement) {
	g.bankCredits = append(g.bankCredits, BankFeedEntry{
		Amount: setl.Amount,
		Date:   dateString(setl.CreatedAt),
		Ref:    setl.UTR,
	})
}

// addBankDebitForDispute appends the independent bank-feed debit for a lost
// dispute's cash claw-back, matched on the dispute id. The amount is the full
// disputed gross (net + gst), mirroring the chargeback_loss credit to bank.
func (g *generator) addBankDebitForDispute(disp Dispute) {
	g.bankDebits = append(g.bankDebits, BankFeedEntry{
		Amount: disp.Amount,
		Date:   dateString(disp.CreatedAt),
		Ref:    disp.ID,
	})
}
