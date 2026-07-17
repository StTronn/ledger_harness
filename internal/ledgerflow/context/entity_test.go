package flowcontext_test

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/ledgerflow/run"
)

// TestEntityResolvesEveryKind drives the tier-2 entity lookup on the committed
// partial-refund world: any object the agent can name resolves to its raw
// snapshot plus the cheap derived facts (booked, graph edges) — the
// self-directed exploration surface behind `context entity <id>`.
func TestEntityResolvesEveryKind(t *testing.T) {
	const root = "../../.."
	g, err := run.BuildRecoveryEngine(root, "dtc", "2026-01")
	if err != nil {
		t.Fatalf("BuildRecoveryEngine: %v", err)
	}

	// payment: raw object + booked + order edge.
	pay, ok := g.Entity("pay_25Zx1xG57xpaIw")
	if !ok || pay.Kind != "payment" {
		t.Fatalf("payment entity = %+v ok=%v", pay, ok)
	}
	if !pay.Booked || pay.Edges["order_id"] != "order_jTYHxOWgY2SlPF" {
		t.Errorf("payment derived = booked=%v edges=%v", pay.Booked, pay.Edges)
	}
	if len(pay.Object) == 0 {
		t.Error("payment entity missing raw object")
	}

	// refund: payment edge + partial marker.
	rf, ok := g.Entity("rfnd_ZtHFpyTP2I9NSz")
	if !ok || rf.Kind != "refund" || rf.Edges["payment_id"] == "" {
		t.Fatalf("refund entity = %+v ok=%v", rf, ok)
	}

	// order: items + rate (no booked flag — orders are not events).
	o, ok := g.Entity("order_jTYHxOWgY2SlPF")
	if !ok || o.Kind != "order" || len(o.Order.Items) != 2 {
		t.Fatalf("order entity = %+v ok=%v", o, ok)
	}

	// rate card channel.
	rc, ok := g.Entity("ratecard/razorpay")
	if !ok || rc.Kind != "ratecard-channel" || rc.RateCard.FeeBps != 200 {
		t.Fatalf("ratecard entity = %+v ok=%v", rc, ok)
	}

	// account: period balance.
	acc, ok := g.Entity("assets/razorpay-settlement-receivable")
	if !ok || acc.Kind != "account" || acc.Balance == nil {
		t.Fatalf("account entity = %+v ok=%v", acc, ok)
	}

	// unknown id: clean miss.
	if _, ok := g.Entity("pay_NOPE"); ok {
		t.Error("unknown id resolved")
	}
}
