package ledger

import "github.com/razorpay/ledger-flow/internal/config"

// PlaybookChart adapts a *config.Playbook to the ledger's accountSet interface,
// so a Ledger can validate posted accounts against the loaded chart and resolve
// each account's normal side for reports. Keeping the adapter here (rather than
// having the ledger core import config directly) preserves the boundary: the
// posting engine depends only on the small interfaces above, and this one file
// is the single seam where the playbook is projected onto them.
type PlaybookChart struct{ pb *config.Playbook }

// NewPlaybookChart wraps a loaded playbook as a ledger chart.
func NewPlaybookChart(pb *config.Playbook) PlaybookChart { return PlaybookChart{pb: pb} }

// HasAccount reports whether the playbook chart contains the given path.
func (c PlaybookChart) HasAccount(path string) bool {
	_, ok := c.pb.Account(path)
	return ok
}

// NormalSide returns the account's normal side, translating config.Side to the
// ledger's Side. It assumes the path exists (HasAccount returned true); an
// unknown path yields an empty side.
func (c PlaybookChart) NormalSide(path string) Side {
	a, ok := c.pb.Account(path)
	if !ok {
		return ""
	}
	return Side(a.NormalBalance())
}

// PlaybookTemplates adapts a *config.Playbook to the ledger's Templates
// interface for binding.
type PlaybookTemplates struct{ pb *config.Playbook }

// NewPlaybookTemplates wraps a loaded playbook as a ledger template source.
func NewPlaybookTemplates(pb *config.Playbook) PlaybookTemplates {
	return PlaybookTemplates{pb: pb}
}

// Template resolves an entry-type name to a Template view over the playbook's
// EntryType. ok is false if the entry type is unknown.
func (t PlaybookTemplates) Template(name string) (Template, bool) {
	e, ok := t.pb.EntryType(name)
	if !ok {
		return nil, false
	}
	return playbookTemplate{e: e}, true
}

// playbookTemplate projects a *config.EntryType onto the ledger's Template
// interface, translating sides and parsed amount terms.
type playbookTemplate struct{ e *config.EntryType }

func (p playbookTemplate) Name() string    { return p.e.Name }
func (p playbookTemplate) TxParam() string { return p.e.TxParam }

func (p playbookTemplate) BindLines() []TemplateLine {
	out := make([]TemplateLine, 0, len(p.e.Lines))
	for _, l := range p.e.Lines {
		terms := l.Terms() // []struct{ Param string; Plus bool }
		tt := make([]TemplateTerm, len(terms))
		for i, t := range terms {
			tt[i] = TemplateTerm{Param: t.Param, Plus: t.Plus}
		}
		out = append(out, TemplateLine{
			Side:    Side(l.Side),
			Account: l.Account,
			Terms:   tt,
		})
	}
	return out
}
