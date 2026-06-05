package closer_test

import (
	"testing"

	"github.com/razorpay/close-agent/internal/closer"
	"github.com/razorpay/close-agent/internal/money"
	"github.com/razorpay/close-agent/internal/reconcile"
	"github.com/razorpay/close-agent/internal/seed"
)

// seedPeriodWith seeds the dtc/2026-05 substrate with the given injection into a
// fresh temp root, returning the root and the injection result (which records the
// settlement/refund the break targets). It mirrors seedPeriod but exercises the
// Phase-5 break-injection path.
func seedPeriodWith(t *testing.T, inj seed.Inject) (string, seed.InjectResult) {
	t.Helper()
	root := t.TempDir()
	res, err := seed.SeedWith(root, "dtc", "2026-05", seed.Options{Inject: inj})
	if err != nil {
		t.Fatalf("seed with inject %q: %v", inj, err)
	}
	return root, res.Inject
}

// TestCleanPeriodReconcilesFully is the Phase-5 gate (SPEC §7): the clean
// dtc/2026-05 period reconciles with 0 breaks AND still scores 100%.
func TestCleanPeriodReconcilesFully(t *testing.T) {
	root := seedPeriod(t)

	res, err := closer.Run(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("closer.Run: %v", err)
	}
	if len(res.Breaks) != 0 {
		t.Errorf("clean period produced %d reconcile breaks, want 0: %+v", len(res.Breaks), res.Breaks)
	}
	if !res.Score.IsPerfect() || res.Score.Percent() != 100 {
		t.Errorf("clean period score = %d%% (perfect=%v), want 100%% perfect",
			res.Score.Percent(), res.Score.IsPerfect())
	}
}

// TestRefundInBatchBreakDetected is the Phase-5 seeded-break gate (SPEC §5, §7,
// §12): the refund-in-batch injection is detected as >=1 reconcile break carrying
// context (settlement id, expected vs actual, candidate event ids), and the run
// does not crash. (The companion seed test, TestInjectRefundInBatchKeepsTruth,
// asserts the period's hidden truth GL still balances and still includes the
// omitted refund — that property is about the seeder, which is allowed to read
// truth, so it lives there rather than reaching across the isolation boundary.)
func TestRefundInBatchBreakDetected(t *testing.T) {
	root, inj := seedPeriodWith(t, seed.InjectRefundInBatch)
	if inj.SettlementID == "" || inj.RefundID == "" {
		t.Fatalf("injection did not record a target: %+v", inj)
	}

	res, err := closer.Run(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("closer.Run on injected period crashed: %v", err)
	}

	// At least one break, and the batch-sum break must target the injected
	// settlement.
	if len(res.Breaks) == 0 {
		t.Fatalf("no reconcile breaks detected for injected refund-in-batch")
	}
	var batchSum *reconcile.Break
	for i := range res.Breaks {
		b := &res.Breaks[i]
		if b.Check == reconcile.CheckBatchSum && b.SettlementID == inj.SettlementID {
			batchSum = b
		}
		// Every break must carry investigator context.
		if b.Detail == "" {
			t.Errorf("break %+v has empty Detail", b)
		}
		if len(b.CandidateEventIDs) == 0 {
			t.Errorf("break %+v has no candidate event ids", b)
		}
	}
	if batchSum == nil {
		t.Fatalf("no batch-sum break for injected settlement %s; breaks=%+v", inj.SettlementID, res.Breaks)
	}
	if batchSum.Kind != reconcile.KindBatchSumMismatch {
		t.Errorf("batch-sum break kind = %q, want %q", batchSum.Kind, reconcile.KindBatchSumMismatch)
	}
	// Context: expected (member-implied deposit, refund no longer netted) must
	// exceed actual (the stated net_deposit), because dropping the refund from the
	// batch members removes a subtraction and inflates the implied deposit.
	if batchSum.Expected.Paise() <= batchSum.Actual.Paise() {
		t.Errorf("batch-sum break expected %s should exceed actual %s (omitted refund inflates implied deposit)",
			batchSum.Expected, batchSum.Actual)
	}
	if len(batchSum.CandidateEventIDs) == 0 {
		t.Errorf("batch-sum break carries no candidate batch members")
	}

	// Economic coherence: the implied-vs-stated gap is EXACTLY the dropped refund's
	// gross — that single subtraction is all that was removed from the batch. This
	// pins the break to the refund-in-batch story rather than any incidental delta.
	droppedGross := droppedRefundGross(t, inj.RefundID)
	if gap := batchSum.Expected.Sub(batchSum.Actual); gap != droppedGross {
		t.Errorf("batch-sum gap = %s, want exactly the dropped refund gross %s", gap, droppedGross)
	}

	// Investigator path: the dropped refund's payment must remain a batch member
	// (a candidate), so the Phase-8 agent can walk from the candidate ids to the
	// refund that explains the gap. The refund id itself is no longer a candidate
	// (it was dropped from refund_ids), which is precisely the break.
	wantPay := droppedRefundPaymentID(t, inj.RefundID)
	if !containsString(batchSum.CandidateEventIDs, wantPay) {
		t.Errorf("dropped refund's payment %s not among candidates %v", wantPay, batchSum.CandidateEventIDs)
	}
	if containsString(batchSum.CandidateEventIDs, inj.RefundID) {
		t.Errorf("dropped refund %s should not appear in candidates; it was removed from the batch", inj.RefundID)
	}

	// Truth is untouched by the injection AND the refund is still in refunds.json,
	// so the deterministic close still books every entry — the score stays perfect
	// even though the settlement record no longer reconciles. The break is the only
	// symptom; the books are not (yet) wrong. (This is what a Phase-8 agent will
	// later reconcile away.)
	if !res.Score.IsPerfect() || res.Score.Percent() != 100 {
		t.Errorf("injected period score = %d%% (perfect=%v), want 100%% — truth and books are unaffected",
			res.Score.Percent(), res.Score.IsPerfect())
	}
}

// droppedRefundGross / droppedRefundPaymentID re-derive the clean substrate (the
// injection is a pure post-generation transform, so the clean Generate gives the
// pre-injection record) and return the dropped refund's gross / its payment id.
// They use the seed package's own generator, not truth, keeping the assertion on
// agent-input data.
func droppedRefundGross(t *testing.T, refundID string) money.Money {
	t.Helper()
	r := lookupCleanRefund(t, refundID)
	return r.Amount
}

func droppedRefundPaymentID(t *testing.T, refundID string) string {
	t.Helper()
	return lookupCleanRefund(t, refundID).PaymentID
}

func lookupCleanRefund(t *testing.T, refundID string) seed.Refund {
	t.Helper()
	clean, _, _, err := seed.Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("clean Generate: %v", err)
	}
	for _, r := range clean.Refunds {
		if r.ID == refundID {
			return r
		}
	}
	t.Fatalf("dropped refund %s not found in clean substrate", refundID)
	return seed.Refund{}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
