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
// It is the Phase-4/5 orchestrator: hand-written per-event rules book the events,
// unmatched events are flagged and skipped (never sent anywhere yet, never
// crashing the run); after all events post, the SPEC §7 three checks reconcile
// the ledger against the settlements and the independent bank feed and LIST any
// breaks (Phase 5 — no agent resolves them yet); and the produced ledger is
// scored against the hidden truth GL to print the deterministic baseline.
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

	"github.com/razorpay/close-agent/internal/classify"
	"github.com/razorpay/close-agent/internal/config"
	"github.com/razorpay/close-agent/internal/ingest"
	"github.com/razorpay/close-agent/internal/ledger"
	"github.com/razorpay/close-agent/internal/reconcile"
	"github.com/razorpay/close-agent/internal/score"
)

// Skip records one event the rule engine could not classify: the source event id
// and type, plus the human-readable reason. In Phase 4 a skip is reported (not
// crashed and not silently dropped); in Phase 7 these are exactly the events
// handed to the agent. Slice order follows the event journal order.
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
	Classified int
	Skipped    []Skip
	Breaks     []reconcile.Break // SPEC §7 reconciliation breaks (Phase 5; empty = clean)
	Score      score.Result
	Record     score.RunRecord // FROZEN errors.json record (SPEC §9, §13)
	ErrorsPath string          // path the errors.json artifact was written to
}

// receivableAccount is the chart path whose period-end balance must clear to ~0
// (SPEC §7 check #3, §4.1). It is named here, not read from truth, so reconcile
// stays a pure function over the posted ledger.
const receivableAccount = "assets/razorpay-settlement-receivable"

// Run executes the close pipeline for (world, period) under root and scores it
// against the period's truth GL. root is the base directory containing worlds/.
//
// It returns an error only for hard failures (a fixture missing, a malformed
// truth GL, an entry that fails to bind or post — all of which mean something is
// wrong with the substrate or the playbook, not an ordinary unmatched event).
// An unmatched event is NOT an error: it is recorded as a Skip and the run
// continues, exactly as Phase 4 requires.
func Run(root, world, period string) (Result, error) {
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

	res := Result{Ledger: lg}
	for _, ev := range events {
		c, ok, reason := classify.Classify(ev)
		if !ok {
			res.Skipped = append(res.Skipped, Skip{
				EventID: ev.ID, Type: string(ev.Type), Reason: reason,
			})
			continue
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
		res.Classified++
		res.Produced = append(res.Produced, producedFrom(ev.ID, posted))
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
