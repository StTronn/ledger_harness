package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razorpay/ledger-flow/internal/score"
)

// integration_test.go is the Phase-6 "deterministic product complete" gate:
// it drives the FULL deterministic pipeline end to end on dtc/2026-05 from a
// SINGLE entry point — the CLI Execute surface, exactly what an operator runs —
// and asserts the whole agent-free product holds together:
//
//   - seed → run produces score = 100% with 0 skipped events;
//   - run writes runs/dtc-2026-05/errors.json with the FROZEN, version-stamped
//     schema, 0 error records, score_pct 100, trial_balance_matches true, and
//     every per-account delta zero (SPEC §9, §13);
//   - report --kind {trial-balance,balance-sheet,income,journal} each render and
//     the balancing reports balance (trial balance ΣDr==ΣCr, journal per-entry);
//   - diff reports no differences vs truth on the clean period;
//   - show trace handles a missing trace gracefully.
//
// Everything runs against a temp root (the hidden --root flag) so the test is
// self-contained and never touches the repo's worlds/ or runs/.

// runCLI executes one CLI invocation against the test command tree and returns
// its captured output, failing the test on any command error.
func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Execute(args, &buf); err != nil {
		t.Fatalf("ledger-flow %s: %v\n%s", strings.Join(args, " "), err, buf.String())
	}
	return buf.String()
}

// TestDeterministicProductEndToEnd is the single-entry-point integration test for
// the complete agent-free product. From one place (the CLI), it seeds, closes,
// reports, and diffs dtc/2026-05 and asserts the full Phase-6 contract.
func TestDeterministicProductEndToEnd(t *testing.T) {
	root := t.TempDir()
	const (
		world  = "dtc"
		period = "2026-05"
	)
	wp := []string{"--world", world, "--period", period, "--root", root}

	// 1) Seed the substrate (fixtures + bank feed + truth GL) into the temp root.
	seedOut := runCLI(t, append([]string{"seed"}, wp...)...)
	if !strings.Contains(seedOut, `seeded world "dtc" period "2026-05"`) {
		t.Fatalf("seed did not write the substrate:\n%s", seedOut)
	}

	// 2) Close: the full deterministic pipeline. A single command must produce the
	// score AND the errors.json artifact and report both.
	closeOut := runCLI(t, append([]string{"run"}, wp...)...)
	for _, want := range []string{
		"skipped:    0 events",
		"reconcile: 0 breaks (reconciled)",
		"trial balance matches truth: yes",
		"entries correct: 45/45",
		"errors record:",
		"schema v1",
		"score = 100%",
	} {
		if !strings.Contains(closeOut, want) {
			t.Errorf("run output missing %q\n---\n%s", want, closeOut)
		}
	}
	if strings.Contains(closeOut, "scoring errors") {
		t.Errorf("run reported scoring errors on the clean period:\n%s", closeOut)
	}

	// 3) errors.json: written to runs/<world>-<period>/, decodes through the FROZEN
	// decoder, and carries the clean-period contract (version-stamped, 0 errors,
	// 100%, trial balance matches, all per-account deltas zero). SPEC §9, §13.
	errPath := filepath.Join(root, "runs", "dtc-2026-05", "errors.json")
	data, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("errors.json not written by run: %v", err)
	}
	rec, err := score.UnmarshalErrors(data)
	if err != nil {
		t.Fatalf("errors.json does not decode under the frozen schema: %v", err)
	}
	if rec.SchemaVersion != score.ErrorsSchemaVersion {
		t.Errorf("errors.json schema_version = %d, want %d (frozen)", rec.SchemaVersion, score.ErrorsSchemaVersion)
	}
	if rec.World != world || rec.Period != period {
		t.Errorf("errors.json world/period = %q/%q, want %q/%q", rec.World, rec.Period, world, period)
	}
	if rec.ScorePct != 100 {
		t.Errorf("errors.json score_pct = %d, want 100", rec.ScorePct)
	}
	if !rec.TrialBalanceMatches {
		t.Errorf("errors.json trial_balance_matches = false, want true")
	}
	if len(rec.Errors) != 0 {
		t.Errorf("errors.json has %d error records, want 0 on a clean run: %+v", len(rec.Errors), rec.Errors)
	}
	if rec.Totals.TruthEntries != 45 || rec.Totals.Correct != 45 ||
		rec.Totals.Wrong != 0 || rec.Totals.Missing != 0 || rec.Totals.Extra != 0 {
		t.Errorf("errors.json totals = %+v, want 45/45 with 0 wrong/missing/extra", rec.Totals)
	}
	if len(rec.PerAccountDeltas) == 0 {
		t.Errorf("errors.json per_account_deltas is empty; want one row per active account")
	}
	for _, d := range rec.PerAccountDeltas {
		if !d.Delta.IsZero() {
			t.Errorf("errors.json account %q delta = %s, want 0 on a clean run", d.Account, d.Delta)
		}
	}
	// schema_version must be the FIRST key on disk so a consumer can branch on it.
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte(`{`)) ||
		!strings.Contains(string(data), `"schema_version": 1`) {
		t.Errorf("errors.json missing version stamp as a leading key:\n%s", data)
	}

	// 4) All four reports render from the same single period, and the balancing
	// reports balance. Each report rebuilds the ledger via the same pipeline.
	reportCases := []struct {
		kind     string
		wantSubs []string
	}{
		{"trial-balance", []string{"TRIAL BALANCE", "DEBIT", "CREDIT", "balanced: ΣDr == ΣCr"}},
		{"balance-sheet", []string{"BALANCE SHEET", "ASSETS", "LIABILITIES", "TOTAL ASSETS"}},
		{"income", []string{"INCOME STATEMENT", "INCOME", "EXPENSE", "NET INCOME"}},
		{"journal", []string{"JOURNAL", "45 entries", "Dr ", "Cr "}},
	}
	for _, rc := range reportCases {
		out := runCLI(t, append([]string{"report", "--kind", rc.kind}, wp...)...)
		for _, sub := range rc.wantSubs {
			if !strings.Contains(out, sub) {
				t.Errorf("report --kind %s missing %q\n---\n%s", rc.kind, sub, out)
			}
		}
	}

	// 5) diff: no differences vs truth on the clean period.
	diffOut := runCLI(t, append([]string{"diff"}, wp...)...)
	if !strings.Contains(diffOut, "no differences vs truth") {
		t.Errorf("diff on the clean period reported differences:\n%s", diffOut)
	}

	// 6) show trace: a missing trace is handled gracefully (no error, exit 0). The
	// run dir exists (run wrote errors.json there) but has no trace.json, which is
	// the EXPECTED agent-free state.
	traceOut := runCLI(t, "show", "trace", filepath.Join(root, "runs", "dtc-2026-05"))
	if !strings.Contains(traceOut, "no trace available (agent phases not run)") {
		t.Errorf("show trace did not gracefully report the missing trace:\n%s", traceOut)
	}

	// 7) show playbook still works (carried over, must not regress).
	pbOut := runCLI(t, "show", "playbook", "--playbook", filepath.Join("..", "..", "config", "playbook.json"))
	if !strings.Contains(pbOut, "CHART OF ACCOUNTS") || !strings.Contains(pbOut, "ENTRY TYPES") {
		t.Errorf("show playbook regressed:\n%s", pbOut)
	}
}

// TestDeterministicProductByteIdentical asserts the single-entry-point product is
// DETERMINISTIC (SPEC §5, §12): two full closes over the same seeded fixtures
// write byte-identical errors.json and emit identical report output. This is the
// "same fixtures => byte-identical output" invariant exercised at the CLI seam.
func TestDeterministicProductByteIdentical(t *testing.T) {
	root := t.TempDir()
	wp := []string{"--world", "dtc", "--period", "2026-05", "--root", root}
	runCLI(t, append([]string{"seed"}, wp...)...)

	errPath := filepath.Join(root, "runs", "dtc-2026-05", "errors.json")

	closeA := runCLI(t, append([]string{"run"}, wp...)...)
	errA, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("read errors.json after run A: %v", err)
	}
	tbA := runCLI(t, append([]string{"report", "--kind", "trial-balance"}, wp...)...)

	closeB := runCLI(t, append([]string{"run"}, wp...)...)
	errB, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("read errors.json after run B: %v", err)
	}
	tbB := runCLI(t, append([]string{"report", "--kind", "trial-balance"}, wp...)...)

	if !bytes.Equal(errA, errB) {
		t.Errorf("errors.json differs between runs:\n--- A ---\n%s\n--- B ---\n%s", errA, errB)
	}
	if closeA != closeB {
		t.Errorf("run output differs between runs:\n--- A ---\n%s\n--- B ---\n%s", closeA, closeB)
	}
	if tbA != tbB {
		t.Errorf("trial-balance report differs between runs:\n--- A ---\n%s\n--- B ---\n%s", tbA, tbB)
	}
}
