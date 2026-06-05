package seed

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/razorpay/close-agent/internal/truth"
)

// TestSeedWritesArtifactTree asserts Seed writes the full SPEC §4.4 artifact tree
// and that the files are well-formed JSON parsing back into the seed-model and
// truth schemas (fixtures validate against the schemas).
func TestSeedWritesArtifactTree(t *testing.T) {
	root := t.TempDir()
	res, err := Seed(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	l := res.Layout

	// payments.json -> []Payment
	var payments []Payment
	readJSON(t, l.PaymentsPath(), &payments)
	if len(payments) != res.NumPayments || len(payments) == 0 {
		t.Errorf("payments count %d, result said %d", len(payments), res.NumPayments)
	}
	for _, p := range payments {
		if p.Entity != "payment" || p.Currency != "INR" || p.Amount.Sign() <= 0 {
			t.Errorf("malformed payment: %+v", p)
		}
		if p.Notes.SKU == "" || p.Notes.GSTRate == "" {
			t.Errorf("payment %s missing notes sku/gst_rate", p.ID)
		}
	}

	var refunds []Refund
	readJSON(t, l.RefundsPath(), &refunds)
	for _, r := range refunds {
		if r.Entity != "refund" || r.PaymentID == "" {
			t.Errorf("malformed refund: %+v", r)
		}
	}

	var settlements []Settlement
	readJSON(t, l.SettlementsPath(), &settlements)
	if len(settlements) != numBatches {
		t.Errorf("settlements = %d, want %d batches", len(settlements), numBatches)
	}
	for _, s := range settlements {
		if s.Entity != "settlement" || s.UTR == "" || len(s.PaymentIDs) == 0 {
			t.Errorf("malformed settlement: %+v", s)
		}
	}

	var disputes []Dispute
	readJSON(t, l.DisputesPath(), &disputes)

	var feed BankFeed
	readJSON(t, l.BankFeedPath(), &feed)
	if feed.Period != "2026-05" {
		t.Errorf("bank feed period = %q, want 2026-05", feed.Period)
	}
	if len(feed.Credits) != len(settlements) {
		t.Errorf("bank credits %d != settlements %d", len(feed.Credits), len(settlements))
	}

	var gl truth.GL
	readJSON(t, l.TruthGLPath(), &gl)
	if !gl.IsBalanced() {
		t.Error("written truth GL does not balance")
	}

	// The on-disk truth GL must load through the canonical reader — the single
	// allowed reader of truth/gl.json (SPEC §4.4, §12) — which also re-validates
	// the schema version and that every entry balances.
	read, err := truth.ReadTruth(l.TruthGLPath())
	if err != nil {
		t.Fatalf("truth.ReadTruth on written GL: %v", err)
	}
	if read.Version != truth.SchemaVersion || read.World != "dtc" || read.Period != "2026-05" {
		t.Errorf("ReadTruth header mismatch: %+v", read)
	}
	if len(read.Entries) != res.NumGLEntries {
		t.Errorf("ReadTruth entries = %d, result said %d", len(read.Entries), res.NumGLEntries)
	}
}

// TestSeedTruthBytesMatchCanonicalWriter asserts the on-disk truth/gl.json the
// seeder wrote is byte-identical to what internal/truth's canonical writer
// produces for the same GL. This pins the invariant that routing the truth write
// through truth.WriteTruth did not change the on-disk bytes (the reproducibility
// gate and downstream phases depend on stable bytes).
func TestSeedTruthBytesMatchCanonicalWriter(t *testing.T) {
	root := t.TempDir()
	res, err := Seed(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	onDisk := readFile(t, res.Layout.TruthGLPath())

	// Re-generate the same GL and marshal it through the canonical encoder.
	_, _, gl, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	canonical, err := truth.MarshalGL(gl)
	if err != nil {
		t.Fatalf("MarshalGL: %v", err)
	}
	if !bytes.Equal(onDisk, canonical) {
		t.Errorf("on-disk truth GL (%d bytes) != canonical MarshalGL (%d bytes)", len(onDisk), len(canonical))
	}
}

// TestSeedReproducibleFiles is the byte-identical gate: running Seed twice into
// two temp roots produces identical bytes for every artifact file (SPEC §2, §12;
// gate: "running seed twice yields byte-identical files").
func TestSeedReproducibleFiles(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	resA, err := Seed(rootA, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("Seed A: %v", err)
	}
	resB, err := Seed(rootB, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("Seed B: %v", err)
	}

	files := []struct {
		name string
		a, b string
	}{
		{"payments", resA.Layout.PaymentsPath(), resB.Layout.PaymentsPath()},
		{"refunds", resA.Layout.RefundsPath(), resB.Layout.RefundsPath()},
		{"settlements", resA.Layout.SettlementsPath(), resB.Layout.SettlementsPath()},
		{"disputes", resA.Layout.DisputesPath(), resB.Layout.DisputesPath()},
		{"bank-feed", resA.Layout.BankFeedPath(), resB.Layout.BankFeedPath()},
		{"truth-gl", resA.Layout.TruthGLPath(), resB.Layout.TruthGLPath()},
	}
	for _, f := range files {
		ba := readFile(t, f.a)
		bb := readFile(t, f.b)
		if !bytes.Equal(ba, bb) {
			t.Errorf("%s not byte-identical across two seed runs (%d vs %d bytes)", f.name, len(ba), len(bb))
		}
	}
}

// TestSeedOverwriteStable asserts re-seeding the SAME root overwrites with
// identical bytes (idempotent re-seed).
func TestSeedOverwriteStable(t *testing.T) {
	root := t.TempDir()
	res, err := Seed(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("Seed #1: %v", err)
	}
	before := readFile(t, res.Layout.TruthGLPath())
	if _, err := Seed(root, "dtc", "2026-05"); err != nil {
		t.Fatalf("Seed #2: %v", err)
	}
	after := readFile(t, res.Layout.TruthGLPath())
	if !bytes.Equal(before, after) {
		t.Error("re-seeding the same root changed truth/gl.json bytes")
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := json.Unmarshal(readFile(t, path), v); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}
