package closer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/razorpay/close-agent/internal/closer"
	"github.com/razorpay/close-agent/internal/seed"
)

// seedPeriod seeds the dtc/2026-05 substrate (fixtures + bank feed + truth GL)
// into a fresh temp root and returns that root, so the close pipeline runs
// against a self-contained period and never touches the repo's worlds/.
func seedPeriod(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := seed.Seed(root, "dtc", "2026-05"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return root
}

// TestRunScoresPerfectOnCleanPeriod is the Phase-4 gate at the package level: on
// the clean seeded period every event classifies (0 skips), the produced ledger
// equals truth entry-by-entry, the trial balance matches, and the score is 100%.
func TestRunScoresPerfectOnCleanPeriod(t *testing.T) {
	root := seedPeriod(t)

	res, err := closer.Run(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("closer.Run: %v", err)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("skipped %d events, want 0: %+v", len(res.Skipped), res.Skipped)
	}
	if res.Classified != 45 {
		t.Errorf("classified = %d, want 45", res.Classified)
	}
	if res.Ledger.Len() != 45 {
		t.Errorf("posted entries = %d, want 45", res.Ledger.Len())
	}
	if res.Score.Total != 45 || res.Score.Correct != 45 {
		t.Errorf("score correct/total = %d/%d, want 45/45", res.Score.Correct, res.Score.Total)
	}
	if !res.Score.IsPerfect() {
		t.Errorf("score not perfect; errors=%v", res.Score.Errors)
	}
	if !res.Score.TrialBalanceMatches {
		t.Errorf("trial balance does not match truth")
	}
	if pct := res.Score.Percent(); pct != 100 {
		t.Errorf("score = %d%%, want 100%%", pct)
	}
}

// TestRunDeterministic asserts the close is byte-deterministic at the result
// level: two runs over the same fixtures produce identical produced entries and
// the same score (SPEC §5, §12).
func TestRunDeterministic(t *testing.T) {
	root := seedPeriod(t)
	a, err := closer.Run(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("run a: %v", err)
	}
	b, err := closer.Run(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("run b: %v", err)
	}
	if a.Classified != b.Classified || len(a.Produced) != len(b.Produced) {
		t.Fatalf("counts differ between runs")
	}
	for i := range a.Produced {
		pa, pb := a.Produced[i], b.Produced[i]
		if pa.EventID != pb.EventID || pa.EntryType != pb.EntryType || pa.TxID != pb.TxID {
			t.Fatalf("produced[%d] header differs: %+v vs %+v", i, pa, pb)
		}
		if len(pa.Lines) != len(pb.Lines) {
			t.Fatalf("produced[%d] line count differs", i)
		}
		for j := range pa.Lines {
			if pa.Lines[j] != pb.Lines[j] {
				t.Fatalf("produced[%d] line %d differs: %+v vs %+v", i, j, pa.Lines[j], pb.Lines[j])
			}
		}
	}
	if a.Score.Percent() != b.Score.Percent() || a.Score.Correct != b.Score.Correct {
		t.Fatalf("score differs between runs")
	}
}

// TestRunFlagsSyntheticMissingMetadata is the SPEC's flag-and-skip case: a
// payment with no gst_rate (its notes stripped) is reported as a SKIP, not
// crashed, and not posted — and because that event is now unbooked, its truth
// entry is MISSING in the score (the run still completes and scores cleanly).
func TestRunFlagsSyntheticMissingMetadata(t *testing.T) {
	root := seedPeriod(t)
	stripped := stripOneGSTRate(t, root)

	res, err := closer.Run(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("closer.Run after stripping gst_rate: %v", err)
	}

	// Exactly one event must be skipped, attributed to the stripped payment, with
	// a missing-gst_rate reason.
	if len(res.Skipped) != 1 {
		t.Fatalf("skipped = %+v, want exactly 1 (the stripped payment)", res.Skipped)
	}
	sk := res.Skipped[0]
	if sk.EventID != stripped {
		t.Errorf("skipped event = %q, want %q", sk.EventID, stripped)
	}
	if sk.Type != "payment" {
		t.Errorf("skipped type = %q, want payment", sk.Type)
	}
	if sk.Reason == "" {
		t.Errorf("skip reason is empty")
	}

	// 44 of 45 classify; the skipped event's truth entry is missing.
	if res.Classified != 44 {
		t.Errorf("classified = %d, want 44", res.Classified)
	}
	if res.Score.Correct != 44 || res.Score.Total != 45 {
		t.Errorf("score = %d/%d, want 44/45", res.Score.Correct, res.Score.Total)
	}
	if len(res.Score.Errors) != 1 || res.Score.Errors[0].EventID != stripped {
		t.Errorf("score errors = %+v, want one missing for %q", res.Score.Errors, stripped)
	}
}

// stripOneGSTRate rewrites the first payment in the seeded payments.json so its
// notes carry no gst_rate, simulating an event with absent tax metadata. It
// returns the affected payment id so the caller can assert the skip targets it.
// This edits a copy on disk in the temp root; the repo fixtures are untouched.
func stripOneGSTRate(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "worlds", "dtc", "2026-05", "razorpay", "payments.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read payments: %v", err)
	}
	payments, id := blankFirstGSTRate(t, data)
	if err := os.WriteFile(path, payments, 0o644); err != nil {
		t.Fatalf("write payments: %v", err)
	}
	return id
}
