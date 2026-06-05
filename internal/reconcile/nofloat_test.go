package reconcile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoFloatInSource statically enforces the money invariant (SPEC §1, §4) for
// the reconcile package: no float type may appear in the check arithmetic. Every
// amount compared by the three checks is integer paise (money.Money); a float64
// sneaking into a sum or a tolerance computation would silently lose paise and
// let a real break slip through, so we fail the build rather than trust review.
// Test files are excluded — only the package's own .go sources are scanned.
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
