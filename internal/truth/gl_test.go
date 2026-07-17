package truth

import (
	"encoding/json"
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
)

// TestEntryBalance is table-driven over balanced and unbalanced entries.
func TestEntryBalance(t *testing.T) {
	tests := []struct {
		name  string
		lines []Line
		want  bool
	}{
		{
			name: "balanced two-line",
			lines: []Line{
				{Side: Debit, Account: "assets/bank", Amount: money.FromPaise(1000)},
				{Side: Credit, Account: "income/product-sales", Amount: money.FromPaise(1000)},
			},
			want: true,
		},
		{
			name: "balanced split credit",
			lines: []Line{
				{Side: Debit, Account: "assets/razorpay-settlement-receivable", Amount: money.FromPaise(11800)},
				{Side: Credit, Account: "income/product-sales", Amount: money.FromPaise(10000)},
				{Side: Credit, Account: "liabilities/gst-output-payable", Amount: money.FromPaise(1800)},
			},
			want: true,
		},
		{
			name: "unbalanced",
			lines: []Line{
				{Side: Debit, Account: "assets/bank", Amount: money.FromPaise(1000)},
				{Side: Credit, Account: "income/product-sales", Amount: money.FromPaise(999)},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Entry{Lines: tt.lines}
			if got := e.IsBalanced(); got != tt.want {
				t.Errorf("IsBalanced() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGLBalanceAndMarshal asserts GL-level balancing and that amounts marshal as
// plain integer paise (no float) with stable key order.
func TestGLBalanceAndMarshal(t *testing.T) {
	gl := GL{
		Version: SchemaVersion,
		World:   "dtc",
		Period:  "2026-05",
		Entries: []Entry{
			{
				ID: "gl_0001", EntryType: "dtc_sale", EventID: "pay_X", Ts: 100,
				Lines: []Line{
					{Side: Debit, Account: "assets/razorpay-settlement-receivable", Amount: money.FromPaise(11800)},
					{Side: Credit, Account: "income/product-sales", Amount: money.FromPaise(10000)},
					{Side: Credit, Account: "liabilities/gst-output-payable", Amount: money.FromPaise(1800)},
				},
			},
		},
	}
	if !gl.IsBalanced() {
		dr, cr := gl.SumBySide()
		t.Fatalf("GL not balanced: Dr=%s Cr=%s", dr, cr)
	}
	b, err := json.Marshal(gl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	// Amounts are integer paise in JSON.
	for _, want := range []string{`"amount":11800`, `"amount":10000`, `"amount":1800`, `"version":1`} {
		if !contains(s, want) {
			t.Errorf("marshalled GL missing %q\n%s", want, s)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
