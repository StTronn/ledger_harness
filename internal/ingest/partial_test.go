package ingest

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
)

// TestNormalizePartialRefundCarriesParentAmount pins the partial-refund
// enrichment: a refund whose amount is LESS than its parent payment's carries the
// parent's gross on ParentAmount (the §4.3 journal records the anomaly), while a
// full refund carries nil (so the committed periods' golden journals stay
// byte-identical — omitempty never fires for them).
func TestNormalizePartialRefundCarriesParentAmount(t *testing.T) {
	raw := Raw{
		Payments: []RawPayment{
			{ID: "pay_P1", Amount: money.FromPaise(100000), CreatedAt: 10},
			{ID: "pay_P2", Amount: money.FromPaise(50000), CreatedAt: 20},
		},
		Refunds: []RawRefund{
			{ID: "rfnd_PARTIAL", PaymentID: "pay_P1", Amount: money.FromPaise(40000), CreatedAt: 30},
			{ID: "rfnd_FULL", PaymentID: "pay_P2", Amount: money.FromPaise(50000), CreatedAt: 40},
		},
	}
	events, err := Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	byID := map[string]NormalizedEvent{}
	for _, e := range events {
		byID[e.ID] = e
	}

	partial := byID["rfnd_PARTIAL"]
	if partial.ParentAmount == nil || partial.ParentAmount.Paise() != 100000 {
		t.Errorf("partial refund ParentAmount = %v, want 100000", partial.ParentAmount)
	}
	full := byID["rfnd_FULL"]
	if full.ParentAmount != nil {
		t.Errorf("full refund ParentAmount = %v, want nil (golden stability)", full.ParentAmount)
	}
}
