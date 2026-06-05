package agentclient

import "github.com/razorpay/close-agent/internal/money"

// EventSummary is the source-agnostic snapshot of one normalized event the agent
// classifies (SPEC §8 `in: { event: NormalizedEvent }`). It is the SUBSET of
// ingest.NormalizedEvent the §8 classify surface needs — id, type, gross amount,
// the order/payment links, and the (possibly-incomplete) notes — kept here so
// agentclient does not depend on the ingest package's full event type and so the
// recorded fixture and the trace carry a stable, reviewable event description.
//
// Money is integer paise (money.Money). Notes mirrors the event's free-form
// metadata; on the hard period the stripped payments arrive here with an EMPTY
// GSTRate — that absence is exactly why the rule engine missed and the agent is
// asked to recover it from the order (SPEC §1, §2).
type EventSummary struct {
	EventID string      `json:"event_id"`
	Type    string      `json:"type"`   // payment | refund | settlement | dispute
	Amount  money.Money `json:"amount"` // event gross in paise
	OrderID string      `json:"order_id,omitempty"`
	GSTRate string      `json:"gst_rate,omitempty"` // present only when the event still carries it
	SKU     string      `json:"sku,omitempty"`
}

// ClassifyResult is the §8 `POST /agents/classify` response (SPEC §8):
//
//	{ entry_type, params, rationale }            // a classification
//	| { unclassifiable: true, reason }           // -> escalate
//
// EXACTLY ONE of the two shapes is meaningful: when Unclassifiable is false the
// agent picked an entry type and bound its params; when true the agent declined
// and Reason explains why (the orchestrator escalates — it never guesses). The
// agent returns ONLY {entry_type, params}; it never emits raw debits/credits
// (SPEC §3, §8) — the Go ledger expands the params into a balanced entry and
// validates it (balance-or-reject).
//
// Params maps each of the chosen entry type's declared params to its paise value
// (money.Money), keyed to the playbook (e.g. {gross, net, gst, payment_id} for
// dtc_sale), the same shape the rule engine's classify.Classification carries.
type ClassifyResult struct {
	EntryType      string                 `json:"entry_type,omitempty"`
	Params         map[string]money.Money `json:"params,omitempty"`
	Rationale      string                 `json:"rationale,omitempty"`
	Unclassifiable bool                   `json:"unclassifiable,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
}

// Classifiable reports whether the result is a usable classification (an entry
// type the orchestrator can bind+post) rather than an escalation. It is the
// inverse of Unclassifiable plus a guard that an entry type is actually present.
func (r ClassifyResult) Classifiable() bool {
	return !r.Unclassifiable && r.EntryType != ""
}

// Unclassified builds the {unclassifiable, reason} escalation result. It is the
// single constructor for the escalate path so the reason is always recorded and
// the classification fields stay zero.
func Unclassified(reason string) ClassifyResult {
	return ClassifyResult{Unclassifiable: true, Reason: reason}
}

// Mode is the agent-client transport mode (SPEC §11 Phase 7, §12). It is a
// string for stable, human-readable JSON in the trace and recorded fixture.
type Mode string

const (
	// ModeReplay reads the committed recorded-response fixture — the DEFAULT for
	// CI: pure, deterministic, no network/LLM.
	ModeReplay Mode = "replay"
	// ModeLive posts to the configurable Flue endpoint and records the response.
	// Built but NOT exercised in CI (SPEC §12 "live LLM only in a separate eval").
	ModeLive Mode = "live"
	// ModeOff is the agent-off baseline: no agent client is consulted at all; a
	// rule miss is flagged and skipped (the documented Phase-4 baseline). It is
	// carried here so the orchestrator and CLI name the mode consistently.
	ModeOff Mode = "off"
)

// Client is the §8 classify-agent seam (SPEC §8): one single-shot, stateless
// call mapping one event to a classification or an escalation, plus the FROZEN
// trace of that decision (SPEC §9, §13). Both the replay and live implementations
// satisfy it, so the orchestrator depends only on this interface and never on a
// concrete transport.
//
// Classify returns an error ONLY for an infrastructure failure (a malformed
// recorded fixture, a network/transport error in live mode) — NOT for an event
// the agent declines, which is a normal {unclassifiable, reason} ClassifyResult.
// The Trace is always returned (even alongside an error or an unclassifiable
// result) so every agent consultation is recorded.
type Client interface {
	Classify(ev EventSummary) (ClassifyResult, Trace, error)
	// Mode reports which transport this client is (replay/live), for the trace
	// and for operator-facing reporting.
	Mode() Mode
}
