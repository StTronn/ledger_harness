package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoFloatInSource statically enforces the money invariant (SPEC §1, §4) for
// the ledger package: no float type may appear anywhere in the posting/report
// path. Money is integer paise end to end; a float64 sneaking in (e.g. via a
// careless amount computation) would silently lose paise and break the trial
// balance, so we fail the build rather than trust review. Test files are
// excluded — only the package's own .go sources are scanned.
func TestNoFloatInSource(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	banned := []string{"float64", "float32"}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(data)
		for _, b := range banned {
			if strings.Contains(src, b) {
				t.Errorf("%s contains banned float type %q — money must be integer paise only", name, b)
			}
		}
	}
}
