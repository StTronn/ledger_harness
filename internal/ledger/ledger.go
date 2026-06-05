// Package ledger is the deterministic double-entry posting engine of
// close-agent. It is the ONLY source of money truth (SPEC §1 golden rule, §6):
// every entry either BALANCES and is posted, or is REJECTED — it is never
// silently fixed. The ledger knows NOTHING about Razorpay; it speaks only in
// chart-of-accounts paths, debits/credits, and integer paise.
//
// # Sign / normal-balance convention (the one convention, documented once)
//
// A posted Line carries a POSITIVE magnitude (internal/money.Money, integer
// paise) tagged with a Side — Debit (Dr) or Credit (Cr). There are no negative
// line amounts: direction is expressed by the side, never by the sign of the
// number. An Entry is BALANCED iff the sum of its Dr magnitudes equals the sum
// of its Cr magnitudes (ΣDr == ΣCr). post() enforces this and rejects anything
// else.
//
// An account's reported balance is derived from its NORMAL side, which comes
// from its root type (config.RootType.NormalBalance):
//
//	assets, expense       -> normal Debit  (balance = ΣDr − ΣCr)
//	liabilities, income   -> normal Credit (balance = ΣCr − ΣDr)
//
// So a normal-Debit account reads positive when its debits exceed its credits,
// and a normal-Credit account reads positive when its credits exceed its
// debits. This is the standard double-entry convention and is consistent with
// the accounting equation Assets − Liabilities = Income − Expenses (SPEC §4.1).
// The reports reuse exactly this rule so there is a single place that defines
// what a balance "means".
//
// # Determinism
//
// The ledger holds posted entries in memory in posting order. Every report is a
// PURE function of those entries (no hidden side-state, no wall-clock, no
// randomness): the same sequence of posts yields byte-identical reports.
//
// # Idempotency
//
// Posting is idempotent on the idempotency key (IK). Re-posting the same IK with
// byte-identical bound content is a no-op that returns the already-posted entry.
// Re-posting the same IK with DIFFERENT content is an error — the ledger never
// overwrites or merges.
package ledger

import (
	"errors"
	"fmt"

	"github.com/razorpay/close-agent/internal/money"
)

// Side is the debit/credit side a posted line sits on. It mirrors
// config.Side ("Dr"/"Cr") but is redeclared here so the ledger's posted-entry
// types do not depend on the playbook package's symbolic template types.
type Side string

const (
	// Debit (Dr) and Credit (Cr) are the only two sides a line may carry.
	Debit  Side = "Dr"
	Credit Side = "Cr"
)

// Valid reports whether s is one of the two known sides.
func (s Side) Valid() bool { return s == Debit || s == Credit }

// Line is a single concrete posting: a positive Amount of money on one Side of
// one Account (a chart-of-accounts path such as "income/product-sales").
//
// INVARIANT: Amount is a non-negative magnitude in paise. Direction lives in
// Side, never in the sign of Amount. post() rejects negative amounts.
type Line struct {
	Side    Side
	Account string
	Amount  money.Money
}

// Entry is a balanced set of Lines posted together under one idempotency key.
// Type is the playbook entry-type name it was bound from (e.g. "dtc_sale"),
// kept for the journal and for audit; it has no effect on balancing. IK is the
// idempotency key. TxID, if non-empty, is the external transaction id stamped
// from the entry type's tx_param (e.g. a payment_id / bank_tx_id), recorded for
// reconciliation; the ledger itself does not interpret it.
//
// Ts is an OPTIONAL event timestamp in Unix seconds (the normalized event's
// "ts", SPEC §4.3). It is purely a reporting filter axis: the period-scoped
// reports (TrialBalance/BalanceSheet/IncomeStatement/Journal with a Period)
// select entries by Ts. Zero means "unstamped"; an unstamped entry is included
// only by an unbounded report (report-over-all), which is the Phase 1 default.
// Ts has NO effect on balancing or on posting order, and the ledger never reads
// a wall clock to fill it — it is supplied by the caller, preserving
// determinism.
type Entry struct {
	Type  string
	IK    string
	TxID  string
	Ts    int64
	Lines []Line
}

// sumByside returns the total Dr magnitude and total Cr magnitude of the
// entry's lines, in paise. It is the single definition of "the two sides of an
// entry" used by both balance checking and posting.
func (e Entry) sumBySide() (dr, cr money.Money) {
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

// Sentinel errors. Callers may test with errors.Is; the wrapped message carries
// the specifics (which account, which IK, the actual sums).
var (
	// ErrUnbalanced is returned when ΣDr != ΣCr. Nothing is posted.
	ErrUnbalanced = errors.New("ledger: entry does not balance")
	// ErrUnknownAccount is returned when a line names an account not in the
	// chart. Nothing is posted.
	ErrUnknownAccount = errors.New("ledger: unknown account")
	// ErrBadLine is returned for a structurally invalid line: no lines, an
	// invalid side, or a negative amount. Nothing is posted.
	ErrBadLine = errors.New("ledger: invalid line")
	// ErrIKConflict is returned when an IK is re-posted with different content.
	// The existing entry is left untouched.
	ErrIKConflict = errors.New("ledger: idempotency-key conflict")
	// ErrEmptyIK is returned when an entry is posted with an empty IK.
	ErrEmptyIK = errors.New("ledger: empty idempotency key")
)

// accountSet is the contract the ledger needs from a chart of accounts: tell me
// whether a path exists, and tell me a path's normal side. The playbook
// (config.Playbook) satisfies this via a thin adapter (see PlaybookChart), which
// keeps the ledger free of any import of the schema-loading package and lets
// tests supply a hand-built chart.
type accountSet interface {
	// HasAccount reports whether path is a known chart account.
	HasAccount(path string) bool
	// NormalSide returns the account's normal side (Debit/Credit). It is only
	// called for paths for which HasAccount returned true.
	NormalSide(path string) Side
}

// Ledger is an in-memory store of posted entries plus the chart that bounds
// which accounts may be posted to. It is not safe for concurrent use; the
// close workflow drives it from a single goroutine (SPEC §5), and keeping it
// lock-free preserves determinism and simplicity.
type Ledger struct {
	chart   accountSet
	entries []Entry        // posting order, the source of truth for reports
	byIK    map[string]int // IK -> index into entries, for idempotency
}

// New constructs an empty Ledger bound to the given chart. The chart is used
// only to validate account paths and to resolve normal sides for reports.
func New(chart accountSet) *Ledger {
	return &Ledger{
		chart: chart,
		byIK:  make(map[string]int),
	}
}

// Post validates and posts an entry under balance-or-reject + idempotency rules
// (SPEC §6):
//
//  1. The IK must be non-empty.
//  2. If the IK already exists: identical bound content is a no-op returning the
//     existing entry; different content returns ErrIKConflict and changes
//     nothing.
//  3. Every line must be structurally valid (known side, non-negative amount)
//     and name an account that exists in the chart, else ErrBadLine /
//     ErrUnknownAccount — and NOTHING is posted.
//  4. ΣDr must equal ΣCr, else ErrUnbalanced — and NOTHING is posted.
//
// On success the entry is appended in posting order and indexed by IK; the
// returned Entry is the stored copy. All validation happens before any mutation,
// so a rejected entry leaves the ledger byte-identical to before the call.
func (lg *Ledger) Post(e Entry) (Entry, error) {
	if e.IK == "" {
		return Entry{}, fmt.Errorf("%w", ErrEmptyIK)
	}

	// Idempotency is checked first: a duplicate IK with identical content must
	// succeed as a no-op even before we re-validate, and a conflicting IK must
	// fail without touching anything.
	if idx, ok := lg.byIK[e.IK]; ok {
		existing := lg.entries[idx]
		if entriesEqual(existing, e) {
			return existing, nil
		}
		return Entry{}, fmt.Errorf("%w: IK %q already posted with different content", ErrIKConflict, e.IK)
	}

	if len(e.Lines) == 0 {
		return Entry{}, fmt.Errorf("%w: entry %q has no lines", ErrBadLine, e.IK)
	}
	for i, l := range e.Lines {
		if !l.Side.Valid() {
			return Entry{}, fmt.Errorf("%w: entry %q line %d has invalid side %q", ErrBadLine, e.IK, i, l.Side)
		}
		if l.Amount.Sign() < 0 {
			return Entry{}, fmt.Errorf("%w: entry %q line %d amount %s is negative (use the side, not a sign)",
				ErrBadLine, e.IK, i, l.Amount)
		}
		if !lg.chart.HasAccount(l.Account) {
			return Entry{}, fmt.Errorf("%w: entry %q line %d account %q", ErrUnknownAccount, e.IK, i, l.Account)
		}
	}

	dr, cr := e.sumBySide()
	if dr != cr {
		return Entry{}, fmt.Errorf("%w: entry %q ΣDr=%s ΣCr=%s", ErrUnbalanced, e.IK, dr, cr)
	}

	// Store a defensive copy so a caller mutating its Lines slice after posting
	// cannot retroactively change the ledger (determinism / purity).
	stored := e
	stored.Lines = append([]Line(nil), e.Lines...)
	lg.byIK[e.IK] = len(lg.entries)
	lg.entries = append(lg.entries, stored)
	return stored, nil
}

// Entries returns a copy of the posted entries in posting order. It is a pure
// read; the returned slice and its line slices are copies, so callers cannot
// mutate the ledger through it.
func (lg *Ledger) Entries() []Entry {
	out := make([]Entry, len(lg.entries))
	for i, e := range lg.entries {
		e.Lines = append([]Line(nil), e.Lines...)
		out[i] = e
	}
	return out
}

// Len returns the number of posted entries.
func (lg *Ledger) Len() int { return len(lg.entries) }

// entriesEqual reports whether two entries have identical bound content for the
// purposes of idempotency: same type, IK, tx id, timestamp, and exact same
// lines in the same order. Two posts that produce the same content under one IK
// are "the same"; any difference (reordered lines, different amount, different
// account, different ts) is a conflict.
func entriesEqual(a, b Entry) bool {
	if a.Type != b.Type || a.IK != b.IK || a.TxID != b.TxID || a.Ts != b.Ts {
		return false
	}
	if len(a.Lines) != len(b.Lines) {
		return false
	}
	for i := range a.Lines {
		if a.Lines[i] != b.Lines[i] {
			return false
		}
	}
	return true
}
