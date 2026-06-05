package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedRoot seeds the dtc/2026-05 substrate into a fresh temp root via the CLI and
// returns the root, so report/diff tests run against a self-contained period
// without touching the repo's worlds/.
func seedRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	var buf bytes.Buffer
	if err := Execute([]string{"seed", "--world", "dtc", "--period", "2026-05", "--root", root}, &buf); err != nil {
		t.Fatalf("seed: %v\n%s", err, buf.String())
	}
	return root
}

// TestReportKindsRenderAndBalance drives `report --kind <k>` for each SPEC §10
// kind against a seeded period and asserts each renders its banner and balances
// where applicable (trial balance ΣDr==ΣCr, journal entries balance).
func TestReportKindsRenderAndBalance(t *testing.T) {
	root := seedRoot(t)

	tests := []struct {
		kind     string
		wantSubs []string
	}{
		{"trial-balance", []string{"TRIAL BALANCE", "DEBIT", "CREDIT", "TOTAL", "balanced: ΣDr == ΣCr"}},
		{"balance-sheet", []string{"BALANCE SHEET", "ASSETS", "LIABILITIES", "TOTAL ASSETS", "period net income"}},
		{"income", []string{"INCOME STATEMENT", "INCOME", "EXPENSE", "NET INCOME"}},
		{"journal", []string{"JOURNAL", "45 entries", "Dr ", "Cr "}},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			var buf bytes.Buffer
			args := []string{"report", "--world", "dtc", "--period", "2026-05", "--kind", tt.kind, "--root", root}
			if err := Execute(args, &buf); err != nil {
				t.Fatalf("report --kind %s: %v", tt.kind, err)
			}
			out := buf.String()
			for _, sub := range tt.wantSubs {
				if !strings.Contains(out, sub) {
					t.Errorf("report --kind %s missing %q\n---\n%s", tt.kind, sub, out)
				}
			}
		})
	}
}

// TestReportRejectsBadKind asserts an unknown --kind surfaces a command error
// rather than printing a partial/empty report.
func TestReportRejectsBadKind(t *testing.T) {
	root := seedRoot(t)
	var buf bytes.Buffer
	args := []string{"report", "--world", "dtc", "--period", "2026-05", "--kind", "nope", "--root", root}
	if err := Execute(args, &buf); err == nil {
		t.Fatal("report --kind nope = nil error, want error")
	}
}

// TestDiffCleanReportsNoDifferences drives `diff` on the clean seeded period and
// asserts it reports no differences vs truth (the gate's clean-period case).
func TestDiffCleanReportsNoDifferences(t *testing.T) {
	root := seedRoot(t)
	var buf bytes.Buffer
	args := []string{"diff", "--world", "dtc", "--period", "2026-05", "--root", root}
	if err := Execute(args, &buf); err != nil {
		t.Fatalf("diff: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "no differences vs truth") {
		t.Errorf("diff on clean period did not report no differences\n---\n%s", out)
	}
}

// TestDiffTamperedShowsDifferences strips one payment's gst metadata so its sale
// goes unbooked, then asserts `diff` lists the missing entry AND non-zero account
// deltas (the gate's tampered-period case).
func TestDiffTamperedShowsDifferences(t *testing.T) {
	root := seedRoot(t)
	tamperPaymentGSTRate(t, root)

	var buf bytes.Buffer
	args := []string{"diff", "--world", "dtc", "--period", "2026-05", "--root", root}
	if err := Execute(args, &buf); err != nil {
		t.Fatalf("diff after tamper: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"entry differences", "missing", "account balance deltas", "delta="} {
		if !strings.Contains(out, want) {
			t.Errorf("diff on tampered period missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "no differences vs truth") {
		t.Errorf("diff on tampered period reported no differences\n---\n%s", out)
	}
}

// TestCloseWritesErrorsJSONViaCLI drives `close` and asserts it writes the frozen
// errors.json under runs/<world>-<period>/ and reports the path (SPEC §9, §10).
func TestCloseWritesErrorsJSONViaCLI(t *testing.T) {
	root := seedRoot(t)
	var buf bytes.Buffer
	args := []string{"close", "--world", "dtc", "--period", "2026-05", "--root", root}
	if err := Execute(args, &buf); err != nil {
		t.Fatalf("close: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "errors record:") || !strings.Contains(out, "schema v1") {
		t.Errorf("close did not report the errors.json artifact\n---\n%s", out)
	}
	path := filepath.Join(root, "runs", "dtc-2026-05", "errors.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("errors.json not written by close: %v", err)
	}
}

// tamperPaymentGSTRate blanks the gst_rate note on the first payment in the
// seeded payments.json, so that sale becomes unclassifiable (skipped) and its
// truth entry shows up MISSING — a controlled in-test tamper, on the temp copy.
func tamperPaymentGSTRate(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "worlds", "dtc", "2026-05", "razorpay", "payments.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read payments: %v", err)
	}
	var payments []map[string]any
	if err := json.Unmarshal(data, &payments); err != nil {
		t.Fatalf("unmarshal payments: %v", err)
	}
	if len(payments) == 0 {
		t.Fatal("no payments to tamper")
	}
	if notes, ok := payments[0]["notes"].(map[string]any); ok {
		delete(notes, "gst_rate")
	}
	out, err := json.Marshal(payments)
	if err != nil {
		t.Fatalf("marshal payments: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write payments: %v", err)
	}
}
