// Package config loads the playbook — the chart of accounts plus the
// entry-type templates — from a human-readable JSON file (config/playbook.json).
//
// The playbook is the single source of schema truth in close-agent: the rule
// engine reads it to classify events, the ledger reads it to expand and validate
// entries, and (later) the agent Skill is generated from it so the agent and the
// engine cannot drift (SPEC §6, §8). Because the future learning layer edits the
// playbook rather than code (SPEC §13), the schema is fully data-driven here.
//
// # Money & sign convention (documented here, enforced by the ledger's post())
//
// All money is integer minor units — paise — carried by internal/money.Money.
// No float touches money anywhere (SPEC §1, §4). This package stores no amounts:
// templates carry symbolic expressions (param names combined with +/-), and the
// concrete paise are bound at post time.
//
// Each account knows its root type, which fixes its normal balance:
//
//	assets   -> normal balance Debit  (Dr increases, Cr decreases)
//	expense  -> normal balance Debit
//	liabilities -> normal balance Credit (Cr increases, Dr decreases)
//	income   -> normal balance Credit
//
// This is the standard double-entry convention and matches the accounting
// equation Assets − Liabilities = Income − Expenses (SPEC §4.1). The ledger's
// post() layers signed amounts on top of this; the playbook only declares the
// structure (which side each line posts to) so a template is balanced by
// construction: every entry type's ΣDr expressions must equal its ΣCr
// expressions symbolically, which the ledger checks numerically after binding.
//
// # Template arithmetic
//
// Line amount expressions use +/- arithmetic over declared param names ONLY
// (e.g. "net+gst"). Any derived amount that needs multiplication or division —
// such as splitting GST out of a gross via a rate — is computed by the caller at
// BIND time and passed in as a param; it never appears in a template (SPEC §4.2).
//
// # Load-time validation
//
// Load/Parse fully validate the playbook before returning it: roots are known,
// account paths are unique and well-formed, every line references an account
// that exists in the chart, every side is "Dr" or "Cr", and every param
// referenced in an expression is declared in that entry type's params. Decoding
// is strict (DisallowUnknownFields) so schema drift fails loudly.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// RootType is one of the four chart-of-accounts roots (SPEC §4.1). It fixes an
// account's normal balance and partitions the chart for the balance sheet /
// income statement.
type RootType string

const (
	// RootAssets and RootExpense are normal-Debit roots.
	RootAssets  RootType = "assets"
	RootExpense RootType = "expense"
	// RootLiabilities and RootIncome are normal-Credit roots.
	RootLiabilities RootType = "liabilities"
	RootIncome      RootType = "income"
)

// rootTypes lists the four valid roots in chart order. Membership in this list
// is the only thing that makes a path's first segment a legal root.
var rootTypes = []RootType{RootAssets, RootLiabilities, RootIncome, RootExpense}

// Side is the debit/credit side a template line posts to.
type Side string

const (
	Debit  Side = "Dr"
	Credit Side = "Cr"
)

// NormalBalance reports the side on which a root's balance is "positive" in the
// usual double-entry sense: Debit for assets/expense, Credit for
// liabilities/income. The ledger uses this to render report balances and to
// enforce its sign convention in post().
func (r RootType) NormalBalance() Side {
	switch r {
	case RootAssets, RootExpense:
		return Debit
	case RootLiabilities, RootIncome:
		return Credit
	default:
		return ""
	}
}

// Valid reports whether r is one of the four known roots.
func (r RootType) Valid() bool { return r.NormalBalance() != "" }

// Playbook is the decoded, validated chart of accounts plus entry-type
// templates. The JSON shape is locked to {"accounts": [...], "entry_types":
// [...]} and decoded strictly.
//
// Built indexes (accountIndex / entryTypeIndex) are populated by Parse so the
// rule engine and ledger get O(1) lookups without re-scanning slices. They are
// unexported and not serialized.
type Playbook struct {
	Accounts   []Account   `json:"accounts"`
	EntryTypes []EntryType `json:"entry_types"`

	accountIndex   map[string]*Account
	entryTypeIndex map[string]*EntryType
}

// Account is a node in the chart of accounts. Path is a Fragment-style
// segmentable path (e.g. "income/product-sales"); keeping it a path means a
// per-channel split like "income/product-sales/<channel>" is additive later
// without expanding the node count (SPEC §4.1, §13). Note is human documentation
// only and never affects behavior.
type Account struct {
	Path string `json:"path"`
	Note string `json:"note,omitempty"`

	// root is the cached first path segment as a RootType, set during Parse.
	root RootType
}

// Root returns the account's root type (the first path segment). It is valid
// only after the Account has been through Parse, which validates the root.
func (a Account) Root() RootType { return a.root }

// NormalBalance returns the side on which this account's balance is positive,
// derived from its root type (assets/expense -> Dr, liabilities/income -> Cr).
func (a Account) NormalBalance() Side { return a.root.NormalBalance() }

// EntryType is a declarative, balanced-by-construction posting template
// (SPEC §4.2). Params are the names a caller must bind; Lines reference those
// params symbolically via +/- expressions. TxParam, if set, names the param that
// carries the external transaction id stamped onto the entry's tx line (e.g.
// payment_id / bank_tx_id) — it must be one of Params.
type EntryType struct {
	Name    string   `json:"name"`
	Doc     string   `json:"doc,omitempty"`
	Params  []string `json:"params"`
	TxParam string   `json:"tx_param,omitempty"`
	Lines   []Line   `json:"lines"`
}

// Line is a single template posting: which Side, which account Path, and an
// Amount expression over the entry type's params.
type Line struct {
	Side    Side   `json:"side"`
	Account string `json:"account"`
	Amount  string `json:"amount"`

	// terms is the parsed Amount expression, set during Parse: an ordered list
	// of (param, sign) pairs whose signed sum is the line's bound amount.
	terms []term
}

// term is one signed param reference within a parsed amount expression. plus is
// true for a "+param" term, false for a "-param" term.
type term struct {
	param string
	plus  bool
}

// Terms returns the line's parsed amount expression as ordered (param, +/-)
// pairs. It is populated by Parse; the ledger uses it to bind concrete paise.
func (l Line) Terms() []struct {
	Param string
	Plus  bool
} {
	out := make([]struct {
		Param string
		Plus  bool
	}, len(l.terms))
	for i, t := range l.terms {
		out[i].Param = t.param
		out[i].Plus = t.plus
	}
	return out
}

// Account looks up a chart account by exact path. ok is false if no such account
// exists. The lookup is O(1) via the index built in Parse.
func (p *Playbook) Account(path string) (*Account, bool) {
	a, ok := p.accountIndex[path]
	return a, ok
}

// EntryType looks up an entry-type template by name. ok is false if no such
// entry type exists.
func (p *Playbook) EntryType(name string) (*EntryType, bool) {
	e, ok := p.entryTypeIndex[name]
	return e, ok
}

// Load reads the playbook JSON at path and validates it.
func Load(path string) (*Playbook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read playbook %q: %w", path, err)
	}
	pb, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("config: playbook %q: %w", path, err)
	}
	return pb, nil
}

// Parse decodes playbook JSON from raw bytes and runs full load-time validation.
// It is separated from Load so it is table-testable without the filesystem.
//
// Decoding is strict: unknown fields are rejected so the playbook stays a tight
// contract. On any validation failure Parse returns a descriptive error and a
// nil Playbook — a partially-built chart is never returned.
func Parse(data []byte) (*Playbook, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var pb Playbook
	if err := dec.Decode(&pb); err != nil {
		return nil, fmt.Errorf("config: decode playbook: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("config: decode playbook: trailing data after JSON object")
	}

	if err := pb.validateAccounts(); err != nil {
		return nil, err
	}
	if err := pb.validateEntryTypes(); err != nil {
		return nil, err
	}
	return &pb, nil
}

// validateAccounts checks every account path is non-empty, rooted at a known
// root type, free of empty segments, and unique; it then builds accountIndex and
// caches each account's root.
func (p *Playbook) validateAccounts() error {
	p.accountIndex = make(map[string]*Account, len(p.Accounts))
	for i := range p.Accounts {
		a := &p.Accounts[i]
		if a.Path == "" {
			return fmt.Errorf("config: account %d has an empty path", i)
		}
		segs := strings.Split(a.Path, "/")
		root := RootType(segs[0])
		if !root.Valid() {
			return fmt.Errorf("config: account %q has unknown root %q (want one of %s)",
				a.Path, segs[0], rootList())
		}
		if len(segs) < 2 {
			return fmt.Errorf("config: account %q must be a child path under its root (e.g. %s/<name>)", a.Path, root)
		}
		for _, s := range segs {
			if s == "" {
				return fmt.Errorf("config: account %q has an empty path segment", a.Path)
			}
		}
		if _, dup := p.accountIndex[a.Path]; dup {
			return fmt.Errorf("config: duplicate account path %q", a.Path)
		}
		a.root = root
		p.accountIndex[a.Path] = a
	}
	return nil
}

// validateEntryTypes checks each entry type: unique non-empty name, declared
// params (unique, non-empty), an optional tx_param that is a declared param,
// non-empty lines, each line a valid side referencing an existing account, and
// each amount expression parseable into terms over declared params only. It then
// builds entryTypeIndex.
func (p *Playbook) validateEntryTypes() error {
	p.entryTypeIndex = make(map[string]*EntryType, len(p.EntryTypes))
	for i := range p.EntryTypes {
		e := &p.EntryTypes[i]
		if e.Name == "" {
			return fmt.Errorf("config: entry type %d has an empty name", i)
		}
		if _, dup := p.entryTypeIndex[e.Name]; dup {
			return fmt.Errorf("config: duplicate entry type name %q", e.Name)
		}

		params := make(map[string]bool, len(e.Params))
		for _, name := range e.Params {
			if name == "" {
				return fmt.Errorf("config: entry type %q has an empty param name", e.Name)
			}
			if params[name] {
				return fmt.Errorf("config: entry type %q declares param %q twice", e.Name, name)
			}
			params[name] = true
		}

		if e.TxParam != "" && !params[e.TxParam] {
			return fmt.Errorf("config: entry type %q tx_param %q is not a declared param", e.Name, e.TxParam)
		}

		if len(e.Lines) == 0 {
			return fmt.Errorf("config: entry type %q has no lines", e.Name)
		}
		for j := range e.Lines {
			l := &e.Lines[j]
			if l.Side != Debit && l.Side != Credit {
				return fmt.Errorf("config: entry type %q line %d has invalid side %q (want %q or %q)",
					e.Name, j, l.Side, Debit, Credit)
			}
			if _, ok := p.accountIndex[l.Account]; !ok {
				return fmt.Errorf("config: entry type %q line %d references unknown account %q",
					e.Name, j, l.Account)
			}
			terms, err := parseAmountExpr(l.Amount, params)
			if err != nil {
				return fmt.Errorf("config: entry type %q line %d amount %q: %w", e.Name, j, l.Amount, err)
			}
			l.terms = terms
		}

		p.entryTypeIndex[e.Name] = e
	}
	return nil
}

// parseAmountExpr parses a +/- expression over declared param names into ordered
// signed terms. The grammar is intentionally tiny (SPEC §4.2 — templates use +/-
// only; anything needing × or ÷ is computed at bind time and passed as a param):
//
//	expr  := [ '+' | '-' ] param ( ( '+' | '-' ) param )*
//	param := one of the entry type's declared params
//
// It rejects empty expressions, unknown params, empty terms (e.g. "a++b",
// trailing operator), and any character outside the param/operator grammar
// (notably '*' and '/').
func parseAmountExpr(expr string, declared map[string]bool) ([]term, error) {
	if expr == "" {
		return nil, fmt.Errorf("empty amount expression")
	}
	// Reject multiplication/division explicitly with a pointed message, since
	// that's the most likely template authoring mistake.
	if strings.ContainsAny(expr, "*/") {
		return nil, fmt.Errorf("multiplication/division is not allowed in templates; compute derived amounts at bind time")
	}

	var terms []term
	plus := true // sign of the term currently being accumulated
	cur := strings.Builder{}

	flush := func() error {
		name := cur.String()
		if name == "" {
			return fmt.Errorf("empty term in expression")
		}
		if !declared[name] {
			return fmt.Errorf("undeclared param %q", name)
		}
		terms = append(terms, term{param: name, plus: plus})
		cur.Reset()
		return nil
	}

	for i := 0; i < len(expr); i++ {
		c := expr[i]
		switch c {
		case '+', '-':
			// An operator either sets the sign of the first term (only valid at
			// position 0) or separates two terms (flush the previous one).
			if i == 0 {
				plus = c == '+'
				continue
			}
			if cur.Len() == 0 {
				return nil, fmt.Errorf("operator %q with no preceding term", string(c))
			}
			if err := flush(); err != nil {
				return nil, err
			}
			plus = c == '+'
		case ' ', '\t':
			return nil, fmt.Errorf("whitespace is not allowed in amount expressions")
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() == 0 {
		return nil, fmt.Errorf("expression ends with a dangling operator")
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return terms, nil
}

// rootList renders the known roots for error messages, in chart order.
func rootList() string {
	ss := make([]string, len(rootTypes))
	for i, r := range rootTypes {
		ss[i] = string(r)
	}
	return strings.Join(ss, ", ")
}

// AccountsByRoot returns the chart accounts grouped under each root, with roots
// in chart order and accounts within a root sorted by path. It is a pure view
// over the loaded chart, convenient for rendering reports and `show playbook`.
func (p *Playbook) AccountsByRoot() []struct {
	Root     RootType
	Accounts []Account
} {
	groups := make(map[RootType][]Account, len(rootTypes))
	for _, a := range p.Accounts {
		groups[a.root] = append(groups[a.root], a)
	}
	out := make([]struct {
		Root     RootType
		Accounts []Account
	}, 0, len(rootTypes))
	for _, r := range rootTypes {
		accts := groups[r]
		sort.Slice(accts, func(i, j int) bool { return accts[i].Path < accts[j].Path })
		out = append(out, struct {
			Root     RootType
			Accounts []Account
		}{Root: r, Accounts: accts})
	}
	return out
}
