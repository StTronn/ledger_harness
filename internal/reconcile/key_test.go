package reconcile

import "testing"

// TestBreakKey pins the canonical stable break key — "check<N>:<kind>:<settlement>"
// — that the recorded-investigation fixture, the trace, and the read model all key
// on. It is the single source of the scheme so a break and a lookup of it agree.
func TestBreakKey(t *testing.T) {
	cases := []struct {
		b    Break
		want string
	}{
		{Break{Check: CheckReceivableClears, Kind: "receivable-residual", SettlementID: ""},
			"check3:receivable-residual:"},
		{Break{Check: CheckBatchSum, Kind: "batch-sum-mismatch", SettlementID: "setl_X"},
			"check2:batch-sum-mismatch:setl_X"},
	}
	for _, c := range cases {
		if got := c.b.Key(); got != c.want {
			t.Errorf("Break.Key() = %q, want %q", got, c.want)
		}
	}
}
