package seed

import (
	"fmt"
)

// This file implements the seeded reconciliation BREAKS (SPEC §5, §7, §12
// "seed each break class … and assert detection"). A break is injected as a
// post-generation TRANSFORM of the otherwise-clean substrate: the deterministic
// generator runs untouched (so the RNG stream, ids, amounts, and dates are
// byte-identical to a clean seed), then the named injection perturbs exactly one
// fixture field to create an inconsistency the Phase-5 reconcile detects. The
// hidden truth GL is left INTACT and balanced — the books still describe the
// correct world; only the agent-input record is made inconsistent, which is what
// a real reconciliation break is.

// Inject names a reconciliation-break class to seed into a period. The empty
// Inject ("") means a clean period (no break). The CLI validates the flag value
// against the known kinds before seeding.
type Inject string

const (
	// InjectNone is the default: a clean, fully-reconciling period.
	InjectNone Inject = ""
	// InjectRefundInBatch drops one refund's id from the settlement batch that
	// netted it, WITHOUT changing the deposit (the deposit still has the refund
	// netted out). The refund itself still exists in refunds.json and in the truth
	// GL (its refund_reversal entry is untouched), so truth still balances and
	// still includes the omitted refund. The settlement's batch members no longer
	// account for its (refund-reduced) deposit, so SPEC §7 check #2 (batch-sum)
	// detects it: Σpay − Σrefund(now missing) − Σfees > net_deposit, by exactly the
	// dropped refund's gross. This is the canonical "refund-in-batch" break of
	// SPEC §1: a refund economically reduced the settlement's bank deposit and is
	// in the books (truth), but the settlement record the close reconciles against
	// no longer accounts for it. The dropped refund's payment remains a batch
	// member, so the (Phase-8) investigate agent has a path from the break's
	// candidate ids to the refund that explains the gap.
	InjectRefundInBatch Inject = "refund-in-batch"

	// InjectUnbookedRefund models the SPEC §7 check #3 "settled-but-not-booked"
	// break — the ONE break class an investigate POSTING can resolve (Phase 8). It
	// strips the gst_rate from a refund that a settlement netted, leaving the refund
	// otherwise intact: still in refunds.json (so its id/amount/payment_id stay
	// recoverable), still listed in the settlement's refund_ids, and the deposit
	// unchanged. Because the deterministic rule engine cannot split a refund's GST
	// without a rate (internal/classify), the close SKIPS booking it — so the
	// refund_reversal that truth contains is missing from the produced books. The
	// receivable that the missing reversal should have credited is left short, so
	// SPEC §7 check #3 (receivable-clears) raises a residual break of exactly the
	// refund's gross; check #2 (batch-sum) stays GREEN because every amount and the
	// member list are untouched (it never reads gst_rate). The investigate agent
	// recovers the rate from the order (orders.json, NOT truth), books the
	// refund_reversal, and the receivable clears — the canonical Phase-8 resolution.
	// truth/gl.json is unperturbed and still books the refund at its true rate.
	InjectUnbookedRefund Inject = "unbooked-refund"

	// FUTURE break classes (SPEC §1, §7, §12 — "seed each break class … and assert
	// detection"). Only refund-in-batch is implemented in Phase 5; the others are
	// named here so the extensibility seam is explicit and the CLI/help can grow
	// without reshaping Options or the GenerateWith/SeedWith signatures. To add one,
	// declare its const, list it in KnownInjects, add a case to validateInject and
	// applyInject, and write the post-generation transform (a pure perturbation of
	// the agent-input fixtures that leaves truth/gl.json intact). Each maps to a
	// hypothesis the investigate agent forms (SPEC §1):
	//
	//   - InjectDisputeHold ("dispute-hold"): a settlement's deposit is short by a
	//     reserve Razorpay withheld against an open dispute; the held amount lands
	//     in a later period. Fires check #1 (amount short vs bank) and/or #3
	//     (receivable residual = the held reserve).
	//   - InjectTimingLag ("timing-lag"): a settlement's bank credit value-dates
	//     outside the check #1 tolerance window (legitimately settled, but late),
	//     so check #1's date match fails and check #3 may carry the in-transit gross.
	//   - InjectMisbookedFee ("mis-booked-fee"): a settlement's stated fee/tax does
	//     not match the contracted rate on its gross, so the batch-sum (check #2)
	//     fails by the fee delta even though every member id is present.
	//
	// These are intentionally NOT in KnownInjects yet — declaring an unimplemented
	// kind there would let the CLI accept a flag that applyInject cannot honour.
)

// Options carries the knobs the seeder accepts beyond (world, period): the
// reconciliation break injection (SPEC §5, §7) and the missing-metadata ambiguity
// long tail (SPEC §1, §2, §11 Phase 7). Both are post-generation transforms of
// the clean substrate (the clean RNG stream and truth GL are never perturbed), so
// keeping them as struct fields lets later phases add knobs without reshaping the
// GenerateWith/SeedWith signatures.
type Options struct {
	// Inject seeds a reconciliation break into the agent-input fixtures.
	Inject Inject
	// Ambiguity, when true, strips gst_rate from a deterministic ~15% of payments
	// (keeping sku + order_id), producing the rule-miss long tail the agent fills.
	Ambiguity bool
}

// KnownInjects is the closed set of supported injection kinds, used by the CLI to
// validate the --inject flag and to print the available values. It excludes
// InjectNone (the absence of an injection).
var KnownInjects = []Inject{InjectRefundInBatch, InjectUnbookedRefund}

// validateInject reports whether inj is a supported injection (or the empty
// no-op). An unknown value is a CLI error, surfaced before any IO so a typo never
// silently seeds a clean period.
func validateInject(inj Inject) error {
	switch inj {
	case InjectNone, InjectRefundInBatch, InjectUnbookedRefund:
		return nil
	default:
		return fmt.Errorf("seed: unknown inject %q (known: %v)", inj, KnownInjects)
	}
}

// applyInject mutates the freshly generated fixtures in place to seed the named
// break. It is called AFTER Generate has produced the clean, internally
// consistent substrate, so it perturbs a known-good world rather than tangling
// the break into the generation rules. The truth GL is NOT passed in and is
// never touched — the injected break lives only in the agent-input fixtures.
//
// It returns the affected ids (for the CLI summary and tests) and an error only
// if the injection cannot be applied to this period's substrate (e.g. no batch
// had a refund to drop), which would be a generation-shape regression worth
// failing loudly rather than silently seeding a clean period.
func applyInject(inj Inject, fx *Fixtures) (InjectResult, error) {
	switch inj {
	case InjectNone:
		return InjectResult{}, nil
	case InjectRefundInBatch:
		return injectRefundInBatch(fx)
	case InjectUnbookedRefund:
		return injectUnbookedRefund(fx)
	default:
		return InjectResult{}, fmt.Errorf("seed: unknown inject %q", inj)
	}
}

// InjectResult records what an injection changed, so the CLI can report it and a
// test can assert the break targets the expected ids. It is empty for a clean
// (no-inject) seed.
type InjectResult struct {
	Kind         Inject
	SettlementID string // the settlement whose batch (refund-in-batch) or netted refund (unbooked-refund) was perturbed
	RefundID     string // the refund id dropped from the batch (refund-in-batch) or whose gst_rate was stripped (unbooked-refund)
}

// injectRefundInBatch finds the FIRST settlement that netted a refund and removes
// that refund's id from the settlement's refund_ids — leaving the refund in
// refunds.json and the deposit unchanged. Choosing the first such settlement in
// fixture order keeps the injection deterministic. The dropped refund id is the
// one whose reversal the batch can no longer account for.
func injectRefundInBatch(fx *Fixtures) (InjectResult, error) {
	for i := range fx.Settlements {
		s := &fx.Settlements[i]
		if len(s.RefundIDs) == 0 {
			continue
		}
		dropped := s.RefundIDs[0]
		// Drop the first refund id; the deposit (s.Amount) is deliberately left as
		// the clean, refund-reduced value, so the batch members no longer sum to it.
		s.RefundIDs = append([]string{}, s.RefundIDs[1:]...)
		return InjectResult{
			Kind:         InjectRefundInBatch,
			SettlementID: s.ID,
			RefundID:     dropped,
		}, nil
	}
	return InjectResult{}, fmt.Errorf(
		"seed: cannot inject refund-in-batch — no settlement in this period netted a refund")
}

// injectUnbookedRefund seeds the SPEC §7 check #3 "settled-but-not-booked" break.
// It finds the FIRST settlement that netted a refund, then STRIPS the gst_rate
// from that refund's notes in refunds.json — leaving sku, amount, payment_id, the
// refund's membership in the settlement's refund_ids, and the deposit all intact.
// Stripping only the rate (the same field the §1/§2 ambiguity removes from a
// payment) makes the refund un-bookable by the deterministic rules (classify
// cannot split GST without a rate), so the close skips it and the receivable is
// left short by the refund's gross — the check #3 residual the investigate agent
// resolves by recovering the rate from the order and booking the refund_reversal.
//
// Choosing the first netted refund in settlement order keeps the injection
// deterministic. truth/gl.json is not passed in and is never touched, so truth
// still books the refund at its true rate (the correct resolution). It errors only
// if no settlement netted a refund (a generation-shape regression).
func injectUnbookedRefund(fx *Fixtures) (InjectResult, error) {
	for i := range fx.Settlements {
		s := &fx.Settlements[i]
		if len(s.RefundIDs) == 0 {
			continue
		}
		target := s.RefundIDs[0]
		for j := range fx.Refunds {
			r := &fx.Refunds[j]
			if r.ID != target {
				continue
			}
			// Strip ONLY the gst_rate; the refund stays in refunds.json and in the
			// settlement's refund_ids, and its amount/payment_id are untouched — so
			// check #2 (batch-sum, which never reads gst_rate) stays green while the
			// refund goes unbooked and surfaces as a check #3 receivable residual.
			r.Notes.GSTRate = ""
			return InjectResult{
				Kind:         InjectUnbookedRefund,
				SettlementID: s.ID,
				RefundID:     r.ID,
			}, nil
		}
	}
	return InjectResult{}, fmt.Errorf(
		"seed: cannot inject unbooked-refund — no settlement in this period netted a refund")
}
