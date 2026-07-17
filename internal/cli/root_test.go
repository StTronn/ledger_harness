package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShowTraceMissingGraceful drives `show trace` at a path with no trace and
// asserts it handles the absence GRACEFULLY (SPEC §10 gate): no error, no crash,
// a clear "no trace found" message. In the agent-free Phase-6 product no traces
// exist, so this is the expected outcome.
func TestShowTraceMissingGraceful(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "runs", "dtc-2026-05")
	var buf bytes.Buffer
	if err := Execute([]string{"show", "trace", missing}, &buf); err != nil {
		t.Fatalf("Execute(show trace) on missing path returned error: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "no trace found") {
		t.Errorf("show trace on missing path = %q, want a graceful 'no trace found' message", got)
	}
}

// TestShowTraceEmptyFileGraceful asserts an EMPTY (whitespace-only) trace file is
// handled the same as a missing one (SPEC §10 gate, Phase-6 task): no error, the
// graceful "no trace available (agent phases not run)" message, exit 0.
func TestShowTraceEmptyFileGraceful(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "trace.json")
	if err := os.WriteFile(trace, []byte("   \n"), 0o644); err != nil {
		t.Fatalf("write empty trace: %v", err)
	}
	var buf bytes.Buffer
	if err := Execute([]string{"show", "trace", trace}, &buf); err != nil {
		t.Fatalf("Execute(show trace) on empty file returned error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"no trace found", "no trace available (agent phases not run)"} {
		if !strings.Contains(out, want) {
			t.Errorf("show trace on empty file = %q, want %q", out, want)
		}
	}
}

// TestShowTracePrettyPrintsPresent asserts that when a trace IS present (the
// Phase-7+ case, simulated here with a minified JSON file), `show trace`
// pretty-prints it with 2-space indentation rather than dumping it verbatim.
func TestShowTracePrettyPrintsPresent(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "trace.json")
	if err := os.WriteFile(trace, []byte(`{"event_id":"pay_1","decision":"dtc_sale"}`), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	var buf bytes.Buffer
	if err := Execute([]string{"show", "trace", trace}, &buf); err != nil {
		t.Fatalf("Execute(show trace) on present file returned error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "no trace found") {
		t.Errorf("show trace on a present trace printed the missing-trace message:\n%s", out)
	}
	// Pretty-printed JSON re-indents onto multiple lines with the recorded fields.
	for _, want := range []string{"\"event_id\": \"pay_1\"", "\"decision\": \"dtc_sale\""} {
		if !strings.Contains(out, want) {
			t.Errorf("show trace did not pretty-print %q:\n%s", want, out)
		}
	}
}

// TestShowTraceResolvesDirToTraceFile asserts that pointing `show trace` at a run
// DIRECTORY reads trace.json inside it (the SPEC §10 `show trace runs/<...>` form).
func TestShowTraceResolvesDirToTraceFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trace.json"), []byte(`{"k":"v"}`), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	var buf bytes.Buffer
	if err := Execute([]string{"show", "trace", dir}, &buf); err != nil {
		t.Fatalf("Execute(show trace <dir>) returned error: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "\"k\": \"v\"") {
		t.Errorf("show trace <dir> did not read trace.json inside it:\n%s", out)
	}
}

// TestHelpListsSubcommands asserts `ledger-flow --help` lists every top-level
// subcommand named in the gate.
func TestHelpListsSubcommands(t *testing.T) {
	var buf bytes.Buffer
	if err := Execute([]string{"--help"}, &buf); err != nil {
		t.Fatalf("Execute(--help) returned error: %v", err)
	}
	out := buf.String()
	for _, sub := range []string{"seed", "run", "report", "show", "diff"} {
		if !strings.Contains(out, sub) {
			t.Errorf("--help output missing subcommand %q\n---\n%s", sub, out)
		}
	}
}

// TestShowPlaybookPrintsRealSchema drives `show playbook` against the committed
// config/playbook.json and asserts it loads, validates, and prints the real
// chart of accounts plus the entry-type templates (SPEC §4.1, §4.2, §10).
func TestShowPlaybookPrintsRealSchema(t *testing.T) {
	playbook := filepath.Join("..", "..", "config", "playbook.json")
	var buf bytes.Buffer
	if err := Execute([]string{"show", "playbook", "--playbook", playbook}, &buf); err != nil {
		t.Fatalf("Execute(show playbook) returned error: %v", err)
	}
	out := buf.String()
	wantSubstrings := []string{
		"CHART OF ACCOUNTS",
		"ENTRY TYPES",
		"assets/", "liabilities/", "income/", "expense/",
		"bank", "razorpay-settlement-receivable", "gst-output-payable",
		"product-sales", "sales-returns", "processor-fees", "chargeback-loss",
		"normal balance: Debit", "normal balance: Credit",
		"dtc_sale", "razorpay_settlement", "refund_reversal", "chargeback_loss",
		"net+gst",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(out, s) {
			t.Errorf("show playbook output missing %q\n---\n%s", s, out)
		}
	}
}

// TestShowPlaybookMissingFileErrors asserts a bad path surfaces as a command
// error rather than printing a partial/empty playbook.
func TestShowPlaybookMissingFileErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := Execute([]string{"show", "playbook", "--playbook", "no-such-file.json"}, &buf); err == nil {
		t.Fatal("Execute(show playbook --playbook no-such-file.json) = nil error, want error")
	}
}

// TestShowTraceRequiresPath asserts the trace sub-subcommand enforces its
// positional argument, so misuse is reported rather than silently ignored.
func TestShowTraceRequiresPath(t *testing.T) {
	var buf bytes.Buffer
	if err := Execute([]string{"show", "trace"}, &buf); err == nil {
		t.Fatal("Execute(show trace) with no path = nil error, want arg error")
	}
}

// TestSeedCommandWritesSubstrate drives `ledger-flow seed` against a temp root
// (via the hidden --root flag) and asserts it writes the full SPEC §4.4 artifact
// tree under worlds/<world>/<period>/ and prints a summary. It uses a temp dir so
// the test never pollutes the repo's worlds/.
func TestSeedCommandWritesSubstrate(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	args := []string{"seed", "--world", "dtc", "--period", "2026-05", "--root", root}
	if err := Execute(args, &buf); err != nil {
		t.Fatalf("Execute(seed) returned error: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "seeded world \"dtc\" period \"2026-05\"") {
		t.Errorf("seed output missing summary header:\n%s", out)
	}
	base := filepath.Join(root, "worlds", "dtc", "2026-05")
	for _, rel := range []string{
		filepath.Join("razorpay", "payments.json"),
		filepath.Join("razorpay", "refunds.json"),
		filepath.Join("razorpay", "settlements.json"),
		filepath.Join("razorpay", "disputes.json"),
		"bank-feed.json",
		filepath.Join("truth", "gl.json"),
	} {
		p := filepath.Join(base, rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected artifact %s: %v", p, err)
		}
	}
}

// TestSeedCommandRejectsBadPeriod asserts an invalid --period surfaces as a
// command error rather than writing a misnamed directory.
func TestSeedCommandRejectsBadPeriod(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	args := []string{"seed", "--world", "dtc", "--period", "2026-13", "--root", root}
	if err := Execute(args, &buf); err == nil {
		t.Fatal("Execute(seed) with bad period = nil error, want error")
	}
}

// TestCloseScoresSeededPeriod seeds a fresh period into a temp root, then closes
// it through the CLI and asserts the Phase-4 gate: every event classifies (0
// skips) and the score is the 100% deterministic baseline against truth. Seeding
// and closing share the same hidden --root flag so the test never touches the
// repo's worlds/.
func TestCloseScoresSeededPeriod(t *testing.T) {
	root := t.TempDir()
	var seedBuf bytes.Buffer
	if err := Execute([]string{"seed", "--world", "dtc", "--period", "2026-05", "--root", root}, &seedBuf); err != nil {
		t.Fatalf("seed returned error: %v", err)
	}

	var buf bytes.Buffer
	if err := Execute([]string{"run", "--world", "dtc", "--period", "2026-05", "--root", root}, &buf); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"skipped:    0 events",
		"trial balance matches truth: yes",
		"entries correct: 45/45",
		"score = 100%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("run output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "scoring errors") {
		t.Errorf("run reported scoring errors on a clean period\n---\n%s", out)
	}
}

// TestCloseAgainstCommittedFixtures closes the committed worlds/dtc/2026-05
// (pointing --root at the repo root) and asserts the 100% baseline, guarding that
// the committed fixtures + truth GL and the live pipeline agree.
func TestCloseAgainstCommittedFixtures(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	var buf bytes.Buffer
	if err := Execute([]string{"run", "--world", "dtc", "--period", "2026-05", "--root", repoRoot}, &buf); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "score = 100%") {
		t.Errorf("run against committed fixtures did not score 100%%\n---\n%s", out)
	}
}

// TestRootIsNamedCloseAgent guards the root command name (used in usage output
// and the binary name) against accidental renames.
func TestRootIsNamedCloseAgent(t *testing.T) {
	root := NewRootCmd(&bytes.Buffer{})
	if root.Use != "ledger-flow" {
		t.Errorf("root.Use = %q, want %q", root.Use, "ledger-flow")
	}
}
