// types.ts mirrors the Go wire shapes EXACTLY (internal/classifyq + internal/agentclient).
// The Go side reads these stores with DisallowUnknownFields, so the field names here
// are a contract: no extra keys, exact spelling. Money is integer paise (a number).

// EventSummary mirrors agentclient.EventSummary — the source-agnostic event the
// agent sees. amount is integer paise.
export interface EventSummary {
  event_id: string;
  type: string; // payment | refund | settlement | dispute
  amount: number; // paise
  order_id?: string;
  gst_rate?: string; // present only if the event still carries it
  sku?: string;
}

// WorkItem mirrors classifyq.WorkItem — one parked rule miss.
export interface WorkItem {
  event_id: string;
  event: EventSummary;
  reason: string;
}

// ProposalsFile mirrors classifyq.ProposalsFile — the work queue emitted by
// `close --agent off`.
export interface ProposalsFile {
  schema_version: number;
  world: string;
  period: string;
  items: WorkItem[];
}

// Source mirrors classifyq.Source — a machine-checkable provenance citation.
export interface Source {
  tool: string;
  object: string;
  path: string;
}

// Recovered mirrors classifyq.Recovered — one recovered fact + its citation.
export interface Recovered {
  field: string;
  value: string;
  source: Source;
}

export const StatusProposed = "proposed";
export const StatusEscalated = "escalated";

// Result mirrors classifyq.Result — the agent's answer for one event. It carries
// recovered FACTS (+ citations), never money; the Go apply stage derives net/gst.
export interface Result {
  event_id: string;
  status: string;
  entry_type?: string;
  recovered?: Recovered[];
  tools_used?: string[];
  rationale?: string;
  reason?: string;
}

// ResultsFile mirrors classifyq.ResultsFile — what the worker writes for apply.
export interface ResultsFile {
  schema_version: number;
  world: string;
  period: string;
  results: Result[];
}

export const SchemaVersion = 1;
