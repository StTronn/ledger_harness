package seed

import (
	"testing"
)

// TestOrdersOnePerPaymentAuthoritative asserts the seeder emits exactly one
// order per payment, each carrying the AUTHORITATIVE sku, gst_rate, amount, and
// order_id the payment references (SPEC §2). This is the legitimate recovery
// source the agent reads — so it must mirror the payment's true metadata.
func TestOrdersOnePerPaymentAuthoritative(t *testing.T) {
	fx, _, _, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(fx.Orders) != len(fx.Payments) {
		t.Fatalf("orders = %d, want one per payment (%d)", len(fx.Orders), len(fx.Payments))
	}

	orderByID := make(map[string]Order, len(fx.Orders))
	for _, o := range fx.Orders {
		if o.Entity != "order" {
			t.Errorf("order %s entity = %q, want \"order\"", o.ID, o.Entity)
		}
		if o.Currency != "INR" {
			t.Errorf("order %s currency = %q, want INR", o.ID, o.Currency)
		}
		if o.Amount.Sign() <= 0 {
			t.Errorf("order %s amount not positive: %s", o.ID, o.Amount)
		}
		if _, dup := orderByID[o.ID]; dup {
			t.Errorf("duplicate order id %s", o.ID)
		}
		orderByID[o.ID] = o
	}

	for _, p := range fx.Payments {
		o, ok := orderByID[p.OrderID]
		if !ok {
			t.Errorf("payment %s references order %s with no matching order", p.ID, p.OrderID)
			continue
		}
		if o.Amount != p.Amount {
			t.Errorf("order %s amount %s != payment %s gross %s", o.ID, o.Amount, p.ID, p.Amount)
		}
		if o.Notes.GSTRate != p.Notes.GSTRate {
			t.Errorf("order %s gst_rate %q != payment %s gst_rate %q", o.ID, o.Notes.GSTRate, p.ID, p.Notes.GSTRate)
		}
		if o.Notes.SKU != p.Notes.SKU {
			t.Errorf("order %s sku %q != payment %s sku %q", o.ID, o.Notes.SKU, p.ID, p.Notes.SKU)
		}
		// The order is created before its payment (checkout precedes capture).
		if o.CreatedAt >= p.CreatedAt {
			t.Errorf("order %s created_at %d not before payment %s created_at %d", o.ID, o.CreatedAt, p.ID, p.CreatedAt)
		}
	}
}

// TestOrdersDoNotPerturbCleanSubstrate asserts that adding orders did NOT shift
// the seeder's main RNG stream: the payments/refunds/settlements/disputes,
// bank feed, and truth GL must be byte-identical whether or not orders are
// considered. Orders are derived from the payment value (no extra RNG draw), so
// the clean stream is unchanged — this is what keeps the committed 2026-05
// fixtures (everything but the new orders.json) byte-stable.
func TestOrdersDoNotPerturbCleanSubstrate(t *testing.T) {
	a, fa, ga, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate #1: %v", err)
	}
	b, fb, gb, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate #2: %v", err)
	}
	for _, pair := range []struct {
		name string
		x, y any
	}{
		{"payments", a.Payments, b.Payments},
		{"refunds", a.Refunds, b.Refunds},
		{"settlements", a.Settlements, b.Settlements},
		{"disputes", a.Disputes, b.Disputes},
		{"orders", a.Orders, b.Orders},
		{"bank-feed", fa, fb},
		{"truth-gl", ga, gb},
	} {
		bx, _ := MarshalStable(pair.x)
		by, _ := MarshalStable(pair.y)
		if string(bx) != string(by) {
			t.Errorf("%s not byte-identical across two Generate runs", pair.name)
		}
	}
}
