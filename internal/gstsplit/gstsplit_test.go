package gstsplit

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
)

// TestSplitInclusive asserts the canonical inclusive-GST split is byte-for-byte
// the seeder's formula: net = gross*100/(100+rate) (integer division) and
// gst = gross - net, with net+gst == gross to the paise. It is table-driven over
// hand-checked cases at the three v1 catalogue rates (5/12/18) plus edge amounts.
func TestSplitInclusive(t *testing.T) {
	tests := []struct {
		name    string
		gross   int64
		rate    int
		wantNet int64
		wantGST int64
	}{
		// Clean round grosses at each catalogue rate.
		{"5pct round", 10500, 5, 10000, 500},
		{"12pct round", 11200, 12, 10000, 1200},
		{"18pct round", 11800, 18, 10000, 1800},

		// Non-round grosses: net is truncated, the remainder folds into gst.
		// 328117 @ 5%: 328117*100/105 = 312492 (trunc), gst = 15625.
		{"5pct trunc", 328117, 5, 312492, 15625},
		// 100001 @ 18%: 100001*100/118 = 84746 (trunc), gst = 15255.
		{"18pct trunc", 100001, 18, 84746, 15255},
		// 99999 @ 12%: 99999*100/112 = 89284 (trunc), gst = 10715.
		{"12pct trunc", 99999, 12, 89284, 10715},

		// Edge amounts: smallest non-zero gross at each rate. net truncates to 0,
		// the entire paise becomes gst, and net+gst == gross holds.
		{"1 paise @5pct", 1, 5, 0, 1},
		{"1 paise @12pct", 1, 12, 0, 1},
		{"1 paise @18pct", 1, 18, 0, 1},
		// 105 @ 5% is the smallest gross whose net is exactly 1 paise of net here:
		// 105*100/105 = 100, gst = 5.
		{"105 paise @5pct", 105, 5, 100, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			net, gst := SplitInclusive(money.FromPaise(tt.gross), tt.rate)
			if net.Paise() != tt.wantNet {
				t.Errorf("net = %d, want %d", net.Paise(), tt.wantNet)
			}
			if gst.Paise() != tt.wantGST {
				t.Errorf("gst = %d, want %d", gst.Paise(), tt.wantGST)
			}
			if got := net.Add(gst); got != money.FromPaise(tt.gross) {
				t.Errorf("net+gst = %d, want gross %d (split must be exact)", got.Paise(), tt.gross)
			}
		})
	}
}

// TestSplitInclusiveExactSweep asserts net+gst == gross (and neither component
// goes negative) for every gross in a dense range at each catalogue rate. The
// exactness invariant must hold for ALL values, not just the hand-picked ones,
// or a posted entry could fail to balance against truth.
func TestSplitInclusiveExactSweep(t *testing.T) {
	for _, rate := range []int{5, 12, 18} {
		for g := int64(1); g <= 20000; g++ {
			gross := money.FromPaise(g)
			net, gst := SplitInclusive(gross, rate)
			if net.Add(gst) != gross {
				t.Fatalf("rate %d gross %d: net(%d)+gst(%d) != gross", rate, g, net.Paise(), gst.Paise())
			}
			if net.Sign() < 0 || gst.Sign() < 0 {
				t.Fatalf("rate %d gross %d: negative component net=%d gst=%d", rate, g, net.Paise(), gst.Paise())
			}
		}
	}
}

// TestSplitInclusivePanicsOnNonPositiveRate asserts a missing/zero/negative rate
// is a hard error, not a silent divide-by-something-wrong. Callers with untrusted
// metadata must flag-and-skip before calling, per SPEC §2 Phase 4.
func TestSplitInclusivePanicsOnNonPositiveRate(t *testing.T) {
	for _, rate := range []int{0, -1, -18} {
		t.Run("", func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("SplitInclusive(_, %d) did not panic", rate)
				}
			}()
			SplitInclusive(money.FromPaise(11800), rate)
		})
	}
}

// TestNoFloatInSource statically parses every non-test Go file in this package
// and fails if it references float64 or float32 — the project invariant that no
// float ever touches money, enforced at the source level (SPEC §1, §4, §13.8).
func TestNoFloatInSource(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		path := filepath.Join(".", name)
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := parser.ParseFile(fset, path, data, 0); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		src := string(data)
		for _, banned := range []string{"float64", "float32"} {
			if strings.Contains(src, banned) {
				t.Errorf("%s contains banned float type %q — money must be integer paise only", name, banned)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no source files checked")
	}
}
