package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContextEntityResolvesAndRefuses drives the tier-2 lookup end to end on the
// committed partial-refund world: a refund resolves to its raw object + edges, a
// rate-card channel resolves to the contracted row, and an unknown id is a clean
// error (never a guess).
func TestContextEntityResolvesAndRefuses(t *testing.T) {
	const repoRoot = "../.."
	wp := []string{"--world", "dtc", "--period", "2026-01", "--root", repoRoot}

	out := runCLI(t, append([]string{"context", "entity", "rfnd_ZtHFpyTP2I9NSz"}, wp...)...)
	var v struct {
		Kind   string            `json:"kind"`
		Edges  map[string]string `json:"edges"`
		Object json.RawMessage   `json:"object"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("entity output not JSON: %v\n%s", err, out)
	}
	if v.Kind != "refund" || v.Edges["payment_id"] == "" || len(v.Object) == 0 {
		t.Errorf("refund entity = %+v", v)
	}

	rcOut := runCLI(t, append([]string{"context", "entity", "ratecard/razorpay"}, wp...)...)
	if !strings.Contains(rcOut, `"fee_bps": 200`) {
		t.Errorf("ratecard entity missing contracted row:\n%s", rcOut)
	}

	var buf strings.Builder
	if err := Execute(append([]string{"context", "entity", "pay_NOPE"}, wp...), &buf); err == nil {
		t.Error("unknown id did not error")
	}
}
