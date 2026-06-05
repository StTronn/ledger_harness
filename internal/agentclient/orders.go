package agentclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/razorpay/close-agent/internal/money"
)

// orders.go reads orders.json — the LEGITIMATE, snapshotted recovery source the
// agent "fetches the order" from (SPEC §1, §2) — and NOTHING from truth (SPEC
// §4.4). It mirrors the on-disk order shape (the seeder's seed.Order / a Razorpay
// order object) as a local type so agentclient does not depend on internal/seed,
// and indexes orders by id for recovery.

// order is the subset of a Razorpay-shaped order this package needs to recover a
// payment's tax metadata: the order id, its amount, and its authoritative notes
// (sku + gst_rate). Unknown fields in the file are tolerated so a richer order
// object still decodes.
type order struct {
	ID     string      `json:"id"`
	Amount money.Money `json:"amount"`
	Notes  orderNotes  `json:"notes"`
}

// orderNotes is an order's authoritative notes: the product SKU and the GST rate
// percent as a string ("18", "12", "5"). The order keeps the TRUE rate even after
// a payment's own notes have been stripped, which is what makes recovery possible.
type orderNotes struct {
	SKU     string `json:"sku"`
	GSTRate string `json:"gst_rate"`
}

// ordersIndex maps order id -> order for O(1) recovery lookups.
type ordersIndex map[string]order

// readOrders loads worlds/<world>/<period>/razorpay/orders.json under root and
// indexes it by order id. orders.json is an agent INPUT (under razorpay/), never
// ground truth. A missing or malformed file is a clear error: the recovery source
// is required to generate the recorded responses, so its absence must fail loudly
// rather than silently recover nothing.
func readOrders(root, world, period string) (ordersIndex, error) {
	path := filepath.Join(root, "worlds", world, period, "razorpay", "orders.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("agentclient: orders recovery source %s not found", path)
		}
		return nil, fmt.Errorf("agentclient: read %s: %w", path, err)
	}
	var orders []order
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&orders); err != nil {
		return nil, fmt.Errorf("agentclient: decode %s: %w", path, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("agentclient: %s has trailing data after the JSON value", path)
	}
	idx := make(ordersIndex, len(orders))
	for _, o := range orders {
		if _, dup := idx[o.ID]; !dup {
			idx[o.ID] = o
		}
	}
	return idx, nil
}
