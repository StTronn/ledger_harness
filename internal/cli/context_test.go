package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContextEventBundleSurfacesRecoveredRefund drives the `context` command
// surface against the committed 2026-03 fixtures. The missing GST rate is now
// recovered and posted before reconciliation, so there is no break; the event
// bundle still exposes the recovered fact and booked state.
func TestContextEventBundleSurfacesRecoveredRefund(t *testing.T) {
	const repoRoot = "../.."
	wp := []string{"--world", "dtc", "--period", "2026-03", "--root", repoRoot}

	breaksOut := runCLI(t, append([]string{"context", "breaks"}, wp...)...)
	if strings.TrimSpace(breaksOut) != "{\n  \"breaks\": []\n}" {
		t.Fatalf("context breaks = %s, want no remaining breaks", breaksOut)
	}

	bundleOut := runCLI(t, append([]string{"context", "event", "rfnd_2q9UwRRE21Gf2r"}, wp...)...)
	var bundle struct {
		Event struct {
			Booked  bool   `json:"booked"`
			EventID string `json:"event_id"`
		} `json:"event"`
		Recovered *struct {
			GSTRate string `json:"gst_rate"`
		} `json:"recovered"`
	}
	if err := json.Unmarshal([]byte(bundleOut), &bundle); err != nil {
		t.Fatalf("context break did not emit valid JSON: %v\n%s", err, bundleOut)
	}
	if !bundle.Event.Booked || bundle.Event.EventID != "rfnd_2q9UwRRE21Gf2r" {
		t.Errorf("event bundle = %+v, want booked recovered refund", bundle.Event)
	}
	if bundle.Recovered == nil || bundle.Recovered.GSTRate == "" {
		t.Errorf("expected a recovered GST rate in the bundle:\n%s", bundleOut)
	}
}
