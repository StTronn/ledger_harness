package truth_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// truthPkg is the import path of the package whose importers we police.
const truthPkg = "github.com/razorpay/close-agent/internal/truth"

// allowedImporters is the closed set of packages permitted to import
// internal/truth (SPEC §4.4, §12: "truth/ must never be read by ingest, classify,
// reconcile, or any agent — only the scorer reads it"):
//
//   - the seeder, which WRITES truth/gl.json;
//   - the (future) scorer, which is the ONLY reader;
//   - the truth package's own (potential) sub-packages.
//
// Anything else importing internal/truth — ingest, normalize, classify,
// reconcile, the agents, or a generic CLI command — is the isolation boundary
// being violated, and this test fails the build. When the scorer package lands,
// add its import path here (and nothing else).
var allowedImporters = map[string]bool{
	"github.com/razorpay/close-agent/internal/truth": true,
	"github.com/razorpay/close-agent/internal/seed":  true,
	// "github.com/razorpay/close-agent/internal/scorer": true, // Phase 6
}

// goListPackage is the subset of `go list -json` output we need: a package's
// import path and the packages it imports (including test imports, so a test
// reaching into truth from a forbidden package is also caught).
type goListPackage struct {
	ImportPath   string
	Imports      []string
	TestImports  []string
	XTestImports []string
}

// TestTruthIsolation enforces the truth/ isolation invariant at the package
// boundary: it lists every package in the module and asserts that the only ones
// importing internal/truth are in allowedImporters. This is the executable form
// of SPEC §12's "the truth/ isolation test … assert (e.g. via package
// boundaries) that no non-scorer code path can read truth/".
func TestTruthIsolation(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-test", "-json",
		"github.com/razorpay/close-agent/...").Output()
	if err != nil {
		// Surface stderr from `go list` if available, for a useful failure.
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("go list failed: %v", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	seen := map[string]bool{} // pkg import paths already reported, to dedupe
	for dec.More() {
		var p goListPackage
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		// Only police packages within this module.
		if !strings.HasPrefix(p.ImportPath, "github.com/razorpay/close-agent/") &&
			p.ImportPath != "github.com/razorpay/close-agent" {
			continue
		}
		all := append(append(append([]string{}, p.Imports...), p.TestImports...), p.XTestImports...)
		for _, imp := range all {
			if imp != truthPkg {
				continue
			}
			// Normalize test-variant import paths (e.g. "...truth [.test]") to the
			// base package path before checking the allow-list.
			base := strings.Fields(p.ImportPath)[0]
			if allowedImporters[base] {
				continue
			}
			if !seen[base] {
				seen[base] = true
				t.Errorf("package %q imports %q but is not an allowed importer; "+
					"truth/ is SCORER-ONLY (SPEC §4.4, §12). If this is the scorer, "+
					"add it to allowedImporters.", base, truthPkg)
			}
		}
	}
}
