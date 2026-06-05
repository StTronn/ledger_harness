// Package closer wires the deterministic close pipeline end to end (SPEC §5):
//
//	raw      = ingest(world, period)            // internal/ingest
//	events   = normalize(raw)                   // internal/ingest
//	for e in events:
//	  c, ok  = classify(e)                       // internal/classify (rule engine)
//	  if !ok: FLAG + SKIP (Phase 4; agent in Phase 7)
//	  entry  = ledger.Bind(c.EntryType, c.IK, c.Params)
//	  ledger.Post(entry)                          // balance-or-reject
//	result   = score(produced, truth)            // internal/score
//
// It is the Phase-4 orchestrator: hand-written per-event rules book the events,
// unmatched events are flagged and skipped (never sent anywhere yet, never
// crashing the run), and the produced ledger is scored against the hidden truth
// GL to print the deterministic baseline.
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
// closer imports ingest, classify, ledger, config, money, and score. It does NOT
// import internal/truth directly — only internal/score reads truth (SPEC §4.4),
// and closer reaches truth solely through the scorer. The truth-isolation guard
// test confirms closer never imports truth.
package closer

import (
	"fmt"

	"github.com/razorpay/close-agent/internal/classify"
	"github.com/razorpay/close-agent/internal/config"
	"github.com/razorpay/close-agent/internal/ingest"
	"github.com/razorpay/close-agent/internal/ledger"
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
type Result struct {
	Ledger     *ledger.Ledger
	Produced   []score.Produced
	Classified int
	Skipped    []Skip
	Score      score.Result
}

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

	_, events, err := ingest.IngestAndNormalize(root, world, period)
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

	// Scoring needs the period's truth GL. Only the scorer may read truth/gl.json
	// (SPEC §4.4), so closer hands the produced entries to score.RunScore, which
	// loads truth behind its own allowed boundary and returns the diff. closer
	// never names a truth type and never imports internal/truth — the
	// truth-isolation guard confirms it.
	sc, err := score.RunScore(root, world, period, res.Produced)
	if err != nil {
		return Result{}, err
	}
	res.Score = sc
	return res, nil
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
