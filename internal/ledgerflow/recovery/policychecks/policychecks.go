// Package policychecks is the POLICY layer of the agent harness
// (internal/harness): the registry of recovery/verification rules that turn a
// raw event into the evidence the §8 agent judges. Where the ledger graph
// (internal/harness/ledgergraph) is the MECHANISM — indexes, edges, booked
// flags — a Policy here is the KNOWLEDGE: "for THIS kind of gap, THE authority
// lives THERE, and a sane value looks like THIS."
//
// Each policy is self-selecting (AppliesTo), walks the graph through the
// read-only Graph lens, and contributes a Finding: recovered FACTS (each with a
// machine-checkable citation and a validation verdict) and/or evidence
// CANDIDATES (e.g. line-item matches for a partial refund). Policies NEVER
// guess: an empty Finding, or a fact marked Valid=false, is a first-class
// honest answer.
//
// The registry is the seam the system grows through (the "recovery registry"
// of the roadmap): a new problem class is a new Policy in Default() — data-
// shaped code, one table — not new bundler logic. The v3 learning layer
// promotes repeated agent explorations into entries here.
package policychecks

import (
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// Graph is the read-only lens a policy looks through, implemented by the ledger
// graph. Policies never read fixtures or the ledger directly — everything goes
// through this interface, which keeps them pure and testable.
type Graph interface {
	OrderIDForPayment(paymentID string) (string, bool)
	PaymentIDForRefund(refundID string) (string, bool)
	OrderInfo(orderID string) (feeds.OrderInfo, bool)
	RateCard() (feeds.RateCardFile, bool)
	// SettlementMembers returns a settlement's batch member ids.
	SettlementMembers(settlementID string) (paymentIDs, refundIDs []string, ok bool)
	// PaymentAmount returns a payment's gross (paise).
	PaymentAmount(paymentID string) (money.Money, bool)
	// RemittanceDeductions returns a COD remittance's per-shipment deduction lines
	// (each with the shipment's lifecycle status), for the rto-fee policy.
	RemittanceDeductions(remittanceID string) ([]Deduction, bool)
	// CourierChannel returns the COD courier channel name (for the rate-card lookup).
	CourierChannel() (string, bool)
}

// Event is the policy-input projection of one normalized event: just the fields
// policies select and walk on. Money is integer paise. ParentAmount is set only
// on a PARTIAL refund; Fee/Tax only where the event bears them (settlements,
// payments).
type Event struct {
	EventID      string
	Type         string // payment | refund | settlement | dispute
	Amount       money.Money
	GSTRate      string // the event's OWN rate ("" = absent)
	Reason       string // ops annotation (e.g. "goodwill")
	ParentAmount *money.Money
	Fee          *money.Money
	Tax          *money.Money
}

// Citation is the provenance of a recovered fact: the snapshot object and field
// path it was read from, so a validator (or a human) can re-read that exact
// field and confirm the value was found, not invented.
type Citation struct {
	Object string `json:"object"`
	Path   string `json:"path"`
}

// Fact is one recovered, validated fact. Valid reports whether the value passed
// the policy's own validation (a closed slab set, a rate-card match, …); an
// invalid fact is still surfaced — visibly wrong beats silently dropped.
type Fact struct {
	Field  string   `json:"field"`
	Value  string   `json:"value"`
	Source Citation `json:"_source"`
	Valid  bool     `json:"valid"`
	Note   string   `json:"note,omitempty"`
	Policy string   `json:"policy"`
}

// CandidateKind names how an evidence candidate was derived.
type CandidateKind string

const (
	// CandidateItemMatch: the refund amount equals exactly ONE line item.
	CandidateItemMatch CandidateKind = "item-match"
	// CandidatePairMatch: the refund equals a PAIR of line items (matching is
	// capped at pairs by design).
	CandidatePairMatch CandidateKind = "pair-match"
	// CandidateNoMatch: matching was tried and failed — stated explicitly so the
	// agent knows the evidence is absent, not unexamined.
	CandidateNoMatch CandidateKind = "no-match"
)

// Candidate is one evidence-style finding: a possible explanation with the
// entry type it implies, the rate it carries, and the citation to re-verify.
type Candidate struct {
	Kind      CandidateKind `json:"kind"`
	EntryType string        `json:"entry_type,omitempty"`
	GSTRate   string        `json:"gst_rate,omitempty"`
	Items     []int         `json:"items,omitempty"`
	Source    Citation      `json:"_source,omitempty"`
	Note      string        `json:"note,omitempty"`
	Policy    string        `json:"policy,omitempty"`
}

// Finding is what one policy contributes for one event.
type Finding struct {
	Facts      []Fact
	Candidates []Candidate
}

// merge folds another finding in (registry use).
func (f *Finding) merge(o Finding) {
	f.Facts = append(f.Facts, o.Facts...)
	f.Candidates = append(f.Candidates, o.Candidates...)
}

// Policy is ONE rule: self-selecting, walking, validating, citing.
type Policy interface {
	// Name is the stable identifier stamped on every fact/candidate the policy
	// produces (the audit tag the bundle, traces, and UI surface).
	Name() string
	// AppliesTo reports whether this policy has anything to say about ev.
	AppliesTo(ev Event) bool
	// Recover walks the graph and returns the policy's findings. It never
	// guesses; finding nothing is a normal result.
	Recover(ev Event, g Graph) Finding
}

// Registry is an ordered set of policies.
type Registry []Policy

// Default returns the registered policy table — THE one place the harness's
// recovery knowledge is enumerated. Order is contribution order in bundles.
func Default() Registry {
	return Registry{
		gstRateFromOrder{},
		refundLineItemMatch{},
		feeTierFromRateCard{},
		rtoFeeFromRateCard{},
	}
}

// Run applies every applicable policy to ev and merges their findings.
func (r Registry) Run(ev Event, g Graph) Finding {
	var out Finding
	for _, p := range r {
		if p.AppliesTo(ev) {
			out.merge(p.Recover(ev, g))
		}
	}
	return out
}
