package seed

import (
	"os"
	"path/filepath"
	"testing"
)

// repoRoot is the module root relative to this package's test working directory
// (internal/seed). The committed clean fixtures live under repoRoot/worlds/.
const repoRoot = "../.."

// committedPeriod is the single clean period checked into the repo (SPEC §4.4).
const (
	committedWorld  = "dtc"
	committedPeriod = "2026-05"
)

// TestCommittedCleanFixturesByteIdentical is the Phase-5 regression guard for the
// seeded-break work: a FRESH clean seed (no injection) must reproduce the
// committed worlds/dtc/2026-05 fixtures byte-for-byte. The break-injection
// plumbing (Options/GenerateWith/applyInject) runs the clean generator untouched
// and only perturbs fixtures when --inject is given, so adding it must not shift
// the clean RNG stream or the on-disk bytes. If this fails, the clean substrate
// drifted and the committed fixtures (and every golden that pins them) are stale.
//
// It reseeds into a temp root and diffs the produced bytes against the committed
// files via the seeder's own stable writer (MarshalStable for the agent-input
// files; the truth GL is compared as the raw committed bytes since it is written
// through internal/truth). truth/gl.json is read here only to compare bytes —
// this is the seeder's own test, and the seeder is an allowed reader of truth/
// (SPEC §4.4, §12; the isolation guard permits internal/seed).
func TestCommittedCleanFixturesByteIdentical(t *testing.T) {
	// If the repo's worlds/ tree is not present (e.g. a stripped checkout), skip
	// rather than fail — the byte-identity gate only applies where the committed
	// fixtures exist. The determinism gate (TestGenerateDeterministic) covers
	// reproducibility independently.
	committed := Layout{Root: repoRoot, World: committedWorld, Period: committedPeriod}
	if _, err := os.Stat(committed.PaymentsPath()); err != nil {
		t.Skipf("committed fixtures not present (%v); skipping byte-identity guard", err)
	}

	fx, feed, gl, err := Generate(committedWorld, committedPeriod)
	if err != nil {
		t.Fatalf("Generate clean: %v", err)
	}

	// Agent-input files: compare the stable-marshalled fresh bytes against the
	// committed file bytes.
	cases := []struct {
		name string
		path string
		v    any
	}{
		{"payments", committed.PaymentsPath(), fx.Payments},
		{"refunds", committed.RefundsPath(), fx.Refunds},
		{"settlements", committed.SettlementsPath(), fx.Settlements},
		{"disputes", committed.DisputesPath(), fx.Disputes},
		{"bank-feed", committed.BankFeedPath(), feed},
	}
	for _, c := range cases {
		fresh, err := MarshalStable(c.v)
		if err != nil {
			t.Fatalf("marshal fresh %s: %v", c.name, err)
		}
		onDisk, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("read committed %s: %v", c.name, err)
		}
		if string(fresh) != string(onDisk) {
			t.Errorf("committed %s (%s) is no longer byte-identical to a fresh clean seed",
				c.name, filepath.Base(c.path))
		}
	}

	// Truth GL: compare the stable-marshalled fresh GL against the committed file.
	// (The on-disk writer routes through internal/truth, which the seed write_test
	// already proves is byte-identical to MarshalStable.)
	freshGL, err := MarshalStable(gl)
	if err != nil {
		t.Fatalf("marshal fresh truth GL: %v", err)
	}
	onDiskGL, err := os.ReadFile(committed.TruthGLPath())
	if err != nil {
		t.Fatalf("read committed truth GL: %v", err)
	}
	if string(freshGL) != string(onDiskGL) {
		t.Errorf("committed truth/gl.json is no longer byte-identical to a fresh clean seed")
	}
}
