package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecuteSubcommands drives each command surface from SPEC §10 and asserts
// it runs without error and prints its not-implemented stub. This is the
// behavioral contract of the Phase 0 gate: nothing crashes, nothing errors.
func TestExecuteSubcommands(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantSubst string
	}{
		{
			name:      "report",
			args:      []string{"report", "--world", "dtc", "--period", "2026-05", "--kind", "trial-balance"},
			wantSubst: "close-agent report --world dtc --period 2026-05 --kind trial-balance: not implemented yet",
		},
		{
			name:      "show trace",
			args:      []string{"show", "trace", "runs/dtc-2026-05"},
			wantSubst: "close-agent show trace runs/dtc-2026-05: not implemented yet",
		},
		{
			name:      "diff",
			args:      []string{"diff", "--world", "dtc", "--period", "2026-05"},
			wantSubst: "close-agent diff --world dtc --period 2026-05: not implemented yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Execute(tt.args, &buf); err != nil {
				t.Fatalf("Execute(%v) returned error: %v", tt.args, err)
			}
			if got := buf.String(); !strings.Contains(got, tt.wantSubst) {
				t.Errorf("Execute(%v) output = %q, want substring %q", tt.args, got, tt.wantSubst)
			}
		})
	}
}

// TestHelpListsSubcommands asserts `close-agent --help` lists every top-level
// subcommand named in the gate.
func TestHelpListsSubcommands(t *testing.T) {
	var buf bytes.Buffer
	if err := Execute([]string{"--help"}, &buf); err != nil {
		t.Fatalf("Execute(--help) returned error: %v", err)
	}
	out := buf.String()
	for _, sub := range []string{"seed", "close", "report", "show", "diff"} {
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

// TestSeedCommandWritesSubstrate drives `close-agent seed` against a temp root
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
	if err := Execute([]string{"close", "--world", "dtc", "--period", "2026-05", "--root", root}, &buf); err != nil {
		t.Fatalf("close returned error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"skipped:    0 events",
		"trial balance matches truth: yes",
		"entries correct: 45/45",
		"score = 100%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("close output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "scoring errors") {
		t.Errorf("close reported scoring errors on a clean period\n---\n%s", out)
	}
}

// TestCloseAgainstCommittedFixtures closes the committed worlds/dtc/2026-05
// (pointing --root at the repo root) and asserts the 100% baseline, guarding that
// the committed fixtures + truth GL and the live pipeline agree.
func TestCloseAgainstCommittedFixtures(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	var buf bytes.Buffer
	if err := Execute([]string{"close", "--world", "dtc", "--period", "2026-05", "--root", repoRoot}, &buf); err != nil {
		t.Fatalf("close returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "score = 100%") {
		t.Errorf("close against committed fixtures did not score 100%%\n---\n%s", out)
	}
}

// TestRootIsNamedCloseAgent guards the root command name (used in usage output
// and the binary name) against accidental renames.
func TestRootIsNamedCloseAgent(t *testing.T) {
	root := NewRootCmd(&bytes.Buffer{})
	if root.Use != "close-agent" {
		t.Errorf("root.Use = %q, want %q", root.Use, "close-agent")
	}
}
