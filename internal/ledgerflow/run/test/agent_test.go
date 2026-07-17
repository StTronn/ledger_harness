package run_test

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/agentclient"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/run"
	"github.com/razorpay/ledger-flow/internal/seed"
)

// seedHardPeriod seeds the hard missing-metadata period (dtc/2026-04 with the
// ambiguity transform) into a fresh temp root and ALSO generates the committed-
// style recorded-response fixture from orders.json, so the replay path has
// something to replay. It returns the root and the number of stripped payments.
func seedHardPeriod(t *testing.T) (string, int) {
	t.Helper()
	root := t.TempDir()
	res, err := seed.SeedWith(root, "dtc", "2026-04", seed.Options{Ambiguity: true})
	if err != nil {
		t.Fatalf("seed hard: %v", err)
	}
	if res.Ambiguity.NumStripped == 0 {
		t.Fatal("hard period stripped no gst_rate")
	}
	f, err := agentclient.GenerateRecorded(root, "dtc", "2026-04")
	if err != nil {
		t.Fatalf("generate recorded: %v", err)
	}
	if err := agentclient.WriteRecorded(agentclient.RecordedPath(root, "dtc", "2026-04"), f); err != nil {
		t.Fatalf("write recorded: %v", err)
	}
	inf, err := run.GenerateInvestigateRecorded(root, "dtc", "2026-04")
	if err != nil {
		t.Fatalf("generate investigate recorded: %v", err)
	}
	if err := agentclient.WriteInvestigateRecorded(agentclient.InvestigateRecordedPath(root, "dtc", "2026-04"), inf); err != nil {
		t.Fatalf("write investigate recorded: %v", err)
	}
	return root, res.Ambiguity.NumStripped
}

// TestAgentOffUsesDeterministicRecovery asserts that missing GST rates are
// recovered and posted without consulting the judgment agent.
func TestAgentOffUsesDeterministicRecovery(t *testing.T) {
	root, stripped := seedHardPeriod(t)

	res, err := run.RunWith(root, "dtc", "2026-04", run.Options{Agent: agentclient.ModeOff})
	if err != nil {
		t.Fatalf("RunWith off: %v", err)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("skipped = %d, want 0 (recovery should post %d stripped payments)", len(res.Skipped), stripped)
	}
	if res.AgentReviewed != 0 {
		t.Errorf("agent reviewed %d events with the agent off, want 0", res.AgentReviewed)
	}
	if len(res.Traces) != 0 {
		t.Errorf("agent off emitted %d traces, want 0", len(res.Traces))
	}
	if res.Score.Percent() != 100 {
		t.Errorf("agent-off score = %d%%, want 100%%", res.Score.Percent())
	}
	if len(res.Breaks) != 0 {
		t.Errorf("agent-off run left %d reconcile breaks after recovery", len(res.Breaks))
	}
}

// TestAgentReplaySkipsSafeRecovery asserts that replay mode does not consult
// the judgment agent when recovery has a safe deterministic candidate.
func TestAgentReplaySkipsSafeRecovery(t *testing.T) {
	root, _ := seedHardPeriod(t)

	res, err := run.RunWith(root, "dtc", "2026-04", run.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay: %v", err)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("skipped = %d, want 0", len(res.Skipped))
	}
	if res.AgentReviewed != 0 {
		t.Errorf("agent reviewed %d events, want 0 for safe recovery", res.AgentReviewed)
	}
	if res.Score.Percent() != 100 {
		t.Errorf("agent-replay score = %d%%, want 100%%", res.Score.Percent())
	}
	if len(res.Breaks) != 0 {
		t.Errorf("agent-replay left %d reconcile breaks", len(res.Breaks))
	}
	if len(res.Traces) != 0 {
		t.Fatalf("emitted %d traces, want 0 for safe recovery", len(res.Traces))
	}
	if res.TracePath != "" {
		t.Errorf("agent-replay wrote a trace for safe recovery: %s", res.TracePath)
	}
}

// TestAgentReplayByteDeterministic asserts two replay closes over the same
// fixtures produce identical results — same classified count, same agent count,
// same produced entries, same score (SPEC §5, §12).
func TestAgentReplayByteDeterministic(t *testing.T) {
	root, _ := seedHardPeriod(t)

	a, err := run.RunWith(root, "dtc", "2026-04", run.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("run a: %v", err)
	}
	b, err := run.RunWith(root, "dtc", "2026-04", run.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("run b: %v", err)
	}
	if a.Classified != b.Classified || a.AgentReviewed != b.AgentReviewed || len(a.Produced) != len(b.Produced) {
		t.Fatalf("counts differ between replay runs: %+v vs %+v", a, b)
	}
	for i := range a.Produced {
		pa, pb := a.Produced[i], b.Produced[i]
		if pa.EventID != pb.EventID || pa.EntryType != pb.EntryType || pa.TxID != pb.TxID {
			t.Fatalf("produced[%d] differs: %+v vs %+v", i, pa, pb)
		}
		if len(pa.Lines) != len(pb.Lines) {
			t.Fatalf("produced[%d] line count differs", i)
		}
		for j := range pa.Lines {
			if pa.Lines[j] != pb.Lines[j] {
				t.Fatalf("produced[%d] line %d differs: %+v vs %+v", i, j, pa.Lines[j], pb.Lines[j])
			}
		}
	}
	if a.Score.Percent() != b.Score.Percent() {
		t.Fatalf("score differs: %d vs %d", a.Score.Percent(), b.Score.Percent())
	}
	if len(a.Traces) != len(b.Traces) {
		t.Fatalf("trace count differs: %d vs %d", len(a.Traces), len(b.Traces))
	}
	for i := range a.Traces {
		if a.Traces[i].EventID != b.Traces[i].EventID {
			t.Fatalf("trace[%d] event id differs: %q vs %q", i, a.Traces[i].EventID, b.Traces[i].EventID)
		}
	}
}

// TestAgentReplayCleanPeriodNeedsNoFixture asserts a CLEAN period (no rule misses)
// runs in --agent replay WITHOUT a recorded fixture present — the agent is lazily
// built and never consulted — and still scores 100% with no traces. This is the
// gate's "2026-05 scores 100% both ways" at the package level.
func TestAgentReplayCleanPeriodNeedsNoFixture(t *testing.T) {
	root := t.TempDir()
	if _, err := seed.Seed(root, "dtc", "2026-05"); err != nil {
		t.Fatalf("seed clean: %v", err)
	}
	// Note: no recorded fixture is written for the clean period.

	res, err := run.RunWith(root, "dtc", "2026-05", run.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay on clean period: %v", err)
	}
	if len(res.Skipped) != 0 || res.AgentReviewed != 0 || len(res.Traces) != 0 {
		t.Errorf("clean replay consulted the agent: skipped=%d reviewed=%d traces=%d",
			len(res.Skipped), res.AgentReviewed, len(res.Traces))
	}
	if res.Score.Percent() != 100 {
		t.Errorf("clean replay score = %d%%, want 100", res.Score.Percent())
	}
}

// TestUnknownAgentModeRejected asserts a typo'd agent mode fails fast rather than
// silently running agent-off.
func TestUnknownAgentModeRejected(t *testing.T) {
	root := t.TempDir()
	if _, err := seed.Seed(root, "dtc", "2026-05"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := run.RunWith(root, "dtc", "2026-05", run.Options{Agent: agentclient.Mode("bogus")}); err == nil {
		t.Error("RunWith accepted an unknown agent mode")
	}
}
