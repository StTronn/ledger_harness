package run_test

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/ledgerflow/run"
)

// TestBuildRecoveryEngineContextOnRealPeriod is the end-to-end proof the Tier-1
// context bundle works on actual fixtures. The missing GST rate on the 2026-03
// refund is recovered from the order even though the new flow posts it before
// reconciliation, so there is no remaining break to investigate.
func TestBuildRecoveryEngineReconBundleOnRealPeriod(t *testing.T) {
	const root = "../../../../"
	m, err := run.BuildRecoveryEngine(root, "dtc", "2026-03")
	if err != nil {
		t.Fatalf("BuildRecoveryEngine: %v", err)
	}

	b, ok := m.EventContext("rfnd_2q9UwRRE21Gf2r")
	if !ok {
		t.Fatal("EventContext for recovered refund not found")
	}
	if b.Recovered == nil || b.Recovered.GSTRate == "" {
		t.Fatalf("recovered rate = %+v, want a validated GST rate", b.Recovered)
	}
	if len(m.BreakKeys()) != 0 {
		t.Errorf("break keys = %v, want none after deterministic recovery", m.BreakKeys())
	}
	if b.Event.EventID != "rfnd_2q9UwRRE21Gf2r" {
		t.Errorf("event id = %q", b.Event.EventID)
	}
}
