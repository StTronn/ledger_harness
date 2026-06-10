package closer

import (
	"fmt"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/classify"
	"github.com/razorpay/close-agent/internal/config"
	"github.com/razorpay/close-agent/internal/ingest"
	"github.com/razorpay/close-agent/internal/ledger"
	"github.com/razorpay/close-agent/internal/reconcile"
	"github.com/razorpay/close-agent/internal/score"
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
type resolveMiss func(ev ingest.NormalizedEvent) (c *classify.Classification, skipReason string, handled bool, trace *agentclient.Trace, err error)

// eventRun is the result of running the shared book loop: the posted ledger and the
// per-run tallies/collections the wrappers need to finish (park the queue, run
// investigate, write traces, reconcile, score).
type eventRun struct {
	lg        *ledger.Ledger
	tmpls     ledger.Templates
	raw       ingest.Raw
	events    []ingest.NormalizedEvent
	eventByID map[string]ingest.NormalizedEvent

	produced      []score.Produced
	classified    int    // events booked by the rule engine
	agentDone     int    // rule-missed events the resolver booked
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

	er := &eventRun{
		lg:        lg,
		tmpls:     tmpls,
		raw:       raw,
		events:    events,
		eventByID: make(map[string]ingest.NormalizedEvent, len(events)),
	}
	for _, ev := range events {
		er.eventByID[ev.ID] = ev
		c, ok, ruleReason := classify.Classify(ev)
		if ok {
			er.classified++
		} else {
			ac, skip, handled, trace, err := resolve(ev)
			if err != nil {
				return nil, err
			}
			if trace != nil {
				er.traces = append(er.traces, *trace)
			}
			if !handled {
				er.skipped = append(er.skipped, Skip{
					EventID: ev.ID, Type: string(ev.Type), Reason: skipReason(ruleReason, skip),
				})
				er.skippedEvents = append(er.skippedEvents, ev)
				continue
			}
			er.agentDone++
			c = ac
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

// reconcileInput builds the SPEC §7 reconcile input from the posted ledger and the
// raw records (never truth). periodEnd is the exclusive period cutoff. It is a
// method so callers can re-build it after investigate posts (a fresh receivable
// balance) for the re-reconcile.
func (er *eventRun) reconcileInput(periodEnd string) reconcile.Input {
	return reconcile.Input{
		Settlements:       er.raw.Settlements,
		Payments:          er.raw.Payments,
		Refunds:           er.raw.Refunds,
		BankFeed:          er.raw.BankFeed,
		ReceivableBalance: er.lg.AccountBalance(receivableAccount).Balance,
		PeriodEnd:         periodEnd,
		DateToleranceDays: settlementDateToleranceDays,
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
