package feeds_test

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// TestOrdersFeedReadsCommittedPeriod pins the canonical orders reader on the
// committed partial-refund world: every order carries its authoritative rate,
// and the 2026-01 orders carry line items.
func TestOrdersFeedReadsCommittedPeriod(t *testing.T) {
	const root = "../../.."
	orders, err := feeds.Orders(root, "dtc", "2026-01")
	if err != nil {
		t.Fatalf("Orders: %v", err)
	}
	o, ok := orders["order_jTYHxOWgY2SlPF"] // R1's parent order
	if !ok {
		t.Fatal("missing order_jTYHxOWgY2SlPF")
	}
	if o.GSTRate != "18" {
		t.Errorf("rate = %q, want 18", o.GSTRate)
	}
	if len(o.Items) != 2 || o.Items[0].Amount.Paise() != 28128 {
		t.Errorf("items = %+v, want 2 items with items[0]=28128", o.Items)
	}
}

// TestRateCardFeed pins the ratecard reader: the Razorpay channel's contracted
// fee (2.00% = 200 bps) and the GST rate on that fee (18%), matching the
// generation rules the seeder prices fees with — so the fee-tier policy's
// expected fee equals the actual fee on a clean period by construction.
func TestRateCardFeed(t *testing.T) {
	const root = "../../.."
	rc, err := feeds.RateCard(root, "dtc", "2026-01")
	if err != nil {
		t.Fatalf("RateCard: %v", err)
	}
	ch, ok := rc.Channel("razorpay")
	if !ok {
		t.Fatal("ratecard missing channel razorpay")
	}
	if ch.FeeBps != 200 || ch.FeeGSTRate != 18 {
		t.Errorf("razorpay channel = %+v, want fee_bps=200 fee_gst_rate=18", ch)
	}
}
