package run_test

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/agentclient"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/run"
	"github.com/razorpay/ledger-flow/internal/seed"
)

// TestPartialRefundRoutesToJudgmentAgent protects the other side of the
// recovery switch: line-item evidence is useful, but partial-refund policy
// still requires judgment and must not auto-post the refund.
func TestPartialRefundRoutesToJudgmentAgent(t *testing.T) {
	root := t.TempDir()
	result, err := seed.SeedWith(root, "dtc", "2026-01", seed.Options{PartialRefunds: true})
	if err != nil {
		t.Fatalf("seed partial period: %v", err)
	}
	if len(result.Partial.Refunds) == 0 {
		t.Fatal("seed produced no partial refunds")
	}

	classified, err := agentclient.GenerateRecorded(root, "dtc", "2026-01")
	if err != nil {
		t.Fatalf("generate classify recording: %v", err)
	}
	if err := agentclient.WriteRecorded(agentclient.RecordedPath(root, "dtc", "2026-01"), classified); err != nil {
		t.Fatalf("write classify recording: %v", err)
	}
	investigated, err := run.GenerateInvestigateRecorded(root, "dtc", "2026-01")
	if err != nil {
		t.Fatalf("generate investigate recording: %v", err)
	}
	if err := agentclient.WriteInvestigateRecorded(agentclient.InvestigateRecordedPath(root, "dtc", "2026-01"), investigated); err != nil {
		t.Fatalf("write investigate recording: %v", err)
	}

	res, err := run.RunWith(root, "dtc", "2026-01", run.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("run partial period: %v", err)
	}
	if res.AgentReviewed == 0 || len(res.Traces) == 0 {
		t.Fatalf("partial refunds did not reach the judgment agent: reviewed=%d traces=%d", res.AgentReviewed, len(res.Traces))
	}
	if len(res.Skipped) == 0 {
		t.Fatal("partial refunds were posted automatically; expected review-only skips")
	}
}
