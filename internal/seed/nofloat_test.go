package seed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoFloatInSource statically enforces the money invariant (SPEC §1, §4) for
// the seed package: no float type may appear in any non-test source file. The
// seeder derives every amount (gross, fee, tax, GST split, net deposit) in
// integer paise; a float64 sneaking into the GST or fee math would silently lose
// paise and could throw the truth GL out of balance, so we fail the build rather
// than trust review.
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
