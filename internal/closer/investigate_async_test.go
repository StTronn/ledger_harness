package closer_test

import (
	"testing"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/classifyq"
	"github.com/razorpay/close-agent/internal/closer"
	"github.com/razorpay/close-agent/internal/seed"
)

// seedBreakAsync seeds the unbooked-refund break period, runs the agent-off close
// (which parks proposals.json + breaks.json), and writes the two agent stores the
// way the TS flue-agent would: results.json (the refund escalated by classify) and
// resolutions.json (investigate's refund_reversal with a gst_rate citation to the
// parent order). It returns the temp root + the refund id and its true rate.
func seedBreakAsync(t *testing.T) (root, refundID, rate string) {
	t.Helper()
	root = t.TempDir()
	sr, err := seed.SeedWith(root, "dtc", "2026-03", seed.Options{Inject: seed.InjectUnbookedRefund})
	if err != nil {
		t.Fatalf("seed break: %v", err)
	}
	refundID = sr.Inject.RefundID
	if refundID == "" {
		t.Fatal("no unbooked refund injected")
	}

	// Recover the refund's parent order + true rate from the generated fixtures
	// (same data the TS agent reaches via getRefund->getPayment->getOrder).
	fx, _, _, _, _, err := seed.GenerateWith("dtc", "2026-03", seed.Options{Inject: seed.InjectUnbookedRefund})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var paymentID, orderID string
	for _, r := range fx.Refunds {
		if r.ID == refundID {
			paymentID = r.PaymentID
		}
	}
	for _, p := range fx.Payments {
		if p.ID == paymentID {
			orderID = p.OrderID
		}
	}
	for _, o := range fx.Orders {
		if o.ID == orderID {
			rate = o.Notes.GSTRate
		}
	}
	if orderID == "" || rate == "" {
		t.Fatalf("could not recover order/rate for refund %s", refundID)
	}

	// Park the queue (agent-off close emits proposals.json + breaks.json).
	if _, err := closer.RunWith(root, "dtc", "2026-03", closer.Options{Agent: agentclient.ModeOff}); err != nil {
		t.Fatalf("close --agent off: %v", err)
	}

	// results.json: classify escalates the refund (not a payment).
	if err := classifyq.WriteResults(classifyq.ResultsPath(root, "dtc", "2026-03"), classifyq.ResultsFile{
		SchemaVersion: classifyq.SchemaVersion, World: "dtc", Period: "2026-03",
		Results: []classifyq.Result{{EventID: refundID, Status: classifyq.StatusEscalated, Reason: "refund not recovered by classify"}},
	}); err != nil {
		t.Fatalf("write results: %v", err)
	}
	return root, refundID, rate
}

func writeResolution(t *testing.T, root, refundID, orderID, rate string) {
	t.Helper()
	if err := classifyq.WriteResolutions(classifyq.ResolutionsPath(root, "dtc", "2026-03"), classifyq.ResolutionsFile{
		SchemaVersion: classifyq.SchemaVersion, World: "dtc", Period: "2026-03",
		Resolutions: []classifyq.Resolution{{
			BreakKey: "check3:receivable-residual:",
			Status:   classifyq.StatusResolved,
			Postings: []classifyq.ResolutionPosting{{
				EventID:   refundID,
				EntryType: "refund_reversal",
				Recovered: []classifyq.Recovered{{Field: "gst_rate", Value: rate, Source: classifyq.Source{Tool: "getOrder", Object: orderID, Path: "notes.gst_rate"}}},
			}},
		}},
	}); err != nil {
		t.Fatalf("write resolutions: %v", err)
	}
}

// orderForRefund recovers the order id for the injected refund (helper).
func orderForRefund(t *testing.T, refundID string) string {
	t.Helper()
	fx, _, _, _, _, err := seed.GenerateWith("dtc", "2026-03", seed.Options{Inject: seed.InjectUnbookedRefund})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pay := ""
	for _, r := range fx.Refunds {
		if r.ID == refundID {
			pay = r.PaymentID
		}
	}
	for _, p := range fx.Payments {
		if p.ID == pay {
			return p.OrderID
		}
	}
	return ""
}

// TestInvestigateApplyResolves: a valid investigate resolution (refund_reversal with
// a correct gst_rate citation) is validated, the money is derived, it posts, the
// receivable clears, and the score reaches 100%.
func TestInvestigateApplyResolves(t *testing.T) {
	root, refundID, rate := seedBreakAsync(t)
	writeResolution(t, root, refundID, orderForRefund(t, refundID), rate)

	res, err := closer.RunInvestigateApply(root, "dtc", "2026-03", closer.ApplyOptions{})
	if err != nil {
		t.Fatalf("investigate apply: %v", err)
	}
	if res.InvestigateDone != 1 {
		t.Errorf("InvestigateDone = %d, want 1", res.InvestigateDone)
	}
	if len(res.Breaks) != 0 {
		t.Errorf("want 0 breaks after investigate apply, got %+v", res.Breaks)
	}
	if res.Score.Percent() != 100 || !res.Score.TrialBalanceMatches {
		t.Errorf("want 100%% + TB match, got %d%% tb=%v", res.Score.Percent(), res.Score.TrialBalanceMatches)
	}
}

// TestInvestigateApplyRejectsForgedCitation: a resolution whose rate does not match
// the cited order is rejected (not posted), so the break stays and the score does not
// reach 100%.
func TestInvestigateApplyRejectsForgedCitation(t *testing.T) {
	root, refundID, rate := seedBreakAsync(t)
	forged := "5"
	if rate == "5" {
		forged = "18"
	}
	writeResolution(t, root, refundID, orderForRefund(t, refundID), forged)

	res, err := closer.RunInvestigateApply(root, "dtc", "2026-03", closer.ApplyOptions{})
	if err != nil {
		t.Fatalf("investigate apply: %v", err)
	}
	if res.InvestigateDone != 0 {
		t.Errorf("forged citation must not resolve, got InvestigateDone=%d", res.InvestigateDone)
	}
	if len(res.Breaks) == 0 {
		t.Error("break should remain after a rejected resolution")
	}
	if res.Score.Percent() == 100 {
		t.Error("score should not reach 100%% with a rejected resolution")
	}
}
