package config_test

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/config"
	"github.com/razorpay/ledger-flow/internal/ledger"
	"github.com/razorpay/ledger-flow/internal/money"
)

// TestPriceAdjustmentEntryType pins the playbook's price_adjustment entry type —
// the credit-note treatment of a goodwill / unmatchable partial refund (truth
// books R2/R3 of the partial-refund world with it). It must bind balanced:
//
//	Dr income/discounts-allowances           {net}
//	Dr liabilities/gst-output-payable        {gst}
//	Cr assets/razorpay-settlement-receivable {net+gst}
func TestPriceAdjustmentEntryType(t *testing.T) {
	pb, err := config.DefaultPlaybook()
	if err != nil {
		t.Fatalf("DefaultPlaybook: %v", err)
	}
	if _, ok := pb.Account("income/discounts-allowances"); !ok {
		t.Fatal("playbook missing account income/discounts-allowances")
	}
	if _, ok := pb.EntryType("price_adjustment"); !ok {
		t.Fatal("playbook missing entry type price_adjustment")
	}

	// A ₹200.00 goodwill credit at 18%: net 16,949 + gst 3,051 = 20,000 paise.
	e, err := ledger.Bind(ledger.NewPlaybookTemplates(pb), "price_adjustment", "refund:rfnd_T1",
		map[string]money.Money{
			"net":       money.FromPaise(16949),
			"gst":       money.FromPaise(3051),
			"refund_id": money.FromPaise(0),
		})
	if err != nil {
		t.Fatalf("Bind price_adjustment: %v", err)
	}
	lg := ledger.New(ledger.NewPlaybookChart(pb))
	if _, err := lg.Post(e); err != nil {
		t.Fatalf("Post price_adjustment (must balance): %v", err)
	}
	if got := lg.AccountBalance("income/discounts-allowances").Balance; got.Paise() != -16949 {
		// Contra-revenue: a Dr against a normal-Cr income account states as negative.
		t.Errorf("discounts-allowances balance = %d paise, want -16949 (contra)", got.Paise())
	}
}
