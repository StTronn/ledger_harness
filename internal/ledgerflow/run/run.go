// Package run wires the deterministic ledger flow end to end (SPEC §5):
//
//	raw      = ingest(world, period)            // internal/ingest
//	events   = normalize(raw)                   // internal/ingest
//	for e in events:
//	  c, ok  = posting.Classify(e)
//	  if !ok: recovery decides whether to post or review
//	  entry  = ledger.Bind(c.EntryType, c.IK, c.Params)
//	  ledger.Post(entry)                          // balance-or-reject
//	breaks   = reconcile(ledger, raw.settlements, bankFeed)  // internal/reconcile (3 checks)
//	result   = score(produced, truth)            // internal/score
//
// It is the flow orchestrator: hand-written per-event rules book the known
// events; on a rule miss the recovery engine decides whether a safe candidate
// can return to posting. Only unresolved cases reach the judgment agent.
// After all deterministic events post, the SPEC §7
// three checks reconcile the ledger against the settlements and the independent
// bank feed and LIST any breaks (no agent resolves reconcile breaks yet); and the
// produced ledger is scored against the hidden truth GL.
//
// # The judgment agent seam (Phase 7a, SPEC §5, §8, §11, §12)
//
// Recovery calls the swappable §8 agent client only when it cannot produce a safe
// candidate. The agent returns a recommendation (never raw debits/credits); the
// recommendation is recorded in a FROZEN trace and the event remains unposted.
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
package run

import (
	"fmt"
	"time"

	"github.com/razorpay/ledger-flow/internal/agentclient"
	"github.com/razorpay/ledger-flow/internal/ledger"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/recovery"
	"github.com/razorpay/ledger-flow/internal/reconcile"
	"github.com/razorpay/ledger-flow/internal/score"
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
	Ledger        *ledger.Ledger
	Produced      []score.Produced
	Classified    int                 // events booked by the rule engine
	AgentMode     agentclient.Mode    // which agent mode this run used (off/replay/live)
	AgentReviewed int                 // rule-missed events reviewed by the agent
	Traces        []agentclient.Trace // FROZEN agent traces, one per agent consultation (SPEC §9, §13)
	TracePath     string              // path the trace artifact was written to (empty if none)
	Skipped       []Skip
	Breaks        []reconcile.Break // SPEC §7 breaks left UNRESOLVED after investigate (empty = fully reconciled)
	// Investigate seam (Phase 8, SPEC §7, §8). InvestigateReviewed is the number of
	// breaks the §8 investigate agent reviewed; recommendations are logged but never
	// posted automatically. Escalations lists the breaks left for human review;
	// InvestigateTraces is the FROZEN trace of every investigate consultation and
	// InvestigateTracePath where they were written.
	InvestigateReviewed  int
	Escalations          []Escalation
	InvestigateTraces    []agentclient.InvestigateTrace
	InvestigateTracePath string
	Score                score.Result
	Record               score.RunRecord // FROZEN errors.json record (SPEC §9, §13)
	ErrorsPath           string          // path the errors.json artifact was written to
}

// Escalation records one reconcile break that was NOT resolved by a posting: the
// agent escalated it ({escalate, reason}) or the agent is off (the break is listed
// as-is). It carries the break's stable key and kind plus the human-readable
// reason, so the operator can tell an escalated break from a resolved one. The
// break itself remains in Result.Breaks.
type Escalation struct {
	BreakKey string
	Kind     string
	Reason   string
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

// codReceivableAccount is the COD-rail receivable (ROADMAP §8.3) whose period-end
// balance the COD twin of check #3 clears. Like receivableAccount it is named
// here, not read from truth.
const codReceivableAccount = "assets/cod-receivable"

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
	// Resolve the §8 classify agent for the requested mode (the fixture loads lazily
	// on the first miss; the client NEVER reads truth). The agent supplies the shared
	// spine's miss-resolver strategy.
	agent, mode, err := newAgent(root, world, period, opts)
	if err != nil {
		return Result{}, err
	}

	// Run the shared book loop with the inline-agent resolver (SPEC §5).
	er, err := runEvents(root, world, period, agent.resolver())
	if err != nil {
		return Result{}, err
	}
	res := Result{
		Ledger:        er.lg,
		AgentMode:     mode,
		Produced:      er.produced,
		Classified:    er.classified,
		AgentReviewed: er.agentReviewed,
		Traces:        er.traces,
		Skipped:       er.skipped,
	}

	// Persist the classify traces (if any) so `show trace` can print them. The
	// agent-off baseline produces none.
	if len(res.Traces) > 0 {
		tp, err := writeTraces(root, world, period, res.Traces)
		if err != nil {
			return Result{}, err
		}
		res.TracePath = tp
	}

	// Reconcile (SPEC §5, §7) over the posted ledger + raw records.
	periodEnd, err := nextMonthFirst(period)
	if err != nil {
		return Result{}, err
	}
	res.Breaks = reconcile.Reconcile(er.reconcileInput(periodEnd))
	// Refresh the read-only recovery snapshot with the discovered breaks before
	// handing investigation context to the agent.
	er.recovery = recovery.New(er.lg, er.events, er.raw, er.orders, res.Breaks, er.ratecard)

	// Investigate (SPEC §7, §8, Phase 8): when the agent is enabled, hand each break
	// to the §8 investigate agent for review. Record its recommendation and keep the
	// break listed; no agent response is bound or posted.
	if agent.enabled() && len(res.Breaks) > 0 {
		candidates := summarizeEvents(er.skippedEvents)
		if err := investigateBreaks(agent, er.recovery, candidates, &res); err != nil {
			return Result{}, err
		}
	}
	if len(res.InvestigateTraces) > 0 {
		ip, err := writeInvestigateTraces(root, world, period, res.InvestigateTraces)
		if err != nil {
			return Result{}, err
		}
		res.InvestigateTracePath = ip
	}

	// Score against truth (scorer-only boundary) + emit the frozen errors.json.
	sc, rec, path, err := scoreAndWrite(root, world, period, res.Produced)
	if err != nil {
		return Result{}, err
	}
	res.Score = sc
	res.Record = rec
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
