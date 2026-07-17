package posting

import (
	"strings"
	"testing"

	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/money"
)

// TestPartialRefundIsARuleMiss pins the partial-refund guard: a refund whose
// ParentAmount is set (normalize joined the parent payment and found the refund
// smaller) is a RULE MISS even when it carries a perfectly usable gst_rate —
// because the ambiguity is the ENTRY TYPE (return vs goodwill credit note), not
// the arithmetic. Booking it as a refund_reversal would be the wrong-but-balanced
// failure: balanced books, clean reconcile, silently misclassified.
func TestPartialRefundIsARuleMiss(t *testing.T) {
	parent := money.FromPaise(118000)
	ev := ingest.NormalizedEvent{
		ID: "rfnd_PART", Type: ingest.EventRefund, TS: 100,
		Amount:       money.FromPaise(41300),
		Notes:        &ingest.Notes{SKU: "SERUM-30", GSTRate: "18"}, // rate present!
		ParentAmount: &parent,
	}
	c, ok, reason := Classify(ev)
	if ok || c != nil {
		t.Fatalf("partial refund classified as %+v; want a rule miss", c)
	}
	if !strings.HasPrefix(reason, MissPartialRefund) {
		t.Errorf("miss reason %q does not carry the %q marker", reason, MissPartialRefund)
	}
}

// TestFullRefundStillClassifies guards the existing path: a refund with no
// ParentAmount (full) and a usable rate books refund_reversal exactly as before.
func TestFullRefundStillClassifies(t *testing.T) {
	ev := ingest.NormalizedEvent{
		ID: "rfnd_FULL", Type: ingest.EventRefund, TS: 100,
		Amount: money.FromPaise(118000),
		Notes:  &ingest.Notes{SKU: "SERUM-30", GSTRate: "18"},
	}
	c, ok, _ := Classify(ev)
	if !ok || c == nil || c.EntryType != "refund_reversal" {
		t.Fatalf("full refund => %+v ok=%v, want refund_reversal", c, ok)
	}
}
