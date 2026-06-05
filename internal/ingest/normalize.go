package ingest

import (
	"encoding/json"
	"fmt"
	"sort"
)

// rawObject holds the original Razorpay object attached to a normalized event
// (SPEC §4.3 "raw"). It is a json.RawMessage so the event marshals the object
// inline (not as a quoted string), and is produced by re-marshalling the typed
// raw struct through the canonical encoder — which fixes key order from the
// struct tags — so the journal is byte-stable across runs (SPEC §12). Embedding
// the typed object's canonical bytes (rather than the source file's original
// bytes) keeps the journal independent of incidental fixture whitespace, while
// still round-tripping every field the raw type models.
type rawObject = json.RawMessage

// Normalize flattens a Raw bundle into the ordered, normalized event journal of
// SPEC §4.3. It is a PURE function of raw: same input => identical output (no
// wall clock, no map iteration in the output path), which is what makes the
// golden test reproducible.
//
// The bank feed (raw.BankFeed) is deliberately NOT part of the journal — it is
// the independent record consumed by the Phase 5 reconcile stage (SPEC §4.4,
// §7), not an accounting event. Normalize reads only the four Razorpay slices.
//
// Ordering (SPEC §5 "ordered event journal"): events are sorted by (ts, id).
// ts is the primary key so the journal reads chronologically; id is the
// tie-breaker because many fixture objects share a created_at (a whole batch of
// payments captures on the same day), and id is unique and stable, so the order
// is total and deterministic.
func Normalize(raw Raw) ([]NormalizedEvent, error) {
	events := make([]NormalizedEvent, 0,
		len(raw.Payments)+len(raw.Refunds)+len(raw.Settlements)+len(raw.Disputes))

	for _, p := range raw.Payments {
		e, err := normalizePayment(p)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	for _, r := range raw.Refunds {
		e, err := normalizeRefund(r)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	for _, s := range raw.Settlements {
		e, err := normalizeSettlement(s)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	for _, d := range raw.Disputes {
		e, err := normalizeDispute(d)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}

	sortJournal(events)
	return events, nil
}

// sortJournal orders the journal by (ts, id) — chronological, with the unique id
// as a stable tie-breaker for events sharing a timestamp. sort.Slice with a
// total order makes this deterministic regardless of input order.
func sortJournal(events []NormalizedEvent) {
	sort.Slice(events, func(i, j int) bool {
		if events[i].TS != events[j].TS {
			return events[i].TS < events[j].TS
		}
		return events[i].ID < events[j].ID
	})
}

// normalizePayment maps a captured payment to its §4.3 event. Amount is the
// gross the customer paid (GST-inclusive); Fee is Razorpay's fee on that gross
// and Tax the GST on that fee — both apply to a payment. links carries the order
// and the payment id (the payment links to itself, matching the §4.3 example).
func normalizePayment(p RawPayment) (NormalizedEvent, error) {
	rawMsg, err := marshalRaw(p)
	if err != nil {
		return NormalizedEvent{}, fmt.Errorf("ingest: normalize payment %s: %w", p.ID, err)
	}
	fee := p.Fee
	tax := p.Tax
	return NormalizedEvent{
		ID:     p.ID,
		Type:   EventPayment,
		TS:     p.CreatedAt,
		Amount: p.Amount,
		Fee:    &fee,
		Tax:    &tax,
		Links:  Links{OrderID: p.OrderID, PaymentID: p.ID},
		Notes:  notesFrom(p.Notes),
		Raw:    rawMsg,
	}, nil
}

// normalizeRefund maps a refund to its §4.3 event. Amount is the gross refunded;
// a refund bears no Razorpay fee/tax, so Fee/Tax are nil. links carries the
// payment_id the refund reverses (no order_id on a refund object).
func normalizeRefund(r RawRefund) (NormalizedEvent, error) {
	rawMsg, err := marshalRaw(r)
	if err != nil {
		return NormalizedEvent{}, fmt.Errorf("ingest: normalize refund %s: %w", r.ID, err)
	}
	return NormalizedEvent{
		ID:     r.ID,
		Type:   EventRefund,
		TS:     r.CreatedAt,
		Amount: r.Amount,
		Links:  Links{PaymentID: r.PaymentID},
		Notes:  notesFrom(r.Notes),
		Raw:    rawMsg,
	}, nil
}

// normalizeSettlement maps a settlement payout to its §4.3 event. Amount is the
// NET deposited to the bank; Fee and Tax are the batch-aggregate Razorpay fee
// and GST-on-fee netted out, so both apply. A settlement has no order/payment
// link and no notes — its batch members and UTR live in raw for the Phase 4
// gross-up and Phase 5 batch-sum recon.
func normalizeSettlement(s RawSettlement) (NormalizedEvent, error) {
	rawMsg, err := marshalRaw(s)
	if err != nil {
		return NormalizedEvent{}, fmt.Errorf("ingest: normalize settlement %s: %w", s.ID, err)
	}
	fee := s.Fee
	tax := s.Tax
	return NormalizedEvent{
		ID:     s.ID,
		Type:   EventSettlement,
		TS:     s.CreatedAt,
		Amount: s.Amount,
		Fee:    &fee,
		Tax:    &tax,
		Links:  Links{},
		Raw:    rawMsg,
	}, nil
}

// normalizeDispute maps a dispute to its §4.3 event. Amount is the disputed
// gross; a dispute bears no Razorpay fee/tax. links carries the payment_id under
// dispute. The status (lost/won/open) stays in raw for the classify stage to
// decide whether to book a chargeback_loss.
func normalizeDispute(d RawDispute) (NormalizedEvent, error) {
	rawMsg, err := marshalRaw(d)
	if err != nil {
		return NormalizedEvent{}, fmt.Errorf("ingest: normalize dispute %s: %w", d.ID, err)
	}
	return NormalizedEvent{
		ID:     d.ID,
		Type:   EventDispute,
		TS:     d.CreatedAt,
		Amount: d.Amount,
		Links:  Links{PaymentID: d.PaymentID},
		Notes:  notesFrom(d.Notes),
		Raw:    rawMsg,
	}, nil
}

// notesFrom lifts a RawNotes into the event's *Notes, returning nil when both
// fields are empty so the event omits an empty notes object. The seeder always
// stamps sku + gst_rate on payments/refunds/disputes, so in practice this
// returns a populated Notes for those events.
func notesFrom(n RawNotes) *Notes {
	if n.SKU == "" && n.GSTRate == "" {
		return nil
	}
	return &Notes{SKU: n.SKU, GSTRate: n.GSTRate}
}

// marshalRaw re-marshals a typed raw object to canonical JSON for the event's
// "raw" field. Using json.Marshal on the typed struct fixes key order from the
// struct tags (no map nondeterminism) and drops the source file's incidental
// whitespace, so the embedded raw — and therefore the whole journal — is
// byte-stable across runs (SPEC §12). money.Money marshals as its int64 paise,
// so amounts stay integer in the raw too (SPEC §1).
func marshalRaw(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// IngestAndNormalize is the convenience used by the orchestrator (SPEC §5):
// read the period's fixtures off disk and flatten them to the ordered event
// journal in one call. It returns both the Raw bundle (so the caller still has
// the bank feed and batch id lists for the reconcile stage) and the journal.
func IngestAndNormalize(root, world, period string) (Raw, []NormalizedEvent, error) {
	raw, err := Read(root, world, period)
	if err != nil {
		return Raw{}, nil, err
	}
	events, err := Normalize(raw)
	if err != nil {
		return Raw{}, nil, err
	}
	return raw, events, nil
}
