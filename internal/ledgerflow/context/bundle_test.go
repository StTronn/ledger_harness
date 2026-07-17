package flowcontext_test

import (
	"slices"
	"testing"

	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/context"
	"github.com/razorpay/ledger-flow/internal/reconcile"
)

// TestEventContextRecoversRateForUnbookedPayment builds a model where a payment
// arrived with no gst_rate (the rule-miss case) and asserts the bundle surfaces the
// recovered rate (with its citation), the applicable entry type, and booked=false.
func TestEventContextRecoversRateForUnbookedPayment(t *testing.T) {
	raw := ingest.Raw{Payments: []ingest.RawPayment{{ID: "pay_M", OrderID: "order_M"}}}
	events := []ingest.NormalizedEvent{{
		ID: "pay_M", Type: ingest.EventPayment, Amount: 118000,
		Notes: &ingest.Notes{SKU: "SERUM-30", GSTRate: ""}, // own rate stripped
	}}
	// ledger has a DIFFERENT sale posted, so pay_M is unbooked.
	m := flowcontext.New(postedLedger(t, "pay_OTHER"), events, raw, map[string]flowcontext.OrderInfo{"order_M": {GSTRate: "18"}}, nil)

	b, ok := m.EventContext("pay_M")
	if !ok {
		t.Fatal("EventContext(pay_M) not found")
	}
	if b.Event.Booked {
		t.Error("pay_M should be unbooked")
	}
	if b.Recovered == nil || b.Recovered.GSTRate != "18" {
		t.Fatalf("recovered rate = %+v, want 18", b.Recovered)
	}
	if b.Recovered.Source.Object != "order_M" || b.Recovered.Source.Path != "notes.gst_rate" {
		t.Errorf("citation = %+v, want {order_M, notes.gst_rate}", b.Recovered.Source)
	}
	if len(b.ApplicableEntryTypes) != 1 || b.ApplicableEntryTypes[0] != "dtc_sale" {
		t.Errorf("applicable = %v, want [dtc_sale]", b.ApplicableEntryTypes)
	}
}

// TestReconciliationContextSurfacesUnbookedRefund builds a model with a check-3
// residual break over a settlement whose batch contains an unbooked refund, and
// asserts the bundle flags that refund (booked=false), recovers its rate, names the
// settlement, and lists refund_reversal as an applicable resolution.
func TestReconciliationContextSurfacesUnbookedRefund(t *testing.T) {
	raw := ingest.Raw{
		Payments:    []ingest.RawPayment{{ID: "pay_M", OrderID: "order_M"}},
		Refunds:     []ingest.RawRefund{{ID: "rfnd_R", PaymentID: "pay_M"}},
		Settlements: []ingest.RawSettlement{{ID: "setl_X", UTR: "UTRX", RefundIDs: []string{"rfnd_R"}}},
	}
	events := []ingest.NormalizedEvent{
		{ID: "rfnd_R", Type: ingest.EventRefund, Amount: 248591, Notes: &ingest.Notes{GSTRate: ""}},
	}
	brk := reconcile.Break{
		Check: reconcile.CheckReceivableClears, Kind: "receivable-residual",
		SettlementID: "setl_X", Expected: 0, Actual: 248591,
		CandidateEventIDs: []string{"rfnd_R"},
	}
	m := flowcontext.New(postedLedger(t, "pay_M"), events, raw, map[string]flowcontext.OrderInfo{"order_M": {GSTRate: "18"}}, []reconcile.Break{brk})

	b, ok := m.ReconciliationContext(brk.Key())
	if !ok {
		t.Fatal("ReconciliationContext not found")
	}
	if b.Settlement == nil || b.Settlement.ID != "setl_X" {
		t.Fatalf("settlement = %+v, want setl_X", b.Settlement)
	}
	var refund *flowcontext.BatchMember
	for i := range b.Batch {
		if b.Batch[i].EventID == "rfnd_R" {
			refund = &b.Batch[i]
		}
	}
	if refund == nil {
		t.Fatal("rfnd_R not in batch")
	}
	if refund.Booked {
		t.Error("rfnd_R should be unbooked")
	}
	if refund.Recovered == nil || refund.Recovered.GSTRate != "18" {
		t.Errorf("refund recovered = %+v, want 18", refund.Recovered)
	}
	if !slices.Contains(b.ApplicableEntryTypes, "refund_reversal") {
		t.Errorf("applicable = %v, want to contain refund_reversal", b.ApplicableEntryTypes)
	}
}
