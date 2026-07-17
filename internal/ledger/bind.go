package ledger

import (
	"errors"
	"fmt"
	"sort"

	"github.com/razorpay/ledger-flow/internal/money"
)

// Template is the shape the binder needs from one entry-type template: its name,
// the param that carries the external tx id (may be empty), and its lines as
// (side, account, signed-terms-over-params). It deliberately mirrors a subset of
// config.EntryType so the ledger does not import the schema-loading package; the
// PlaybookTemplates adapter projects config.Playbook onto this interface.
type Template interface {
	// Name is the entry-type name (e.g. "dtc_sale").
	Name() string
	// TxParam names the param holding the external tx id, or "" if none.
	TxParam() string
	// BindLines returns the template's lines as (side, account, terms).
	BindLines() []TemplateLine
}

// TemplateLine is one template line for binding: a Side, an account path, and an
// amount expressed as ordered signed terms over param names (e.g. net+gst).
type TemplateLine struct {
	Side    Side
	Account string
	Terms   []TemplateTerm
}

// TemplateTerm is one signed reference to a param within a line's amount
// expression. Plus is true for "+param", false for "-param".
type TemplateTerm struct {
	Param string
	Plus  bool
}

// Templates is the lookup the binder needs: resolve an entry-type name to its
// Template. config.Playbook satisfies this via PlaybookTemplates.
type Templates interface {
	// Template returns the template named name; ok is false if unknown.
	Template(name string) (Template, bool)
}

// Bind errors. Distinct sentinels so callers (and tests) can branch precisely.
var (
	// ErrUnknownEntryType is returned when no template matches the requested
	// entry-type name.
	ErrUnknownEntryType = errors.New("ledger: unknown entry type")
	// ErrMissingParam is returned when a template line references a param that
	// the caller did not supply.
	ErrMissingParam = errors.New("ledger: missing param")
	// ErrNegativeBoundAmount is returned when an evaluated line amount is
	// negative. A template line's bound magnitude must be >= 0 (direction is the
	// side); a negative result means the params are inconsistent with the
	// template's intent and would be rejected by Post anyway.
	ErrNegativeBoundAmount = errors.New("ledger: negative bound amount")
)

// Bind expands an entry-type template against caller-supplied params into a
// concrete, ready-to-post Entry. params maps each declared param name to its
// value in PAISE (int64) — the binder never sees floats, upholding the money
// invariant; any derived amount that needed × or ÷ (e.g. a GST split) was
// computed by the caller in integer space before being passed here (SPEC §4.2).
//
// For each template line it evaluates the +/- term expression over params and
// produces a Line with that magnitude on the template's side. The tx id, if the
// template declares a tx_param, is taken from params as an integer and recorded
// on the Entry (it is an opaque identifier to the ledger).
//
// Bind is balance-preserving but not balance-checking: because the templates are
// balanced by construction (ΣDr terms == ΣCr terms symbolically) and the same
// param value is substituted everywhere it appears, consistent params yield a
// balanced set — but it is Post(), not Bind(), that enforces ΣDr == ΣCr. Bind
// only rejects structural problems (unknown type, missing param, negative
// magnitude).
//
// ik is the idempotency key to stamp on the resulting Entry; it is passed
// through unchanged for Post to enforce idempotency.
func Bind(templates Templates, entryType, ik string, params map[string]money.Money) (Entry, error) {
	tmpl, ok := templates.Template(entryType)
	if !ok {
		return Entry{}, fmt.Errorf("%w: %q", ErrUnknownEntryType, entryType)
	}

	lines := make([]Line, 0, len(tmpl.BindLines()))
	for i, tl := range tmpl.BindLines() {
		amt, err := evalTerms(tl.Terms, params)
		if err != nil {
			return Entry{}, fmt.Errorf("entry type %q line %d (%s %s): %w", entryType, i, tl.Side, tl.Account, err)
		}
		if amt.Sign() < 0 {
			return Entry{}, fmt.Errorf("%w: entry type %q line %d (%s %s) evaluated to %s",
				ErrNegativeBoundAmount, entryType, i, tl.Side, tl.Account, amt)
		}
		lines = append(lines, Line{Side: tl.Side, Account: tl.Account, Amount: amt})
	}

	entry := Entry{Type: entryType, IK: ik, Lines: lines}
	if tp := tmpl.TxParam(); tp != "" {
		v, ok := params[tp]
		if !ok {
			return Entry{}, fmt.Errorf("%w: entry type %q tx_param %q not supplied", ErrMissingParam, entryType, tp)
		}
		entry.TxID = formatTxID(v)
	}
	return entry, nil
}

// evalTerms computes the signed sum of an expression's terms in paise. Every
// referenced param must be present in params; a missing param is an error
// (rather than defaulting to zero), so a caller who forgets a param fails loudly
// instead of silently posting a smaller amount.
func evalTerms(terms []TemplateTerm, params map[string]money.Money) (money.Money, error) {
	var sum money.Money
	for _, t := range terms {
		v, ok := params[t.Param]
		if !ok {
			return 0, fmt.Errorf("%w: param %q", ErrMissingParam, t.Param)
		}
		if t.Plus {
			sum = sum.Add(v)
		} else {
			sum = sum.Sub(v)
		}
	}
	return sum, nil
}

// formatTxID renders a tx-id param value. The tx id is carried through the
// params map as a money.Money (paise int64) like every other param so the seam
// stays single-typed, but it is an opaque external identifier — here we render
// it as a plain integer string (not a rupee amount).
func formatTxID(v money.Money) string {
	return fmt.Sprintf("%d", v.Paise())
}

// MissingParams returns the param names a template references but that are not
// present in params, sorted. It is a convenience for callers that want to
// validate a bind up front; Bind itself fails on the first missing param.
func MissingParams(tmpl Template, params map[string]money.Money) []string {
	missing := map[string]bool{}
	for _, tl := range tmpl.BindLines() {
		for _, t := range tl.Terms {
			if _, ok := params[t.Param]; !ok {
				missing[t.Param] = true
			}
		}
	}
	if tp := tmpl.TxParam(); tp != "" {
		if _, ok := params[tp]; !ok {
			missing[tp] = true
		}
	}
	out := make([]string, 0, len(missing))
	for k := range missing {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ParamsFromPaise is a small helper to build a params map from raw int64 paise
// counts, keeping the "money is paise" conversion explicit at the call seam.
func ParamsFromPaise(kv map[string]int64) map[string]money.Money {
	out := make(map[string]money.Money, len(kv))
	for k, v := range kv {
		out[k] = money.FromPaise(v)
	}
	return out
}
