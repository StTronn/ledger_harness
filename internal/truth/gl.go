// Package truth defines the schema of the hidden ground-truth ledger
// (truth/gl.json) — the double-entry journal the scorer diffs the agent's
// produced ledger against (SPEC §4.4, §9).
//
// # Isolation invariant (SPEC §4.4, §12)
//
// truth/gl.json is GROUND TRUTH and must NEVER be read by ingest, normalize,
// classify, reconcile, or any agent — only the scorer reads it (and the seeder
// writes it). To enforce that at the package level, this package is the ONLY
// place that defines the truth GL types, and a guard test
// (internal/truth/isolation_test.go) asserts that the import graph of this
// package is reached only from the allowed packages: the seeder writes it, the
// (future) scorer reads it, and the truth package itself. ingest/normalize/
// classify/reconcile/agents importing this package is a build-the-wrong-thing
// defect that the guard test fails on.
//
// # Money & sign convention
//
// All amounts are integer minor units — paise — carried by internal/money.Money
// (no float touches money, SPEC §1). A GL line sits on a Side (Dr/Cr) with a
// non-negative magnitude exactly like a posted ledger.Line: direction is the
// side, never the sign of the number. A GL balances iff ΣDr == ΣCr over all of
// its lines, which is the property the seeder guarantees by construction and a
// test asserts.
//
// This package holds only data + pure helpers; it imports internal/money and
// nothing else from the project, so it cannot accidentally pull in ingest or
// classify logic.
package truth

import "github.com/razorpay/close-agent/internal/money"

// Side is the debit/credit side a ground-truth line sits on. It mirrors
// ledger.Side ("Dr"/"Cr") but is redeclared here so the truth schema does not
// depend on the posting engine (and vice versa): the truth GL is a standalone
// record the scorer compares against, not something produced by the ledger.
type Side string

const (
	// Debit (Dr) and Credit (Cr) are the only two sides a truth line may carry.
	Debit  Side = "Dr"
	Credit Side = "Cr"
)

// Valid reports whether s is one of the two known sides.
func (s Side) Valid() bool { return s == Debit || s == Credit }

// Line is a single ground-truth posting: a non-negative Amount of money in paise
// on one Side of one chart-of-accounts Account path (e.g. "income/product-sales").
//
// Amount is serialized as an integer paise count (JSON number) so the on-disk
// truth GL is exact and float-free; money.Money marshals as its int64 paise.
type Line struct {
	Side    Side        `json:"side"`
	Account string      `json:"account"`
	Amount  money.Money `json:"amount"`
}

// Entry is one balanced ground-truth journal entry: the entry type it was
// generated from, a stable id, the source event id it derives from (so the
// scorer can attribute per-error records to an event, SPEC §9), and its lines.
//
// EntryType / EventID / TxID are kept human-readable so a person can eyeball the
// truth GL. Balancing is a property of Lines alone (ΣDr == ΣCr).
type Entry struct {
	ID        string `json:"id"`
	EntryType string `json:"entry_type"`
	EventID   string `json:"event_id"`
	TxID      string `json:"tx_id,omitempty"`
	Ts        int64  `json:"ts"`
	Lines     []Line `json:"lines"`
}

// SumBySide returns the total Dr magnitude and total Cr magnitude of the entry's
// lines, in paise. It is the single definition of "the two sides of an entry"
// used by IsBalanced and by the seeder's by-construction balance guarantee.
func (e Entry) SumBySide() (dr, cr money.Money) {
	for _, l := range e.Lines {
		switch l.Side {
		case Debit:
			dr = dr.Add(l.Amount)
		case Credit:
			cr = cr.Add(l.Amount)
		}
	}
	return dr, cr
}

// IsBalanced reports whether this single entry balances (ΣDr == ΣCr).
func (e Entry) IsBalanced() bool {
	dr, cr := e.SumBySide()
	return dr == cr
}

// GL is the ground-truth ledger: a metadata header plus the ordered list of
// journal entries. The header records which (world, period) the GL belongs to so
// a misfiled truth file is caught, and a schema Version so the frozen format can
// evolve deliberately (SPEC §9, §13 "freeze the seams").
//
// The on-disk shape is locked to {"version", "world", "period", "entries":[...]}
// and the JSON keys are fixed by these struct tags, giving stable key ordering.
type GL struct {
	Version int     `json:"version"`
	World   string  `json:"world"`
	Period  string  `json:"period"`
	Entries []Entry `json:"entries"`
}

// SchemaVersion is the current truth-GL schema version stamped into GL.Version.
// Bump it only with a deliberate, documented format change (it is a frozen seam,
// SPEC §13).
const SchemaVersion = 1

// SumBySide returns the GL-wide Dr and Cr totals across every entry's lines.
func (g GL) SumBySide() (dr, cr money.Money) {
	for _, e := range g.Entries {
		ed, ec := e.SumBySide()
		dr = dr.Add(ed)
		cr = cr.Add(ec)
	}
	return dr, cr
}

// IsBalanced reports whether the whole GL balances: ΣDr == ΣCr across all
// entries. Because every entry balances by construction, the GL total balances
// too; the seeder asserts both levels.
func (g GL) IsBalanced() bool {
	dr, cr := g.SumBySide()
	return dr == cr
}
