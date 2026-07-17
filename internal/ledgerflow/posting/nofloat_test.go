package posting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoFloatInSource statically enforces the money invariant (SPEC §1, §4) for
// the classify package: no float type may appear in any non-test source file. The
// rule engine computes every derived amount (GST split, settlement gross-up) in
// integer paise via money.Money and gstsplit; a float64 sneaking in could lose a
// paise and make a posted entry fail to equal truth, so we fail the build rather
// than trust review.
func TestNoFloatInSource(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	banned := []string{"float64", "float32"}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
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
	if checked == 0 {
		t.Fatal("no source files checked")
	}
}
