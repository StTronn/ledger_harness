package seed

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
)

// TestPartialRefundsWorld pins the partial-refund world (the "judgment" period):
// with Options.PartialRefunds on,
//
//   - every order carries TWO line items (same gst_rate, summing to the order
//     amount) — the matching substrate for the refund-intent judgment;
//   - the first three refunds become the judgment spectrum:
//     R1 — amount equals its order's items[0] (truth: refund_reversal at the rate),
//     R2 — matches no item, notes.reason="goodwill" (truth: price_adjustment),
//     R3 — matches no item, no reason (truth: price_adjustment; the system can
//     only escalate — the honest sub-100%);
//   - all partial amounts stay strictly inside (0, parent amount);
//   - every settlement batch still sums (Σpay − Σrefund − fee − tax == deposit),
//     because the partial amounts flow through the same netting as full refunds;
//   - truth still balances (GenerateWith enforces it; reaching here implies it).
func TestPartialRefundsWorld(t *testing.T) {
	fx, _, gl, res, err := GenerateFull("dtc", "2026-01", Options{PartialRefunds: true})
	if err != nil {
		t.Fatalf("GenerateFull(partial-refunds): %v", err)
	}
	pr := res.Partial

	// --- orders carry two same-rate items summing to the amount ---
	for _, o := range fx.Orders {
		if len(o.Items) != 2 {
			t.Fatalf("order %s has %d items, want 2", o.ID, len(o.Items))
		}
		if got := o.Items[0].Amount.Add(o.Items[1].Amount); got != o.Amount {
			t.Errorf("order %s items sum %s != amount %s", o.ID, got, o.Amount)
		}
		for _, it := range o.Items {
			if it.GSTRate != o.Notes.GSTRate {
				t.Errorf("order %s item rate %q != order rate %q", o.ID, it.GSTRate, o.Notes.GSTRate)
			}
			if it.SKU == "" {
				t.Errorf("order %s item missing sku", o.ID)
			}
		}
	}

	// --- the three designated refunds ---
	if len(pr.Refunds) != 3 {
		t.Fatalf("partial result has %d refunds, want 3 (%+v)", len(pr.Refunds), pr)
	}
	refundByID := map[string]Refund{}
	for _, r := range fx.Refunds {
		refundByID[r.ID] = r
	}
	payByID := map[string]Payment{}
	for _, p := range fx.Payments {
		payByID[p.ID] = p
	}
	orderByID := map[string]Order{}
	for _, o := range fx.Orders {
		orderByID[o.ID] = o
	}
	truthTypeByEvent := map[string]string{}
	for _, e := range gl.Entries {
		truthTypeByEvent[e.EventID] = e.EntryType
	}

	wantClasses := []PartialClass{PartialItemMatch, PartialGoodwill, PartialUnexplained}
	wantTruth := []string{"refund_reversal", "price_adjustment", "price_adjustment"}
	for i, d := range pr.Refunds {
		rf, ok := refundByID[d.RefundID]
		if !ok {
			t.Fatalf("designated refund %s not in refunds.json", d.RefundID)
		}
		if d.Class != wantClasses[i] {
			t.Errorf("refund %d class = %q, want %q", i, d.Class, wantClasses[i])
		}
		parent := payByID[rf.PaymentID]
		if !(rf.Amount.Paise() > 0 && rf.Amount.Paise() < parent.Amount.Paise()) {
			t.Errorf("refund %s amount %s not strictly partial of %s", rf.ID, rf.Amount, parent.Amount)
		}
		if got := truthTypeByEvent[rf.ID]; got != wantTruth[i] {
			t.Errorf("truth entry for %s = %q, want %q", rf.ID, got, wantTruth[i])
		}
		order := orderByID[parent.OrderID]
		matchesItem0 := rf.Amount == order.Items[0].Amount
		switch d.Class {
		case PartialItemMatch:
			if !matchesItem0 {
				t.Errorf("R1 %s amount %s != items[0] %s", rf.ID, rf.Amount, order.Items[0].Amount)
			}
			if rf.Notes.Reason != "" {
				t.Errorf("R1 carries an unexpected reason %q", rf.Notes.Reason)
			}
		case PartialGoodwill:
			if rf.Notes.Reason != "goodwill" {
				t.Errorf("R2 reason = %q, want goodwill", rf.Notes.Reason)
			}
			if matchesItem0 || rf.Amount == order.Items[1].Amount {
				t.Errorf("R2 %s accidentally matches a line item", rf.ID)
			}
		case PartialUnexplained:
			if rf.Notes.Reason != "" {
				t.Errorf("R3 reason = %q, want empty", rf.Notes.Reason)
			}
			if matchesItem0 || rf.Amount == order.Items[1].Amount {
				t.Errorf("R3 %s accidentally matches a line item", rf.ID)
			}
		}
	}

	// --- settlement batches still sum with partial netting ---
	for _, s := range fx.Settlements {
		var paySum, refundSum money.Money
		for _, id := range s.PaymentIDs {
			paySum = paySum.Add(payByID[id].Amount)
		}
		for _, id := range s.RefundIDs {
			refundSum = refundSum.Add(refundByID[id].Amount)
		}
		want := paySum.Sub(refundSum).Sub(s.Fee).Sub(s.Tax)
		if s.Amount != want {
			t.Errorf("settlement %s deposit %s != batch sum %s", s.ID, s.Amount, want)
		}
	}
}

// TestPartialRefundsOffIsUnchanged guards byte-stability of the existing worlds:
// with the option off, orders carry NO items and refunds remain full-amount, so
// re-seeding the committed periods stays byte-identical.
func TestPartialRefundsOffIsUnchanged(t *testing.T) {
	fx, _, _, res, err := GenerateFull("dtc", "2026-05", Options{})
	if err != nil {
		t.Fatalf("GenerateFull(clean): %v", err)
	}
	if len(res.Partial.Refunds) != 0 {
		t.Errorf("clean generation reported partial refunds: %+v", res.Partial)
	}
	for _, o := range fx.Orders {
		if len(o.Items) != 0 {
			t.Fatalf("order %s has items in a clean generation", o.ID)
		}
	}
	payByID := map[string]Payment{}
	for _, p := range fx.Payments {
		payByID[p.ID] = p
	}
	for _, r := range fx.Refunds {
		if r.Amount != payByID[r.PaymentID].Amount {
			t.Errorf("refund %s is partial in a clean generation", r.ID)
		}
		if r.Notes.Reason != "" {
			t.Errorf("refund %s carries a reason in a clean generation", r.ID)
		}
	}
}
