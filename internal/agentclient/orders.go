package agentclient

import (
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// orders.go re-exports the orders recovery source for this package's consumers.
// The CANONICAL reader lives in internal/harness/feeds — one reader per feed, so
// the on-disk shape cannot drift between the recorded-response generator, the
// ledger graph, and the orchestrator. These wrappers keep agentclient's existing
// surface (OrderGSTRates / OrderInfos) stable for its callers. Nothing here is
// ever ground truth (SPEC §4.4).

// OrderItem mirrors feeds.OrderItem for agentclient's exported surface.
type OrderItem = feeds.OrderItem

// OrderInfo mirrors feeds.OrderInfo for agentclient's exported surface.
type OrderInfo = feeds.OrderInfo

// OrderInfos loads orders.json for (world, period) under root: the order-level
// recovery view (rate + line items). Delegates to the canonical feeds reader.
func OrderInfos(root, world, period string) (map[string]OrderInfo, error) {
	return feeds.Orders(root, world, period)
}

// OrderGSTRates loads orders.json and returns order id -> authoritative
// gst_rate string — the legitimate, snapshotted tax-metadata recovery source
// (SPEC §1, §2). Delegates to the canonical feeds reader.
func OrderGSTRates(root, world, period string) (map[string]string, error) {
	infos, err := feeds.Orders(root, world, period)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(infos))
	for id, o := range infos {
		m[id] = o.GSTRate
	}
	return m, nil
}
