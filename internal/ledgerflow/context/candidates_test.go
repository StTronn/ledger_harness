package flowcontext_test

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/context"
	"github.com/razorpay/ledger-flow/internal/money"
)

func pm(p int64) *money.Money { m := money.FromPaise(p); return &m }

// partialWorldModel builds a model with one order (two line items) and three
// partial refunds mirroring the seeded R1/R2/R3 spectrum.
func partialWorldModel(t *testing.T) *flowcontext.Graph {
	t.Helper()
	raw := ingest.Raw{
		Payments: []ingest.RawPayment{{ID: "pay_P", OrderID: "order_O", Amount: money.FromPaise(100000)}},
		Refunds: []ingest.RawRefund{
			{ID: "rfnd_R1", PaymentID: "pay_P", Amount: money.FromPaise(40000)},
			{ID: "rfnd_R2", PaymentID: "pay_P", Amount: money.FromPaise(30001)},
		},
	}
	events := []ingest.NormalizedEvent{
		{ID: "rfnd_R1", Type: ingest.EventRefund, Amount: 40000, ParentAmount: pm(100000),
			Notes: &ingest.Notes{GSTRate: "18"}},
		{ID: "rfnd_R2", Type: ingest.EventRefund, Amount: 30001, ParentAmount: pm(100000),
			Notes: &ingest.Notes{GSTRate: "18", Reason: "goodwill"}},
	}
	orders := map[string]flowcontext.OrderInfo{
		"order_O": {GSTRate: "18", Items: []flowcontext.OrderItem{
			{SKU: "SERUM-30", Amount: money.FromPaise(40000), GSTRate: "18"},
			{SKU: "SERUM-30-ADDON", Amount: money.FromPaise(60000), GSTRate: "18"},
		}},
	}
	return flowcontext.New(postedLedger(t, "pay_OTHER"), events, raw, orders, nil)
}

// TestEventContextPartialRefundCandidates pins the Tier-1 candidate precompute:
// R1 (amount == items[0]) yields a refund_reversal candidate citing the matched
// item; R2 (matches nothing, annotated goodwill) yields no item match and
// surfaces the annotation. Both carry the parent amount and the order's items so
// the agent sees the full matching substrate in one call.
func TestEventContextPartialRefundCandidates(t *testing.T) {
	m := partialWorldModel(t)

	b1, ok := m.EventContext("rfnd_R1")
	if !ok {
		t.Fatal("EventContext(rfnd_R1) not found")
	}
	if b1.Event.ParentAmount == nil || b1.Event.ParentAmount.Paise() != 100000 {
		t.Errorf("R1 parent amount = %v, want 100000", b1.Event.ParentAmount)
	}
	if len(b1.OrderItems) != 2 {
		t.Fatalf("R1 bundle carries %d order items, want 2", len(b1.OrderItems))
	}
	var match *flowcontext.RefundCandidate
	for i := range b1.Candidates {
		if b1.Candidates[i].Kind == flowcontext.CandidateItemMatch {
			match = &b1.Candidates[i]
		}
	}
	if match == nil {
		t.Fatalf("R1 has no item-match candidate: %+v", b1.Candidates)
	}
	if match.EntryType != "refund_reversal" || match.GSTRate != "18" {
		t.Errorf("R1 match = %+v, want refund_reversal @18", match)
	}
	if match.Source.Object != "order_O" || match.Source.Path != "items.0" {
		t.Errorf("R1 match citation = %+v, want order_O/items.0", match.Source)
	}

	b2, ok := m.EventContext("rfnd_R2")
	if !ok {
		t.Fatal("EventContext(rfnd_R2) not found")
	}
	for _, c := range b2.Candidates {
		if c.Kind == flowcontext.CandidateItemMatch || c.Kind == flowcontext.CandidatePairMatch {
			t.Errorf("R2 must not match items, got %+v", c)
		}
	}
	if b2.Event.Reason != "goodwill" {
		t.Errorf("R2 bundle reason = %q, want goodwill", b2.Event.Reason)
	}
}
