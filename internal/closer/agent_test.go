package closer_test

import (
	"testing"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/closer"
	"github.com/razorpay/close-agent/internal/seed"
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
	return root, res.Ambiguity.NumStripped
}

// TestAgentOffIsPartialBaseline asserts the documented agent-off baseline on the
// hard period: the stripped payments are skipped, the score is PARTIAL (< 100%),
// and a check#3 receivable break surfaces (the unposted sales). No traces emitted.
func TestAgentOffIsPartialBaseline(t *testing.T) {
	root, stripped := seedHardPeriod(t)

	res, err := closer.RunWith(root, "dtc", "2026-04", closer.Options{Agent: agentclient.ModeOff})
	if err != nil {
		t.Fatalf("RunWith off: %v", err)
	}
	if len(res.Skipped) != stripped {
		t.Errorf("skipped = %d, want %d (the stripped payments)", len(res.Skipped), stripped)
	}
	if res.AgentDone != 0 {
		t.Errorf("agent classified %d events with the agent off, want 0", res.AgentDone)
	}
	if len(res.Traces) != 0 {
		t.Errorf("agent off emitted %d traces, want 0", len(res.Traces))
	}
	if res.Score.Percent() >= 100 {
		t.Errorf("agent-off score = %d%%, want PARTIAL (<100)", res.Score.Percent())
	}
	if len(res.Breaks) == 0 {
		t.Errorf("agent-off run had no reconcile break; the unposted sales should leave a receivable residual")
	}
}

// TestAgentReplayRisesToFull is the module's headline gate: with --agent replay on
// the hard period, the previously-skipped payments are classified from the
// committed recorded responses (recovered from orders.json, not truth), the score
// RISES to 100%, the receivable break clears, the trial balance matches truth, and
// a FROZEN versioned trace is emitted per recovered event.
func TestAgentReplayRisesToFull(t *testing.T) {
	root, stripped := seedHardPeriod(t)

	res, err := closer.RunWith(root, "dtc", "2026-04", closer.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay: %v", err)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("skipped = %+v, want 0 (the agent recovered them all)", res.Skipped)
	}
	if res.AgentDone != stripped {
		t.Errorf("agent classified %d events, want %d (the stripped payments)", res.AgentDone, stripped)
	}
	if res.Score.Percent() != 100 {
		t.Errorf("agent-replay score = %d%%, want 100", res.Score.Percent())
	}
	if !res.Score.IsPerfect() {
		t.Errorf("agent-replay score not perfect; errors=%v", res.Score.Errors)
	}
	if !res.Score.TrialBalanceMatches {
		t.Errorf("agent-replay trial balance does not match truth")
	}
	if len(res.Breaks) != 0 {
		t.Errorf("agent-replay run still has %d reconcile break(s); the receivable should clear", len(res.Breaks))
	}
	if len(res.Traces) != stripped {
		t.Fatalf("emitted %d traces, want %d (one per recovered event)", len(res.Traces), stripped)
	}
	for _, tr := range res.Traces {
		if tr.SchemaVersion != agentclient.TraceSchemaVersion {
			t.Errorf("trace for %s schema_version = %d, want %d (frozen)", tr.EventID, tr.SchemaVersion, agentclient.TraceSchemaVersion)
		}
		if tr.Mode != agentclient.ModeReplay {
			t.Errorf("trace for %s mode = %q, want replay", tr.EventID, tr.Mode)
		}
		if tr.Decision.EntryType != "dtc_sale" {
			t.Errorf("trace for %s decision.entry_type = %q, want dtc_sale", tr.EventID, tr.Decision.EntryType)
		}
	}
	if res.TracePath == "" {
		t.Errorf("agent-replay run wrote no trace.json path")
	}
}

// TestAgentReplayByteDeterministic asserts two replay closes over the same
// fixtures produce identical results — same classified count, same agent count,
// same produced entries, same score (SPEC §5, §12).
func TestAgentReplayByteDeterministic(t *testing.T) {
	root, _ := seedHardPeriod(t)

	a, err := closer.RunWith(root, "dtc", "2026-04", closer.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("run a: %v", err)
	}
	b, err := closer.RunWith(root, "dtc", "2026-04", closer.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("run b: %v", err)
	}
	if a.Classified != b.Classified || a.AgentDone != b.AgentDone || len(a.Produced) != len(b.Produced) {
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

	res, err := closer.RunWith(root, "dtc", "2026-05", closer.Options{Agent: agentclient.ModeReplay})
	if err != nil {
		t.Fatalf("RunWith replay on clean period: %v", err)
	}
	if len(res.Skipped) != 0 || res.AgentDone != 0 || len(res.Traces) != 0 {
		t.Errorf("clean replay consulted the agent: skipped=%d agentDone=%d traces=%d",
			len(res.Skipped), res.AgentDone, len(res.Traces))
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
	if _, err := closer.RunWith(root, "dtc", "2026-05", closer.Options{Agent: agentclient.Mode("bogus")}); err == nil {
		t.Error("RunWith accepted an unknown agent mode")
	}
}
