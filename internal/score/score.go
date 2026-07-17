// Package score diffs a produced double-entry ledger against the hidden
// ground-truth GL and reports how many journal entries are correct (SPEC §9). It
// is the deterministic scoring oracle of the close: the primary metric is the
// percentage of journal entries whose lines (side + account + amount) match
// truth, with the trial-balance-matches boolean as a secondary signal.
//
// # truth/ isolation (SPEC §4.4, §12)
//
// This package is the SCORER: it is the only place outside the seeder permitted
// to read internal/truth (it loads truth/gl.json to compare against). It is on
// the truth-isolation allow-list. The classify/ingest/reconcile stages never
// import it, and it never reaches back into them — it takes already-produced
// entries plus the truth GL and returns a pure diff. Keeping the comparison here,
// behind the truth boundary, means the produced books are built with no knowledge
// of the answer key (the whole point of scoring).
//
// # Matching by event id (1 entry per event in v1)
//
// In the v1 DTC world each event books exactly one entry, and both the produced
// ledger and the truth GL attribute their entries to a source event id. Posting
// ORDER differs between the two (the close pipeline posts in journal/(ts,id)
// order; the seeder posts per settlement batch), so entries are matched by EVENT
// ID, not position. An event present in truth but not produced is MISSING
// (a rule miss that was skipped); one produced but not in truth is EXTRA.
//
// # Money invariant (SPEC §1, §4)
//
// All amounts are integer paise (money.Money). No float touches the math; a guard
// test asserts that statically.
package score

import (
	"sort"

	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/truth"
)

// Line is one produced posting to compare against truth: a side ("Dr"/"Cr"), an
// account path, and a non-negative paise amount. It mirrors a ledger.Line /
// truth.Line without depending on either package's type, so the orchestrator
// projects its posted ledger.Entry onto these and the scorer compares against the
// truth.Line values it reads.
type Line struct {
	Side    string
	Account string
	Amount  money.Money
}

// Produced is one entry the close pipeline posted, attributed to the source event
// it derives from. EventID is the match key against truth (truth.Entry.EventID);
// EntryType and Lines are compared for correctness.
type Produced struct {
	EventID   string
	EntryType string
	TxID      string
	Lines     []Line
}

// ErrorClass categorises a per-entry scoring error, the frozen vocabulary the
// future learning layer clusters on (SPEC §9, §13 "freeze the seams"). v1 emits
// the three structural classes the deterministic close can produce.
type ErrorClass string

const (
	// ErrMissing: truth has an entry for this event but the pipeline produced
	// none (a rule miss that was flagged and skipped, Phase 4).
	ErrMissing ErrorClass = "missing"
	// ErrExtra: the pipeline produced an entry for an event truth has none for.
	ErrExtra ErrorClass = "extra"
	// ErrWrong: both sides have an entry for the event but they differ (entry
	// type or any line's side/account/amount).
	ErrWrong ErrorClass = "wrong"
)

// ErrorRecord is one per-error record keyed to a source event (SPEC §9). Got and
// Want are short human-readable renderings of the produced vs truth entry so the
// record is self-contained for the diff command and the learning seam. EventID +
// Class are the stable fields; the schema is intentionally small and frozen.
type ErrorRecord struct {
	EventID string     `json:"event_id"`
	Class   ErrorClass `json:"error_class"`
	Got     string     `json:"got,omitempty"`
	Want    string     `json:"want,omitempty"`
}

// Result is the outcome of scoring (SPEC §9). Total is the number of truth
// entries (the denominator); Correct is how many produced entries matched their
// truth entry exactly. Errors lists one record per missing/extra/wrong entry, in
// a deterministic order (by event id). TrialBalanceMatches is the secondary
// boolean: do the produced and truth ledgers have equal ΣDr and ΣCr totals.
type Result struct {
	Total               int
	Correct             int
	Errors              []ErrorRecord
	TrialBalanceMatches bool
}

// Percent returns the primary metric: the percentage of truth entries the
// pipeline booked correctly, in integer percent (0..100), computed in integer
// space (no float). With zero truth entries it returns 100 (an empty period is
// trivially fully correct). Rounding is toward zero, so a result only reads 100
// when every entry matched.
func (r Result) Percent() int {
	if r.Total == 0 {
		return 100
	}
	return r.Correct * 100 / r.Total
}

// IsPerfect reports the Phase-4 baseline condition: every truth entry was
// produced and matched exactly, with no extra entries. It is stricter than
// Percent()==100 because it also rules out extras (which do not lower Percent,
// whose denominator is the truth count).
func (r Result) IsPerfect() bool {
	return r.Total > 0 && r.Correct == r.Total && len(r.Errors) == 0
}

// Score diffs the produced entries against the truth GL and returns the Result.
// It is a PURE function of its inputs (SPEC §9 determinism): the same produced
// entries and truth GL always yield the same Result, including the error order
// (sorted by event id). It does not read any file — the caller loads truth via
// truth.ReadTruth and passes it in.
//
// An event id appearing more than once on either side is treated by first
// occurrence for the primary diff; v1 guarantees one entry per event, so this is
// not exercised, but the function never panics on it.
func Score(produced []Produced, gl truth.GL) Result {
	prodByEvent := make(map[string]Produced, len(produced))
	for _, p := range produced {
		if _, dup := prodByEvent[p.EventID]; !dup {
			prodByEvent[p.EventID] = p
		}
	}
	truthByEvent := make(map[string]truth.Entry, len(gl.Entries))
	for _, e := range gl.Entries {
		if _, dup := truthByEvent[e.EventID]; !dup {
			truthByEvent[e.EventID] = e
		}
	}

	res := Result{Total: len(truthByEvent)}

	// Compare every truth entry against the produced entry for its event.
	for eventID, te := range truthByEvent {
		pe, ok := prodByEvent[eventID]
		if !ok {
			res.Errors = append(res.Errors, ErrorRecord{
				EventID: eventID, Class: ErrMissing, Want: renderTruth(te),
			})
			continue
		}
		if entriesMatch(pe, te) {
			res.Correct++
			continue
		}
		res.Errors = append(res.Errors, ErrorRecord{
			EventID: eventID, Class: ErrWrong, Got: renderProduced(pe), Want: renderTruth(te),
		})
	}

	// Any produced entry whose event truth has none for is an extra.
	for eventID, pe := range prodByEvent {
		if _, ok := truthByEvent[eventID]; !ok {
			res.Errors = append(res.Errors, ErrorRecord{
				EventID: eventID, Class: ErrExtra, Got: renderProduced(pe),
			})
		}
	}

	sort.Slice(res.Errors, func(i, j int) bool {
		if res.Errors[i].EventID != res.Errors[j].EventID {
			return res.Errors[i].EventID < res.Errors[j].EventID
		}
		return res.Errors[i].Class < res.Errors[j].Class
	})

	res.TrialBalanceMatches = trialBalanceMatches(produced, gl)
	return res
}

// entriesMatch reports whether a produced entry equals its truth entry for
// scoring: same entry type and the same lines (side + account + amount), compared
// order-independently. Order independence matters because the produced posting
// order within an entry need not match truth's; what counts is the SET of lines.
func entriesMatch(pe Produced, te truth.Entry) bool {
	if pe.EntryType != te.EntryType {
		return false
	}
	if len(pe.Lines) != len(te.Lines) {
		return false
	}
	// Count each (side, account, amount) triple on both sides; equal multisets
	// mean the entries post the same lines.
	type key struct {
		side    string
		account string
		amount  int64
	}
	counts := make(map[key]int, len(pe.Lines))
	for _, l := range pe.Lines {
		counts[key{l.Side, l.Account, l.Amount.Paise()}]++
	}
	for _, l := range te.Lines {
		k := key{string(l.Side), l.Account, l.Amount.Paise()}
		counts[k]--
		if counts[k] < 0 {
			return false
		}
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

// trialBalanceMatches reports whether the produced ledger and the truth GL have
// equal trial-balance totals (ΣDr and ΣCr). It is the secondary metric of SPEC
// §9; equal totals are necessary (not sufficient) for the books to match, so it
// complements the per-entry diff.
func trialBalanceMatches(produced []Produced, gl truth.GL) bool {
	var pDr, pCr money.Money
	for _, p := range produced {
		for _, l := range p.Lines {
			switch l.Side {
			case string(truth.Debit):
				pDr = pDr.Add(l.Amount)
			case string(truth.Credit):
				pCr = pCr.Add(l.Amount)
			}
		}
	}
	tDr, tCr := gl.SumBySide()
	return pDr == tDr && pCr == tCr
}
