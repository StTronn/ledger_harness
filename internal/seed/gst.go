package seed

import "github.com/razorpay/close-agent/internal/money"

// This file holds the seeder's money arithmetic: splitting a GST-inclusive gross
// into net + GST, and computing Razorpay's fee and GST-on-fee. All of it is
// integer-paise (money.Money) only — no float (SPEC §1, §4). Division uses
// integer division with an explicit, documented rounding rule, and the remainder
// is folded back so the parts ALWAYS sum exactly to the whole. Exact summing is
// what keeps the truth GL balanced by construction.

// gstRatePercents is the fixed set of GST rate percentages a DTC product may be
// sold at in v1. Stored as integers (percent) so all GST math stays in integer
// space. These mirror common Indian GST slabs (5%, 12%, 18%).
var gstRatePercents = []int{5, 12, 18}

// splitGSTInclusive splits a GST-inclusive gross amount into its net (taxable
// base) and GST components at the given integer rate percent, such that
// net + gst == gross EXACTLY (SPEC §4.2: derived amounts via division are
// computed at bind time, in integer space).
//
// The maths: gross = net * (100 + rate) / 100, so
//
//	net = gross * 100 / (100 + rate)   (integer division, truncated)
//	gst = gross - net                  (remainder folded into GST)
//
// Folding the truncation remainder into gst (rather than rounding net
// independently) guarantees net + gst == gross to the paise, which is the
// invariant the ledger's balance check relies on. rate must be > 0; the function
// panics on a non-positive rate (a generator-rule bug, not runtime input).
func splitGSTInclusive(gross money.Money, ratePercent int) (net, gst money.Money) {
	if ratePercent <= 0 {
		panic("seed: splitGSTInclusive called with non-positive rate")
	}
	g := gross.Paise()
	// net = g * 100 / (100 + rate). g is paise (int64); 100*g cannot realistically
	// overflow int64 for any sane period total, but we compute as int64 directly.
	netPaise := g * 100 / int64(100+ratePercent)
	net = money.FromPaise(netPaise)
	gst = gross.Sub(net) // remainder folded in, so net+gst == gross exactly
	return net, gst
}

// feeForGross computes Razorpay's processing fee on a gross amount at the given
// fee rate in basis points (bps; 1 bps = 0.01%). Integer division truncates the
// fraction of a paise, which mirrors how a real fee is rounded down to the paise.
// feeBps must be >= 0.
//
//	fee = gross * feeBps / 10000
func feeForGross(gross money.Money, feeBps int) money.Money {
	if feeBps < 0 {
		panic("seed: feeForGross called with negative bps")
	}
	return money.FromPaise(gross.Paise() * int64(feeBps) / 10000)
}

// gstOnFee computes the GST charged on Razorpay's fee at the given integer rate
// percent (Razorpay's services are taxed at 18% GST). The fee is GST-EXCLUSIVE
// here, so gst_on_fee = fee * rate / 100 (integer division). ratePercent must be
// >= 0.
func gstOnFee(fee money.Money, ratePercent int) money.Money {
	if ratePercent < 0 {
		panic("seed: gstOnFee called with negative rate")
	}
	return money.FromPaise(fee.Paise() * int64(ratePercent) / 100)
}

// razorpayGSTRate is the GST percentage applied to Razorpay's own service fee.
// Payment-gateway services are taxed at 18% GST; this is a separate axis from the
// product GST rate carried in a payment's notes.
const razorpayGSTRate = 18
