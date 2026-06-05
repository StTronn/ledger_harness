// Package classify is the deterministic PER-EVENT rule engine of close-agent
// (SPEC §2 Phase 4, §4.2, §5). It maps each normalized event to exactly one of
// the four v1 playbook entry types and computes the derived, integer-paise
// params those entry types need (the inclusive-GST split for sales/refunds/
// disputes, and the fee gross-up for settlements). It is the deterministic 95%
// of the close: hand-written rules, no LLM, no wall clock, no randomness.
//
// # The agent seam (SPEC §3, §8)
//
// Classification is ALSO the seam the Phase-7 Flue agent will fill on a rule
// miss. The agent's only way to affect the ledger is to return an
// {entry_type, params} pair that the ledger then validates (balance-or-reject);
// it never emits raw debits/credits. So Classification carries exactly that —
// an EntryType name and a Params map keyed to the playbook — plus the
// bookkeeping the orchestrator needs to post it (IK, TxID, Ts). Whether a
// Classification came from a rule here or from the agent later, the orchestrator
// binds and posts it identically.
//
// # Rule miss, never crash (SPEC §2 Phase 4)
//
// Classify returns ok=false on a RULE MISS — an event no rule covers, or an
// event missing the metadata a rule needs (e.g. a payment with no gst_rate). A
// miss carries a human-readable Reason and is FLAGGED and SKIPPED by the
// orchestrator in Phase 4 (handed to the agent in Phase 7); it is never a panic
// and never a silently-mis-booked entry. The classifier therefore validates
// untrusted metadata (the gst_rate string) ITSELF before calling
// gstsplit.SplitInclusive, which panics on a non-positive rate — a missing rate
// becomes a clean miss, not a crash.
//
// # Boundaries (SPEC §4.4, §12)
//
// classify imports ONLY internal/money, internal/ingest (the NormalizedEvent
// type it consumes), and internal/gstsplit (the single canonical GST split it
// shares with the seeder). It MUST NOT import internal/truth or internal/seed:
// the rule engine derives its books from events alone and may never peek at the
// ground truth it is scored against. The truth-isolation guard test enforces the
// truth boundary at the package level.
//
// # Money invariant (SPEC §1, §4)
//
// Every amount is integer minor units — paise — as money.Money. No float type
// appears in this package; a guard test asserts that statically.
package classify

import (
	"fmt"

	"github.com/razorpay/close-agent/internal/gstsplit"
	"github.com/razorpay/close-agent/internal/ingest"
	"github.com/razorpay/close-agent/internal/money"
)

// Classification is the result of classifying one event: the playbook entry type
// to post and the integer-paise params to bind it with, plus the idempotency key,
// the external transaction id, and the event timestamp the ledger needs.
//
// This is the shared seam with the Phase-7 agent (SPEC §8): a rule here and the
// agent later both produce a Classification, and the orchestrator binds+posts it
// the same way. Params is keyed to the matching entry type's declared params
// (e.g. {gross, net, gst, payment_id} for dtc_sale).
//
// # The tx-id param vs TxID (mirrors the seeder's truthBinder)
//
// A playbook entry type declares a tx_param (payment_id / bank_tx_id / refund_id
// / dispute_id) that the ledger carries through the money.Money param channel —
// it only needs the param PRESENT and renders it as an opaque integer. The REAL
// id STRING (e.g. "pay_…", a settlement UTR) does not fit that integer channel,
// so it travels separately on TxID, exactly as the seeder's truthBinder stamps
// the string TxID on each truth.Entry. We put a zero placeholder in Params for
// the tx_param so Bind finds it present, and the meaningful id on TxID.
type Classification struct {
	// EntryType is the playbook entry-type name to bind (e.g. "dtc_sale").
	EntryType string
	// Params maps each of the entry type's declared params to its paise value.
	// The tx_param is present as a zero placeholder (see the type doc).
	Params map[string]money.Money
	// IK is the idempotency key for the post; it follows the truth IK scheme
	// EXACTLY ("sale:"+id / "settle:"+id / "refund:"+id / "dispute:"+id) so a
	// re-run posts idempotently and the scorer can line entries up with truth.
	IK string
	// TxID is the external transaction id STRING surfaced on the posted entry
	// (payment_id / settlement UTR / refund_id / dispute_id).
	TxID string
	// Ts is the event timestamp (Unix seconds), passed through to the entry for
	// the period-scoped reports. It never comes from a wall clock.
	Ts int64
	// Reason is a short human-readable note. On a SUCCESSFUL classification it
	// records which rule fired (audit); it is unused on the miss path, where the
	// reason is returned to the caller separately.
	Reason string
}

// Classify applies the per-event rules and returns the Classification for ev.
// ok is true and reason is "" on a match. On a RULE MISS — an unknown event type
// or an event missing the metadata its rule needs — it returns (nil, false,
// reason): the orchestrator flags and skips such events (Phase 4) and the agent
// handles them later (Phase 7). Classify NEVER panics on bad/missing metadata: a
// missing or invalid gst_rate is detected here and returned as a miss before any
// code path could reach gstsplit.SplitInclusive's non-positive-rate panic.
//
// The IK/TxID schemes below mirror the seeder's truth IK scheme verbatim
// (internal/seed/events.go), and the GST split uses the SAME gstsplit function
// the seeder uses, so a clean period's posted entries equal truth to the paise.
func Classify(ev ingest.NormalizedEvent) (*Classification, bool, string) {
	switch ev.Type {
	case ingest.EventPayment:
		return classifyPayment(ev)
	case ingest.EventSettlement:
		return classifySettlement(ev)
	case ingest.EventRefund:
		return classifyRefund(ev)
	case ingest.EventDispute:
		return classifyDispute(ev)
	default:
		return nil, false, fmt.Sprintf("unknown event type %q", ev.Type)
	}
}

// classifyPayment maps a captured payment to a dtc_sale. The gross the customer
// paid is GST-inclusive; the net (taxable base) and output GST are split out of
// it at the payment's gst_rate (the SAME inclusive split the seeder used).
//
//	gross = amount;  (net, gst) = SplitInclusive(gross, gst_rate)
//	params{gross, net, gst, payment_id};  IK="sale:"+id;  TxID=payment_id
//
// A payment with no usable gst_rate is a rule MISS (the agent would fetch the
// order to recover the rate, SPEC §1) — not a crash, not a guessed rate.
func classifyPayment(ev ingest.NormalizedEvent) (*Classification, bool, string) {
	rate, ok, reason := gstRateOf(ev)
	if !ok {
		return nil, false, reason
	}
	gross := ev.Amount
	net, gst := gstsplit.SplitInclusive(gross, rate)
	return &Classification{
		EntryType: "dtc_sale",
		Params: map[string]money.Money{
			"gross":      gross,
			"net":        net,
			"gst":        gst,
			"payment_id": txIDPlaceholder,
		},
		IK:     "sale:" + ev.ID,
		TxID:   paymentTxID(ev),
		Ts:     ev.TS,
		Reason: "payment -> dtc_sale",
	}, true, ""
}

// classifySettlement maps a settlement payout to a razorpay_settlement. Razorpay
// nets its fee and GST-on-fee out of the gross receivable and deposits the
// remainder; the event carries net_deposit (Amount), fee (Fee), and gst_on_fee
// (Tax), and the gross is the gross-up of those three:
//
//	net_deposit = amount;  fee = fee;  gst_on_fee = tax
//	gross = net_deposit + fee + gst_on_fee
//	params{net_deposit, fee, gst_on_fee, gross, bank_tx_id};  IK="settle:"+id;  TxID=UTR
//
// This is the SAME identity the seeder posts (net_deposit + fee + tax == gross
// by construction), so the entry balances and equals truth. A settlement missing
// its fee/tax fields is a rule MISS (it cannot be grossed up reliably).
func classifySettlement(ev ingest.NormalizedEvent) (*Classification, bool, string) {
	if ev.Fee == nil || ev.Tax == nil {
		return nil, false, "settlement missing fee/tax for gross-up"
	}
	netDeposit := ev.Amount
	fee := *ev.Fee
	gstOnFee := *ev.Tax
	gross := netDeposit.Add(fee).Add(gstOnFee)

	utr, ok, reason := settlementUTR(ev)
	if !ok {
		return nil, false, reason
	}
	return &Classification{
		EntryType: "razorpay_settlement",
		Params: map[string]money.Money{
			"net_deposit": netDeposit,
			"fee":         fee,
			"gst_on_fee":  gstOnFee,
			"gross":       gross,
			"bank_tx_id":  txIDPlaceholder,
		},
		IK:     "settle:" + ev.ID,
		TxID:   utr,
		Ts:     ev.TS,
		Reason: "settlement -> razorpay_settlement",
	}, true, ""
}

// classifyRefund maps a refund to a refund_reversal. The refund gross is
// GST-inclusive (the refund copies the payment's gst_rate into its notes), so it
// is split the SAME way as the sale it reverses:
//
//	gross = amount;  (net, gst) = SplitInclusive(gross, gst_rate)
//	params{net, gst, refund_id};  IK="refund:"+id;  TxID=refund_id
//
// The template's credit line is net+gst (== gross), so the entry clears the
// receivable for exactly the refunded gross. A refund with no usable gst_rate is
// a rule MISS.
func classifyRefund(ev ingest.NormalizedEvent) (*Classification, bool, string) {
	rate, ok, reason := gstRateOf(ev)
	if !ok {
		return nil, false, reason
	}
	net, gst := gstsplit.SplitInclusive(ev.Amount, rate)
	return &Classification{
		EntryType: "refund_reversal",
		Params: map[string]money.Money{
			"net":       net,
			"gst":       gst,
			"refund_id": txIDPlaceholder,
		},
		IK:     "refund:" + ev.ID,
		TxID:   ev.ID,
		Ts:     ev.TS,
		Reason: "refund -> refund_reversal",
	}, true, ""
}

// classifyDispute maps a (lost) dispute to a chargeback_loss. The disputed gross
// is GST-inclusive (the dispute copies the payment's gst_rate), split the SAME
// way; the template's bank credit is net+gst (== gross), clawing the full
// disputed cash back out of the bank:
//
//	gross = amount;  (net, gst) = SplitInclusive(gross, gst_rate)
//	params{net, gst, dispute_id};  IK="dispute:"+id;  TxID=dispute_id
//
// A dispute with no usable gst_rate is a rule MISS. (Status handling — booking a
// loss only for a lost dispute — matches the v1 substrate, which only emits lost
// disputes; the seeder's truth GL books every dispute as a chargeback_loss, so
// the rule does the same to stay byte-identical to truth.)
func classifyDispute(ev ingest.NormalizedEvent) (*Classification, bool, string) {
	rate, ok, reason := gstRateOf(ev)
	if !ok {
		return nil, false, reason
	}
	net, gst := gstsplit.SplitInclusive(ev.Amount, rate)
	return &Classification{
		EntryType: "chargeback_loss",
		Params: map[string]money.Money{
			"net":        net,
			"gst":        gst,
			"dispute_id": txIDPlaceholder,
		},
		IK:     "dispute:" + ev.ID,
		TxID:   ev.ID,
		Ts:     ev.TS,
		Reason: "dispute -> chargeback_loss",
	}, true, ""
}

// txIDPlaceholder is the value put in Params for an entry type's tx_param. The
// ledger only needs the param PRESENT (it renders it as an opaque integer); the
// meaningful id STRING travels on Classification.TxID. Zero keeps the param
// present without inventing a misleading integer id — matching the seeder's
// txIDParam() so bound entries are byte-identical to truth.
var txIDPlaceholder = money.FromPaise(0)

// gstRateOf reads the integer GST rate percent off an event's notes and
// validates it for the inclusive split. It is the single guard standing between
// untrusted metadata and gstsplit.SplitInclusive's non-positive-rate panic:
//
//   - no notes, or an empty/absent gst_rate -> rule MISS (the rule cannot split
//     GST without a rate; the agent would recover it from the order).
//   - a non-numeric gst_rate, or a rate <= 0 -> rule MISS (a zero/garbage rate is
//     not a usable tax slab and would panic the split).
//
// On success it returns a strictly-positive rate that SplitInclusive accepts.
func gstRateOf(ev ingest.NormalizedEvent) (rate int, ok bool, reason string) {
	if ev.Notes == nil || ev.Notes.GSTRate == "" {
		return 0, false, "missing gst_rate"
	}
	rate, ok = parseRatePercent(ev.Notes.GSTRate)
	if !ok {
		return 0, false, fmt.Sprintf("invalid gst_rate %q", ev.Notes.GSTRate)
	}
	if rate <= 0 {
		return 0, false, fmt.Sprintf("non-positive gst_rate %q", ev.Notes.GSTRate)
	}
	return rate, true, ""
}

// parseRatePercent parses a GST rate string ("18", "12", "5") into an int in
// integer space only — no strconv float paths, mirroring the seeder's
// gstRatePercentOf (internal/seed/events.go). ok is false for an empty string or
// any non-digit byte, so garbage metadata becomes a clean rule miss rather than a
// silently-wrong rate.
func parseRatePercent(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// paymentTxID returns the external tx id string for a payment's dtc_sale. The
// seeder stamps the payment id itself as the truth entry's TxID (a payment links
// to itself, SPEC §4.3), so we use the same id here to match truth.
func paymentTxID(ev ingest.NormalizedEvent) string {
	return ev.ID
}

// settlementUTR returns the settlement's bank UTR, which the seeder stamps as the
// truth entry's TxID (the bank_tx_id). The normalized event keeps the UTR in its
// raw object (a settlement has no notes/links), so we recover it from raw rather
// than re-deriving it. A settlement with no UTR is a rule MISS — without the bank
// reference the deposit cannot be tied to its bank-feed credit (SPEC §7 check #1).
func settlementUTR(ev ingest.NormalizedEvent) (utr string, ok bool, reason string) {
	u, err := utrFromRaw(ev.Raw)
	if err != nil {
		return "", false, fmt.Sprintf("settlement UTR unreadable: %v", err)
	}
	if u == "" {
		return "", false, "settlement missing UTR"
	}
	return u, true, ""
}
