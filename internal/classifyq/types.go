// Package classifyq is the ASYNCHRONOUS classify pipeline (the redefined §8
// classify seam). Where internal/agentclient.Classify is a synchronous, inline,
// single-shot call, classifyq decouples the work into three stages joined by two
// KEYED stores, so the classify agent runs OUT OF BAND from the deterministic
// close:
//
//	PROPOSE  (deterministic, internal/closer)  rule misses -> WorkItem[]  -> proposals.json
//	WORK     (async worker, the "agent brain")  WorkItem -> Result        -> results.json
//	APPLY    (deterministic, internal/closer)   Result -> validate -> review -> derive -> post
//
// # Determinism anchor (keyed stores)
//
// Both stores are keyed by event_id and written in stable, sorted JSON. The APPLY
// stage only ever does a keyed lookup, so HOW the results were produced (sync,
// async, concurrent, remote) never affects the booked close — same results in =
// byte-identical books out.
//
// # The agent emits recovered FACTS, not money (numeric-surface hardening)
//
// A Result does not carry net/gst rupee values. It carries the minimal recovered
// FACT — the gst_rate — together with a machine-checkable CITATION of where it
// came from (which order object, which field). The APPLY stage re-reads that
// citation against the snapshot (Validate), then DERIVES the money itself via the
// canonical gstsplit. So the worker cannot inject an arbitrary number: it can only
// point at a field, and the pointer is re-verified.
//
// # Boundaries
//
// classifyq imports internal/agentclient (for the EventSummary shape and the
// orders.json recovery source) and internal/money. It MUST NOT import
// internal/truth — the recovered rate comes from orders.json, never the answer key.
package classifyq

import "github.com/razorpay/close-agent/internal/agentclient"

// SchemaVersion is the frozen version stamped into both stores (proposals.json /
// results.json). Bump deliberately on a format change.
const SchemaVersion = 1

// WorkItem is one rule-missed event handed to the async worker (PROPOSE -> WORK).
// Event is the same source-agnostic EventSummary the synchronous classify agent
// sees; Reason is why the deterministic rules missed (for the worker's context and
// the audit trail).
type WorkItem struct {
	EventID string                   `json:"event_id"`
	Event   agentclient.EventSummary `json:"event"`
	Reason  string                   `json:"reason"`
}

// Source is a machine-checkable CITATION for a recovered fact: the read-only tool
// the worker used, the object it read, and the field path within it. The APPLY
// stage dereferences this against the snapshot to confirm the value is real
// (Validate), so a fabricated value would need a fabricated — and detectable —
// citation.
type Source struct {
	Tool   string `json:"tool"`   // e.g. "orders.fetch"
	Object string `json:"object"` // e.g. "order_SabVgcKXqe0eqv"
	Path   string `json:"path"`   // e.g. "notes.gst_rate"
}

// Recovered is one fact the worker recovered, with its citation. In v1 the only
// recovered fact is the gst_rate; Value is the raw string as read from the source
// (e.g. "5"), kept as a string so it equals the cited field byte-for-byte.
type Recovered struct {
	Field  string `json:"field"` // e.g. "gst_rate"
	Value  string `json:"value"` // raw value as read from Source
	Source Source `json:"source"`
}

// Result status values.
const (
	// StatusProposed: the worker produced a classification proposal (entry_type +
	// recovered facts) for the APPLY stage to validate, review, and post.
	StatusProposed = "proposed"
	// StatusEscalated: the worker could not recover the event; APPLY leaves it
	// skipped (never guessed). Reason explains why.
	StatusEscalated = "escalated"
)

// Result is the worker's answer for one event (WORK -> APPLY). On a proposal it
// carries the chosen entry_type and the recovered facts (with citations) — but NOT
// the money; APPLY derives net/gst from the recovered rate. On an escalation only
// Status+Reason are set. ToolsUsed mirrors the read-only tools consulted.
type Result struct {
	EventID   string      `json:"event_id"`
	Status    string      `json:"status"`
	EntryType string      `json:"entry_type,omitempty"`
	Recovered []Recovered `json:"recovered,omitempty"`
	ToolsUsed []string    `json:"tools_used,omitempty"`
	Rationale string      `json:"rationale,omitempty"`
	Reason    string      `json:"reason,omitempty"`
}

// Proposed reports whether the result is a usable proposal (vs an escalation).
func (r Result) Proposed() bool { return r.Status == StatusProposed && r.EntryType != "" }

// ProposalsFile is the on-disk proposals store: a version stamp plus the work
// items, sorted by event_id for a byte-stable, reviewable file.
type ProposalsFile struct {
	SchemaVersion int        `json:"schema_version"`
	World         string     `json:"world"`
	Period        string     `json:"period"`
	Items         []WorkItem `json:"items"`
}

// ResultsFile is the on-disk results store: a version stamp plus the worker's
// results, sorted by event_id.
type ResultsFile struct {
	SchemaVersion int      `json:"schema_version"`
	World         string   `json:"world"`
	Period        string   `json:"period"`
	Results       []Result `json:"results"`
}

// index builds the event_id -> Result lookup the APPLY stage uses (first wins on a
// duplicate; the file is generated unique+sorted, so this is defensive only).
func (f ResultsFile) index() map[string]Result {
	m := make(map[string]Result, len(f.Results))
	for _, r := range f.Results {
		if _, dup := m[r.EventID]; !dup {
			m[r.EventID] = r
		}
	}
	return m
}

// Index exposes the event_id -> Result lookup for the APPLY stage.
func (f ResultsFile) Index() map[string]Result { return f.index() }
