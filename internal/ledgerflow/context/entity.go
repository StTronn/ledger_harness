package flowcontext

import (
	"encoding/json"
	"strings"

	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// entity.go is the TIER-2 lookup behind `context entity <id>`: the agent's
// self-directed exploration surface. Where the Tier-1 bundles pre-solve the
// KNOWN problem classes, Entity lets the agent (or a human) fetch ANY object it
// can name — an event's raw snapshot, an order with its line items, a rate-card
// channel, an account balance — plus the cheap derived facts (booked, graph
// edges) only the graph can compute. Read-only over snapshots, like everything
// in the harness; routing is by id shape, one table (EntityKinds).

// EntityView is one resolved object: its kind, the raw snapshot (events), the
// typed views (orders / rate-card rows / balances), and the derived facts.
type EntityView struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// Events: whether an entry is posted for it, plus its graph edges
	// (order_id / payment_id) and the raw Razorpay object.
	Booked bool              `json:"booked,omitempty"`
	Edges  map[string]string `json:"edges,omitempty"`
	Object json.RawMessage   `json:"object,omitempty"`
	// Orders: the recovery view (authoritative rate + line items).
	Order *feeds.OrderInfo `json:"order,omitempty"`
	// Rate-card channels: the contracted row.
	RateCard *feeds.Channel `json:"ratecard,omitempty"`
	// Accounts: the period-end balance (paise, stated on the normal side).
	Balance *money.Money `json:"balance,omitempty"`
}

// EntityKinds documents the id shapes Entity resolves — the SAME table the CLI
// help and the agent's tool description list, exported so they cannot drift.
var EntityKinds = []string{
	"pay_…    payment (raw object + booked + order edge)",
	"rfnd_…   refund (raw object + booked + payment edge)",
	"setl_…   settlement (raw object + booked + batch members)",
	"disp_…   dispute (raw object + booked + payment edge)",
	"order_…  order (authoritative gst_rate + line items)",
	"ratecard/<channel>  contracted fee row",
	"<root>/<leaf> account path (period-end balance)",
}

// Entity resolves one id to its EntityView. ok is false for an id that names
// nothing in this period's snapshot.
func (m *Graph) Entity(id string) (EntityView, bool) {
	// Events (payment/refund/settlement/dispute) — the normalized journal carries
	// the original Razorpay object verbatim.
	if ev, ok := m.events[id]; ok {
		view := EntityView{
			Kind:   string(ev.Type),
			ID:     id,
			Booked: m.Booked(ev),
			Edges:  map[string]string{},
			Object: ev.Raw,
		}
		if ev.Links.OrderID != "" {
			view.Edges["order_id"] = ev.Links.OrderID
		}
		if ev.Links.PaymentID != "" {
			view.Edges["payment_id"] = ev.Links.PaymentID
		}
		if s, ok := m.settlements[id]; ok {
			view.Edges["payments"] = strings.Join(s.PaymentIDs, ",")
			view.Edges["refunds"] = strings.Join(s.RefundIDs, ",")
		}
		return view, true
	}

	// Orders — the recovery source view.
	if strings.HasPrefix(id, "order_") {
		if o, ok := m.orders[id]; ok {
			return EntityView{Kind: "order", ID: id, Order: &o}, true
		}
		return EntityView{}, false
	}

	// Rate-card channels: ratecard/<channel>.
	if name, found := strings.CutPrefix(id, "ratecard/"); found {
		rc, ok := m.RateCard()
		if !ok {
			return EntityView{}, false
		}
		ch, ok := rc.Channel(name)
		if !ok {
			return EntityView{}, false
		}
		return EntityView{Kind: "ratecard-channel", ID: id, RateCard: &ch}, true
	}

	// Account paths: anything with a slash that the chart knows.
	if strings.Contains(id, "/") && m.lg != nil {
		bal := m.lg.AccountBalance(id)
		if bal.Account == id {
			b := bal.Balance
			return EntityView{Kind: "account", ID: id, Balance: &b}, true
		}
	}

	return EntityView{}, false
}
