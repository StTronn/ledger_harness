package score

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/truth"
)

// errorsfile.go defines and persists the errors.json artifact (SPEC §9, §10) —
// the per-run scoring record the close pipeline emits to
// runs/<world>-<period>/errors.json.
//
// # FROZEN SCHEMA (SPEC §9, §13)
//
// RunRecord (and everything it embeds: Totals, AccountDelta, ErrorRecord) is the
// FROZEN learning-layer seam. SPEC §9: "errors.json … is the only artifact the
// future learning layer consumes — freeze its schema." SPEC §13 reinforces:
// "freeze the errors.json and trace schemas early … Changing them later breaks
// the learner."
//
// Therefore:
//
//   - The on-disk shape is locked. The JSON keys below — and their nesting —
//     are a contract. Do NOT rename a field, drop a field, change a field's type,
//     or re-nest anything without BUMPING ErrorsSchemaVersion (and coordinating
//     with the learning layer that consumes it).
//   - schema_version is the FIRST key in every record so a consumer can branch on
//     it before parsing the rest.
//   - The writer (WriteErrors) is deterministic and stable-key: the same Result
//     for the same (world, period) produces byte-identical errors.json across
//     runs (slices are pre-sorted; no Go map is marshalled). The learning layer
//     can therefore diff records across runs meaningfully.
//
// # Money in the record
//
// All money fields are integer paise via money.Money (which marshals as its
// int64 paise), never a float (SPEC §1). got_balance/want_balance/delta are
// signed paise on the account's normal side (see AccountDelta).
//
// # truth/ isolation
//
// This file lives in the scorer, the ONLY package allowed to read truth/gl.json
// (SPEC §4.4). It reads truth via the score package's truth boundary; nothing
// outside the scorer constructs a RunRecord from truth.

// ErrorsSchemaVersion is the schema version stamped into every RunRecord's
// SchemaVersion field. It is FROZEN per SPEC §13: bump it ONLY with a deliberate,
// documented change to the errors.json shape, in lockstep with the learning
// layer that consumes the artifact. v1 ships version 1.
const ErrorsSchemaVersion = 1

// RunRecord is the FROZEN top-level errors.json document (SPEC §9, §13): one
// record per close run, summarizing how the produced ledger scored against the
// hidden ground-truth GL.
//
// Field order here fixes the JSON key order (encoding/json emits struct fields
// in declaration order, and the writer marshals no maps), giving a byte-stable
// artifact. schema_version is first by design so a consumer reads it before the
// body.
//
// FROZEN: see the package/file doc. Any change requires bumping
// ErrorsSchemaVersion.
type RunRecord struct {
	// SchemaVersion is the frozen schema version (ErrorsSchemaVersion). A consumer
	// branches on this before interpreting the rest.
	SchemaVersion int `json:"schema_version"`
	// World and Period identify which (world, period) close produced this record,
	// so a misfiled artifact is detectable and records are self-describing.
	World  string `json:"world"`
	Period string `json:"period"`
	// ScorePct is the PRIMARY metric (SPEC §9): the percentage of truth entries the
	// pipeline booked correctly, integer 0..100 (Result.Percent), computed in
	// integer space.
	ScorePct int `json:"score_pct"`
	// TrialBalanceMatches is the SECONDARY boolean metric (SPEC §9): do the produced
	// and truth ledgers have equal ΣDr and ΣCr totals.
	TrialBalanceMatches bool `json:"trial_balance_matches"`
	// Totals is the count breakdown (denominator + per-class tallies).
	Totals Totals `json:"totals"`
	// PerAccountDeltas is the SECONDARY per-account balance-delta metric (SPEC §9),
	// one row per account that appears in either the produced or truth ledger,
	// sorted by account path. On a clean (100%) run every delta is zero.
	PerAccountDeltas []AccountDelta `json:"per_account_deltas"`
	// Errors is one record per wrong/missing/extra entry (SPEC §9), sorted by event
	// id then class. Empty on a clean run.
	Errors []ErrorRecord `json:"errors"`
}

// Totals is the FROZEN count breakdown in a RunRecord. TruthEntries is the
// denominator of ScorePct (the number of truth entries). Correct/Wrong/Missing/
// Extra tally the four scoring outcomes; Correct + Wrong + Missing == TruthEntries,
// and Extra counts produced entries with no truth counterpart (which do not lower
// the percentage, whose denominator is the truth count).
//
// FROZEN: any change requires bumping ErrorsSchemaVersion.
type Totals struct {
	TruthEntries int `json:"truth_entries"`
	Correct      int `json:"correct"`
	Wrong        int `json:"wrong"`
	Missing      int `json:"missing"`
	Extra        int `json:"extra"`
}

// AccountDelta is one account's balance difference between the produced ledger
// and truth (SPEC §9 "per-account balance deltas"), part of the FROZEN schema.
//
// GotBalance and WantBalance are the account's net balance on its NORMAL side
// (see normalSideForAccount), in signed paise: produced vs truth. Delta is
// GotBalance − WantBalance, so a non-zero Delta is exactly the magnitude (and
// direction) by which the produced books mis-state that account. On a clean run
// every Delta is zero.
//
// FROZEN: any change requires bumping ErrorsSchemaVersion.
type AccountDelta struct {
	Account     string      `json:"account"`
	GotBalance  money.Money `json:"got_balance"`
	WantBalance money.Money `json:"want_balance"`
	Delta       money.Money `json:"delta"`
}

// BuildRunRecord assembles the FROZEN RunRecord for (world, period) from a scored
// Result and the truth GL the Result was scored against. It is a PURE function:
// the same inputs always yield the same record, with deterministically ordered
// per-account deltas (by account path) and errors (Result already sorts them by
// event id). It is the single constructor of the frozen artifact.
//
// produced is the same projection that was passed to Score (the scorer needs the
// produced lines to compute per-account got balances); gl is the truth used for
// the want balances. Passing both keeps BuildRunRecord a pure function of values
// the caller already has — it does not re-read truth.
func BuildRunRecord(world, period string, produced []Produced, gl truth.GL, res Result) RunRecord {
	rec := RunRecord{
		SchemaVersion:       ErrorsSchemaVersion,
		World:               world,
		Period:              period,
		ScorePct:            res.Percent(),
		TrialBalanceMatches: res.TrialBalanceMatches,
		Totals:              totalsFrom(res),
		PerAccountDeltas:    perAccountDeltas(produced, gl),
		Errors:              res.Errors,
	}
	// Errors must never be a JSON null (the learning layer expects an array); an
	// empty run carries [].
	if rec.Errors == nil {
		rec.Errors = []ErrorRecord{}
	}
	return rec
}

// totalsFrom derives the count breakdown from a Result: Correct comes straight
// from the diff, and the per-class tallies are counted from the error records.
func totalsFrom(res Result) Totals {
	t := Totals{TruthEntries: res.Total, Correct: res.Correct}
	for _, e := range res.Errors {
		switch e.Class {
		case ErrWrong:
			t.Wrong++
		case ErrMissing:
			t.Missing++
		case ErrExtra:
			t.Extra++
		}
	}
	return t
}

// perAccountDeltas computes the secondary per-account balance-delta metric (SPEC
// §9): for every account that appears in the produced ledger OR truth, the
// produced (got) and truth (want) net balances on the account's normal side, and
// their difference (got − want). Rows are sorted by account path for a stable,
// byte-identical artifact. An account that matches has a zero delta but is still
// listed, so the record is a complete per-account picture (a clean run is "every
// row delta 0").
func perAccountDeltas(produced []Produced, gl truth.GL) []AccountDelta {
	got := producedBalances(produced)
	want := truthBalances(gl)

	// Union of account paths from both sides, deterministically ordered.
	seen := make(map[string]struct{}, len(got)+len(want))
	for a := range got {
		seen[a] = struct{}{}
	}
	for a := range want {
		seen[a] = struct{}{}
	}
	accounts := make([]string, 0, len(seen))
	for a := range seen {
		accounts = append(accounts, a)
	}
	sort.Strings(accounts)

	deltas := make([]AccountDelta, 0, len(accounts))
	for _, a := range accounts {
		g := got[a]
		w := want[a]
		deltas = append(deltas, AccountDelta{
			Account:     a,
			GotBalance:  g,
			WantBalance: w,
			Delta:       g.Sub(w),
		})
	}
	return deltas
}

// producedBalances folds the produced entries into per-account net balances on
// each account's normal side (the same sign convention as truthBalances), so a
// got-balance compares like-for-like with its want-balance.
func producedBalances(produced []Produced) map[string]money.Money {
	type ds struct{ dr, cr money.Money }
	raw := make(map[string]*ds)
	for _, p := range produced {
		for _, l := range p.Lines {
			d := raw[l.Account]
			if d == nil {
				d = &ds{}
				raw[l.Account] = d
			}
			switch l.Side {
			case string(truth.Debit):
				d.dr = d.dr.Add(l.Amount)
			case string(truth.Credit):
				d.cr = d.cr.Add(l.Amount)
			}
		}
	}
	out := make(map[string]money.Money, len(raw))
	for a, d := range raw {
		out[a] = normalNetForAccount(a, d.dr, d.cr)
	}
	return out
}

// truthBalances folds the truth GL into per-account net balances on each
// account's normal side, the want side of every delta.
func truthBalances(gl truth.GL) map[string]money.Money {
	type ds struct{ dr, cr money.Money }
	raw := make(map[string]*ds)
	for _, e := range gl.Entries {
		for _, l := range e.Lines {
			d := raw[l.Account]
			if d == nil {
				d = &ds{}
				raw[l.Account] = d
			}
			switch l.Side {
			case truth.Debit:
				d.dr = d.dr.Add(l.Amount)
			case truth.Credit:
				d.cr = d.cr.Add(l.Amount)
			}
		}
	}
	out := make(map[string]money.Money, len(raw))
	for a, d := range raw {
		out[a] = normalNetForAccount(a, d.dr, d.cr)
	}
	return out
}

// normalNetForAccount returns an account's net balance stated on its NORMAL side,
// applying the SPEC §4.1 / ledger sign convention WITHOUT importing the ledger or
// the playbook (keeping the scorer's dependency surface minimal). The convention
// is fixed and documented:
//
//	assets, expense       -> normal Debit  (balance = ΣDr − ΣCr)
//	liabilities, income   -> normal Credit (balance = ΣCr − ΣDr)
//
// The account's root (first path segment) selects the normal side. This must
// agree with ledger.normalNet and config.RootType.NormalBalance; the clean-period
// round-trip test (got == want, all deltas 0) is the cross-check that it does.
func normalNetForAccount(account string, dr, cr money.Money) money.Money {
	if normalSideForAccount(account) == truth.Credit {
		return cr.Sub(dr)
	}
	return dr.Sub(cr)
}

// normalSideForAccount maps an account path to its normal side from its root
// segment, per the fixed convention above. An unrecognized root defaults to
// Debit (the conservative ΣDr − ΣCr net); the v1 chart has only the four known
// roots, so this default is never hit in practice but keeps the function total.
func normalSideForAccount(account string) truth.Side {
	switch rootSegment(account) {
	case "liabilities", "income":
		return truth.Credit
	default: // assets, expense, and anything else
		return truth.Debit
	}
}

// rootSegment returns the first path segment of an account path ("income" for
// "income/product-sales"). A rootless path is returned unchanged.
func rootSegment(account string) string {
	for i := 0; i < len(account); i++ {
		if account[i] == '/' {
			return account[:i]
		}
	}
	return account
}

// MarshalErrors encodes a RunRecord to the canonical on-disk JSON form: two-space
// indent, HTML escaping disabled, trailing newline. This is the exact byte format
// WriteErrors persists, so the artifact is reproducible (byte-identical across
// runs) and diffs cleanly. Key order is fixed by the struct tags; the record
// contains no Go maps (PerAccountDeltas and Errors are pre-sorted slices), so
// there is no map-iteration nondeterminism. Exported so a test can assert
// round-trip byte stability.
func MarshalErrors(rec RunRecord) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return nil, fmt.Errorf("score: marshal errors record: %w", err)
	}
	return buf.Bytes(), nil
}

// UnmarshalErrors decodes errors.json bytes back into a RunRecord, rejecting
// unknown keys so a drifted/hand-edited artifact is surfaced rather than silently
// accepted (keeping the FROZEN schema honest, mirroring truth.ReadTruth). It is
// the inverse of MarshalErrors and exists for the round-trip stability test and
// any future consumer reading the artifact in-process.
func UnmarshalErrors(data []byte) (RunRecord, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var rec RunRecord
	if err := dec.Decode(&rec); err != nil {
		return RunRecord{}, fmt.Errorf("score: decode errors record: %w", err)
	}
	if rec.SchemaVersion != ErrorsSchemaVersion {
		return RunRecord{}, fmt.Errorf("score: errors record has schema version %d, want %d (frozen)",
			rec.SchemaVersion, ErrorsSchemaVersion)
	}
	return rec, nil
}

// ErrorsPath resolves runs/<world>-<period>/errors.json under root — the SPEC §9
// artifact location. runs/ is gitignored (it holds generated run output). The
// directory naming is "<world>-<period>" (matching SPEC §9 "runs/<world>-<period>/").
func ErrorsPath(root, world, period string) string {
	return filepath.Join(root, "runs", world+"-"+period, "errors.json")
}

// WriteErrors writes the FROZEN errors.json record for (world, period) under root
// to runs/<world>-<period>/errors.json, creating the run directory as needed. The
// write is ATOMIC (temp file in the same dir, then rename) so an interrupted run
// never leaves a half-written artifact a consumer would read as valid, and
// re-running produces byte-identical content (MarshalErrors is stable). It
// returns the path written so the CLI can report it.
func WriteErrors(root, world, period string, rec RunRecord) (string, error) {
	path := ErrorsPath(root, world, period)
	data, err := MarshalErrors(rec)
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("score: create run dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("score: temp file for %s: %w", filepath.Base(path), err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails before the rename succeeds.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("score: write %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("score: close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("score: finalize %s: %w", filepath.Base(path), err)
	}
	return path, nil
}
