package ingest

import "github.com/razorpay/close-agent/internal/money"

// EventType is the kind of normalized event (SPEC §4.3): one of payment, refund,
// settlement, dispute. It is a string for stable, human-readable JSON.
type EventType string

const (
	// The four event types the v1 DTC world produces (SPEC §4.3).
	EventPayment    EventType = "payment"
	EventRefund     EventType = "refund"
	EventSettlement EventType = "settlement"
	EventDispute    EventType = "dispute"
)

// Links carries the cross-references SPEC §4.3 names: the originating order and
// the payment an event relates to. Both are omitempty so the journal only
// records the links that apply to each event type (e.g. a refund has a
// payment_id but no order_id; a settlement has neither — its batch members live
// in raw).
type Links struct {
	OrderID   string `json:"order_id,omitempty"`
	PaymentID string `json:"payment_id,omitempty"`
}

// Notes mirrors SPEC §4.3's free-form notes, typed to the sku + gst_rate the
// substrate stamps. It is attached only to events that carry it (payments,
// refunds, disputes); settlements have no notes, so NormalizedEvent.Notes is a
// pointer that is nil (and omitted from JSON) for settlements.
type Notes struct {
	SKU     string `json:"sku,omitempty"`
	GSTRate string `json:"gst_rate,omitempty"`
}

// NormalizedEvent is the flattened, source-agnostic event of SPEC §4.3 — the
// unit of the event journal that the classify stage consumes. It is produced by
// normalize from a raw Razorpay object and intentionally drops Razorpay-specific
// plumbing (entity tag, currency, status, batch id lists) down into Raw, keeping
// only what the playbook rules need plus the original object for audit.
//
// Money fields are integer paise (money.Money). Fee and Tax are pointers so they
// are present only "where applicable" (SPEC §4.3): on payments (Razorpay's fee +
// GST-on-fee on that payment) and settlements (the batch-aggregate fee + GST-on-
// fee). Refunds and disputes carry no fee/tax, so the fields are nil and omitted
// from JSON, keeping the journal explicit about which events bear fees.
type NormalizedEvent struct {
	ID     string       `json:"id"`              // the source object id (pay_/rfnd_/setl_/disp_)
	Type   EventType    `json:"type"`            // payment | refund | settlement | dispute
	TS     int64        `json:"ts"`              // Unix seconds; the journal's primary sort key
	Amount money.Money  `json:"amount"`          // event gross in paise (see per-type docs in normalize)
	Fee    *money.Money `json:"fee,omitempty"`   // Razorpay fee in paise, where applicable
	Tax    *money.Money `json:"tax,omitempty"`   // GST on Razorpay's fee in paise, where applicable
	Links  Links        `json:"links"`           // order_id / payment_id cross-references
	Notes  *Notes       `json:"notes,omitempty"` // sku + gst_rate, where the event carries them
	Raw    rawObject    `json:"raw"`             // the original Razorpay object (canonical re-marshal)
}
