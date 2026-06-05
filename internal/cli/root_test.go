package cli

import (
	"bytes"
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
			name:      "seed",
			args:      []string{"seed", "--world", "dtc", "--period", "2026-05"},
			wantSubst: "close-agent seed --world dtc --period 2026-05: not implemented yet",
		},
		{
			name:      "close",
			args:      []string{"close", "--world", "dtc", "--period", "2026-05"},
			wantSubst: "close-agent close --world dtc --period 2026-05: not implemented yet",
		},
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

// TestRootIsNamedCloseAgent guards the root command name (used in usage output
// and the binary name) against accidental renames.
func TestRootIsNamedCloseAgent(t *testing.T) {
	root := NewRootCmd(&bytes.Buffer{})
	if root.Use != "close-agent" {
		t.Errorf("root.Use = %q, want %q", root.Use, "close-agent")
	}
}
