// brain_investigate.ts is the deterministic INVESTIGATE brain (default; no LLM, no
// deps). It resolves the one break class a posting can fix — the §7 check #3
// "settled-but-not-booked" receivable residual — by tracing it to the unbooked
// refund among the candidates and recovering that refund's gst_rate via the tools
// (refund -> its payment -> its order). Every other break escalates: no ledger
// posting resolves a check #1 (cash) or check #2 (batch-data) break.
//
// The live Flue/LLM investigate brain would do the same multi-hop reasoning with the
// read-only tools; it drops into this slot later. Either way the resolution carries
// recovered FACTS + citations, never money — the Go apply derives net/gst and
// re-verifies the citation.

import type { BreakWork, Resolution, ResolutionPosting } from "./types.ts";
import { StatusEscalated, StatusResolved } from "./types.ts";
import type { Tools } from "./tools.ts";

const orderFetchTool = "getOrder";

export interface InvestigateBrain {
  readonly name: string;
  investigate(work: BreakWork, tools: Tools): Promise<Resolution>;
}

// kindReceivableResidual is the only break kind v1 resolves by posting.
const kindReceivableResidual = "receivable-residual";

export const deterministicInvestigateBrain: InvestigateBrain = {
  name: "deterministic",
  async investigate(work: BreakWork, tools: Tools): Promise<Resolution> {
    const b = work.break;
    if (b.kind !== kindReceivableResidual) {
      return {
        break_key: b.key,
        status: StatusEscalated,
        reason: `a ${b.kind} break cannot be resolved by a posting; it needs a data correction or human review`,
        tools_used: [],
      };
    }

    // Trace the residual to the unbooked refund(s) among the candidates: for each
    // candidate refund, hop refund -> payment -> order and recover the gst_rate,
    // citing the order field. Build a refund_reversal posting per recovered refund.
    const postings: ResolutionPosting[] = [];
    for (const cand of work.candidates) {
      if (cand.type !== "refund") continue;
      const refund = tools.getRefund(cand.event_id);
      if (refund === undefined) continue;
      const payment = tools.getPayment(refund.payment_id);
      if (payment === undefined) continue;
      const order = tools.getOrder(payment.order_id);
      if (order === undefined) continue;
      const rate = order.notes.gst_rate ?? "";
      if (rate === "") continue;
      postings.push({
        event_id: cand.event_id,
        entry_type: "refund_reversal",
        recovered: [
          { field: "gst_rate", value: rate, source: { tool: orderFetchTool, object: payment.order_id, path: "notes.gst_rate" } },
        ],
      });
    }

    if (postings.length === 0) {
      return {
        break_key: b.key,
        status: StatusEscalated,
        reason: "could not recover an unbooked refund explaining the receivable residual",
        tools_used: [orderFetchTool],
      };
    }
    return {
      break_key: b.key,
      status: StatusResolved,
      postings,
      tools_used: [orderFetchTool],
      rationale: `receivable residual traced to ${postings.length} unbooked refund(s); recovered each refund's gst_rate from its order and proposed a refund_reversal`,
    };
  },
};

// selectInvestigateBrain returns the deterministic brain by default, or the live
// Flue/LLM brain when live is set (not yet wired — escalates with guidance).
export async function selectInvestigateBrain(live: boolean): Promise<InvestigateBrain> {
  if (!live) return deterministicInvestigateBrain;
  throw new Error("flue-agent investigate --live is not wired yet; run without --live (deterministic) for now");
}
