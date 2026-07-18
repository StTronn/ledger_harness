/**
 * RunFilm — the review story of one close run.
 *
 * Where LedgerFilm is the *posted* double-entry matrix, a RunFilm narrates the
 * review queue around it: rule misses, recovery context, agent recommendations,
 * and the evidence or escalation recorded for a human.
 *
 * This is a separate contract from LedgerFilm and does not touch it.
 */

export type ConsultRole = "classify" | "investigate";
export type ConsultStatus = "recommended" | "escalated";

/** A recovered fact with its provenance — the citation chip in the UI. */
export interface Citation {
  /** field recovered, e.g. "gst_rate" */
  field: string;
  /** recovered value as a string, e.g. "18" (a percent) */
  value: string;
  /** source object id, e.g. "order_4JBeBpmdz9tXpM" */
  object: string;
  /** dotted path within the source object, e.g. "notes.gst_rate" */
  path: string;
  /** tool that produced it, e.g. "orders.fetch" */
  tool?: string;
}

/** One Dr/Cr line in a possible approved entry (minor units). */
export interface RunPosting {
  side: "Dr" | "Cr";
  account: string;
  amount: number;
}

/** A member of the settlement batch examined during an investigation. */
export interface BatchMember {
  eventId: string;
  type: string;
  amount: number;
  booked: boolean;
  gstRate?: string;
  /** the one unbooked event the break traces to */
  highlight?: boolean;
}

/** The reconciliation break that triggered an investigation. */
export interface BreakInfo {
  key: string;
  check: number;
  kind: string;
  /** expected residual (minor units) */
  expected: number;
  /** observed residual (minor units) */
  actual: number;
  detail: string;
  /** the account carrying the residual */
  account: string;
  /** candidate settlements the agent scanned */
  candidates: string[];
  /** the full settlement batch, with booked ticks */
  batch: BatchMember[];
}

export interface Consultation {
  id: string;
  role: ConsultRole;
  status: ConsultStatus;
  /** what the recommendation concerns: a skipped event or a reconcile break */
  resolves: "skip" | "break";

  /** subject event */
  eventId: string;
  eventType: string;
  amount: number;
  sku?: string;
  orderId?: string;

  /** why the deterministic rules could not book it */
  whyMissed: string;

  /** the recovered citation (absent when the agent escalated without recovery) */
  citation?: Citation;
  toolsUsed: string[];

  /** present on investigations */
  break?: BreakInfo;

  /** the agent's review recommendation */
  entryType?: string;
  params?: Record<string, number>;
  rationale: string;

  /** optional illustrative lines; the agent path never posts them */
  postings: RunPosting[];
  /** retained source reference for an approved posting, when present */
  postedId?: string;
  /** illustrative account effect, before → after (minor units) */
  clears?: { account: string; before: number; after: number };
}

export interface RunCounters {
  booked: number;
  skipped: number;
  breaks: number;
  truth: number;
  scorePct: number;
}

export interface RunMeta {
  world: string;
  period: string;
  title: string;
  symbol: string;
  minorPerMajor: number;
}

export interface RunFilm {
  meta: RunMeta;
  /** rules only — the agent-off baseline */
  baseline: RunCounters;
  /** after the agent review — ledger remains unchanged */
  final: RunCounters;
  consultations: Consultation[];
}

/* ------------------------------------------------------------------ frames */

export type ConsultStage =
  | "flagged"
  | "agent"
  | "decision"
  | "reviewed"
  | "escalated";

export interface RunFrame {
  index: number;
  /** furthest stage reached per consultation at this frame */
  stages: ConsultStage[];
  /** the consultation animating on this frame (null on the opening frame) */
  active: number | null;
  activeStage: ConsultStage | null;
  /** live counters */
  booked: number;
  skipped: number;
  breaks: number;
  /** agent recommendations recorded so far */
  reviewed: number;
  /** floor(booked / truth * 100) — unchanged by review-only agent activity */
  scorePct: number;
}

/**
 * Build the run timeline. Length = 1 + 3 * consultations.length.
 *  - frame 0: the rules-only baseline; every consultation is "flagged".
 *  - each consultation contributes three beats: agent → decision → terminal
 *    (reviewed | escalated). A review terminal records a recommendation but does
 *    not change booked entries, skips, breaks, or score.
 *
 * Playback is a pure function of (film, index): the UI holds a single number.
 */
export function buildRunFrames(film: RunFilm): RunFrame[] {
  const { baseline } = film;
  const stages: ConsultStage[] = film.consultations.map(() => "flagged");
  const frames: RunFrame[] = [];

  const snapshot = (
    index: number,
    active: number | null,
    activeStage: ConsultStage | null,
  ): RunFrame => {
    let reviewed = 0;
    film.consultations.forEach((_c, i) => {
      if (stages[i] === "reviewed") {
        reviewed++;
      }
    });
    const booked = baseline.booked;
    return {
      index,
      active,
      activeStage,
      stages: [...stages],
      booked,
      skipped: baseline.skipped,
      breaks: baseline.breaks,
      reviewed,
      scorePct: baseline.scorePct,
    };
  };

  frames.push(snapshot(0, null, null));

  let idx = 1;
  film.consultations.forEach((c, i) => {
    const beats: ConsultStage[] = [
      "agent",
      "decision",
      c.status === "recommended" ? "reviewed" : "escalated",
    ];
    for (const stage of beats) {
      stages[i] = stage;
      frames.push(snapshot(idx, i, stage));
      idx++;
    }
  });

  return frames;
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Shallow runtime validation of parsed JSON into a RunFilm. */
export function loadRunFilm(json: unknown): RunFilm {
  if (!isObject(json)) throw new Error("loadRunFilm: expected an object");
  const { meta, baseline, final, consultations } = json;
  if (!isObject(meta)) throw new Error("loadRunFilm: missing 'meta'");
  if (!isObject(baseline)) throw new Error("loadRunFilm: missing 'baseline'");
  if (!isObject(final)) throw new Error("loadRunFilm: missing 'final'");
  if (!Array.isArray(consultations)) {
    throw new Error("loadRunFilm: missing 'consultations'");
  }
  return json as unknown as RunFilm;
}
