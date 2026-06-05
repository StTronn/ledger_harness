package money

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Money
		wantErr bool
	}{
		// Happy paths.
		{name: "spec example two decimals", input: "2360.00", want: 236000},
		{name: "whole rupees no decimal", input: "2360", want: 236000},
		{name: "trailing dot is zero paise", input: "2360.", want: 236000},
		{name: "one decimal pads to tens of paise", input: "2360.5", want: 236050},
		{name: "two decimals", input: "2360.05", want: 236005},
		{name: "zero", input: "0", want: 0},
		{name: "zero with decimals", input: "0.00", want: 0},
		{name: "sub-rupee", input: "0.05", want: 5},
		{name: "explicit plus sign", input: "+2360.00", want: 236000},
		{name: "negative amount", input: "-2360.00", want: -236000},
		{name: "negative sub-rupee", input: "-0.05", want: -5},
		{name: "negative whole", input: "-7", want: -700},
		{name: "leading zeros in rupees", input: "007.50", want: 750},

		// Rejections — strict, float-free parsing.
		{name: "empty string", input: "", wantErr: true},
		{name: "sign only", input: "-", wantErr: true},
		{name: "plus only", input: "+", wantErr: true},
		{name: "three decimal places", input: "1.005", wantErr: true},
		{name: "missing rupee part", input: ".50", wantErr: true},
		{name: "two decimal points", input: "1.0.0", wantErr: true},
		{name: "alpha garbage", input: "twelve", wantErr: true},
		{name: "trailing garbage", input: "12.5x", wantErr: true},
		{name: "leading whitespace", input: " 12.50", wantErr: true},
		{name: "trailing whitespace", input: "12.50 ", wantErr: true},
		{name: "grouping separators", input: "1,234.00", wantErr: true},
		{name: "currency symbol", input: "₹12.00", wantErr: true},
		{name: "scientific notation", input: "1e2", wantErr: true},
		{name: "internal space", input: "12 .50", wantErr: true},
		{name: "double sign", input: "--5", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %d, nil; want error", tt.input, got)
				}
				if !errors.Is(err, ErrParse) {
					t.Errorf("Parse(%q) error = %v; want wrapping ErrParse", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %d paise; want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name string
		in   Money
		want string
	}{
		{name: "spec example", in: 236000, want: "2360.00"},
		{name: "zero", in: 0, want: "0.00"},
		{name: "sub-rupee", in: 5, want: "0.05"},
		{name: "tens of paise", in: 50, want: "0.50"},
		{name: "negative", in: -236000, want: "-2360.00"},
		{name: "negative sub-rupee", in: -5, want: "-0.05"},
		{name: "exact rupee", in: 100, want: "1.00"},
		{name: "max int64", in: math.MaxInt64, want: "92233720368547758.07"},
		{name: "min int64 special case", in: minMoney, want: minMoneyString},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.String(); got != tt.want {
				t.Errorf("Money(%d).String() = %q; want %q", int64(tt.in), got, tt.want)
			}
			// Format is documented as an alias of String.
			if got := tt.in.Format(); got != tt.want {
				t.Errorf("Money(%d).Format() = %q; want %q", int64(tt.in), got, tt.want)
			}
		})
	}
}

// TestRoundTrip asserts Parse and String are exact inverses across the values
// the ledger will actually carry, so no paise are ever lost in formatting.
func TestRoundTrip(t *testing.T) {
	values := []Money{
		0, 1, 5, 50, 99, 100, 720, 4720, 236000, -1, -5, -236000,
		math.MaxInt64, math.MinInt64 + 1, // MinInt64 itself is covered by String's special case
	}
	for _, v := range values {
		s := v.String()
		got, err := Parse(s)
		if err != nil {
			t.Errorf("Parse(%q) (from Money %d): %v", s, int64(v), err)
			continue
		}
		if got != v {
			t.Errorf("round trip: Money(%d) -> %q -> Money(%d)", int64(v), s, int64(got))
		}
	}
}

func TestArithmetic(t *testing.T) {
	// dtc_sale-style split: gross = net + gst, exactly, in paise.
	net := FromPaise(235280)
	gst := FromPaise(720)
	gross := net.Add(gst)
	if gross != 236000 {
		t.Errorf("Add: 235280 + 720 = %d; want 236000", int64(gross))
	}
	if back := gross.Sub(gst); back != net {
		t.Errorf("Sub: 236000 - 720 = %d; want %d", int64(back), int64(net))
	}
	if n := net.Neg(); n != -235280 {
		t.Errorf("Neg: -(235280) = %d; want -235280", int64(n))
	}
	if FromRupees(2360) != 236000 {
		t.Errorf("FromRupees(2360) = %d; want 236000", int64(FromRupees(2360)))
	}
	if FromPaise(236000).Paise() != 236000 {
		t.Errorf("Paise() round trip failed")
	}
}

func TestSignAndZero(t *testing.T) {
	tests := []struct {
		in       Money
		wantSign int
		wantZero bool
	}{
		{in: 0, wantSign: 0, wantZero: true},
		{in: 1, wantSign: 1, wantZero: false},
		{in: -1, wantSign: -1, wantZero: false},
		{in: 236000, wantSign: 1, wantZero: false},
		{in: -236000, wantSign: -1, wantZero: false},
	}
	for _, tt := range tests {
		if got := tt.in.Sign(); got != tt.wantSign {
			t.Errorf("Money(%d).Sign() = %d; want %d", int64(tt.in), got, tt.wantSign)
		}
		if got := tt.in.IsZero(); got != tt.wantZero {
			t.Errorf("Money(%d).IsZero() = %v; want %v", int64(tt.in), got, tt.wantZero)
		}
	}
}

// TestNoFloatInSource statically parses every non-test Go file in this package
// and fails if it references float64 or float32. This enforces the project
// invariant that no float ever touches money — at the source level, not just by
// convention.
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
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if id.Name == "float64" || id.Name == "float32" {
				t.Errorf("%s: forbidden float type %q at %s — money must stay integer paise",
					name, id.Name, fset.Position(id.Pos()))
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no non-test .go files found to scan for floats")
	}
}
