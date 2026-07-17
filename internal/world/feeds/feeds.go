// Package feeds is the SOURCES layer of the agent harness
// (internal/harness): the canonical readers for the snapshotted, agent-input
// fixtures a period ships — orders.json (authoritative tax metadata + line
// items) and ratecard.json (the merchant's contracted fee schedule). Policy
// checks (internal/harness/policychecks) recover facts FROM these feeds via the
// ledger graph; nothing here is ever ground truth (truth/gl.json is scorer-only
// and unreachable from the harness — the isolation guard enforces it).
//
// One reader per feed, defined once: every consumer (the ledger graph, the
// recorded-response generator, the CLI) reads through this package, so a feed's
// shape cannot drift between consumers.
package feeds

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/razorpay/ledger-flow/internal/money"
)

// OrderItem is one line item of an order: what was sold, for how much (paise,
// GST-inclusive), at which rate. Present only in worlds seeded with line items.
type OrderItem struct {
	SKU     string      `json:"sku"`
	Amount  money.Money `json:"amount"`
	GSTRate string      `json:"gst_rate"`
}

// OrderInfo is the order-level recovery view: the authoritative gst_rate plus
// the line items (empty when the world has none).
type OrderInfo struct {
	GSTRate string
	Items   []OrderItem
}

// order mirrors the on-disk Razorpay-shaped order; unknown fields are tolerated.
type order struct {
	ID    string `json:"id"`
	Notes struct {
		GSTRate string `json:"gst_rate"`
	} `json:"notes"`
	Items []OrderItem `json:"items,omitempty"`
}

// Orders loads worlds/<world>/<period>/razorpay/orders.json under root and
// indexes it by order id. A missing or malformed file is a loud error — the
// recovery source is required wherever recovery is attempted.
func Orders(root, world, period string) (map[string]OrderInfo, error) {
	var orders []order
	if err := readJSON(filepath.Join(root, "worlds", world, period, "razorpay", "orders.json"), &orders); err != nil {
		return nil, err
	}
	m := make(map[string]OrderInfo, len(orders))
	for _, o := range orders {
		if _, dup := m[o.ID]; !dup {
			m[o.ID] = OrderInfo{GSTRate: o.Notes.GSTRate, Items: o.Items}
		}
	}
	return m, nil
}

// Channel is one contracted channel row of the rate card: the platform fee in
// basis points of gross, and the GST rate applied to that fee. A COURIER channel
// (the COD rail, ROADMAP §8.3) additionally contracts a flat per-shipment COD
// collection fee and a flat return-to-origin fee, both in paise and both taxed at
// FeeGSTRate; the omitempty COD fields stay absent on a Razorpay channel, so
// existing rate cards are byte-unchanged.
type Channel struct {
	Channel     string `json:"channel"`
	FeeBps      int    `json:"fee_bps"`
	FeeGSTRate  int    `json:"fee_gst_rate"`
	CODFeePaise int64  `json:"cod_fee_paise,omitempty"` // flat COD collection fee per shipment
	RTOFeePaise int64  `json:"rto_fee_paise,omitempty"` // flat return-to-origin fee per RTO
}

// RateCardFile is the on-disk rate card: the merchant's contracted fee schedule
// per channel (SPEC v1.5 fee-audit seam). It is an agent-input snapshot like
// orders.json — the validation table the fee-tier policy checks settlements
// against — and never ground truth.
type RateCardFile struct {
	SchemaVersion int       `json:"schema_version"`
	Channels      []Channel `json:"channels"`
}

// Channel returns the contracted row for a channel name, if present.
func (rc RateCardFile) Channel(name string) (Channel, bool) {
	for _, c := range rc.Channels {
		if c.Channel == name {
			return c, true
		}
	}
	return Channel{}, false
}

// RateCard loads worlds/<world>/<period>/ratecard.json under root.
func RateCard(root, world, period string) (RateCardFile, error) {
	var rc RateCardFile
	if err := readJSON(filepath.Join(root, "worlds", world, period, "ratecard.json"), &rc); err != nil {
		return RateCardFile{}, err
	}
	return rc, nil
}

// readJSON reads one fixture file into v, rejecting trailing data so a corrupt
// snapshot fails loudly rather than half-parsing.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("feeds: snapshot %s not found", path)
		}
		return fmt.Errorf("feeds: read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("feeds: decode %s: %w", path, err)
	}
	if dec.More() {
		return fmt.Errorf("feeds: %s has trailing data after the JSON value", path)
	}
	return nil
}
