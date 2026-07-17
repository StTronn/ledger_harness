package run

import (
	"github.com/razorpay/ledger-flow/internal/agentclient"
	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/recovery"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// recovery.go assembles the read-only context projection for a period. It joins
// the posted ledger, reconciliation breaks, raw fixtures, and order recovery
// source. It reads no truth data.

// BuildRecoveryEngine runs the deterministic flow for (world, period) under root
// and projects it into a recovery.Engine containing the posted ledger, breaks,
// normalized events, raw fixtures, and order recovery source.
//
// A missing orders.json is tolerated (recovery is simply unavailable for that
// period): a clean period need not ship one. Any other ingest/close error is
// returned.
func BuildRecoveryEngine(root, world, period string) (*recovery.Engine, error) {
	res, err := Run(root, world, period)
	if err != nil {
		return nil, err
	}
	raw, events, err := ingest.IngestAndNormalize(root, world, period)
	if err != nil {
		return nil, err
	}
	// orders.json is the legitimate recovery source; its absence is not fatal.
	// Types are shared with the feeds layer (aliased), so no conversion.
	orders, err := agentclient.OrderInfos(root, world, period)
	if err != nil {
		orders = nil
	}
	// The contracted rate card (fee-tier policy's validation table); periods
	// seeded before it existed simply don't get the fee check.
	var ratecard *feeds.RateCardFile
	if rc, rcErr := feeds.RateCard(root, world, period); rcErr == nil {
		ratecard = &rc
	}
	return recovery.New(res.Ledger, events, raw, orders, res.Breaks, ratecard), nil
}
