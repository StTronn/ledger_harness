package closer

import (
	"fmt"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/classifyq"
	"github.com/razorpay/close-agent/internal/ledger"
	"github.com/razorpay/close-agent/internal/reconcile"
)

// investigate_async.go is the ASYNC investigate stages, parallel to async.go for
// classify. A close run that ends with breaks PARKS them as breaks.json (the
// investigate work queue); `flue-agent investigate` writes resolutions.json;
// RunInvestigateApply rebuilds the books (classify results) + applies the
// resolutions (validate citation -> derive money -> post) -> re-reconcile -> score.

// writeBreaksQueue parks the final reconcile breaks as the investigate work queue,
// each carrying the candidate events (the unbooked suspects) the agent inspects.
func writeBreaksQueue(root, world, period string, breaks []reconcile.Break, candidates []agentclient.EventSummary) (string, error) {
	bf := classifyq.BreaksFile{
		SchemaVersion: classifyq.SchemaVersion,
		World:         world,
		Period:        period,
		Breaks:        make([]classifyq.BreakWork, 0, len(breaks)),
	}
	for _, b := range breaks {
		bf.Breaks = append(bf.Breaks, classifyq.BreakWork{Break: breakSummaryFrom(b), Candidates: candidates})
	}
	path := classifyq.BreaksPath(root, world, period)
	if err := classifyq.WriteBreaks(path, bf); err != nil {
		return "", err
	}
	return path, nil
}

// RunInvestigateApply runs the back half of the async INVESTIGATE flow: rebuild the
// books from the classify results store, then apply the investigate resolutions
// (re-verify each posting's citation, derive the money, bind+post), re-reconcile,
// and score. It is the investigate analogue of RunApply.
func RunInvestigateApply(root, world, period string, opts ApplyOptions) (Result, error) {
	reviewer := opts.Reviewer
	if reviewer == nil {
		reviewer = classifyq.AutoReviewer{}
	}

	// Rebuild the books exactly as `classify apply` does (so the receivable residual
	// that investigate resolves is present), using the classify results store.
	rf, err := classifyq.ReadResults(classifyq.ResultsPath(root, world, period))
	if err != nil {
		return Result{}, err
	}
	rates, err := agentclient.OrderGSTRates(root, world, period)
	if err != nil {
		return Result{}, err
	}
	er, err := runEvents(root, world, period, asyncResolver(rf.Index(), rates, reviewer))
	if err != nil {
		return Result{}, err
	}
	res := Result{
		Ledger:     er.lg,
		AgentMode:  agentclient.Mode("investigate-apply"),
		Produced:   er.produced,
		Classified: er.classified,
		AgentDone:  er.agentDone,
		Skipped:    er.skipped,
	}

	// Apply the investigate resolutions onto the rebuilt books.
	resns, err := classifyq.ReadResolutions(classifyq.ResolutionsPath(root, world, period))
	if err != nil {
		return Result{}, err
	}
	for _, r := range resns.Resolutions {
		if !r.Resolved() {
			res.Escalations = append(res.Escalations, Escalation{
				BreakKey: r.BreakKey, Reason: investigateResolutionReason(r),
			})
			continue
		}
		applied := false
		for _, post := range r.Postings {
			ok, err := applyResolutionPosting(er, post, rates, &res)
			if err != nil {
				return Result{}, err
			}
			applied = applied || ok
			if !ok {
				// A posting whose citation failed: record it as an escalation reason but
				// do not abort — the break simply will not clear.
				res.Escalations = append(res.Escalations, Escalation{
					BreakKey: r.BreakKey, Reason: "rejected (citation) on posting for " + post.EventID,
				})
			}
		}
		if applied {
			res.InvestigateDone++
		}
	}

	periodEnd, err := nextMonthFirst(period)
	if err != nil {
		return Result{}, err
	}
	res.Breaks = reconcile.Reconcile(er.reconcileInput(periodEnd))

	sc, rec, path, err := scoreAndWrite(root, world, period, res.Produced)
	if err != nil {
		return Result{}, err
	}
	res.Score = sc
	res.Record = rec
	res.ErrorsPath = path
	return res, nil
}

// applyResolutionPosting re-verifies one investigate posting's citation, derives its
// money from the recovered rate, and binds+posts it (keyed to its source event). It
// returns ok=false (without erroring) when the citation fails validation — the
// posting is then skipped and the break will not clear.
func applyResolutionPosting(er *eventRun, post classifyq.ResolutionPosting, rates map[string]string, res *Result) (bool, error) {
	rate, err := classifyq.ValidateRecoveredRate("investigate posting "+post.EventID, post.Recovered, rates)
	if err != nil {
		return false, nil // citation rejected: skip, never guess.
	}
	ev, ok := er.eventByID[post.EventID]
	if !ok {
		return false, nil // no such event to attribute the posting to.
	}
	params, err := deriveParams(ev, post.EntryType, rate)
	if err != nil {
		return false, fmt.Errorf("closer: investigate apply %s for event %s: %w", post.EntryType, post.EventID, err)
	}
	ik, txID, err := entryRefsForType(post.EntryType, post.EventID)
	if err != nil {
		return false, fmt.Errorf("closer: investigate apply %s for event %s: %w", post.EntryType, post.EventID, err)
	}
	entry, err := ledger.Bind(er.tmpls, post.EntryType, ik, params)
	if err != nil {
		return false, fmt.Errorf("closer: bind %s for event %s: %w", post.EntryType, post.EventID, err)
	}
	entry.Ts = ev.TS
	entry.TxID = txID
	posted, err := er.lg.Post(entry)
	if err != nil {
		return false, fmt.Errorf("closer: post %s for event %s: %w", post.EntryType, post.EventID, err)
	}
	res.Produced = append(res.Produced, producedFrom(post.EventID, posted))
	return true, nil
}

// investigateResolutionReason renders why a resolution did not resolve.
func investigateResolutionReason(r classifyq.Resolution) string {
	if r.Reason != "" {
		return "agent escalated: " + r.Reason
	}
	return "agent escalated (no reason given)"
}
