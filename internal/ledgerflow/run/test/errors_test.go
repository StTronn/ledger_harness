package run_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/razorpay/ledger-flow/internal/ledgerflow/run"
	"github.com/razorpay/ledger-flow/internal/score"
)

// TestRunWritesErrorsArtifact is the Phase-6 gate at the package level: a close on
// the clean seeded period writes runs/<world>-<period>/errors.json with the FROZEN
// schema, 0 error records, score 100%, trial_balance_matches true, and every
// per-account delta zero (SPEC §9, §13).
func TestRunWritesErrorsArtifact(t *testing.T) {
	root := seedPeriod(t)

	res, err := run.Run(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("run.Run: %v", err)
	}

	// The artifact path is reported and located at runs/dtc-2026-05/errors.json.
	wantPath := filepath.Join(root, "runs", "dtc-2026-05", "errors.json")
	if res.ErrorsPath != wantPath {
		t.Errorf("ErrorsPath = %q, want %q", res.ErrorsPath, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("errors.json not written: %v", err)
	}

	// Read it back through the frozen decoder and assert the clean-period contract.
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read errors.json: %v", err)
	}
	rec, err := score.UnmarshalErrors(data)
	if err != nil {
		t.Fatalf("decode errors.json: %v", err)
	}
	if rec.SchemaVersion != score.ErrorsSchemaVersion {
		t.Errorf("schema_version = %d, want %d", rec.SchemaVersion, score.ErrorsSchemaVersion)
	}
	if rec.World != "dtc" || rec.Period != "2026-05" {
		t.Errorf("world/period = %q/%q, want dtc/2026-05", rec.World, rec.Period)
	}
	if rec.ScorePct != 100 {
		t.Errorf("score_pct = %d, want 100", rec.ScorePct)
	}
	if !rec.TrialBalanceMatches {
		t.Errorf("trial_balance_matches = false, want true")
	}
	if len(rec.Errors) != 0 {
		t.Errorf("errors = %v, want 0 records on clean period", rec.Errors)
	}
	if rec.Totals.TruthEntries != 45 || rec.Totals.Correct != 45 {
		t.Errorf("totals = %+v, want 45 truth_entries / 45 correct", rec.Totals)
	}
	for _, d := range rec.PerAccountDeltas {
		if !d.Delta.IsZero() {
			t.Errorf("account %q delta = %s, want 0 on clean period", d.Account, d.Delta)
		}
	}
	if len(rec.PerAccountDeltas) == 0 {
		t.Errorf("per_account_deltas is empty; want one row per active account")
	}
}

// TestRunErrorsArtifactDeterministic: two closes over the same fixtures write
// BYTE-IDENTICAL errors.json (SPEC §9 determinism, §13 frozen seam).
func TestRunErrorsArtifactDeterministic(t *testing.T) {
	root := seedPeriod(t)
	path := filepath.Join(root, "runs", "dtc-2026-05", "errors.json")

	if _, err := run.Run(root, "dtc", "2026-05"); err != nil {
		t.Fatalf("run a: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after run a: %v", err)
	}
	if _, err := run.Run(root, "dtc", "2026-05"); err != nil {
		t.Fatalf("run b: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after run b: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("errors.json differs between runs:\n--- a ---\n%s\n--- b ---\n%s", first, second)
	}
}

// TestRunErrorsArtifactTampered: stripping one event's gst metadata is recovered
// from the order, so the emitted errors.json remains clean.
func TestRunErrorsArtifactTampered(t *testing.T) {
	root := seedPeriod(t)
	stripped := stripOneGSTRate(t, root)

	res, err := run.Run(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("run.Run after stripping gst_rate: %v", err)
	}

	rec := res.Record
	if rec.ScorePct != 100 {
		t.Errorf("score_pct = %d after deterministic recovery, want 100", rec.ScorePct)
	}
	if rec.Totals.Missing != 0 {
		t.Errorf("totals.missing = %d, want 0", rec.Totals.Missing)
	}
	if len(rec.Errors) != 0 {
		t.Errorf("errors = %+v, want none after recovering %q", rec.Errors, stripped)
	}
	for _, d := range rec.PerAccountDeltas {
		if !d.Delta.IsZero() {
			t.Errorf("account %q delta = %s, want 0 after recovery", d.Account, d.Delta)
		}
	}

	// The artifact on disk reflects the tamper (it is the persisted record).
	data, err := os.ReadFile(res.ErrorsPath)
	if err != nil {
		t.Fatalf("read errors.json: %v", err)
	}
	disk, err := score.UnmarshalErrors(data)
	if err != nil {
		t.Fatalf("decode errors.json: %v", err)
	}
	if disk.Totals.Missing != 0 || disk.ScorePct != 100 {
		t.Errorf("on-disk record does not reflect recovery: %+v score=%d", disk.Totals, disk.ScorePct)
	}
}
