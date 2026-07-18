/**
 * RtoFilm — the cash-on-delivery / RTO story of one close run (ROADMAP §8.3).
 *
 * Where RunFilm narrates classify/investigate consultations in general, an
 * RtoFilm is purpose-built for the COD demo's one distinctive move: a courier
 * remittance arrives netted; reconcile finds the cod-receivable short; and the
 * recovery engine DECOMPOSES the remittance — identifying the rate-card-backed
 * RTO fee and the unverified weight dispute. The agent reviews those findings;
 * neither recommendation is posted automatically, so the residual remains open.
 *
 * Every field traces to a real ledger-flow artifact (the break bundle, the
 * recorded resolution, the courier feed); the generator never invents numbers.
 */

export interface RtoMeta {
  world: string;
  period: string;
  title: string;
  symbol: string;
  minorPerMajor: number;
  courier: string;
}

/** The courier's netted weekly payout. gross = net + fee + gstOnFee + Σ deductions. */
export interface Remittance {
  id: string;
  utr: string;
  grossCollected: number;
  collectionFee: number;
  gstOnFee: number;
  netDeposit: number;
  deductionsTotal: number;
}

/** Lifecycle tally for the period's COD shipments. */
export interface Lifecycle {
  delivered: number;
  rto: number;
}

/** One Dr/Cr line of a posted entry (minor units). */
export interface RtoPosting {
  side: "Dr" | "Cr";
  account: string;
  amount: number;
}

/**
 * One deduction line the courier netted out, with the rate-card verdict the
 * harness's rto-fee policy assigned it. A supported line carries the recommendation
 * and evidence; an unbacked line carries the escalation reason.
 */
export interface Deduction {
  id: string;
  code: string;
  label: string;
  shipmentId: string;
  shipmentStatus: string;
  amount: number;
  backed: boolean;
  /** "supported" | "escalate" */
  verdict: "supported" | "escalate";
  entryType?: string;
  gstRate?: string;
  /** rate-card citation for a backed line */
  citation?: { object: string; path: string };
  /** the agent's rationale for this line */
  note: string;
  /** illustrative entry lines for an approved action (empty when escalated) */
  postings: RtoPosting[];
}

export interface RtoCounters {
  scorePct: number;
  /** truth entries missing from the produced books at this stage */
  missing: number;
}

export interface RtoFilm {
  meta: RtoMeta;
  remittance: Remittance;
  lifecycle: Lifecycle;
  /** the cod-receivable residual the break carries, before the agent runs */
  residualBefore: number;
  /** the residual that remains after review-only agent activity */
  residualAfter: number;
  breakDetail: string;
  deductions: Deduction[];
  /** rules-only (agent off) */
  baseline: RtoCounters;
  /** after the agent (replay) */
  final: RtoCounters;
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Shallow runtime validation of parsed JSON into an RtoFilm. */
export function loadRtoFilm(json: unknown): RtoFilm {
  if (!isObject(json)) throw new Error("loadRtoFilm: expected an object");
  for (const k of ["meta", "remittance", "lifecycle", "baseline", "final"]) {
    if (!isObject(json[k])) throw new Error(`loadRtoFilm: missing '${k}'`);
  }
  if (!Array.isArray(json.deductions)) {
    throw new Error("loadRtoFilm: missing 'deductions'");
  }
  return json as unknown as RtoFilm;
}
