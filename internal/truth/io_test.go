package truth

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
)

// sampleGL is a small, balanced two-entry ground-truth GL used across the IO
// tests. It mirrors the shape the seeder emits (dtc_sale + a settlement-style
// clear) so the round-trip exercises multi-line entries and an omitempty TxID.
func sampleGL() GL {
	return GL{
		Version: SchemaVersion,
		World:   "dtc",
		Period:  "2026-05",
		Entries: []Entry{
			{
				ID: "gl_0001", EntryType: "dtc_sale", EventID: "pay_X", Ts: 100,
				Lines: []Line{
					{Side: Debit, Account: "assets/razorpay-settlement-receivable", Amount: money.FromPaise(11800)},
					{Side: Credit, Account: "income/product-sales", Amount: money.FromPaise(10000)},
					{Side: Credit, Account: "liabilities/gst-output-payable", Amount: money.FromPaise(1800)},
				},
			},
			{
				ID: "gl_0002", EntryType: "razorpay_settlement", EventID: "setl_Y", TxID: "UTR123", Ts: 200,
				Lines: []Line{
					{Side: Debit, Account: "assets/bank", Amount: money.FromPaise(11564)},
					{Side: Debit, Account: "expense/processor-fees", Amount: money.FromPaise(200)},
					{Side: Debit, Account: "expense/gst-input", Amount: money.FromPaise(36)},
					{Side: Credit, Account: "assets/razorpay-settlement-receivable", Amount: money.FromPaise(11800)},
				},
			},
		},
	}
}

// TestWriteReadRoundTrip asserts WriteTruth then ReadTruth recovers an equal GL.
func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gl.json")

	want := sampleGL()
	if err := WriteTruth(path, want); err != nil {
		t.Fatalf("WriteTruth: %v", err)
	}
	got, err := ReadTruth(path)
	if err != nil {
		t.Fatalf("ReadTruth: %v", err)
	}

	wd, wc := want.SumBySide()
	gd, gc := got.SumBySide()
	if wd != gd || wc != gc {
		t.Errorf("round-trip totals differ: want Dr=%s Cr=%s, got Dr=%s Cr=%s", wd, wc, gd, gc)
	}
	if got.Version != want.Version || got.World != want.World || got.Period != want.Period {
		t.Errorf("round-trip header differs: got %+v", got)
	}
	if len(got.Entries) != len(want.Entries) {
		t.Fatalf("round-trip entry count: got %d want %d", len(got.Entries), len(want.Entries))
	}
	for i := range want.Entries {
		w, g := want.Entries[i], got.Entries[i]
		if w.ID != g.ID || w.EntryType != g.EntryType || w.EventID != g.EventID || w.TxID != g.TxID || w.Ts != g.Ts {
			t.Errorf("entry %d header differs: want %+v got %+v", i, w, g)
		}
		if len(w.Lines) != len(g.Lines) {
			t.Fatalf("entry %d line count: got %d want %d", i, len(g.Lines), len(w.Lines))
		}
		for j := range w.Lines {
			if w.Lines[j] != g.Lines[j] {
				t.Errorf("entry %d line %d differs: want %+v got %+v", i, j, w.Lines[j], g.Lines[j])
			}
		}
	}
}

// TestWriteTruthReproducible asserts two writes of the same GL to two paths
// produce byte-identical files (no map nondeterminism, fixed format).
func TestWriteTruthReproducible(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	gl := sampleGL()
	if err := WriteTruth(a, gl); err != nil {
		t.Fatalf("WriteTruth a: %v", err)
	}
	if err := WriteTruth(b, gl); err != nil {
		t.Fatalf("WriteTruth b: %v", err)
	}
	ba, err := os.ReadFile(a)
	if err != nil {
		t.Fatalf("read a: %v", err)
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		t.Fatalf("read b: %v", err)
	}
	if !bytes.Equal(ba, bb) {
		t.Errorf("WriteTruth not byte-identical across two writes")
	}
}

// TestMarshalGLFormat pins the canonical on-disk byte format: integer-paise
// amounts (no float), two-space indent, a trailing newline, and stable key
// order with omitempty TxID dropped from the first entry but present on the
// second.
func TestMarshalGLFormat(t *testing.T) {
	b, err := MarshalGL(sampleGL())
	if err != nil {
		t.Fatalf("MarshalGL: %v", err)
	}
	s := string(b)
	if !strings.HasSuffix(s, "}\n") {
		t.Errorf("MarshalGL output must end with a trailing newline; got %q", s[len(s)-3:])
	}
	if !strings.Contains(s, "\n  \"version\": 1,") {
		t.Errorf("expected two-space-indented version key; got:\n%s", s)
	}
	for _, want := range []string{`"amount": 11800`, `"amount": 10000`, `"amount": 1800`} {
		if !strings.Contains(s, want) {
			t.Errorf("expected integer-paise amount %q in:\n%s", want, s)
		}
	}
	// omitempty: the first entry carries no tx_id, the second does.
	if strings.Count(s, `"tx_id"`) != 1 {
		t.Errorf("expected exactly one tx_id (omitempty on the entry without one):\n%s", s)
	}
	// No float-shaped amount (a decimal point inside a number) should appear.
	if strings.Contains(s, `"amount": 1.18`) || strings.Contains(s, `.0`) {
		t.Errorf("found a float-shaped amount in canonical output:\n%s", s)
	}
}

// TestReadTruthRejectsUnbalanced asserts ReadTruth rejects a corrupt oracle
// (an unbalanced entry) rather than returning it.
func TestReadTruthRejectsUnbalanced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gl.json")
	bad := []byte(`{"version":1,"world":"dtc","period":"2026-05","entries":[` +
		`{"id":"gl_0001","entry_type":"dtc_sale","event_id":"pay_X","ts":1,` +
		`"lines":[{"side":"Dr","account":"assets/bank","amount":1000},` +
		`{"side":"Cr","account":"income/product-sales","amount":999}]}]}`)
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}
	if _, err := ReadTruth(path); err == nil {
		t.Error("ReadTruth accepted an unbalanced GL; want an error")
	}
}

// TestReadTruthRejectsWrongVersion asserts a schema-version mismatch is an error
// (the frozen-seam guard, SPEC §13).
func TestReadTruthRejectsWrongVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gl.json")
	bad := []byte(`{"version":999,"world":"dtc","period":"2026-05","entries":[]}`)
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}
	if _, err := ReadTruth(path); err == nil {
		t.Error("ReadTruth accepted a wrong schema version; want an error")
	}
}

// TestReadTruthRejectsUnknownField asserts DisallowUnknownFields catches a
// drifted/hand-edited file with an unexpected key.
func TestReadTruthRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gl.json")
	bad := []byte(`{"version":1,"world":"dtc","period":"2026-05","entries":[],"bogus":true}`)
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}
	if _, err := ReadTruth(path); err == nil {
		t.Error("ReadTruth accepted an unknown field; want an error")
	}
}

// TestReadTruthMissingFile asserts a clear error for a nonexistent path.
func TestReadTruthMissingFile(t *testing.T) {
	if _, err := ReadTruth(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("ReadTruth on a missing file returned nil error")
	}
}
