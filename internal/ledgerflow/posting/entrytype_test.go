package posting

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/ingest"
)

// TestEntryTypeForEventType pins the event-type -> entry-type mapping the read
// model's bundlers use to name the entry type applicable to an event (the inverse
// of EventTypeForEntryType; 1:1 in v1). It is the single source so the bundle and
// the rule engine never disagree on which entry type books which event.
func TestEntryTypeForEventType(t *testing.T) {
	cases := []struct {
		typ    ingest.EventType
		want   string
		wantOK bool
	}{
		{ingest.EventPayment, "dtc_sale", true},
		{ingest.EventSettlement, "razorpay_settlement", true},
		{ingest.EventRefund, "refund_reversal", true},
		{ingest.EventDispute, "chargeback_loss", true},
		{ingest.EventType("bogus"), "", false},
	}
	for _, c := range cases {
		got, ok := EntryTypeForEventType(c.typ)
		if got != c.want || ok != c.wantOK {
			t.Errorf("EntryTypeForEventType(%q) = (%q,%v), want (%q,%v)", c.typ, got, ok, c.want, c.wantOK)
		}
	}
}
