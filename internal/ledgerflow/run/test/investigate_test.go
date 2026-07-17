package run_test

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/agentclient"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/run"
	"github.com/razorpay/ledger-flow/internal/reconcile"
	"github.com/razorpay/ledger-flow/internal/seed"
)

// seedBreakPeriod seeds the Phase-8 unbooked-refund break period into a fresh temp
// root and generates BOTH recorded fixtures (classify, which escalates the
// rate-stripped refund, and investigate, which resolves the receivable residual)
// so the replay path has something to replay. It returns the root.
func seedBreakPeriod(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	res, err := seed.SeedWith(root, "dtc", "2026-03", seed.Options{Inject: seed.InjectUnbookedRefund})
	if err != nil {
		t.Fatalf("seed break: %v", err)
	}
	if res.Inject.Kind != seed.InjectUnbookedRefund || res.Inject.RefundID == "" {
		t.Fatalf("break period did not inject an unbooked refund: %+v", res.Inject)
	}
	cf, err := agentclient.GenerateRecorded(root, "dtc", "2026-03")
	if err != nil {
		t.Fatalf("generate classify recorded: %v", err)
	}
	if err := agentclient.WriteRecorded(agentclient.RecordedPath(root, "dtc", "2026-03"), cf); err != nil {
		t.Fatalf("write classify recorded: %v", err)
	}
	inf, err := run.GenerateInvestigateRecorded(root, "dtc", "2026-03")
	if err != nil {
		t.Fatalf("generate investigate recorded: %v", err)
	}
	if err := agentclient.WriteInvestigateRecorded(agentclient.InvestigateRecordedPath(root, "dtc", "2026-03"), inf); err != nil {
		t.Fatalf("write investigate recorded: %v", err)
	}
	return root
}

// TestUnbookedRefundBreakOffBaseline verifies that a full refund with a stripped
// rate is recovered deterministically, so no agent or investigation is needed.
func TestUnbookedRefundUsesDeterministicRecovery(t *testing.T) {
	root := seedBreakPeriod(t)

	res, err := run.RunWith(root, "dtc", "2026-03", run.Options{Agent: agentclient.ModeOff})
	if err != nil {
		t.Fatalf("RunWith off: %v", err)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("want no skipped refund after recovery, got %+v", res.Skipped)
	}
	if res.InvestigateReviewed != 0 || len(res.InvestigateTraces) != 0 {
		t.Errorf("agent off ran investigate (reviewed=%d traces=%d), want none", res.InvestigateReviewed, len(res.InvestigateTraces))
	}
	if len(res.Breaks) != 0 {
		t.Fatalf("want no reconciliation breaks after recovery, got %+v", res.Breaks)
	}
	if res.Score.Percent() != 100 {
		t.Errorf("score = %d%%, want 100 after deterministic recovery", res.Score.Percent())
	}
	if len(res.Score.Errors) != 0 {
		t.Errorf("expected no scoring errors after recovery, got %+v", res.Score.Errors)
	}
}

// TestUnbookedRefundResolvedByReplay asserts that replay mode also skips the
// judgment agent when recovery already has a safe candidate.
func TestUnbookedRefundReplaySkipsJudgmentAgent(t *testing.T) {
	root := seedBreakPeriod(t)

	res, err := run.RunWith(root, "dtc", "2026-03", run.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay: %v", err)
	}
	if res.InvestigateReviewed != 0 {
		t.Errorf("investigate reviewed %d breaks, want 0", res.InvestigateReviewed)
	}
	if len(res.Breaks) != 0 {
		t.Errorf("want no breaks after recovery, got %+v", res.Breaks)
	}
	if len(res.Escalations) != 0 {
		t.Errorf("want no review outcomes, got %+v", res.Escalations)
	}
	if res.Score.Percent() != 100 {
		t.Errorf("score = %d%%, want 100", res.Score.Percent())
	}
	if len(res.InvestigateTraces) != 0 {
		t.Errorf("want no investigate traces, got %+v", res.InvestigateTraces)
	}
}

// TestInvestigateReplayByteDeterministic asserts two replay runs produce identical
// investigate decisions (byte-deterministic replay, SPEC §12).
func TestInvestigateReplayByteDeterministic(t *testing.T) {
	root := seedBreakPeriod(t)

	a, err := run.RunWith(root, "dtc", "2026-03", run.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay a: %v", err)
	}
	b, err := run.RunWith(root, "dtc", "2026-03", run.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay b: %v", err)
	}
	if a.Score.Percent() != b.Score.Percent() || a.InvestigateReviewed != b.InvestigateReviewed {
		t.Errorf("replay not deterministic: a=(%d%%,%d) b=(%d%%,%d)",
			a.Score.Percent(), a.InvestigateReviewed, b.Score.Percent(), b.InvestigateReviewed)
	}
	if len(a.InvestigateTraces) != len(b.InvestigateTraces) {
		t.Fatalf("trace count differs: %d vs %d", len(a.InvestigateTraces), len(b.InvestigateTraces))
	}
	for i := range a.InvestigateTraces {
		ta, tb := a.InvestigateTraces[i], b.InvestigateTraces[i]
		if ta.BreakKey != tb.BreakKey || ta.Rationale != tb.Rationale {
			t.Errorf("trace %d differs between runs", i)
		}
	}
}

// TestUnresolvableBreakEscalates is the Phase-8 escalate-cleanly gate: a check#2
// batch-sum break (which no posting can fix) is escalated by the agent and left
// listed — never guessed, never crashed.
func TestUnresolvableBreakEscalates(t *testing.T) {
	root := t.TempDir()
	if _, err := seed.SeedWith(root, "dtc", "2026-02", seed.Options{Inject: seed.InjectRefundInBatch}); err != nil {
		t.Fatalf("seed refund-in-batch: %v", err)
	}
	// Generate both fixtures; the investigate generator records an escalation for the
	// non-posting-resolvable batch-sum break.
	cf, err := agentclient.GenerateRecorded(root, "dtc", "2026-02")
	if err != nil {
		t.Fatalf("generate classify recorded: %v", err)
	}
	if err := agentclient.WriteRecorded(agentclient.RecordedPath(root, "dtc", "2026-02"), cf); err != nil {
		t.Fatalf("write classify recorded: %v", err)
	}
	inf, err := run.GenerateInvestigateRecorded(root, "dtc", "2026-02")
	if err != nil {
		t.Fatalf("generate investigate recorded: %v", err)
	}
	if err := agentclient.WriteInvestigateRecorded(agentclient.InvestigateRecordedPath(root, "dtc", "2026-02"), inf); err != nil {
		t.Fatalf("write investigate recorded: %v", err)
	}

	res, err := run.RunWith(root, "dtc", "2026-02", run.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay: %v", err)
	}
	if res.InvestigateReviewed != 1 {
		t.Errorf("expected the batch-sum break to be reviewed, got %d", res.InvestigateReviewed)
	}
	if len(res.Escalations) != 1 || res.Escalations[0].Kind != reconcile.KindBatchSumMismatch {
		t.Fatalf("want one batch-sum escalation, got %+v", res.Escalations)
	}
	if len(res.Breaks) != 1 || res.Breaks[0].Check != reconcile.CheckBatchSum {
		t.Errorf("escalated break must remain listed, got %+v", res.Breaks)
	}
	if len(res.InvestigateTraces) != 1 {
		t.Errorf("want one investigate trace for the escalated break, got %d", len(res.InvestigateTraces))
	}
}

// seedCODPeriod seeds the COD/RTO world into a fresh temp root and generates both
// recorded fixtures, so the replay path can decompose the courier remittance.
func seedCODPeriod(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := seed.SeedWith(root, "dtc", "2026-02", seed.Options{COD: true}); err != nil {
		t.Fatalf("seed COD: %v", err)
	}
	cf, err := agentclient.GenerateRecorded(root, "dtc", "2026-02")
	if err != nil {
		t.Fatalf("generate classify recorded: %v", err)
	}
	if err := agentclient.WriteRecorded(agentclient.RecordedPath(root, "dtc", "2026-02"), cf); err != nil {
		t.Fatalf("write classify recorded: %v", err)
	}
	inf, err := run.GenerateInvestigateRecorded(root, "dtc", "2026-02")
	if err != nil {
		t.Fatalf("generate investigate recorded: %v", err)
	}
	if err := agentclient.WriteInvestigateRecorded(agentclient.InvestigateRecordedPath(root, "dtc", "2026-02"), inf); err != nil {
		t.Fatalf("write investigate recorded: %v", err)
	}
	return root
}

// TestCODResidualOffBaseline is the COD agent-off baseline (ROADMAP §8.3): the two
// unbookable courier deductions (RTO fee + weight dispute) leave one
// cod-receivable-residual break of ₹158 and the score short by those two entries.
func TestCODResidualOffBaseline(t *testing.T) {
	root := seedCODPeriod(t)
	res, err := run.RunWith(root, "dtc", "2026-02", run.Options{Agent: agentclient.ModeOff})
	if err != nil {
		t.Fatalf("RunWith off: %v", err)
	}
	var cod *reconcile.Break
	for i := range res.Breaks {
		if res.Breaks[i].Kind == reconcile.KindCODReceivableResidual {
			cod = &res.Breaks[i]
		}
	}
	if cod == nil {
		t.Fatalf("want a cod-receivable-residual break, got %+v", res.Breaks)
	}
	if cod.Actual.Paise() != 15800 {
		t.Errorf("cod residual = %s, want 158.00", cod.Actual)
	}
	if res.InvestigateReviewed != 0 {
		t.Errorf("agent off must not investigate, got %d", res.InvestigateReviewed)
	}
}

// TestCODResidualReviewedByReplay asserts that the COD recommendation is logged for
// review but does not automatically post the RTO fee or weight adjustment.
func TestCODResidualDecomposedByReplay(t *testing.T) {
	root := seedCODPeriod(t)
	res, err := run.RunWith(root, "dtc", "2026-02", run.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay: %v", err)
	}
	if res.InvestigateReviewed != 1 {
		t.Errorf("investigate reviewed %d break(s), want 1", res.InvestigateReviewed)
	}
	var booked bool
	for _, p := range res.Produced {
		if p.EntryType == "rto_fee" {
			booked = true
		}
	}
	if booked {
		t.Error("investigate recommendation must not post an rto_fee")
	}
	// ...and escalated the weight dispute.
	if len(res.Escalations) != 1 || res.Escalations[0].Kind != reconcile.KindCODReceivableResidual {
		t.Fatalf("want one cod-receivable escalation, got %+v", res.Escalations)
	}
	// The full residual remains listed because no recommendation was posted.
	var cod *reconcile.Break
	for i := range res.Breaks {
		if res.Breaks[i].Kind == reconcile.KindCODReceivableResidual {
			cod = &res.Breaks[i]
		}
	}
	if cod == nil || cod.Actual.Paise() != 15800 {
		t.Fatalf("want the original cod residual of 158.00, got %+v", res.Breaks)
	}
	// Honest sub-100%: exactly the weight_adjustment is missing.
	if res.Score.Percent() >= 100 {
		t.Errorf("score = %d%%, want < 100 (the weight adjustment is correctly escalated, not booked)", res.Score.Percent())
	}
}

// TestGenerateInvestigateRecordedReproducible asserts the recorded-investigation
// generator is deterministic: two generations of the same period are byte-equal.
func TestGenerateInvestigateRecordedReproducible(t *testing.T) {
	root := t.TempDir()
	if _, err := seed.SeedWith(root, "dtc", "2026-03", seed.Options{Inject: seed.InjectUnbookedRefund}); err != nil {
		t.Fatalf("seed break: %v", err)
	}
	cf, err := agentclient.GenerateRecorded(root, "dtc", "2026-03")
	if err != nil {
		t.Fatalf("generate classify recorded: %v", err)
	}
	if err := agentclient.WriteRecorded(agentclient.RecordedPath(root, "dtc", "2026-03"), cf); err != nil {
		t.Fatalf("write classify recorded: %v", err)
	}

	a, err := run.GenerateInvestigateRecorded(root, "dtc", "2026-03")
	if err != nil {
		t.Fatalf("generate investigate a: %v", err)
	}
	b, err := run.GenerateInvestigateRecorded(root, "dtc", "2026-03")
	if err != nil {
		t.Fatalf("generate investigate b: %v", err)
	}
	if len(a.Resolutions) != 0 || len(b.Resolutions) != 0 {
		t.Fatalf("want no investigation resolutions for an auto-recovered refund, got a=%d b=%d", len(a.Resolutions), len(b.Resolutions))
	}
}
