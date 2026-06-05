// Package closer wires the deterministic close pipeline end to end (SPEC §5):
//
//	raw      = ingest(world, period)            // internal/ingest
//	events   = normalize(raw)                   // internal/ingest
//	for e in events:
//	  c, ok  = classify(e)                       // internal/classify (rule engine)
//	  if !ok: FLAG + SKIP (Phase 4; agent in Phase 7)
//	  entry  = ledger.Bind(c.EntryType, c.IK, c.Params)
//	  ledger.Post(entry)                          // balance-or-reject
//	breaks   = reconcile(ledger, raw.settlements, bankFeed)  // internal/reconcile (3 checks)
//	result   = score(produced, truth)            // internal/score
//
// It is the Phase-4/5/7a orchestrator: hand-written per-event rules book the
// events; on a rule MISS the §8 classify agent is consulted IF the agent is
// enabled (Phase 7a, see below), otherwise the miss is flagged and skipped (the
// documented Phase-4 agent-off baseline). After all events post, the SPEC §7
// three checks reconcile the ledger against the settlements and the independent
// bank feed and LIST any breaks (no agent resolves reconcile breaks yet); and the
// produced ledger is scored against the hidden truth GL.
//
// # The classify agent seam (Phase 7a, SPEC §5, §8, §11, §12)
//
// On a rule miss, when opts.Agent is replay/live, closer asks the swappable §8
// agent client (internal/agentclient) to classify the event. The agent returns
// ONLY {entry_type, params} (never raw debits/credits); closer derives the IK and
// TxID from the event the SAME way the rule engine does, binds the agent's params
// through the ledger (balance-or-reject), and posts the result — so an
// agent-classified entry is indistinguishable from a rule-classified one to the
// rest of the pipeline. Every agent consultation emits a FROZEN trace (SPEC §9,
// §13), and Run writes the traces to runs/<world>-<period>/trace.json so
// `show trace` can print them. In CI the client is REPLAY (deterministic, no
// LLM), so a hard period that scores PARTIAL with rules only RISES to ~100% with
// agent replay, byte-deterministically. With opts.Agent off, closer is exactly
// the Phase-4 flag-and-skip baseline.
//
// # Determinism (SPEC §5, §12)
//
// Given the same fixtures the pipeline produces a byte-identical ledger and
// score: events are normalized in (ts, id) order, classification is a pure
// function, posting is order-deterministic, and scoring matches by event id
// independent of order. No wall clock, no randomness, no map-iteration leaking
// into output.
//
// # Boundaries
//
// closer imports ingest, classify, ledger, reconcile, config, money, and score.
// It does NOT import internal/truth directly — only internal/score reads truth
// (SPEC §4.4), and closer reaches truth solely through the scorer. The
// truth-isolation guard test confirms closer (and reconcile) never import truth.
package closer

import (
	"fmt"
	"time"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/classify"
	"github.com/razorpay/close-agent/internal/config"
	"github.com/razorpay/close-agent/internal/ingest"
	"github.com/razorpay/close-agent/internal/ledger"
	"github.com/razorpay/close-agent/internal/reconcile"
	"github.com/razorpay/close-agent/internal/score"
)

// Skip records one event that ended up UNbooked: the source event id and type,
// plus the human-readable reason. A skip happens when the rule engine missed AND
// either the agent is off (the Phase-4 baseline) or the agent ESCALATED the event
// as {unclassifiable, reason} (Phase 7a). It is reported, never crashed and never
// silently dropped. Slice order follows the event journal order.
type Skip struct {
	EventID string
	Type    string
	Reason  string
}

// Result is the outcome of a close run (SPEC §5): the produced ledger, the
// classification tallies, the events that were skipped, and the score against
// truth. Ledger is the in-memory posted ledger so the caller can render reports;
// Produced is the projection scored against truth.
//
// Record is the FROZEN errors.json RunRecord built from the score (SPEC §9, §13);
// ErrorsPath is where Run persisted it (runs/<world>-<period>/errors.json). The
// CLI prints the path so the operator knows the learning seam was written.
type Result struct {
	Ledger     *ledger.Ledger
	Produced   []score.Produced
	Classified int                 // events booked by the rule engine
	AgentMode  agentclient.Mode    // which agent mode this run used (off/replay/live)
	AgentDone  int                 // rule-missed events the agent classified and booked
	Traces     []agentclient.Trace // FROZEN agent traces, one per agent consultation (SPEC §9, §13)
	TracePath  string              // path the trace artifact was written to (empty if none)
	Skipped    []Skip
	Breaks     []reconcile.Break // SPEC §7 reconciliation breaks (Phase 5; empty = clean)
	Score      score.Result
	Record     score.RunRecord // FROZEN errors.json record (SPEC §9, §13)
	ErrorsPath string          // path the errors.json artifact was written to
}

// Options controls a close run (SPEC §5, §11 Phase 7a). Agent selects the
// classify-agent mode for rule misses: ModeOff (the Phase-4 flag-and-skip
// baseline — the default), ModeReplay (the CI-safe recorded-response client), or
// ModeLive (post to a Flue endpoint; not exercised in CI). LiveBaseURL is the
// Flue agent host used only in ModeLive. The zero value is the agent-off baseline.
type Options struct {
	Agent       agentclient.Mode
	LiveBaseURL string
}

// receivableAccount is the chart path whose period-end balance must clear to ~0
// (SPEC §7 check #3, §4.1). It is named here, not read from truth, so reconcile
// stays a pure function over the posted ledger.
const receivableAccount = "assets/razorpay-settlement-receivable"

// Run executes the close pipeline for (world, period) under root with the agent
// OFF — the documented Phase-4 flag-and-skip baseline — and scores it against the
// period's truth GL. It is a thin wrapper over RunWith for the common case and
// keeps the existing call sites unchanged.
func Run(root, world, period string) (Result, error) {
	return RunWith(root, world, period, Options{})
}

// RunWith executes the close pipeline for (world, period) under root with the
// given Options and scores it against the period's truth GL. root is the base
// directory containing worlds/.
//
// It returns an error only for hard failures (a fixture missing, a malformed
// truth GL, an entry that fails to bind or post, or — in an enabled agent mode —
// a malformed recorded-response fixture). An unmatched event is NOT an error: it
// is classified by the agent if enabled, or recorded as a Skip otherwise, and the
// run continues. An event the agent ESCALATES is likewise a Skip, never an error.
func RunWith(root, world, period string, opts Options) (Result, error) {
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

	// Resolve the §8 classify agent for the requested mode. The mode is validated
	// here, but the recorded-response fixture is loaded LAZILY on the first rule
	// miss (so a clean period needs no fixture in replay mode); a missing/malformed
	// fixture is then a hard error. The client NEVER reads truth.
	agent, mode, err := newAgent(root, world, period, opts)
	if err != nil {
		return Result{}, err
	}

	res := Result{Ledger: lg, AgentMode: mode}
	for _, ev := range events {
		c, ok, reason := classify.Classify(ev)
		if ok {
			res.Classified++
		} else {
			// Rule miss. Consult the agent if enabled; otherwise flag-and-skip.
			ac, agentReason, handled, err := classifyWithAgent(agent, ev, &res)
			if err != nil {
				return Result{}, err
			}
			if !handled {
				res.Skipped = append(res.Skipped, Skip{
					EventID: ev.ID, Type: string(ev.Type), Reason: skipReason(reason, agentReason),
				})
				continue
			}
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

	// Persist the agent traces (if any) to runs/<world>-<period>/trace.json so the
	// `show trace` command can print them (SPEC §9, §10). The deterministic agent-
	// off baseline produces none and writes no file.
	if len(res.Traces) > 0 {
		tp, err := writeTraces(root, world, period, res.Traces)
		if err != nil {
			return Result{}, err
		}
		res.TracePath = tp
	}

	// Reconcile (SPEC §5, §7): after every event has posted, run the three checks
	// over the posted ledger, the raw settlements, the raw batch members, and the
	// independent bank feed. The receivable balance is read from the ledger we just
	// built (the only money truth); reconcile itself reads no truth and no ledger
	// internals — closer hands it plain values, keeping the §7 checks pure. In
	// Phase 5 there is no agent: breaks are detected and listed on the Result.
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

	// Scoring needs the period's truth GL. Only the scorer may read truth/gl.json
	// (SPEC §4.4), so closer hands the produced entries to score.RunScoreRecord,
	// which loads truth behind its own allowed boundary and returns BOTH the diff
	// and the FROZEN errors.json RunRecord (SPEC §9, §13). closer never names a
	// truth type and never imports internal/truth — the truth-isolation guard
	// confirms it.
	sc, rec, err := score.RunScoreRecord(root, world, period, res.Produced)
	if err != nil {
		return Result{}, err
	}
	res.Score = sc
	res.Record = rec

	// Emit the frozen errors.json artifact to runs/<world>-<period>/ (SPEC §9, §10).
	// This is the single learning-layer seam; a deterministic close writes it on
	// every run. runs/ is gitignored generated output.
	path, err := score.WriteErrors(root, world, period, rec)
	if err != nil {
		return Result{}, err
	}
	res.ErrorsPath = path
	return res, nil
}

// settlementDateToleranceDays is the allowed gap (in days) between a settlement's
// date and its bank-credit value date for the check #1 match (SPEC §7 "date
// within a small tolerance"). Razorpay deposits land same-day or a day or two
// later; 3 days comfortably covers a T+2 settlement without masking a genuinely
// misdated credit.
const settlementDateToleranceDays = 3

// nextMonthFirst returns the first day of the month AFTER the given YYYY-MM
// period, as a YYYY-MM-DD string — the exclusive period-end cutoff reconcile
// uses to classify a settlement's bank credit as genuine T+2 in-transit (SPEC §7
// check #3). It uses time only as a calendar calculator in UTC (no wall clock),
// so it is deterministic.
func nextMonthFirst(period string) (string, error) {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return "", fmt.Errorf("closer: invalid period %q (want YYYY-MM): %w", period, err)
	}
	next := t.AddDate(0, 1, 0).UTC()
	return next.Format("2006-01-02"), nil
}

// producedFrom projects a posted ledger.Entry onto the score.Produced shape the
// scorer compares against truth, attributing it to its source event id. The line
// projection is a direct copy of side/account/amount; the scorer matches lines as
// a set, so order is irrelevant.
func producedFrom(eventID string, e ledger.Entry) score.Produced {
	lines := make([]score.Line, len(e.Lines))
	for i, l := range e.Lines {
		lines[i] = score.Line{
			Side:    string(l.Side),
			Account: l.Account,
			Amount:  l.Amount,
		}
	}
	return score.Produced{
		EventID:   eventID,
		EntryType: e.Type,
		TxID:      e.TxID,
		Lines:     lines,
	}
}
