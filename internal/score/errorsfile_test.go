package score

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/truth"
)

// TestBuildRunRecordClean: a produced ledger that exactly equals truth yields a
// 100% record with trial_balance_matches true, zero error records, and EVERY
// per-account delta zero (got == want for all accounts) — the clean-period
// contract of the gate (SPEC §9).
func TestBuildRunRecordClean(t *testing.T) {
	gl := truth.GL{Version: truth.SchemaVersion, World: "dtc", Period: "2026-05", Entries: []truth.Entry{
		saleTruth("pay_1", 11800, 10000, 1800),
		saleTruth("pay_2", 11200, 10000, 1200),
	}}
	produced := []Produced{
		saleProduced("pay_2", 11200, 10000, 1200),
		saleProduced("pay_1", 11800, 10000, 1800),
	}
	res := Score(produced, gl)
	rec := BuildRunRecord("dtc", "2026-05", produced, gl, res)

	if rec.SchemaVersion != ErrorsSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", rec.SchemaVersion, ErrorsSchemaVersion)
	}
	if rec.World != "dtc" || rec.Period != "2026-05" {
		t.Errorf("world/period = %q/%q, want dtc/2026-05", rec.World, rec.Period)
	}
	if rec.ScorePct != 100 {
		t.Errorf("ScorePct = %d, want 100", rec.ScorePct)
	}
	if !rec.TrialBalanceMatches {
		t.Errorf("TrialBalanceMatches = false, want true")
	}
	if len(rec.Errors) != 0 {
		t.Errorf("Errors = %v, want none", rec.Errors)
	}
	wantTotals := Totals{TruthEntries: 2, Correct: 2}
	if rec.Totals != wantTotals {
		t.Errorf("Totals = %+v, want %+v", rec.Totals, wantTotals)
	}
	// Three accounts (receivable, product-sales, gst-output-payable), each with a
	// zero delta and matching got/want.
	if len(rec.PerAccountDeltas) != 3 {
		t.Fatalf("PerAccountDeltas = %v, want 3", rec.PerAccountDeltas)
	}
	for _, d := range rec.PerAccountDeltas {
		if !d.Delta.IsZero() {
			t.Errorf("account %q delta = %s, want 0", d.Account, d.Delta)
		}
		if d.GotBalance != d.WantBalance {
			t.Errorf("account %q got=%s want=%s, want equal", d.Account, d.GotBalance, d.WantBalance)
		}
	}
}

// TestBuildRunRecordDeltasSortedNormalSide checks the per-account deltas are on
// the account's NORMAL side and sorted by account path: income (normal Credit)
// reads positive for credit balances, the receivable (asset, normal Debit) reads
// positive for debit balances.
func TestBuildRunRecordDeltasSortedNormalSide(t *testing.T) {
	gl := truth.GL{Version: truth.SchemaVersion, Entries: []truth.Entry{saleTruth("pay_1", 11800, 10000, 1800)}}
	produced := []Produced{saleProduced("pay_1", 11800, 10000, 1800)}
	rec := BuildRunRecord("dtc", "2026-05", produced, gl, Score(produced, gl))

	wantOrder := []string{
		"assets/razorpay-settlement-receivable",
		"income/product-sales",
		"liabilities/gst-output-payable",
	}
	if len(rec.PerAccountDeltas) != len(wantOrder) {
		t.Fatalf("deltas = %v, want %d", rec.PerAccountDeltas, len(wantOrder))
	}
	for i, want := range wantOrder {
		if rec.PerAccountDeltas[i].Account != want {
			t.Errorf("delta[%d].Account = %q, want %q", i, rec.PerAccountDeltas[i].Account, want)
		}
	}
	// Normal-side want balances: receivable +11800 (Dr), product-sales +10000 (Cr),
	// gst-output-payable +1800 (Cr).
	wantBal := map[string]int64{
		"assets/razorpay-settlement-receivable": 11800,
		"income/product-sales":                  10000,
		"liabilities/gst-output-payable":        1800,
	}
	for _, d := range rec.PerAccountDeltas {
		if got := d.WantBalance.Paise(); got != wantBal[d.Account] {
			t.Errorf("account %q want_balance = %d, want %d (normal side)", d.Account, got, wantBal[d.Account])
		}
	}
}

// TestBuildRunRecordTampered: a tampered produced ledger (one wrong amount, one
// missing, one extra) yields the exact totals AND non-zero deltas on the affected
// accounts (SPEC §9 secondary metric).
func TestBuildRunRecordTampered(t *testing.T) {
	gl := truth.GL{Version: truth.SchemaVersion, Entries: []truth.Entry{
		saleTruth("pay_1", 11800, 10000, 1800), // will be WRONG (amount off)
		saleTruth("pay_2", 11200, 10000, 1200), // will be MISSING (not produced)
	}}
	produced := []Produced{
		// pay_1 booked with product-sales off by 100 paise (gst absorbs it so the
		// entry still balances): a WRONG entry, and a non-zero delta on product-sales.
		{
			EntryType: "dtc_sale", EventID: "pay_1",
			Lines: []Line{
				pline("Dr", "assets/razorpay-settlement-receivable", 11800),
				pline("Cr", "income/product-sales", 9900),
				pline("Cr", "liabilities/gst-output-payable", 1900),
			},
		},
		// pay_X is EXTRA (truth has no such event).
		saleProduced("pay_X", 5900, 5000, 900),
	}
	res := Score(produced, gl)
	rec := BuildRunRecord("dtc", "2026-05", produced, gl, res)

	wantTotals := Totals{TruthEntries: 2, Correct: 0, Wrong: 1, Missing: 1, Extra: 1}
	if rec.Totals != wantTotals {
		t.Errorf("Totals = %+v, want %+v", rec.Totals, wantTotals)
	}
	if rec.ScorePct != 0 {
		t.Errorf("ScorePct = %d, want 0", rec.ScorePct)
	}

	// Error records: wrong=pay_1, missing=pay_2, extra=pay_X (sorted by event id).
	gotClass := map[string]ErrorClass{}
	for _, e := range rec.Errors {
		gotClass[e.EventID] = e.Class
	}
	for id, want := range map[string]ErrorClass{"pay_1": ErrWrong, "pay_2": ErrMissing, "pay_X": ErrExtra} {
		if gotClass[id] != want {
			t.Errorf("event %q class = %q, want %q", id, gotClass[id], want)
		}
	}

	// Non-zero deltas must exist on the accounts the tamper touched. product-sales
	// (normal Credit): got = 9900 + 5000(extra) = 14900; want = 10000 + 10000 = 20000;
	// delta = -5100.
	delta := map[string]int64{}
	for _, d := range rec.PerAccountDeltas {
		delta[d.Account] = d.Delta.Paise()
	}
	if delta["income/product-sales"] == 0 {
		t.Errorf("product-sales delta = 0, want non-zero on a tampered ledger")
	}
	if delta["income/product-sales"] != -5100 {
		t.Errorf("product-sales delta = %d, want -5100", delta["income/product-sales"])
	}
}

// TestErrorsRoundTripStableBytes: a record marshals, unmarshals, and re-marshals
// to BYTE-IDENTICAL output, and the decoded record equals the original — the
// frozen-seam stability the learning layer relies on (SPEC §9, §13).
func TestErrorsRoundTripStableBytes(t *testing.T) {
	gl := truth.GL{Version: truth.SchemaVersion, Entries: []truth.Entry{
		saleTruth("pay_b", 11800, 10000, 1800),
		saleTruth("pay_a", 11200, 10000, 1200),
	}}
	produced := []Produced{saleProduced("pay_a", 11200, 10000, 1200)} // pay_b missing
	rec := BuildRunRecord("dtc", "2026-05", produced, gl, Score(produced, gl))

	data1, err := MarshalErrors(rec)
	if err != nil {
		t.Fatalf("MarshalErrors: %v", err)
	}
	back, err := UnmarshalErrors(data1)
	if err != nil {
		t.Fatalf("UnmarshalErrors: %v", err)
	}
	data2, err := MarshalErrors(back)
	if err != nil {
		t.Fatalf("re-MarshalErrors: %v", err)
	}
	if !bytes.Equal(data1, data2) {
		t.Errorf("round-trip bytes differ:\n--- first ---\n%s\n--- second ---\n%s", data1, data2)
	}

	// Marshalling twice from the same record must also be byte-identical (no map
	// iteration leaking into output).
	again, _ := MarshalErrors(rec)
	if !bytes.Equal(data1, again) {
		t.Errorf("re-marshal of same record is not byte-identical")
	}

	// schema_version must be the FIRST key in the JSON (a consumer reads it before
	// the body).
	if !bytes.HasPrefix(bytes.TrimLeft(data1, " \n\t"), []byte(`{`)) ||
		!bytes.Contains(data1[:40], []byte(`"schema_version"`)) {
		t.Errorf("schema_version is not the leading key:\n%s", data1[:60])
	}
}

// TestUnmarshalErrorsRejectsBadVersion: a record stamped with a different schema
// version is rejected (the frozen seam is honest, not silently upgraded).
func TestUnmarshalErrorsRejectsBadVersion(t *testing.T) {
	bad := []byte(`{"schema_version":999,"world":"dtc","period":"2026-05","score_pct":100,` +
		`"trial_balance_matches":true,"totals":{"truth_entries":0,"correct":0,"wrong":0,` +
		`"missing":0,"extra":0},"per_account_deltas":[],"errors":[]}`)
	if _, err := UnmarshalErrors(bad); err == nil {
		t.Fatal("UnmarshalErrors accepted a record with schema_version 999, want error")
	}
}

// TestUnmarshalErrorsRejectsUnknownField: an extra key (drifted/hand-edited
// artifact) is surfaced, keeping the frozen schema honest.
func TestUnmarshalErrorsRejectsUnknownField(t *testing.T) {
	bad := []byte(`{"schema_version":1,"world":"dtc","period":"2026-05","score_pct":100,` +
		`"trial_balance_matches":true,"surprise":42,"totals":{"truth_entries":0,"correct":0,` +
		`"wrong":0,"missing":0,"extra":0},"per_account_deltas":[],"errors":[]}`)
	if _, err := UnmarshalErrors(bad); err == nil {
		t.Fatal("UnmarshalErrors accepted an unknown field, want error")
	}
}

// TestWriteErrorsArtifact: WriteErrors creates runs/<world>-<period>/errors.json
// under root with the exact MarshalErrors bytes, and re-writing is byte-identical.
func TestWriteErrorsArtifact(t *testing.T) {
	root := t.TempDir()
	gl := truth.GL{Version: truth.SchemaVersion, Entries: []truth.Entry{saleTruth("pay_1", 11800, 10000, 1800)}}
	produced := []Produced{saleProduced("pay_1", 11800, 10000, 1800)}
	rec := BuildRunRecord("dtc", "2026-05", produced, gl, Score(produced, gl))

	path, err := WriteErrors(root, "dtc", "2026-05", rec)
	if err != nil {
		t.Fatalf("WriteErrors: %v", err)
	}
	wantPath := filepath.Join(root, "runs", "dtc-2026-05", "errors.json")
	if path != wantPath {
		t.Errorf("WriteErrors path = %q, want %q", path, wantPath)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written errors.json: %v", err)
	}
	wantBytes, _ := MarshalErrors(rec)
	if !bytes.Equal(onDisk, wantBytes) {
		t.Errorf("on-disk bytes differ from MarshalErrors output")
	}

	// Re-write must be byte-identical (deterministic, stable-key writer).
	if _, err := WriteErrors(root, "dtc", "2026-05", rec); err != nil {
		t.Fatalf("re-WriteErrors: %v", err)
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(onDisk, again) {
		t.Errorf("re-written errors.json is not byte-identical")
	}
}

// TestEmptyPeriodRecordHasEmptyArrays: a record built from an empty period serializes
// per_account_deltas and errors as JSON arrays ([]), never null (the learning layer
// expects arrays).
func TestEmptyPeriodRecordHasEmptyArrays(t *testing.T) {
	rec := BuildRunRecord("dtc", "2026-05", nil, truth.GL{Version: truth.SchemaVersion}, Score(nil, truth.GL{}))
	data, err := MarshalErrors(rec)
	if err != nil {
		t.Fatalf("MarshalErrors: %v", err)
	}
	if !bytes.Contains(data, []byte(`"errors": []`)) {
		t.Errorf("errors not serialized as []:\n%s", data)
	}
	if !bytes.Contains(data, []byte(`"per_account_deltas": []`)) {
		t.Errorf("per_account_deltas not serialized as []:\n%s", data)
	}
	// money fields must marshal as integer paise, not floats.
	_ = money.FromPaise(0)
}
