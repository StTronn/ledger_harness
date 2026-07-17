// Package recovery owns the deterministic, read-only recovery surface used by
// humans and judgment agents. It prepares event and reconciliation context from
// snapshotted inputs, ledger state, and policy checks; it never posts entries and
// never reads the reference ledger.
package recovery

import (
	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledger"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/context"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/posting"
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/reconcile"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// DecisionKind is the deterministic routing decision for a rule-missed event.
// SafeToPost returns a candidate to the posting engine; the other outcomes are
// routed to the judgment agent when one is enabled.
type DecisionKind string

const (
	SafeToPost     DecisionKind = "safe_to_post"
	ReviewRequired DecisionKind = "review_required"
	Unresolved     DecisionKind = "unresolved"
)

// Candidate is a journal-entry candidate recovered from validated source facts.
// Recovery produces it but never applies it; the posting engine binds and posts
// it through the ledger.
type Candidate struct {
	EntryType string
	Params    map[string]money.Money
	IK        string
	TxID      string
	Ts        int64
	Reason    string
}

// Decision is the recovery engine's answer for one rule-missed event.
type Decision struct {
	Kind      DecisionKind
	Candidate *Candidate
	Reason    string
}

// Engine is the read-only recovery and exploration boundary for one period.
// The context graph is an implementation detail; callers use this type so the
// recovery and context implementation can evolve independently from the CLI and
// agent transports.
type Engine struct {
	graph *flowcontext.Graph
}

// New builds a recovery engine over an already-produced ledger snapshot and the
// period's agent-input fixtures. All inputs are read-only from this package's
// perspective.
func New(lg *ledger.Ledger, events []ingest.NormalizedEvent, raw ingest.Raw, orders map[string]feeds.OrderInfo, breaks []reconcile.Break, ratecard *feeds.RateCardFile) *Engine {
	g := flowcontext.New(lg, events, raw, orders, breaks)
	if ratecard != nil {
		g.WithRateCard(*ratecard)
	}
	return &Engine{graph: g}
}

// EventContext returns the prepared context for classification and human
// investigation of one event.
func (e *Engine) EventContext(eventID string) (flowcontext.EventContextBundle, bool) {
	if e == nil || e.graph == nil {
		return flowcontext.EventContextBundle{}, false
	}
	return e.graph.EventContext(eventID)
}

// EventDecision determines whether a rule-missed event has enough deterministic
// evidence to return to the posting engine. It currently supports the safe GST
// recovery path. Partial refunds are deliberately review-required because a
// line-item match does not prove the business intent.
func (e *Engine) EventDecision(eventID string) Decision {
	if e == nil || e.graph == nil {
		return Decision{Kind: Unresolved, Reason: "recovery engine is unavailable"}
	}
	ev, ok := e.graph.Event(eventID)
	if !ok {
		return Decision{Kind: Unresolved, Reason: "event is not in recovery context"}
	}
	bundle, ok := e.graph.EventContext(eventID)
	if !ok {
		return Decision{Kind: Unresolved, Reason: "event context is unavailable"}
	}
	if ev.ParentAmount != nil {
		return Decision{
			Kind:   ReviewRequired,
			Reason: "partial refund intent requires judgment",
		}
	}
	if bundle.Recovered == nil || bundle.Recovered.GSTRate == "" {
		return Decision{Kind: Unresolved, Reason: "recovery found no usable GST rate"}
	}

	// Re-run the shared posting rules with the validated recovered fact. This
	// computes the exact same template parameters as an ordinary event; recovery
	// only supplies the missing input and decides whether it is safe to use.
	recovered := ev
	recovered.Notes = &ingest.Notes{GSTRate: bundle.Recovered.GSTRate}
	classification, matched, reason := posting.Classify(recovered)
	if !matched {
		return Decision{Kind: Unresolved, Reason: "recovered GST rate did not produce a valid posting candidate: " + reason}
	}
	return Decision{
		Kind: SafeToPost,
		Candidate: &Candidate{
			EntryType: classification.EntryType,
			Params:    classification.Params,
			IK:        classification.IK,
			TxID:      classification.TxID,
			Ts:        classification.Ts,
			Reason:    "recovered: " + bundle.Recovered.Policy,
		},
		Reason: "GST rate recovered and validated from the order",
	}
}

// BreakContext returns the prepared context for investigating one reconciliation
// break.
func (e *Engine) BreakContext(key string) (flowcontext.ReconContextBundle, bool) {
	if e == nil || e.graph == nil {
		return flowcontext.ReconContextBundle{}, false
	}
	return e.graph.ReconciliationContext(key)
}

// BreakKeys returns the stable break keys available to explore.
func (e *Engine) BreakKeys() []string {
	if e == nil || e.graph == nil {
		return nil
	}
	return e.graph.BreakKeys()
}

// Entity performs a bounded, read-only lookup for agent exploration. It is the
// tier-2 path for novel cases that the prepared bundles do not fully explain.
func (e *Engine) Entity(id string) (flowcontext.EntityView, bool) {
	if e == nil || e.graph == nil {
		return flowcontext.EntityView{}, false
	}
	return e.graph.Entity(id)
}

// EntityKinds documents the IDs the exploration surface accepts.
var EntityKinds = flowcontext.EntityKinds
