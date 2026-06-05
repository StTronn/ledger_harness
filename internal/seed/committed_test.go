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
	// committed file bytes. orders.json is included so a fresh clean seed must
	// reproduce the committed orders byte-for-byte too (SPEC §2).
	cases := []struct {
		name string
		path string
		v    any
	}{
		{"payments", committed.PaymentsPath(), fx.Payments},
		{"refunds", committed.RefundsPath(), fx.Refunds},
		{"settlements", committed.SettlementsPath(), fx.Settlements},
		{"disputes", committed.DisputesPath(), fx.Disputes},
		{"orders", committed.OrdersPath(), fx.Orders},
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

// TestCommittedHardPeriodByteIdentical is the byte-identity guard for the
// committed missing-metadata HARD period (worlds/dtc/2026-04, seeded with
// --ambiguity; SPEC §11 Phase 7). A fresh seed of (dtc, 2026-04) WITH the
// ambiguity option must reproduce the committed fixtures byte-for-byte — every
// agent-input file (including the gst_rate-stripped payments.json and the
// untouched orders.json) and the unperturbed truth GL. If this fails, the hard
// substrate drifted and any recorded agent responses pinned against it are stale.
func TestCommittedHardPeriodByteIdentical(t *testing.T) {
	committed := Layout{Root: repoRoot, World: hardWorld, Period: hardPeriod}
	if _, err := os.Stat(committed.PaymentsPath()); err != nil {
		t.Skipf("committed hard fixtures not present (%v); skipping byte-identity guard", err)
	}

	fx, feed, gl, _, amb, err := GenerateWith(hardWorld, hardPeriod, Options{Ambiguity: true})
	if err != nil {
		t.Fatalf("GenerateWith ambiguity: %v", err)
	}
	if amb.NumStripped == 0 {
		t.Fatal("hard period stripped no gst_rate; the committed period is not actually hard")
	}

	cases := []struct {
		name string
		path string
		v    any
	}{
		{"payments", committed.PaymentsPath(), fx.Payments},
		{"refunds", committed.RefundsPath(), fx.Refunds},
		{"settlements", committed.SettlementsPath(), fx.Settlements},
		{"disputes", committed.DisputesPath(), fx.Disputes},
		{"orders", committed.OrdersPath(), fx.Orders},
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
			t.Errorf("committed hard %s (%s) is no longer byte-identical to a fresh --ambiguity seed",
				c.name, filepath.Base(c.path))
		}
	}

	freshGL, err := MarshalStable(gl)
	if err != nil {
		t.Fatalf("marshal fresh hard truth GL: %v", err)
	}
	onDiskGL, err := os.ReadFile(committed.TruthGLPath())
	if err != nil {
		t.Fatalf("read committed hard truth GL: %v", err)
	}
	if string(freshGL) != string(onDiskGL) {
		t.Errorf("committed hard truth/gl.json is no longer byte-identical to a fresh --ambiguity seed")
	}
}

// breakWorld/breakPeriod is the committed Phase-8 reconcile-break period
// (worlds/dtc/2026-03, seeded with --inject unbooked-refund; SPEC §7 check #3,
// §8). It carries a refund whose gst_rate is stripped so the close cannot book it,
// leaving a receivable residual the investigate agent resolves.
const (
	breakWorld  = "dtc"
	breakPeriod = "2026-03"
)

// TestCommittedBreakPeriodByteIdentical is the byte-identity guard for the
// committed Phase-8 break period. A fresh seed of (dtc, 2026-03) WITH the
// unbooked-refund injection must reproduce the committed fixtures byte-for-byte —
// every agent-input file (including the gst_rate-stripped refunds.json and the
// untouched orders.json) and the unperturbed truth GL. If this fails, the break
// substrate drifted and the committed recorded responses/investigations pinned
// against it (and the closer Phase-8 gate) are stale.
func TestCommittedBreakPeriodByteIdentical(t *testing.T) {
	committed := Layout{Root: repoRoot, World: breakWorld, Period: breakPeriod}
	if _, err := os.Stat(committed.PaymentsPath()); err != nil {
		t.Skipf("committed break fixtures not present (%v); skipping byte-identity guard", err)
	}

	fx, feed, gl, inj, _, err := GenerateWith(breakWorld, breakPeriod, Options{Inject: InjectUnbookedRefund})
	if err != nil {
		t.Fatalf("GenerateWith unbooked-refund: %v", err)
	}
	if inj.Kind != InjectUnbookedRefund || inj.RefundID == "" {
		t.Fatalf("break period did not inject an unbooked refund: %+v", inj)
	}

	cases := []struct {
		name string
		path string
		v    any
	}{
		{"payments", committed.PaymentsPath(), fx.Payments},
		{"refunds", committed.RefundsPath(), fx.Refunds},
		{"settlements", committed.SettlementsPath(), fx.Settlements},
		{"disputes", committed.DisputesPath(), fx.Disputes},
		{"orders", committed.OrdersPath(), fx.Orders},
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
			t.Errorf("committed break %s (%s) is no longer byte-identical to a fresh --inject unbooked-refund seed",
				c.name, filepath.Base(c.path))
		}
	}

	freshGL, err := MarshalStable(gl)
	if err != nil {
		t.Fatalf("marshal fresh break truth GL: %v", err)
	}
	onDiskGL, err := os.ReadFile(committed.TruthGLPath())
	if err != nil {
		t.Fatalf("read committed break truth GL: %v", err)
	}
	if string(freshGL) != string(onDiskGL) {
		t.Errorf("committed break truth/gl.json is no longer byte-identical to a fresh --inject unbooked-refund seed")
	}
}
