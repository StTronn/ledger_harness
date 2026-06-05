package ingest

import (
	"bytes"
	"testing"

	"github.com/razorpay/close-agent/internal/money"
)

// p builds a *money.Money for the pointer fee/tax fields in expectations.
func p(v int64) *money.Money {
	m := money.FromPaise(v)
	return &m
}

// TestNormalizePerType is table-driven over one of each event type, asserting
// the §4.3 field shaping: which fields are present (fee/tax/notes), the links
// each type carries, and that amount/ts/id map straight through.
func TestNormalizePerType(t *testing.T) {
	raw := Raw{
		Payments: []RawPayment{{
			Entity: "payment", ID: "pay_1", Amount: money.FromPaise(236000), Currency: "INR",
			Status: "captured", OrderID: "order_1", Method: "upi", Captured: true,
			Fee: money.FromPaise(4720), Tax: money.FromPaise(720), CreatedAt: 1000,
			Notes: RawNotes{SKU: "SERUM-30", GSTRate: "18"},
		}},
		Refunds: []RawRefund{{
			Entity: "refund", ID: "rfnd_1", Amount: money.FromPaise(118000), Currency: "INR",
			PaymentID: "pay_1", Status: "processed", CreatedAt: 2000,
			Notes: RawNotes{SKU: "SERUM-30", GSTRate: "18"},
		}},
		Settlements: []RawSettlement{{
			Entity: "settlement", ID: "setl_1", Amount: money.FromPaise(110000), Currency: "INR",
			Status: "processed", Fee: money.FromPaise(4720), Tax: money.FromPaise(720),
			UTR: "UTR1", CreatedAt: 3000, PaymentIDs: []string{"pay_1"}, RefundIDs: []string{"rfnd_1"},
		}},
		Disputes: []RawDispute{{
			Entity: "dispute", ID: "disp_1", Amount: money.FromPaise(236000), Currency: "INR",
			PaymentID: "pay_1", Status: "lost", Reason: "fraud", CreatedAt: 4000,
			Notes: RawNotes{SKU: "SERUM-30", GSTRate: "18"},
		}},
	}

	events, err := Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}

	byID := map[string]NormalizedEvent{}
	for _, e := range events {
		byID[e.ID] = e
	}

	tests := []struct {
		id        string
		typ       EventType
		ts        int64
		amount    money.Money
		fee, tax  *money.Money
		links     Links
		wantNotes bool
	}{
		{"pay_1", EventPayment, 1000, money.FromPaise(236000), p(4720), p(720), Links{OrderID: "order_1", PaymentID: "pay_1"}, true},
		{"rfnd_1", EventRefund, 2000, money.FromPaise(118000), nil, nil, Links{PaymentID: "pay_1"}, true},
		{"setl_1", EventSettlement, 3000, money.FromPaise(110000), p(4720), p(720), Links{}, false},
		{"disp_1", EventDispute, 4000, money.FromPaise(236000), nil, nil, Links{PaymentID: "pay_1"}, true},
	}
	for _, tc := range tests {
		t.Run(string(tc.typ), func(t *testing.T) {
			e := byID[tc.id]
			if e.Type != tc.typ {
				t.Errorf("type = %q, want %q", e.Type, tc.typ)
			}
			if e.TS != tc.ts {
				t.Errorf("ts = %d, want %d", e.TS, tc.ts)
			}
			if e.Amount != tc.amount {
				t.Errorf("amount = %d, want %d", e.Amount.Paise(), tc.amount.Paise())
			}
			if !feeEqual(e.Fee, tc.fee) {
				t.Errorf("fee = %v, want %v", e.Fee, tc.fee)
			}
			if !feeEqual(e.Tax, tc.tax) {
				t.Errorf("tax = %v, want %v", e.Tax, tc.tax)
			}
			if e.Links != tc.links {
				t.Errorf("links = %+v, want %+v", e.Links, tc.links)
			}
			if (e.Notes != nil) != tc.wantNotes {
				t.Errorf("notes present = %v, want %v", e.Notes != nil, tc.wantNotes)
			}
			if len(e.Raw) == 0 {
				t.Errorf("raw is empty; want the original object")
			}
		})
	}
}

// feeEqual compares two *money.Money for the fee/tax pointer fields, treating
// nil as "field absent".
func feeEqual(a, b *money.Money) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// TestNormalizeOrdering asserts the journal is ordered by (ts, id) with id as
// the tie-breaker for events sharing a timestamp, regardless of input order.
func TestNormalizeOrdering(t *testing.T) {
	// Three payments: two share ts=100 (must order by id), one earlier at ts=50.
	raw := Raw{Payments: []RawPayment{
		{Entity: "payment", ID: "pay_c", CreatedAt: 100, Amount: money.FromPaise(3)},
		{Entity: "payment", ID: "pay_a", CreatedAt: 100, Amount: money.FromPaise(1)},
		{Entity: "payment", ID: "pay_z", CreatedAt: 50, Amount: money.FromPaise(9)},
	}}
	events, err := Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	wantOrder := []string{"pay_z", "pay_a", "pay_c"} // ts 50 first, then 100 by id
	for i, want := range wantOrder {
		if events[i].ID != want {
			t.Errorf("event[%d].ID = %q, want %q (order = %v)", i, events[i].ID, want, idsOf(events))
		}
	}
}

// TestNormalizeDeterministic asserts Normalize is a pure function: the same Raw,
// normalized twice (with the slices in different input orders), yields a
// byte-identical journal.
func TestNormalizeDeterministic(t *testing.T) {
	a := Raw{Payments: []RawPayment{
		{Entity: "payment", ID: "pay_1", CreatedAt: 10, Amount: money.FromPaise(1), Fee: money.FromPaise(1), Tax: money.FromPaise(1)},
		{Entity: "payment", ID: "pay_2", CreatedAt: 10, Amount: money.FromPaise(2), Fee: money.FromPaise(1), Tax: money.FromPaise(1)},
	}}
	b := Raw{Payments: []RawPayment{a.Payments[1], a.Payments[0]}} // reversed input

	ea, err := Normalize(a)
	if err != nil {
		t.Fatalf("Normalize a: %v", err)
	}
	eb, err := Normalize(b)
	if err != nil {
		t.Fatalf("Normalize b: %v", err)
	}
	ba, err := MarshalJournal(ea)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	bb, err := MarshalJournal(eb)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	if !bytes.Equal(ba, bb) {
		t.Errorf("Normalize is not order-independent / deterministic:\n a=%s\n b=%s", ba, bb)
	}
}

// TestNormalizeIgnoresBankFeed asserts the bank feed never becomes a journal
// event (SPEC §4.4, §7 — it is the Phase 5 reconcile record). A Raw carrying a
// populated feed but no Razorpay objects normalizes to an empty journal.
func TestNormalizeIgnoresBankFeed(t *testing.T) {
	raw := Raw{
		BankFeed: RawBankFeed{
			Account: "XXXX", Period: "2026-05",
			Credits: []RawBankFeedLine{{Amount: money.FromPaise(100), Date: "2026-05-01", Ref: "UTR1"}},
			Debits:  []RawBankFeedLine{{Amount: money.FromPaise(50), Date: "2026-05-02", Ref: "disp_1"}},
		},
	}
	events, err := Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("journal has %d events from a feed-only Raw, want 0", len(events))
	}
}

// idsOf is a small test helper to render an event slice's ids for failure
// messages.
func idsOf(events []NormalizedEvent) []string {
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	return ids
}
