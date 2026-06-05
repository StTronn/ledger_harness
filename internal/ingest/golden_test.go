package ingest

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// update, when set via `go test -run TestGoldenJournal -update`, rewrites the
// golden journal from the committed fixtures. It is the standard Go golden-file
// idiom: regenerate deliberately, then commit and review the diff. CI never sets
// it, so the golden is a frozen oracle there.
var update = flag.Bool("update", false, "rewrite the golden journal file from fixtures")

// repoRoot returns the module root relative to this test (internal/ingest is two
// directories below the root). Tests run with the package dir as cwd, so the
// committed worlds/dtc/2026-05 fixtures live at ../../worlds/...
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// goldenPath is the committed expected journal for the dtc/2026-05 fixtures.
const goldenPath = "testdata/dtc-2026-05.journal.json"

// TestGoldenJournal is the Phase-3 GATE golden: it ingests the committed
// worlds/dtc/2026-05 fixtures, normalizes them to the §4.3 event journal, and
// asserts the canonical marshalling is byte-identical to the committed golden
// file. Any drift — a changed raw shape, a different field, a reordering, a
// money rounding — fails this test. The golden is regenerated only with
// -update.
func TestGoldenJournal(t *testing.T) {
	root := repoRoot(t)

	raw, events, err := IngestAndNormalize(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("IngestAndNormalize: %v", err)
	}

	got, err := MarshalJournal(events)
	if err != nil {
		t.Fatalf("MarshalJournal: %v", err)
	}

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden %s (%d bytes, %d events)", goldenPath, len(got), len(events))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `go test -run TestGoldenJournal -update` to create it)", goldenPath, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("normalized journal drifted from golden %s\n"+
			"got  %d bytes\nwant %d bytes\n%s",
			goldenPath, len(got), len(want), firstDiff(got, want))
	}

	// Cross-check: the bank feed is ingested into Raw but is NOT part of the
	// journal (SPEC §4.4, §7 — it is for the Phase 5 reconcile stage). Guard that
	// the journal length equals only the four Razorpay slices, never the feed.
	wantLen := len(raw.Payments) + len(raw.Refunds) + len(raw.Settlements) + len(raw.Disputes)
	if len(events) != wantLen {
		t.Errorf("journal has %d events, want %d (payments+refunds+settlements+disputes; bank feed must be excluded)", len(events), wantLen)
	}
}

// firstDiff returns a short, human-readable description of where two byte slices
// first differ, with a little surrounding context, so a golden failure points at
// the exact drift instead of dumping two large blobs.
func firstDiff(got, want []byte) string {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			lo := i - 40
			if lo < 0 {
				lo = 0
			}
			hiG := i + 40
			if hiG > len(got) {
				hiG = len(got)
			}
			hiW := i + 40
			if hiW > len(want) {
				hiW = len(want)
			}
			return "first diff at byte " + itoa(i) +
				"\n  got : ..." + string(got[lo:hiG]) + "...\n  want: ..." + string(want[lo:hiW]) + "..."
		}
	}
	return "one is a prefix of the other (length differs)"
}

// itoa is a tiny int→string helper to keep firstDiff dependency-free.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
