package flowcontext_test

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/config"
	"github.com/razorpay/ledger-flow/internal/gstsplit"
	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledger"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/context"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/posting"
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/reconcile"
)

// postedLedger returns a ledger with a single dtc_sale posted for paymentID, so a
// read model built over it reports that event booked and any other unbooked.
func postedLedger(t *testing.T, paymentID string) *ledger.Ledger {
	t.Helper()
	pb, err := config.DefaultPlaybook()
	if err != nil {
		t.Fatalf("DefaultPlaybook: %v", err)
	}
	lg := ledger.New(ledger.NewPlaybookChart(pb))
	gross := money.FromPaise(118000)
	net, gst := gstsplit.SplitInclusive(gross, 18)
	e, err := ledger.Bind(ledger.NewPlaybookTemplates(pb), "dtc_sale",
		posting.IKFor(ingest.EventPayment, paymentID),
		map[string]money.Money{"gross": gross, "net": net, "gst": gst, "payment_id": money.FromPaise(0)})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if _, err := lg.Post(e); err != nil {
		t.Fatalf("Post: %v", err)
	}
	return lg
}

func TestBookedDistinguishesPostedFromMissing(t *testing.T) {
	lg := postedLedger(t, "pay_BOOKED")
	events := []ingest.NormalizedEvent{
		{ID: "pay_BOOKED", Type: ingest.EventPayment},
		{ID: "pay_MISSING", Type: ingest.EventPayment},
	}
	m := flowcontext.New(lg, events, ingest.Raw{}, nil, nil)

	booked, _ := m.Event("pay_BOOKED")
	if !m.Booked(booked) {
		t.Error("pay_BOOKED should be booked (its IK is posted)")
	}
	missing, _ := m.Event("pay_MISSING")
	if m.Booked(missing) {
		t.Error("pay_MISSING should NOT be booked (no entry posted for its IK)")
	}
}

func TestRecoveredGSTRateAndGraphIndexes(t *testing.T) {
	raw := ingest.Raw{
		Payments: []ingest.RawPayment{{ID: "pay_M", OrderID: "order_M"}},
		Refunds:  []ingest.RawRefund{{ID: "rfnd_R", PaymentID: "pay_M"}},
	}
	m := flowcontext.New(postedLedger(t, "pay_BOOKED"), nil, raw, map[string]flowcontext.OrderInfo{"order_M": {GSTRate: "18"}}, nil)

	if got, ok := m.OrderIDForPayment("pay_M"); !ok || got != "order_M" {
		t.Errorf("OrderIDForPayment(pay_M) = (%q,%v), want (order_M,true)", got, ok)
	}
	if got, ok := m.PaymentIDForRefund("rfnd_R"); !ok || got != "pay_M" {
		t.Errorf("PaymentIDForRefund(rfnd_R) = (%q,%v), want (pay_M,true)", got, ok)
	}
	if got, ok := m.RecoveredGSTRate("order_M"); !ok || got != "18" {
		t.Errorf("RecoveredGSTRate(order_M) = (%q,%v), want (18,true)", got, ok)
	}
}

func TestBreakLookupByKey(t *testing.T) {
	b := reconcile.Break{Check: reconcile.CheckReceivableClears, Kind: "receivable-residual"}
	m := flowcontext.New(postedLedger(t, "pay_X"), nil, ingest.Raw{}, nil, []reconcile.Break{b})

	got, ok := m.Break(b.Key())
	if !ok || got.Kind != "receivable-residual" {
		t.Errorf("Break(%q) = (%+v,%v), want the receivable-residual break", b.Key(), got, ok)
	}
}
