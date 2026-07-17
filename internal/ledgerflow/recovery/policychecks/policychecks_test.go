package policychecks_test

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/ledgerflow/recovery/policychecks"
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// lens is a minimal in-memory Graph for policy tests.
type lens struct {
	payOrder   map[string]string
	rfndPay    map[string]string
	orders     map[string]feeds.OrderInfo
	ratecard   *feeds.RateCardFile
	members    map[string][]string
	payAmt     map[string]money.Money
	deductions map[string][]policychecks.Deduction
	channel    string
}

func (l lens) OrderIDForPayment(id string) (string, bool) { v, ok := l.payOrder[id]; return v, ok }
func (l lens) PaymentIDForRefund(id string) (string, bool) {
	v, ok := l.rfndPay[id]
	return v, ok
}
func (l lens) OrderInfo(id string) (feeds.OrderInfo, bool) { v, ok := l.orders[id]; return v, ok }
func (l lens) RateCard() (feeds.RateCardFile, bool) {
	if l.ratecard == nil {
		return feeds.RateCardFile{}, false
	}
	return *l.ratecard, true
}
func (l lens) SettlementMembers(id string) ([]string, []string, bool) {
	m, ok := l.members[id]
	return m, nil, ok
}
func (l lens) PaymentAmount(id string) (money.Money, bool) { v, ok := l.payAmt[id]; return v, ok }
func (l lens) RemittanceDeductions(id string) ([]policychecks.Deduction, bool) {
	v, ok := l.deductions[id]
	return v, ok
}
func (l lens) CourierChannel() (string, bool) {
	if l.channel == "" {
		return "", false
	}
	return l.channel, true
}

func testLens() lens {
	return lens{
		payOrder: map[string]string{"pay_P": "order_O"},
		rfndPay:  map[string]string{"rfnd_R": "pay_P"},
		orders: map[string]feeds.OrderInfo{
			"order_O": {GSTRate: "18", Items: []feeds.OrderItem{
				{SKU: "SERUM-30", Amount: money.FromPaise(40000), GSTRate: "18"},
				{SKU: "SERUM-30-ADDON", Amount: money.FromPaise(60000), GSTRate: "18"},
			}},
		},
		ratecard: &feeds.RateCardFile{SchemaVersion: 1, Channels: []feeds.Channel{
			{Channel: "razorpay", FeeBps: 200, FeeGSTRate: 18},
		}},
		// a 2-payment batch: per-payment 2% fees floor to 2199 + 2201 = 4400.
		members: map[string][]string{"setl_S": {"pay_A", "pay_B"}},
		payAmt: map[string]money.Money{
			"pay_A": money.FromPaise(109951), // floor(109951/50) = 2199
			"pay_B": money.FromPaise(110057), // floor(110057/50) = 2201
		},
	}
}

// TestGSTRatePolicy pins gst-rate-from-order: applies only when the event's own
// rate is absent; walks payment->order (or refund->payment->order); validates
// against the closed slab set; cites the source field.
func TestGSTRatePolicy(t *testing.T) {
	g := testLens()
	reg := policychecks.Default()

	// payment missing its rate -> recovered, valid, cited.
	ev := policychecks.Event{EventID: "pay_P", Type: "payment", Amount: money.FromPaise(118000)}
	f := reg.Run(ev, g)
	if len(f.Facts) != 1 {
		t.Fatalf("facts = %+v, want exactly one", f.Facts)
	}
	fact := f.Facts[0]
	if fact.Field != "gst_rate" || fact.Value != "18" || !fact.Valid {
		t.Errorf("fact = %+v, want valid gst_rate=18", fact)
	}
	if fact.Source.Object != "order_O" || fact.Source.Path != "notes.gst_rate" {
		t.Errorf("citation = %+v", fact.Source)
	}
	if fact.Policy != "gst-rate-from-order" {
		t.Errorf("policy tag = %q", fact.Policy)
	}

	// refund (2-hop walk) also recovers.
	rf := policychecks.Event{EventID: "rfnd_R", Type: "refund", Amount: money.FromPaise(40000)}
	if f := reg.Run(rf, g); len(f.Facts) != 1 || f.Facts[0].Value != "18" {
		t.Errorf("refund walk = %+v", f)
	}

	// event with its OWN rate: the policy does not apply.
	own := policychecks.Event{EventID: "pay_P", Type: "payment", GSTRate: "18"}
	for _, fa := range reg.Run(own, g).Facts {
		if fa.Field == "gst_rate" {
			t.Errorf("policy applied to an event that already carries a rate: %+v", fa)
		}
	}
}

// TestLineItemMatchPolicy pins refund-line-item-match: partial refunds get
// candidates (exact item, pair-capped, explicit no-match).
func TestLineItemMatchPolicy(t *testing.T) {
	g := testLens()
	reg := policychecks.Default()
	parent := money.FromPaise(100000)

	exact := policychecks.Event{EventID: "rfnd_R", Type: "refund",
		Amount: money.FromPaise(40000), ParentAmount: &parent}
	f := reg.Run(exact, g)
	if len(f.Candidates) != 1 || f.Candidates[0].Kind != policychecks.CandidateItemMatch {
		t.Fatalf("candidates = %+v, want one item-match", f.Candidates)
	}
	if f.Candidates[0].GSTRate != "18" || f.Candidates[0].Source.Path != "items.0" {
		t.Errorf("match = %+v", f.Candidates[0])
	}

	pair := policychecks.Event{EventID: "rfnd_R", Type: "refund",
		Amount: money.FromPaise(100000), ParentAmount: &parent} // == items[0]+items[1]
	pf := reg.Run(pair, g)
	if len(pf.Candidates) != 1 || pf.Candidates[0].Kind != policychecks.CandidatePairMatch {
		t.Errorf("pair candidates = %+v", pf.Candidates)
	}

	none := policychecks.Event{EventID: "rfnd_R", Type: "refund",
		Amount: money.FromPaise(33333), ParentAmount: &parent}
	nf := reg.Run(none, g)
	if len(nf.Candidates) != 1 || nf.Candidates[0].Kind != policychecks.CandidateNoMatch {
		t.Errorf("no-match candidates = %+v", nf.Candidates)
	}

	// a FULL refund: the matcher does not apply.
	full := policychecks.Event{EventID: "rfnd_R", Type: "refund", Amount: money.FromPaise(100000)}
	if ff := reg.Run(full, g); len(ff.Candidates) != 0 {
		t.Errorf("full refund got candidates: %+v", ff.Candidates)
	}
}

// TestFeeTierPolicy pins fee-tier-from-ratecard: a settlement's expected fee is
// the PER-PAYMENT sum of fee_bps over its batch members (fees are priced per
// payment, floored per payment, and not returned on refunds), validated against
// the settlement's actual fee.
func TestFeeTierPolicy(t *testing.T) {
	g := testLens()
	reg := policychecks.Default()
	fee := money.FromPaise(4400) // 2199 + 2201, the per-member sum
	tax := money.FromPaise(792)  // 18% of fee
	ev := policychecks.Event{EventID: "setl_S", Type: "settlement",
		Amount: money.FromPaise(214816), // net deposit = member gross - fee - tax
		Fee:    &fee, Tax: &tax}

	f := reg.Run(ev, g)
	var found *policychecks.Fact
	for i := range f.Facts {
		if f.Facts[i].Field == "expected_fee" {
			found = &f.Facts[i]
		}
	}
	if found == nil {
		t.Fatalf("no expected_fee fact: %+v", f.Facts)
	}
	if found.Value != "4400" || !found.Valid {
		t.Errorf("expected_fee = %+v, want 4400 valid (matches actual)", found)
	}
	if found.Source.Object != "ratecard/razorpay" {
		t.Errorf("citation = %+v", found.Source)
	}

	// an OVERCHARGED settlement: expected != actual -> fact present but invalid.
	badFee := money.FromPaise(5280)
	bad := policychecks.Event{EventID: "setl_S", Type: "settlement",
		Amount: money.FromPaise(213928), Fee: &badFee, Tax: &tax}
	bf := reg.Run(bad, g)
	for _, fa := range bf.Facts {
		if fa.Field == "expected_fee" && fa.Valid {
			t.Errorf("overcharge marked valid: %+v", fa)
		}
	}
}
