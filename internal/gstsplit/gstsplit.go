// Package gstsplit is the SINGLE canonical source of truth for splitting a
// GST-inclusive gross amount into its net (taxable base) and GST components.
//
// Why this lives in one place (SPEC §2 Phase 4, §4.2): the seeder generates the
// hidden truth GL and the rule-engine classifier independently book the same
// events. If their inclusive-GST arithmetic ever drifts — even by a single paise
// of rounding — the classifier's posted entries will not equal truth and the
// score silently falls below 100%. To make drift IMPOSSIBLE, both the seeder
// (internal/seed) and the classifier MUST call SplitInclusive here; neither may
// re-implement the formula.
//
// Money invariant (SPEC §1, §4): everything is integer minor units (paise) as
// money.Money. No float ever touches this math; a guard test enforces that
// statically.
package gstsplit

import "github.com/razorpay/ledger-flow/internal/money"

// SplitInclusive splits a GST-inclusive gross amount into its net (taxable base)
// and GST components at the given integer rate percent, such that
//
//	net + gst == gross   EXACTLY (to the paise)
//
// The maths (integer space only):
//
//	net = gross * 100 / (100 + rate)   (integer division, truncated toward zero)
//	gst = gross - net                  (truncation remainder folded into GST)
//
// Folding the truncation remainder into gst — rather than rounding net
// independently — guarantees net + gst == gross to the paise. That exactness is
// the invariant the ledger's balance check relies on: a dtc_sale posts
// Dr receivable {gross}, Cr product-sales {net}, Cr gst-output-payable {gst}, and
// it can only balance if net + gst == gross.
//
// rate must be > 0; the function panics on a non-positive rate, which is a
// generator/classifier rule bug (a missing or zero GST rate), not valid runtime
// input. Callers that can receive untrusted/missing metadata must validate the
// rate before calling (e.g. flag-and-skip the event), not pass 0 here.
func SplitInclusive(gross money.Money, ratePercent int) (net, gst money.Money) {
	if ratePercent <= 0 {
		panic("gstsplit: SplitInclusive called with non-positive rate")
	}
	g := gross.Paise()
	// net = g * 100 / (100 + rate). g is paise (int64); for any realistic period
	// total 100*g stays well within int64, and integer division truncates toward
	// zero just as the seeder's original formula did.
	netPaise := g * 100 / int64(100+ratePercent)
	net = money.FromPaise(netPaise)
	gst = gross.Sub(net) // remainder folded in, so net+gst == gross exactly
	return net, gst
}
