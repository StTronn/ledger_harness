package seed

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
)

// TestSplitGSTInclusive asserts the GST split is exact (net+gst == gross to the
// paise) across a range of grosses and rates — the property the truth GL relies
// on to balance. It is table-driven over hand-checked cases plus a sweep that
// asserts the summing invariant for many values.
func TestSplitGSTInclusive(t *testing.T) {
	tests := []struct {
		name    string
		gross   int64
		rate    int
		wantNet int64
		wantGST int64
	}{
		// 18% on a clean round gross: 11800 = 10000 + 1800.
		{"18pct round", 11800, 18, 10000, 1800},
		// 5% on 10500 = 10000 + 500.
		{"5pct round", 10500, 5, 10000, 500},
		// 12% on 11200 = 10000 + 1200.
		{"12pct round", 11200, 12, 10000, 1200},
		// Non-round: 328117 @ 5% -> net=312492 (trunc), gst=15625 (remainder).
		{"5pct trunc", 328117, 5, 312492, 15625},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			net, gst := splitGSTInclusive(money.FromPaise(tt.gross), tt.rate)
			if net.Paise() != tt.wantNet {
				t.Errorf("net = %d, want %d", net.Paise(), tt.wantNet)
			}
			if gst.Paise() != tt.wantGST {
				t.Errorf("gst = %d, want %d", gst.Paise(), tt.wantGST)
			}
			if net.Add(gst) != money.FromPaise(tt.gross) {
				t.Errorf("net+gst = %d, want gross %d (split must be exact)", net.Add(gst).Paise(), tt.gross)
			}
		})
	}
}

// TestSplitGSTInclusiveExactSweep asserts net+gst == gross for every gross in a
// dense range at each catalogue rate — the exactness invariant must hold for ALL
// values, not just the hand-picked ones, or the truth GL could fail to balance.
func TestSplitGSTInclusiveExactSweep(t *testing.T) {
	for _, rate := range gstRatePercents {
		for g := int64(1); g <= 5000; g++ {
			gross := money.FromPaise(g)
			net, gst := splitGSTInclusive(gross, rate)
			if net.Add(gst) != gross {
				t.Fatalf("rate %d gross %d: net(%d)+gst(%d) != gross", rate, g, net.Paise(), gst.Paise())
			}
			if net.Sign() < 0 || gst.Sign() < 0 {
				t.Fatalf("rate %d gross %d: negative component net=%d gst=%d", rate, g, net.Paise(), gst.Paise())
			}
		}
	}
}

// TestFeeAndGSTOnFee asserts the fee and GST-on-fee math (integer, truncating).
func TestFeeAndGSTOnFee(t *testing.T) {
	tests := []struct {
		gross   int64
		wantFee int64
		wantTax int64
	}{
		{100000, 2000, 360},  // ₹1000 -> fee ₹20 (2%), tax ₹3.60 (18% of fee)
		{49900, 998, 179},    // floor(49900*200/10000)=998; floor(998*18/100)=179
		{328117, 6562, 1181}, // floor(328117*200/10000)=6562; floor(6562*18/100)=1181
	}
	for _, tt := range tests {
		fee := feeForGross(money.FromPaise(tt.gross), feeBps)
		if fee.Paise() != tt.wantFee {
			t.Errorf("gross %d: fee = %d, want %d", tt.gross, fee.Paise(), tt.wantFee)
		}
		tax := gstOnFee(fee, razorpayGSTRate)
		if tax.Paise() != tt.wantTax {
			t.Errorf("gross %d: tax = %d, want %d", tt.gross, tax.Paise(), tt.wantTax)
		}
	}
}
