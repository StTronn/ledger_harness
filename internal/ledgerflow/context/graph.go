// Package readmodel is the read-only projection of one period's close — the
// "close graph" — that backs the agent harness (the Tier-1 context bundlers, the
// `close` read CLI, and the read-API the live agent's tools call). It joins the
// data the close already produces (the normalized events, the posted ledger, the
// raw Razorpay fixtures, the orders gst-rate recovery source, and the reconcile
// breaks) and precomputes the two derived facts the agent needs but cannot cheaply
// reconstruct itself:
//
//   - Booked — is an entry already posted for this event? Computed via the canonical
//     posting.IKFor scheme against the posted ledger's IKs, so it agrees with what
//     was posted by construction (no heuristic matching).
//   - RecoveredGSTRate — the order's authoritative gst_rate, surfaced for an event
//     whose own rate was stripped (the legitimate "fetch the order" recovery).
//
// # Boundaries (SPEC §4.4, §12)
//
// readmodel reads NO truth. Its inputs are all agent-input or ledger-derived: the
// raw fixtures (razorpay/), the posted ledger, the orders recovery source, and the
// reconcile breaks. The truth-isolation guard enforces this at the package level.
// It is a pure projection: given the same inputs it answers identically.
package flowcontext

import (
	"sort"

	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledger"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/posting"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/recovery/policychecks"
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/reconcile"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// Model is the projected close graph for one period. It is read-only and holds no
// pointers into mutable state except the posted ledger, which it only queries.
type Graph struct {
	lg            *ledger.Ledger
	events        map[string]ingest.NormalizedEvent
	bookedIK      map[string]struct{}             // the set of IKs posted in lg
	raw           ingest.Raw                      // the raw fixtures (for graph edges)
	paymentOrder  map[string]string               // payment id -> order id
	payAmount     map[string]money.Money          // payment id -> gross (paise)
	refundPayment map[string]string               // refund id -> payment id
	orders        map[string]OrderInfo            // order id -> authoritative rate + line items
	breaks        map[string]reconcile.Break      // break key -> break
	settlements   map[string]ingest.RawSettlement // settlement id -> raw settlement
	ratecard      *feeds.RateCardFile             // contracted fee schedule (nil when absent)
	policies      policychecks.Registry           // the recovery/verification policy table
}

// OrderItem / OrderInfo are the order-level recovery views, defined ONCE in the
// feeds layer (internal/harness/feeds) and aliased here so graph callers and the
// canonical reader can never drift.
type (
	OrderItem = feeds.OrderItem
	OrderInfo = feeds.OrderInfo
)

// New builds the projection from the close's outputs. events is the normalized
// event journal, lg the posted ledger, raw the source fixtures, orders the
// recovery source (order id -> rate + line items, e.g. from
// agentclient.OrderInfos), and breaks the reconcile breaks. Any of them may be
// empty/nil; the model answers accordingly. Indexes are built once here so every
// lookup is O(1).
func New(lg *ledger.Ledger, events []ingest.NormalizedEvent, raw ingest.Raw, orders map[string]OrderInfo, breaks []reconcile.Break) *Graph {
	m := &Graph{
		lg:            lg,
		events:        make(map[string]ingest.NormalizedEvent, len(events)),
		bookedIK:      make(map[string]struct{}),
		raw:           raw,
		paymentOrder:  make(map[string]string, len(raw.Payments)),
		payAmount:     make(map[string]money.Money, len(raw.Payments)),
		refundPayment: make(map[string]string, len(raw.Refunds)),
		orders:        orders,
		breaks:        make(map[string]reconcile.Break, len(breaks)),
		settlements:   make(map[string]ingest.RawSettlement, len(raw.Settlements)),
		policies:      policychecks.Default(),
	}
	for _, ev := range events {
		m.events[ev.ID] = ev
	}
	if lg != nil {
		for _, e := range lg.Entries() {
			m.bookedIK[e.IK] = struct{}{}
		}
	}
	for _, p := range raw.Payments {
		m.paymentOrder[p.ID] = p.OrderID
		m.payAmount[p.ID] = p.Amount
	}
	for _, r := range raw.Refunds {
		m.refundPayment[r.ID] = r.PaymentID
	}
	for _, b := range breaks {
		m.breaks[b.Key()] = b
	}
	for _, s := range raw.Settlements {
		m.settlements[s.ID] = s
	}
	return m
}

// Booked reports whether an entry has already been posted for ev — i.e. whether the
// canonical IK for (ev.Type, ev.ID) appears in the posted ledger. This is the
// derived fact that makes "settled-but-not-booked" visible: a rule-missed event
// that was never booked reports false. It uses posting.IKFor so the key is exactly
// the one the posters use.
func (m *Graph) Booked(ev ingest.NormalizedEvent) bool {
	_, ok := m.bookedIK[posting.IKFor(ev.Type, ev.ID)]
	return ok
}

// Event returns the normalized event with the given id, if present.
func (m *Graph) Event(id string) (ingest.NormalizedEvent, bool) {
	ev, ok := m.events[id]
	return ev, ok
}

// OrderIDForPayment returns the order id a payment belongs to (the payment->order
// edge), from the raw fixtures.
func (m *Graph) OrderIDForPayment(paymentID string) (string, bool) {
	id, ok := m.paymentOrder[paymentID]
	return id, ok
}

// PaymentIDForRefund returns the payment id a refund reverses (the refund->payment
// edge), from the raw fixtures. Combined with OrderIDForPayment it walks a refund to
// its parent order for rate recovery.
func (m *Graph) PaymentIDForRefund(refundID string) (string, bool) {
	id, ok := m.refundPayment[refundID]
	return id, ok
}

// RecoveredGSTRate returns an order's authoritative gst_rate string ("18", "12",
// "5"), the legitimate recovery source for an event whose own rate was stripped
// (SPEC §1, §2). ok is false when the order is unknown or carries no rate.
func (m *Graph) RecoveredGSTRate(orderID string) (string, bool) {
	o, ok := m.orders[orderID]
	if !ok || o.GSTRate == "" {
		return "", false
	}
	return o.GSTRate, true
}

// OrderItems returns an order's line items (empty outside the partial-refunds
// world).
func (m *Graph) OrderItems(orderID string) []OrderItem {
	return m.orders[orderID].Items
}

// OrderInfo returns the order-level recovery view (the policychecks.Graph lens).
func (m *Graph) OrderInfo(orderID string) (feeds.OrderInfo, bool) {
	o, ok := m.orders[orderID]
	return o, ok
}

// RateCard returns the period's contracted fee schedule, when snapshotted (the
// policychecks.Graph lens). ok is false for periods without a ratecard.json.
func (m *Graph) RateCard() (feeds.RateCardFile, bool) {
	if m.ratecard == nil {
		return feeds.RateCardFile{}, false
	}
	return *m.ratecard, true
}

// WithRateCard attaches the period's rate card to the graph (builder-style so
// New's signature stays put). Returns the graph for chaining.
func (m *Graph) WithRateCard(rc feeds.RateCardFile) *Graph {
	m.ratecard = &rc
	return m
}

// SettlementMembers returns a settlement's batch member ids (the
// policychecks.Graph lens).
func (m *Graph) SettlementMembers(settlementID string) (paymentIDs, refundIDs []string, ok bool) {
	s, found := m.settlements[settlementID]
	if !found {
		return nil, nil, false
	}
	return s.PaymentIDs, s.RefundIDs, true
}

// PaymentAmount returns a payment's gross (the policychecks.Graph lens).
func (m *Graph) PaymentAmount(paymentID string) (money.Money, bool) {
	a, ok := m.payAmount[paymentID]
	return a, ok
}

// RemittanceDeductions returns a COD remittance's deduction lines, each tagged
// with the concerned shipment's lifecycle status (the policychecks.Graph lens for
// the rto-fee policy). ok is false when the remittance is unknown.
func (m *Graph) RemittanceDeductions(remittanceID string) ([]policychecks.Deduction, bool) {
	status := make(map[string]string, len(m.raw.CourierFeed.Shipments))
	for _, s := range m.raw.CourierFeed.Shipments {
		status[s.ID] = s.Status
	}
	for _, rm := range m.raw.CourierFeed.Remittances {
		if rm.ID != remittanceID {
			continue
		}
		out := make([]policychecks.Deduction, 0, len(rm.Deductions))
		for _, d := range rm.Deductions {
			out = append(out, policychecks.Deduction{
				ID:             d.ID,
				Code:           d.Code,
				ShipmentID:     d.ShipmentID,
				Amount:         d.Amount,
				ShipmentStatus: status[d.ShipmentID],
			})
		}
		return out, true
	}
	return nil, false
}

// CourierChannel returns the COD courier channel name (the policychecks.Graph
// lens for the rate-card lookup). ok is false for a Razorpay-only period.
func (m *Graph) CourierChannel() (string, bool) {
	if m.raw.CourierFeed.Channel == "" {
		return "", false
	}
	return m.raw.CourierFeed.Channel, true
}

// Break returns the reconcile break with the given canonical key (reconcile.Break.Key),
// if present.
func (m *Graph) Break(key string) (reconcile.Break, bool) {
	b, ok := m.breaks[key]
	return b, ok
}

// BreakKeys returns the canonical keys of all breaks in the projection, sorted for
// deterministic iteration (so the CLI and the agent harness list breaks stably).
func (m *Graph) BreakKeys() []string {
	keys := make([]string, 0, len(m.breaks))
	for k := range m.breaks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// AccountBalance returns the period-end balance of a chart account, stated on its
// normal side (a passthrough to the ledger so callers need not hold the ledger).
func (m *Graph) AccountBalance(account string) money.Money {
	if m.lg == nil {
		return money.FromPaise(0)
	}
	return m.lg.AccountBalance(account).Balance
}
