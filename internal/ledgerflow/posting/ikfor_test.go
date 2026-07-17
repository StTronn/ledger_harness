package posting

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/ingest"
)

// TestIKFor pins the canonical idempotency-key scheme: the single source of the
// "sale:/settle:/refund:/dispute:" prefixes that the rule engine, the agent seam,
// the investigate seam, and the read model's `booked` predicate all key on. If
// these drift, an entry can exist while `booked` reports false (or vice-versa).
func TestIKFor(t *testing.T) {
	cases := []struct {
		typ  ingest.EventType
		id   string
		want string
	}{
		{ingest.EventPayment, "pay_A1", "sale:pay_A1"},
		{ingest.EventSettlement, "setl_B1", "settle:setl_B1"},
		{ingest.EventRefund, "rfnd_C1", "refund:rfnd_C1"},
		{ingest.EventDispute, "disp_D1", "dispute:disp_D1"},
	}
	for _, c := range cases {
		if got := IKFor(c.typ, c.id); got != c.want {
			t.Errorf("IKFor(%q, %q) = %q, want %q", c.typ, c.id, got, c.want)
		}
	}
}

// TestEventTypeForEntryType pins the playbook-entry-type -> event-type mapping the
// investigate seam uses to derive an IK when it only knows the entry type it is
// about to book.
func TestEventTypeForEntryType(t *testing.T) {
	cases := []struct {
		entryType string
		want      ingest.EventType
		wantOK    bool
	}{
		{"dtc_sale", ingest.EventPayment, true},
		{"razorpay_settlement", ingest.EventSettlement, true},
		{"refund_reversal", ingest.EventRefund, true},
		{"chargeback_loss", ingest.EventDispute, true},
		{"unknown_type", "", false},
	}
	for _, c := range cases {
		got, ok := EventTypeForEntryType(c.entryType)
		if got != c.want || ok != c.wantOK {
			t.Errorf("EventTypeForEntryType(%q) = (%q, %v), want (%q, %v)", c.entryType, got, ok, c.want, c.wantOK)
		}
	}
}
