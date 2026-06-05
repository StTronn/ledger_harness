package agentclient

import "github.com/razorpay/close-agent/internal/money"

// TraceSchemaVersion is the FROZEN version stamped into every Trace.SchemaVersion
// (SPEC §9, §13 "freeze the trace schema early — it's the learning seam"). The
// trace shape below is locked at v1: the field set, names, and meanings do not
// change without a deliberate version bump here AND a documented migration, so
// the future learning layer that consumes traces has a stable contract.
//
// FROZEN SCHEMA — do not add/rename/repurpose fields without bumping this and
// recording why. New optional information goes in a new versioned trace, never by
// silently widening v1.
const TraceSchemaVersion = 1

// Decision is the agent's verdict as it appears in a trace (SPEC §9 "decision"):
// the entry type it chose and the paise params it bound (the {entry_type, params}
// it returns — never raw debits/credits, SPEC §3, §8). On an escalation both
// fields are zero and Trace.Rationale carries the reason instead.
type Decision struct {
	EntryType string                 `json:"entry_type,omitempty"`
	Params    map[string]money.Money `json:"params,omitempty"`
}

// Trace is the FROZEN, versioned record of ONE agent classify call (SPEC §8 "every
// agent call emits a trace (input, tools used, decision, rationale)"; §9, §13). It
// is the learning seam: the single artifact the (deferred) learning layer consumes
// to understand what the agent did and why, so its schema is frozen at
// TraceSchemaVersion. The fields map 1:1 to SPEC §9's named trace contents:
//
//   - SchemaVersion: the frozen schema stamp (always TraceSchemaVersion for v1).
//   - EventID:       the source event this decision is for (the join key).
//   - Mode:          which transport produced it (replay/live) — provenance.
//   - Input:         the event summary the agent saw (so the trace is self-contained).
//   - ToolsUsed:     the read-only tools the agent consulted (SPEC §8 "tools used"),
//     e.g. "orders.fetch" when it recovered the rate from the order. Ordered and
//     deterministic; never nil (an empty decision still records []).
//   - Decision:      the {entry_type, params} verdict (zero on an escalation).
//   - Rationale:     the agent's human-readable reasoning; on an escalation this
//     holds the unclassifiable reason.
//
// JSON key order is fixed by struct tags so a written trace is byte-stable
// (SPEC §12). All money in Decision.Params is integer paise (money.Money).
type Trace struct {
	SchemaVersion int          `json:"schema_version"`
	EventID       string       `json:"event_id"`
	Mode          Mode         `json:"mode"`
	Input         EventSummary `json:"input"`
	ToolsUsed     []string     `json:"tools_used"`
	Decision      Decision     `json:"decision"`
	Rationale     string       `json:"rationale"`
}

// newTrace assembles a frozen v1 Trace from one classify call's inputs and result.
// It is the single place a Trace is constructed, so the schema version is always
// stamped, tools_used is never nil (an empty slice marshals as []), and the
// decision/rationale are projected consistently from the ClassifyResult (a
// classification fills the decision; an escalation leaves it zero and puts the
// reason in the rationale).
func newTrace(mode Mode, ev EventSummary, tools []string, res ClassifyResult) Trace {
	if tools == nil {
		tools = []string{}
	}
	t := Trace{
		SchemaVersion: TraceSchemaVersion,
		EventID:       ev.EventID,
		Mode:          mode,
		Input:         ev,
		ToolsUsed:     tools,
	}
	if res.Classifiable() {
		t.Decision = Decision{EntryType: res.EntryType, Params: res.Params}
		t.Rationale = res.Rationale
	} else {
		// Escalation: no decision; surface the reason as the rationale so a single
		// field carries "why" regardless of outcome.
		t.Rationale = res.Reason
	}
	return t
}
