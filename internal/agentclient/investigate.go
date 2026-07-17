package agentclient

import (
	"encoding/json"

	"github.com/razorpay/ledger-flow/internal/money"
)

// investigate.go is the Go side of the §8 INVESTIGATE-agent interface — the
// second judgment-agent seam (SPEC §5, §7, §8, §11 Phase 8), parallel to the
// classify seam in this package. Where classify recovers a single rule-missed
// EVENT, investigate reviews a single reconciliation BREAK: it inspects the
// break plus the candidate events and returns a recommendation for review — or
// escalates when the evidence is insufficient.
//
//	Investigate(BreakSummary, []EventSummary)
//	  -> { resolution: {entry_type, params}[], rationale }   // recommendation
//	   | { escalate: true, reason }                          // -> list unresolved
//
// exactly the §8 `POST /agents/investigate` contract. The response is review-only:
// the agent never writes to the ledger or emits raw debits/credits, and it never
// reads internal/truth — the recovery context is derived from snapshotted
// agent-input fixtures (refunds.json / orders.json).

// BreakSummary is the source-agnostic snapshot of one reconcile break the §8
// investigate agent sees (SPEC §8 in: { break: ReconBreak }). It mirrors the
// subset of reconcile.Break the investigate surface needs, kept here so
// agentclient does not depend on internal/reconcile and so the recorded fixture
// and the trace carry a stable, reviewable break description. Money is integer
// paise (money.Money).
//
// Key is the STABLE identifier the recorded fixture is keyed by (BreakKey below):
// it encodes the check number, the kind, and the settlement id, so the same break
// replays the same recorded resolution byte-for-byte.
type BreakSummary struct {
	Key          string      `json:"key"`
	Check        int         `json:"check"` // 1|2|3
	Kind         string      `json:"kind"`  // settlement-bank-mismatch | batch-sum-mismatch | receivable-residual
	SettlementID string      `json:"settlement_id,omitempty"`
	Expected     money.Money `json:"expected"`   // the value the check expected (paise)
	Actual       money.Money `json:"actual"`     // the value it found (paise)
	Candidates   []string    `json:"candidates"` // candidate event ids carried by the break
	Detail       string      `json:"detail"`
}

// Posting is the existing wire shape for one journal-entry recommendation. EventID
// names the source event under review; the recommendation is logged but not applied
// automatically. Params maps the entry type's declared params to paise values.
type Posting struct {
	EventID   string                 `json:"event_id"`
	EntryType string                 `json:"entry_type"`
	Params    map[string]money.Money `json:"params"`
}

// InvestigateResult is the §8 POST /agents/investigate response (SPEC §8):
//
//	{ resolution: {entry_type, params}[], rationale }   // recommendations to review
//	| { escalate: true, reason }                        // -> list unresolved
//
// EXACTLY ONE shape is meaningful. When Escalate is false and Resolution is
// non-empty the agent made a recommendation; the orchestrator logs it and keeps
// the break unresolved. When Escalate is true the agent declined and the
// orchestrator also lists the break unresolved (SPEC §7, §8).
type InvestigateResult struct {
	Resolution []Posting `json:"resolution,omitempty"`
	Rationale  string    `json:"rationale,omitempty"`
	Escalate   bool      `json:"escalate,omitempty"`
	Reason     string    `json:"reason,omitempty"`
}

// Resolvable reports whether the result carries a usable recommendation rather than
// an escalation. The recommendation is still review-only.
func (r InvestigateResult) Resolvable() bool {
	return !r.Escalate && len(r.Resolution) > 0
}

// EscalatedInvestigation builds the {escalate, reason} result. It is the single
// constructor for the escalate path so the reason is always recorded and the
// resolution stays empty.
func EscalatedInvestigation(reason string) InvestigateResult {
	return InvestigateResult{Escalate: true, Reason: reason}
}

// InvestigateClient is the §8 investigate-agent seam (SPEC §8), parallel to
// Client: one single-shot, stateless call mapping one break (+ its candidate
// events) to a resolution or an escalation, plus the FROZEN trace of that
// decision. Both the replay and live implementations satisfy it, so the
// orchestrator depends only on this interface and never on a concrete transport.
//
// Investigate returns an error ONLY for an infrastructure failure (a malformed
// recorded fixture, a network/transport error in live mode) — NOT for a break the
// agent declines, which is a normal {escalate, reason} InvestigateResult. The
// trace is always returned (even alongside an error or an escalation) so every
// agent consultation is recorded.
type InvestigateClient interface {
	// context is the deterministic recovery-engine bundle. The agent may use
	// optional exploration tools for deeper novel-case lookups.
	Investigate(brk BreakSummary, candidates []EventSummary, context ...json.RawMessage) (InvestigateResult, InvestigateTrace, error)
	// Mode reports which transport this client is (replay/live), for the trace and
	// operator-facing reporting.
	Mode() Mode
}
