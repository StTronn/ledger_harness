package closer

import (
	"fmt"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/classify"
	"github.com/razorpay/close-agent/internal/classifyq"
	"github.com/razorpay/close-agent/internal/config"
	"github.com/razorpay/close-agent/internal/gstsplit"
	"github.com/razorpay/close-agent/internal/ingest"
	"github.com/razorpay/close-agent/internal/ledger"
	"github.com/razorpay/close-agent/internal/money"
	"github.com/razorpay/close-agent/internal/reconcile"
	"github.com/razorpay/close-agent/internal/score"
)

// async.go is the deterministic half of the ASYNCHRONOUS classify pipeline (the
// redefined §8 classify seam) — the PROPOSE and APPLY stages that bracket the async
// WORK stage (internal/classifyq). The synchronous inline path (RunWith with
// --agent) is left intact; this is the decoupled execution model:
//
//	Propose   rule misses -> WorkItem[] -> proposals.json        (this file)
//	(classifyq.RunWorker: WorkItem -> Result -> results.json)    (async, out of band)
//	RunApply  Result -> validate citation -> review -> DERIVE money -> Bind+Post -> reconcile -> score  (this file)
//
// The keyed results store is the determinism anchor: APPLY only does a keyed lookup,
// so however the results were produced the booked close is byte-identical.

// writeProposalsQueue emits the async classify WORK QUEUE for the events a close
// run could not book (its Skips). It is called by RunWith — close --agent off is the
// front door of the async flow, so the events it parks become the worker's input.
// The WorkItem carries the same source-agnostic EventSummary the synchronous agent
// would see, plus the skip reason. It returns the proposals store path.
func writeProposalsQueue(root, world, period string, skips []Skip, eventByID map[string]ingest.NormalizedEvent) (string, error) {
	pf := classifyq.ProposalsFile{
		SchemaVersion: classifyq.SchemaVersion,
		World:         world,
		Period:        period,
		Items:         make([]classifyq.WorkItem, 0, len(skips)),
	}
	for _, s := range skips {
		ev, ok := eventByID[s.EventID]
		if !ok {
			continue
		}
		pf.Items = append(pf.Items, classifyq.WorkItem{
			EventID: s.EventID,
			Event:   agentclient.SummarizeEvent(ev),
			Reason:  s.Reason,
		})
	}
	path := classifyq.ProposalsPath(root, world, period)
	if err := classifyq.WriteProposals(path, pf); err != nil {
		return "", err
	}
	return path, nil
}

// ApplyOptions controls the APPLY stage. Reviewer is the human-in-the-loop gate
// consulted on each validated proposal (defaults to classifyq.AutoReviewer — approve
// all — when nil).
type ApplyOptions struct {
	Reviewer classifyq.Reviewer
}

// RunApply runs the deterministic back half of the pipeline: it rebuilds the books
// using the async worker's results store for the rule misses. For each missed event
// it looks up the worker's Result, RE-VERIFIES the citation against orders.json
// (Validate), runs the review gate, DERIVES the money from the recovered rate via
// the canonical gstsplit, and binds+posts — so the worker never supplies a rupee
// value and a forged/stale citation is rejected. It then reconciles and scores
// exactly like the synchronous close.
//
// This stage does NOT run the investigate agent (it is the classify pipeline);
// reconcile breaks are listed as in the agent-off baseline.
func RunApply(root, world, period string, opts ApplyOptions) (Result, error) {
	reviewer := opts.Reviewer
	if reviewer == nil {
		reviewer = classifyq.AutoReviewer{}
	}

	pb, err := config.DefaultPlaybook()
	if err != nil {
		return Result{}, fmt.Errorf("closer: load playbook: %w", err)
	}
	tmpls := ledger.NewPlaybookTemplates(pb)
	lg := ledger.New(ledger.NewPlaybookChart(pb))

	raw, events, err := ingest.IngestAndNormalize(root, world, period)
	if err != nil {
		return Result{}, err
	}

	rf, err := classifyq.ReadResults(classifyq.ResultsPath(root, world, period))
	if err != nil {
		return Result{}, err
	}
	resultsByID := rf.Index()
	rates, err := agentclient.OrderGSTRates(root, world, period)
	if err != nil {
		return Result{}, err
	}

	res := Result{Ledger: lg, AgentMode: agentclient.Mode("async-apply")}
	for _, ev := range events {
		c, ok, _ := classify.Classify(ev)
		if ok {
			res.Classified++
		} else {
			ac, skip, handled, err := applyResolved(ev, resultsByID, rates, reviewer)
			if err != nil {
				return Result{}, err
			}
			if !handled {
				res.Skipped = append(res.Skipped, Skip{EventID: ev.ID, Type: string(ev.Type), Reason: skip})
				continue
			}
			res.AgentDone++
			c = ac
		}
		entry, err := ledger.Bind(tmpls, c.EntryType, c.IK, c.Params)
		if err != nil {
			return Result{}, fmt.Errorf("closer: bind %s for event %s: %w", c.EntryType, ev.ID, err)
		}
		entry.Ts = c.Ts
		entry.TxID = c.TxID
		posted, err := lg.Post(entry)
		if err != nil {
			return Result{}, fmt.Errorf("closer: post %s for event %s: %w", c.EntryType, ev.ID, err)
		}
		res.Produced = append(res.Produced, producedFrom(ev.ID, posted))
	}

	periodEnd, err := nextMonthFirst(period)
	if err != nil {
		return Result{}, err
	}
	res.Breaks = reconcile.Reconcile(reconcile.Input{
		Settlements:       raw.Settlements,
		Payments:          raw.Payments,
		Refunds:           raw.Refunds,
		BankFeed:          raw.BankFeed,
		ReceivableBalance: lg.AccountBalance(receivableAccount).Balance,
		PeriodEnd:         periodEnd,
		DateToleranceDays: settlementDateToleranceDays,
	})

	sc, rec, err := score.RunScoreRecord(root, world, period, res.Produced)
	if err != nil {
		return Result{}, err
	}
	res.Score = sc
	res.Record = rec
	path, err := score.WriteErrors(root, world, period, rec)
	if err != nil {
		return Result{}, err
	}
	res.ErrorsPath = path
	return res, nil
}

// applyResolved turns the async worker's Result for a rule-missed event into a
// bindable Classification: look up the result, re-verify the citation against the
// snapshot (Validate), run the review gate, and DERIVE the money from the recovered
// rate. handled=false (with a skip reason) when there is no result, the worker
// escalated, validation rejects the citation, or review rejects the proposal — the
// event is then skipped, never guessed. It errors only on a derivation/bookkeeping
// failure (an unsupported entry type).
func applyResolved(
	ev ingest.NormalizedEvent,
	resultsByID map[string]classifyq.Result,
	rates map[string]string,
	reviewer classifyq.Reviewer,
) (*classify.Classification, string, bool, error) {
	r, found := resultsByID[ev.ID]
	if !found {
		return nil, "no async classify result for " + ev.ID, false, nil
	}
	if !r.Proposed() {
		reason := r.Reason
		if reason == "" {
			reason = "worker escalated"
		}
		return nil, "worker escalated: " + reason, false, nil
	}

	// Re-verify the worker's provenance citation against orders.json — a forged or
	// stale citation is rejected here, before any money is derived or posted.
	rate, err := classifyq.ValidateRate(r, rates)
	if err != nil {
		return nil, "rejected (citation): " + err.Error(), false, nil
	}

	// Human-in-the-loop review gate (auto-approve by default).
	if d := reviewer.Review(r); d.Verdict != classifyq.VerdictApprove {
		reason := d.Reason
		if reason == "" {
			reason = "rejected by review"
		}
		return nil, "rejected (review): " + reason, false, nil
	}

	// Derive the money from the recovered rate — the worker supplied only the rate,
	// never net/gst (numeric-surface hardening). IK/TxID follow the rule scheme.
	params, err := deriveParams(ev, r.EntryType, rate)
	if err != nil {
		return nil, "", false, fmt.Errorf("closer: async apply %s for event %s: %w", r.EntryType, ev.ID, err)
	}
	ik, txID, err := entryRefsForType(r.EntryType, ev.ID)
	if err != nil {
		return nil, "", false, fmt.Errorf("closer: async apply %s for event %s: %w", r.EntryType, ev.ID, err)
	}
	return &classify.Classification{
		EntryType: r.EntryType,
		Params:    params,
		IK:        ik,
		TxID:      txID,
		Ts:        ev.TS,
		Reason:    "async classify: " + r.Rationale,
	}, "", true, nil
}

// deriveParams computes the entry type's paise params from the event's gross and the
// validated rate, using the canonical gstsplit — so the booked entry equals what the
// rule engine would have produced had the rate been present. v1 supports dtc_sale
// (the only entry type the classify worker proposes).
func deriveParams(ev ingest.NormalizedEvent, entryType string, rate int) (map[string]money.Money, error) {
	switch entryType {
	case "dtc_sale":
		gross := ev.Amount
		net, gst := gstsplit.SplitInclusive(gross, rate)
		return map[string]money.Money{
			"gross":      gross,
			"net":        net,
			"gst":        gst,
			"payment_id": money.FromPaise(0),
		}, nil
	default:
		return nil, fmt.Errorf("entry type %q is not supported by the v1 async classify apply", entryType)
	}
}
