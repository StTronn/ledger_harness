package score

import (
	"path/filepath"

	"github.com/razorpay/close-agent/internal/truth"
)

// runscore.go is the FILE-reading entry point of the scorer: it is the single
// place that loads the period's hidden truth GL off disk (via the scorer-only
// truth.ReadTruth) and diffs the produced entries against it. The orchestrator
// (internal/closer) calls RunScore so it never has to import internal/truth
// itself — keeping the truth-isolation boundary (SPEC §4.4, §12) at exactly the
// scorer, the one package allowed to read truth/gl.json.

// truthGLPath resolves worlds/<world>/<period>/truth/gl.json under root. It
// mirrors the seeder's layout (internal/seed.Layout.TruthGLPath) but is defined
// here so the scorer does not depend on the seeder; both agree only on the
// on-disk path contract.
func truthGLPath(root, world, period string) string {
	return filepath.Join(root, "worlds", world, period, "truth", "gl.json")
}

// RunScore loads the truth GL for (world, period) under root and scores the
// produced entries against it. It is the orchestrator's one-call scoring seam:
// the produced ledger is built with no knowledge of truth, then handed here,
// where truth is read behind the scorer boundary and the pure diff (Score) runs.
//
// A missing or malformed/unbalanced truth GL is a hard error (a corrupt oracle is
// not silently scored against) surfaced from truth.ReadTruth.
func RunScore(root, world, period string, produced []Produced) (Result, error) {
	gl, err := LoadTruth(root, world, period)
	if err != nil {
		return Result{}, err
	}
	return Score(produced, gl), nil
}

// LoadTruth reads and validates the period's ground-truth GL. It is exported so a
// caller that wants the raw GL (e.g. the diff command) can obtain it through the
// scorer boundary rather than importing internal/truth directly. The returned GL
// is the validated ground truth (schema version checked, balances asserted) per
// truth.ReadTruth.
func LoadTruth(root, world, period string) (truth.GL, error) {
	return truth.ReadTruth(truthGLPath(root, world, period))
}
