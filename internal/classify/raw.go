package classify

import (
	"encoding/json"

	"github.com/razorpay/close-agent/internal/ingest"
)

// utrFromRaw extracts the settlement's bank UTR from a normalized event's raw
// object. A settlement carries no notes or links (SPEC §4.3) — its UTR lives in
// the embedded raw Razorpay object — so the settlement rule recovers the external
// tx id by decoding just the "utr" field out of raw.
//
// raw is the canonical re-marshal of a typed raw settlement (ingest.Raw), so the
// "utr" key is always present and a string; decoding into a one-field struct
// ignores every other field. A decode error is surfaced to the caller (a rule
// miss with a reason), never panicked on.
func utrFromRaw(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var only struct {
		UTR string `json:"utr"`
	}
	if err := json.Unmarshal(raw, &only); err != nil {
		return "", err
	}
	return only.UTR, nil
}

// compile-time assertion that the package consumes the ingest event type it is
// built around; if NormalizedEvent's Raw field type changes, this fails to build
// here rather than silently at a call site.
var _ = func(ev ingest.NormalizedEvent) json.RawMessage { return ev.Raw }
