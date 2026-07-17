// brain.ts — the "brain" abstraction and its deterministic stub implementation.
//
// A Brain makes the JUDGMENT calls of the §8 seam (which entry type, which rate,
// whether to escalate) and nothing else. The PARAM COMPUTATION (the inclusive-GST
// split) and the HTTP RESPONSE SHAPING live in classify.ts / investigate.ts and
// are SHARED by every brain. That split is deliberate: the stub brain proves the
// params and the wire shape are correct WITHOUT an API key, and the Flue brain
// (brain_flue.ts) only swaps the judgment — never the arithmetic or the contract.
//
// The stub encodes the SAME decision rules as the generated SKILL.md by reading
// the recovery context bundle supplied in each request, so the whole flow is
// verifiable offline.
import type { BreakContext, EventContext } from "./tools.ts";

// ---- Judgments: the brain's decisions, BEFORE the split is applied ----

// A classify judgment is exactly one of: book this entry type at this rate, or
// decline. `gross` and `rate` are what the shared shaper feeds to split().
export type ClassifyJudgment =
  | { kind: "classify"; entry_type: string; gross: number; rate: number; rationale: string }
  | { kind: "unclassifiable"; reason: string };

// One refund the investigate brain decided to reverse (pre-split).
export interface InvestigatePostingJudgment {
  event_id: string;
  entry_type: string;
  gross: number;
  rate: number;
}

// An investigate judgment is exactly one of: a list of postings to book, or escalate.
export type InvestigateJudgment =
  | { kind: "resolve"; postings: InvestigatePostingJudgment[]; rationale: string }
  | { kind: "escalate"; reason: string };

// The event the orchestrator posts to /agents/classify (request body `event`).
export interface ClassifyEvent {
  event_id: string;
  type: string;
  amount: number;
  order_id?: string;
  gst_rate?: string;
  sku?: string;
}

// The break the orchestrator posts to /agents/investigate (request body `break`).
export interface InvestigateBreak {
  key: string;
  check: number;
  kind: string;
  settlement_id?: string;
  expected: number;
  actual: number;
  candidates?: string[];
  detail?: string;
}

export interface Brain {
  readonly name: string;
  classify(event: ClassifyEvent, context: EventContext, world: string, period: string): Promise<ClassifyJudgment>;
  investigate(
    brk: InvestigateBreak,
    context: BreakContext,
    world: string,
    period: string,
  ): Promise<InvestigateJudgment>;
}

// parseRate turns a notes rate string ("18", "18%", " 5 ") into a positive integer
// percentage, or null when absent/unusable (the caller then declines/escalates
// rather than guess).
function parseRate(raw: string | undefined): number | null {
  if (raw === undefined) return null;
  const trimmed = raw.trim().replace(/%$/, "");
  if (trimmed === "") return null;
  const n = Number(trimmed);
  if (!Number.isInteger(n) || n <= 0) return null;
  return n;
}

// stubBrain — deterministic, no LLM, no API key. It reads the supplied recovery
// context bundle and applies the SKILL.md decision rules mechanically.
export const stubBrain: Brain = {
  name: "stub",

  async classify(event, ctx, world, period): Promise<ClassifyJudgment> {
    // PARTIAL refunds: the judgment world. The bundle carries the order's line
    // items and precomputed match candidates; the policy mirrors SKILL.md:
    //   1. an ops annotation (reason, e.g. "goodwill") => human call, escalate;
    //   2. exactly one item/pair match => that item returned, refund_reversal;
    //   3. ambiguity or no match => escalate, never guess.
    if (event.type === "refund") {
      if (ctx.event.parent_amount === undefined) {
        // A FULL refund reaching classify (e.g. stripped rate) stays the
        // investigate agent's territory in v1.
        return {
          kind: "unclassifiable",
          reason: `rule miss "missing gst_rate" on refund event is not a payment the classify agent recovers in v1`,
        };
      }
      if (ctx.event.reason) {
        return {
          kind: "unclassifiable",
          reason: `partial refund ${event.event_id} is annotated "${ctx.event.reason}" — a goodwill/manual credit is a human policy call, not an agent booking`,
        };
      }
      const matches = (ctx.candidates ?? []).filter(
        (c) => c.kind === "item-match" || c.kind === "pair-match",
      );
      if (matches.length === 1) {
        const m = matches[0];
        const rate = parseRate(m.gst_rate);
        if (rate !== null) {
          const cite = m._source ? `${m._source.object}/${m._source.path}` : "the order's line items";
          return {
            kind: "classify",
            entry_type: "refund_reversal",
            gross: event.amount,
            rate,
            rationale: `partial refund ${event.event_id} equals line item ${cite} — booked the line-item return as a refund_reversal at ${rate}%`,
          };
        }
      }
      if (matches.length > 1) {
        return {
          kind: "unclassifiable",
          reason: `partial refund ${event.event_id} matches ${matches.length} candidate item sets — ambiguous, needs a human`,
        };
      }
      return {
        kind: "unclassifiable",
        reason: `partial refund ${event.event_id} matches no line item or pair — unexplained partial credit needs a human`,
      };
    }

    // Rule 1: classify otherwise only recovers PAYMENTS in v1. A settlement/
    // dispute that reaches classify is not recovered here.
    if (event.type !== "payment") {
      return {
        kind: "unclassifiable",
        reason: `rule miss "missing gst_rate" on ${event.type} event is not a payment the classify agent recovers in v1`,
      };
    }

    // Rule 3: recover the rate from the prepared context (prefer the event's
    // own rate if it already carries one, else the order's notes).
    const rate = parseRate(event.gst_rate) ?? parseRate(ctx.recovered?.gst_rate);
    if (rate === null) {
      return {
        kind: "unclassifiable",
        reason: `payment ${event.event_id} arrived with no gst_rate and none could be recovered from its order`,
      };
    }

    const source = ctx.recovered?._source?.object ?? event.order_id ?? "the order";
    return {
      kind: "classify",
      entry_type: "dtc_sale",
      gross: event.amount,
      rate,
      rationale: `payment ${event.event_id} arrived with no gst_rate; fetched ${source}, recovered gst_rate=${rate}%, and booked the dtc_sale at the recovered rate`,
    };
  },

  async investigate(brk, ctx, world, period): Promise<InvestigateJudgment> {
    // Use the prepared settlement batch to find unbooked refunds — each one leaves
    // exactly its gross stuck in the settlement receivable.
    const unbookedRefunds = ctx.batch.filter((m) => m.booked === false && m.type === "refund");

    // PARTIAL refunds must never be reversed here: their intent (return vs
    // goodwill) is a classify-side judgment, so a residual tracing to partials
    // escalates instead — mirroring the Go recorded-fixture generator.
    const partials = unbookedRefunds.filter((m) => m.parent_amount !== undefined);
    const fulls = unbookedRefunds.filter((m) => m.parent_amount === undefined);
    if (fulls.length === 0 && partials.length > 0) {
      return {
        kind: "escalate",
        reason: `residual traces to ${partials.length} PARTIAL refund(s) whose intent (return vs goodwill credit) is unrecoverable from the snapshot — escalating to a human, never guessing`,
      };
    }

    const postings: InvestigatePostingJudgment[] = [];
    for (const r of fulls) {
      const rate = parseRate(r.recovered?.gst_rate) ?? parseRate(r.gst_rate);
      if (rate === null) continue; // cannot recover this one's rate; skip it
      postings.push({
        event_id: r.event_id,
        entry_type: "refund_reversal",
        gross: r.amount,
        rate,
      });
    }

    if (postings.length === 0) {
      return {
        kind: "escalate",
        reason: `receivable-residual break ${brk.key} had no unbooked refund with a recoverable gst_rate to reverse`,
      };
    }

    const residual = (brk.actual / 100).toFixed(2);
    return {
      kind: "resolve",
      postings,
      rationale: `settlement-receivable residual ${residual} traced to ${postings.length} unbooked refund(s); fetched each refund's order, recovered its gst_rate, and booked the refund_reversal so the receivable clears`,
    };
  },
};
