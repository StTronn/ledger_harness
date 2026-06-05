package closer_test

import (
	"testing"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/closer"
	"github.com/razorpay/close-agent/internal/reconcile"
	"github.com/razorpay/close-agent/internal/seed"
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
	inf, err := closer.GenerateInvestigateRecorded(root, "dtc", "2026-03")
	if err != nil {
		t.Fatalf("generate investigate recorded: %v", err)
	}
	if err := agentclient.WriteInvestigateRecorded(agentclient.InvestigateRecordedPath(root, "dtc", "2026-03"), inf); err != nil {
		t.Fatalf("write investigate recorded: %v", err)
	}
	return root
}

// TestUnbookedRefundBreakOffBaseline is the Phase-8 agent-off baseline: the
// rate-stripped refund is skipped (unbooked), so the score is PARTIAL and exactly
// one check#3 receivable-residual break surfaces. No investigate runs with the
// agent off — the break is listed, never resolved or guessed.
func TestUnbookedRefundBreakOffBaseline(t *testing.T) {
	root := seedBreakPeriod(t)

	res, err := closer.RunWith(root, "dtc", "2026-03", closer.Options{Agent: agentclient.ModeOff})
	if err != nil {
		t.Fatalf("RunWith off: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Type != "refund" {
		t.Fatalf("want exactly one skipped refund, got %+v", res.Skipped)
	}
	if res.InvestigateDone != 0 || len(res.InvestigateTraces) != 0 {
		t.Errorf("agent off ran investigate (done=%d traces=%d), want none", res.InvestigateDone, len(res.InvestigateTraces))
	}
	if len(res.Breaks) != 1 || res.Breaks[0].Check != reconcile.CheckReceivableClears {
		t.Fatalf("want one check#3 receivable-residual break, got %+v", res.Breaks)
	}
	if res.Score.Percent() >= 100 {
		t.Errorf("score = %d%%, want < 100 (the refund_reversal is unbooked)", res.Score.Percent())
	}
	// The missing entry is the unbooked refund.
	foundMissing := false
	for _, e := range res.Score.Errors {
		if e.EventID == res.Skipped[0].EventID && e.Class == "missing" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("expected a 'missing' scoring error for the unbooked refund %s", res.Skipped[0].EventID)
	}
}

// TestUnbookedRefundResolvedByReplay is the Phase-8 resolution gate: the §8
// investigate agent (replay) books the missing refund_reversal, the receivable
// clears (0 breaks), the trial balance matches truth, the score rises to 100%, and
// an investigate trace is emitted.
func TestUnbookedRefundResolvedByReplay(t *testing.T) {
	root := seedBreakPeriod(t)

	res, err := closer.RunWith(root, "dtc", "2026-03", closer.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay: %v", err)
	}
	if res.InvestigateDone != 1 {
		t.Errorf("investigate resolved %d breaks, want 1", res.InvestigateDone)
	}
	if len(res.Breaks) != 0 {
		t.Errorf("want 0 breaks after investigate, got %+v", res.Breaks)
	}
	if len(res.Escalations) != 0 {
		t.Errorf("want 0 escalations, got %+v", res.Escalations)
	}
	if !res.Score.TrialBalanceMatches {
		t.Errorf("trial balance does not match truth after investigate")
	}
	if res.Score.Percent() != 100 {
		t.Errorf("score = %d%%, want 100 after the investigate resolution", res.Score.Percent())
	}
	if len(res.InvestigateTraces) != 1 || res.InvestigateTraces[0].SchemaVersion != agentclient.InvestigateTraceSchemaVersion {
		t.Errorf("want one frozen investigate trace, got %+v", res.InvestigateTraces)
	}
}

// TestInvestigateReplayByteDeterministic asserts two replay runs produce identical
// investigate decisions (byte-deterministic replay, SPEC §12).
func TestInvestigateReplayByteDeterministic(t *testing.T) {
	root := seedBreakPeriod(t)

	a, err := closer.RunWith(root, "dtc", "2026-03", closer.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay a: %v", err)
	}
	b, err := closer.RunWith(root, "dtc", "2026-03", closer.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay b: %v", err)
	}
	if a.Score.Percent() != b.Score.Percent() || a.InvestigateDone != b.InvestigateDone {
		t.Errorf("replay not deterministic: a=(%d%%,%d) b=(%d%%,%d)",
			a.Score.Percent(), a.InvestigateDone, b.Score.Percent(), b.InvestigateDone)
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
	inf, err := closer.GenerateInvestigateRecorded(root, "dtc", "2026-02")
	if err != nil {
		t.Fatalf("generate investigate recorded: %v", err)
	}
	if err := agentclient.WriteInvestigateRecorded(agentclient.InvestigateRecordedPath(root, "dtc", "2026-02"), inf); err != nil {
		t.Fatalf("write investigate recorded: %v", err)
	}

	res, err := closer.RunWith(root, "dtc", "2026-02", closer.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay: %v", err)
	}
	if res.InvestigateDone != 0 {
		t.Errorf("a batch-sum break must not be resolved by posting, got InvestigateDone=%d", res.InvestigateDone)
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

	a, err := closer.GenerateInvestigateRecorded(root, "dtc", "2026-03")
	if err != nil {
		t.Fatalf("generate investigate a: %v", err)
	}
	b, err := closer.GenerateInvestigateRecorded(root, "dtc", "2026-03")
	if err != nil {
		t.Fatalf("generate investigate b: %v", err)
	}
	if len(a.Resolutions) != 1 || len(b.Resolutions) != 1 {
		t.Fatalf("want one resolution each, got a=%d b=%d", len(a.Resolutions), len(b.Resolutions))
	}
	pa, pb := a.Resolutions[0], b.Resolutions[0]
	if pa.BreakKey != pb.BreakKey || len(pa.Resolution) != len(pb.Resolution) {
		t.Fatalf("generator not deterministic across runs")
	}
	if len(pa.Resolution) != 1 || pa.Resolution[0].EntryType != "refund_reversal" {
		t.Errorf("expected a single refund_reversal posting, got %+v", pa.Resolution)
	}
}
