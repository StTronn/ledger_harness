package run

import (
	"encoding/json"
	"fmt"

	"github.com/razorpay/ledger-flow/internal/agentclient"
	"github.com/razorpay/ledger-flow/internal/config"
	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledger"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/posting"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/recovery"
	"github.com/razorpay/ledger-flow/internal/reconcile"
	"github.com/razorpay/ledger-flow/internal/score"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// core.go is the SHARED close spine (SPEC §5): the playbook+ledger setup,
// ingest+normalize, the per-event book loop, the reconcile input, and scoring. The
// ONLY thing that differs between callers — the close (RunWith) and the
// investigate-fixture generator — is HOW a rule miss is resolved, captured by the
// resolveMiss strategy. Keeping the spine here means the determinism/ledger
// invariants are defined once.

// resolveMiss is the per-model strategy for a rule-missed event: turn it into a
// bindable Classification (handled=true), or report it skipped (handled=false, with
// a reason). It may emit a FROZEN agent trace (nil if the model has none at this
// point, e.g. the async apply, whose trace was produced by the worker). It returns
// an error only for an infrastructure failure, never for an event it declines.
type resolveMiss func(ev ingest.NormalizedEvent, context json.RawMessage) (c *posting.Classification, skipReason string, handled bool, trace *agentclient.Trace, err error)

// eventRun is the result of running the shared book loop: the posted ledger and the
// per-run tallies/collections the wrappers need to finish (park the queue, run
// investigate, write traces, reconcile, score).
type eventRun struct {
	lg        *ledger.Ledger
	tmpls     ledger.Templates
	raw       ingest.Raw
	events    []ingest.NormalizedEvent
	eventByID map[string]ingest.NormalizedEvent
	orders    map[string]feeds.OrderInfo
	ratecard  *feeds.RateCardFile
	recovery  *recovery.Engine

	produced      []score.Produced
	classified    int    // events booked by the rule engine
	agentReviewed int    // rule-missed events reviewed by the agent
	skipped       []Skip // events left unbooked (reported, never guessed)
	skippedEvents []ingest.NormalizedEvent
	traces        []agentclient.Trace // frozen agent traces emitted during resolution
}

// runEvents performs setup + the shared per-event book loop with the given miss
// resolver. For each event it tries the rule engine; on a hit it books directly, on
// a miss it calls resolve and books the result or records a Skip. Every booked entry
// is bound+posted through the SAME balance-or-reject ledger (rule, agent, or async
// alike), so the produced ledger is model-independent.
func runEvents(root, world, period string, resolve resolveMiss) (*eventRun, error) {
	pb, err := config.DefaultPlaybook()
	if err != nil {
		return nil, fmt.Errorf("closer: load playbook: %w", err)
	}
	tmpls := ledger.NewPlaybookTemplates(pb)
	lg := ledger.New(ledger.NewPlaybookChart(pb))

	raw, events, err := ingest.IngestAndNormalize(root, world, period)
	if err != nil {
		return nil, err
	}
	orders, err := agentclient.OrderInfos(root, world, period)
	if err != nil {
		orders = nil
	}
	var ratecard *feeds.RateCardFile
	if rc, rcErr := feeds.RateCard(root, world, period); rcErr == nil {
		ratecard = &rc
	}

	er := &eventRun{
		lg:        lg,
		tmpls:     tmpls,
		raw:       raw,
		events:    events,
		eventByID: make(map[string]ingest.NormalizedEvent, len(events)),
		orders:    orders,
		ratecard:  ratecard,
	}
	er.recovery = recovery.New(lg, events, raw, orders, nil, ratecard)
	for _, ev := range events {
		er.eventByID[ev.ID] = ev
		c, ok, ruleReason := posting.Classify(ev)
		if ok {
			er.classified++
		} else {
			decision := er.recovery.EventDecision(ev.ID)
			if decision.Kind == recovery.SafeToPost && decision.Candidate != nil {
				c = classificationFromRecovery(*decision.Candidate)
				er.classified++
			} else {
				var context json.RawMessage
				if bundle, found := er.recovery.EventContext(ev.ID); found {
					context, err = json.Marshal(bundle)
					if err != nil {
						return nil, fmt.Errorf("closer: marshal recovery context for event %s: %w", ev.ID, err)
					}
				}
				ac, skip, handled, trace, err := resolve(ev, context)
				if err != nil {
					return nil, err
				}
				if trace != nil {
					er.traces = append(er.traces, *trace)
					er.agentReviewed++
				}
				if !handled {
					er.skipped = append(er.skipped, Skip{
						EventID: ev.ID, Type: string(ev.Type), Reason: skipReason(decision.Reason, ruleReason, skip),
					})
					er.skippedEvents = append(er.skippedEvents, ev)
					continue
				}
				c = ac
			}
		}
		entry, err := ledger.Bind(tmpls, c.EntryType, c.IK, c.Params)
		if err != nil {
			return nil, fmt.Errorf("closer: bind %s for event %s: %w", c.EntryType, ev.ID, err)
		}
		entry.Ts = c.Ts
		entry.TxID = c.TxID
		posted, err := lg.Post(entry)
		if err != nil {
			return nil, fmt.Errorf("closer: post %s for event %s: %w", c.EntryType, ev.ID, err)
		}
		er.produced = append(er.produced, producedFrom(ev.ID, posted))
	}
	return er, nil
}

func classificationFromRecovery(candidate recovery.Candidate) *posting.Classification {
	return &posting.Classification{
		EntryType: candidate.EntryType,
		Params:    candidate.Params,
		IK:        candidate.IK,
		TxID:      candidate.TxID,
		Ts:        candidate.Ts,
		Reason:    candidate.Reason,
	}
}

// reconcileInput builds the SPEC §7 reconcile input from the posted ledger and the
// raw records (never truth). periodEnd is the exclusive period cutoff. It is a
// method so callers can re-build it after investigate posts (a fresh receivable
// balance) for the re-reconcile.
func (er *eventRun) reconcileInput(periodEnd string) reconcile.Input {
	return reconcile.Input{
		Settlements:          er.raw.Settlements,
		Payments:             er.raw.Payments,
		Refunds:              er.raw.Refunds,
		BankFeed:             er.raw.BankFeed,
		ReceivableBalance:    er.lg.AccountBalance(receivableAccount).Balance,
		CODReceivableBalance: er.lg.AccountBalance(codReceivableAccount).Balance,
		CourierFeed:          er.raw.CourierFeed,
		PeriodEnd:            periodEnd,
		DateToleranceDays:    settlementDateToleranceDays,
	}
}

// scoreAndWrite scores the produced entries against the period's truth GL (the
// scorer is the only allowed truth reader) and writes the frozen errors.json
// artifact. It returns the score result, the run record, and the artifact path.
func scoreAndWrite(root, world, period string, produced []score.Produced) (score.Result, score.RunRecord, string, error) {
	sc, rec, err := score.RunScoreRecord(root, world, period, produced)
	if err != nil {
		return score.Result{}, score.RunRecord{}, "", err
	}
	path, err := score.WriteErrors(root, world, period, rec)
	if err != nil {
		return score.Result{}, score.RunRecord{}, "", err
	}
	return sc, rec, path, nil
}
