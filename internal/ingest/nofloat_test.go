package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoFloatInSource statically enforces the money invariant (SPEC §1, §4) for
// the ingest package: no float type may appear in any non-test source file.
// ingest decodes and carries every amount (payment gross/fee/tax, refund and
// dispute gross, settlement net/fee/tax, bank-feed amounts) as integer paise via
// money.Money. A float64 sneaking in here — e.g. decoding an amount into a float
// — would silently corrupt money before it ever reaches the ledger, so we fail
// the build rather than trust review.
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
