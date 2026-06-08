package closer_test

import (
	"testing"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/classifyq"
	"github.com/razorpay/close-agent/internal/closer"
	"github.com/razorpay/close-agent/internal/seed"
)

// seedAsyncHardPeriod seeds the hard ambiguity period, runs the agent-off close
// (which PARKS its skipped events as the work queue), then runs the WORKER so the
// results store exists for APPLY. Returns the temp root.
func seedAsyncHardPeriod(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	sr, err := seed.SeedWith(root, "dtc", "2026-04", seed.Options{Ambiguity: true})
	if err != nil {
		t.Fatalf("seed hard: %v", err)
	}
	if sr.Ambiguity.NumStripped == 0 {
		t.Fatal("hard period stripped no gst_rate")
	}
	// close --agent off books the bulk and emits proposals.json (its skips = the queue).
	off, err := closer.RunWith(root, "dtc", "2026-04", closer.Options{Agent: agentclient.ModeOff})
	if err != nil {
		t.Fatalf("close --agent off: %v", err)
	}
	if off.ProposalsPath == "" || len(off.Skipped) == 0 {
		t.Fatalf("agent-off close did not park a work queue: %+v", off.ProposalsPath)
	}
	if _, err := classifyq.RunWorker(root, "dtc", "2026-04"); err != nil {
		t.Fatalf("work: %v", err)
	}
	return root
}

// TestAsyncPipelineRecoversToFull: propose -> work -> apply reaches 100% with the
// stub brain, mirroring the synchronous replay path.
func TestAsyncPipelineRecoversToFull(t *testing.T) {
	root := seedAsyncHardPeriod(t)
	res, err := closer.RunApply(root, "dtc", "2026-04", closer.ApplyOptions{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.AgentDone == 0 {
		t.Error("apply booked 0 recovered events, want > 0")
	}
	if len(res.Skipped) != 0 {
		t.Errorf("want 0 skipped, got %+v", res.Skipped)
	}
	if res.Score.Percent() != 100 {
		t.Errorf("async apply score = %d%%, want 100", res.Score.Percent())
	}
	if !res.Score.TrialBalanceMatches {
		t.Error("trial balance does not match truth after async apply")
	}
}

// TestAsyncApplyDeterministic: two applies over the same results store agree.
func TestAsyncApplyDeterministic(t *testing.T) {
	root := seedAsyncHardPeriod(t)
	a, err := closer.RunApply(root, "dtc", "2026-04", closer.ApplyOptions{})
	if err != nil {
		t.Fatalf("apply a: %v", err)
	}
	b, err := closer.RunApply(root, "dtc", "2026-04", closer.ApplyOptions{})
	if err != nil {
		t.Fatalf("apply b: %v", err)
	}
	if a.Score.Percent() != b.Score.Percent() || a.AgentDone != b.AgentDone {
		t.Errorf("async apply not deterministic: a=(%d%%,%d) b=(%d%%,%d)", a.Score.Percent(), a.AgentDone, b.Score.Percent(), b.AgentDone)
	}
}

// TestAsyncApplyRejectsForgedCitation: a result whose recovered rate doesn't match
// the cited order is rejected by the Validator at apply time (skipped, not posted),
// so the score drops — the agent cannot inject an unverifiable number.
func TestAsyncApplyRejectsForgedCitation(t *testing.T) {
	root := seedAsyncHardPeriod(t)
	path := classifyq.ResultsPath(root, "dtc", "2026-04")
	rf, err := classifyq.ReadResults(path)
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	// Forge the first proposed result's rate to a different slab while leaving its
	// citation pointing at the real order (which holds the true rate).
	var forged string
	for i := range rf.Results {
		if rf.Results[i].Proposed() {
			r := &rf.Results[i]
			cur := r.Recovered[0].Value
			r.Recovered[0].Value = map[string]string{"5": "18", "12": "18", "18": "5"}[cur]
			forged = r.EventID
			break
		}
	}
	if forged == "" {
		t.Fatal("no proposed result to forge")
	}
	if err := classifyq.WriteResults(path, rf); err != nil {
		t.Fatalf("write forged results: %v", err)
	}

	res, err := closer.RunApply(root, "dtc", "2026-04", closer.ApplyOptions{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// the forged event must be skipped with a citation-rejection reason
	found := false
	for _, s := range res.Skipped {
		if s.EventID == forged {
			found = true
		}
	}
	if !found {
		t.Errorf("forged event %s was not rejected by the validator (skipped=%+v)", forged, res.Skipped)
	}
	if res.Score.Percent() == 100 {
		t.Error("score should drop when a forged citation is rejected")
	}
}

// TestAsyncApplyRecordedReviewerRejects: a recorded reject verdict skips an event
// even though its citation is valid (the human review gate).
func TestAsyncApplyRecordedReviewerRejects(t *testing.T) {
	root := seedAsyncHardPeriod(t)
	rf, err := classifyq.ReadResults(classifyq.ResultsPath(root, "dtc", "2026-04"))
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	// Approve all but one proposed event; reject that one.
	var rejected string
	af := classifyq.ApprovalsFile{SchemaVersion: classifyq.SchemaVersion, World: "dtc", Period: "2026-04"}
	for _, r := range rf.Results {
		if !r.Proposed() {
			continue
		}
		v := classifyq.VerdictApprove
		if rejected == "" {
			rejected = r.EventID
			v = classifyq.VerdictReject
		}
		af.Approvals = append(af.Approvals, classifyq.Approval{EventID: r.EventID, Verdict: v, Reviewer: "test"})
	}

	res, err := closer.RunApply(root, "dtc", "2026-04", closer.ApplyOptions{Reviewer: classifyq.NewRecordedReviewer(af)})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	found := false
	for _, s := range res.Skipped {
		if s.EventID == rejected {
			found = true
		}
	}
	if !found {
		t.Errorf("review-rejected event %s should be skipped, got skipped=%+v", rejected, res.Skipped)
	}
}
