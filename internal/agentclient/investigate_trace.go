package agentclient

// InvestigateTraceSchemaVersion is the FROZEN version stamped into every
// InvestigateTrace (SPEC §9, §13 "freeze the trace schema early — it's the
// learning seam"). It is the investigate counterpart of TraceSchemaVersion: the
// field set, names, and meanings below are locked at v1 and do not change without
// a deliberate version bump here AND a documented migration, so the future
// learning layer that consumes investigate traces has a stable contract.
//
// FROZEN SCHEMA — do not add/rename/repurpose fields without bumping this.
const InvestigateTraceSchemaVersion = 1

// InvestigateDecision is the agent's verdict as it appears in an investigate
// trace (SPEC §9 "decision"): the postings it proposed to add. On an escalation
// it is empty and the trace's Rationale carries the reason instead.
type InvestigateDecision struct {
	Resolution []Posting `json:"resolution,omitempty"`
}

// InvestigateTrace is the FROZEN, versioned record of ONE agent investigate call
// (SPEC §8 "every agent call emits a trace (input, tools used, decision,
// rationale)"; §9, §13), parallel to Trace for the classify seam. The fields map
// 1:1 to SPEC §9's named trace contents:
//
//   - SchemaVersion: the frozen schema stamp (always InvestigateTraceSchemaVersion).
//   - BreakKey:      the break this decision is for (the join key).
//   - Mode:          which transport produced it (replay/live) — provenance.
//   - Input:         the break summary the agent saw (so the trace is self-contained).
//   - Candidates:    the candidate events the orchestrator supplied for the break
//     (the unbooked suspects), so the trace records what context the decision had.
//   - ToolsUsed:     the read-only tools the agent consulted (e.g. "orders.fetch"
//     to recover a refund's rate). Ordered and deterministic; never nil.
//   - Decision:      the postings to add (empty on an escalation).
//   - Rationale:     the agent's reasoning; on an escalation, the escalate reason.
//
// JSON key order is fixed by struct tags so a written trace is byte-stable
// (SPEC §12). All money in Decision.Resolution params is integer paise.
type InvestigateTrace struct {
	SchemaVersion int                 `json:"schema_version"`
	BreakKey      string              `json:"break_key"`
	Mode          Mode                `json:"mode"`
	Input         BreakSummary        `json:"input"`
	Candidates    []EventSummary      `json:"candidates"`
	ToolsUsed     []string            `json:"tools_used"`
	Decision      InvestigateDecision `json:"decision"`
	Rationale     string              `json:"rationale"`
}

// newInvestigateTrace assembles a frozen v1 InvestigateTrace from one investigate
// call's inputs and result. It is the single place an InvestigateTrace is
// constructed, so the schema version is always stamped, tools_used/candidates are
// never nil (an empty slice marshals as []), and the decision/rationale are
// projected consistently (a resolution fills the decision; an escalation leaves it
// empty and puts the reason in the rationale).
func newInvestigateTrace(mode Mode, brk BreakSummary, candidates []EventSummary, tools []string, res InvestigateResult) InvestigateTrace {
	if tools == nil {
		tools = []string{}
	}
	if candidates == nil {
		candidates = []EventSummary{}
	}
	t := InvestigateTrace{
		SchemaVersion: InvestigateTraceSchemaVersion,
		BreakKey:      brk.Key,
		Mode:          mode,
		Input:         brk,
		Candidates:    candidates,
		ToolsUsed:     tools,
	}
	if res.Resolvable() {
		t.Decision = InvestigateDecision{Resolution: res.Resolution}
		t.Rationale = res.Rationale
	} else {
		t.Rationale = res.Reason
	}
	return t
}
