// investigate.ts — orchestrates POST /agents/investigate: ask the brain for the
// judgment, apply the SHARED split to each recommendation, and shape the §8 response. The
// Go LiveInvestigateClient decodes the body into InvestigateResult, so the shapes
// must match exactly: { resolution: [{event_id, entry_type, params}], rationale }
// OR { escalate, reason }.
import type { Brain, InvestigateBreak, ClassifyEvent } from "./brain.ts";
import { split } from "./split.ts";
import type { BreakContext } from "./tools.ts";

export interface InvestigateRequest {
  break: InvestigateBreak;
  context: BreakContext;
  // The orchestrator carries candidate events for traceability; the recovery
  // engine's context bundle is the primary input to the brain.
  candidates?: ClassifyEvent[];
  world: string;
  period: string;
}

export interface ResolutionPosting {
  event_id: string;
  entry_type: string;
  params: Record<string, number>;
}
export type InvestigateResponse =
  | { resolution: ResolutionPosting[]; rationale: string }
  | { escalate: true; reason: string };

export async function handleInvestigate(
  brain: Brain,
  req: InvestigateRequest,
): Promise<InvestigateResponse> {
  const judgment = await brain.investigate(req.break, req.context, req.world, req.period);

  if (judgment.kind === "escalate") {
    return { escalate: true, reason: judgment.reason };
  }

  // Shared param computation: refund_reversal recommends { net, gst, refund_id:0 }
  // (no gross param). refund_id is always 0 — the Go ledger carries the real id.
  const resolution: ResolutionPosting[] = judgment.postings.map((p) => {
    const { net, gst } = split(p.gross, p.rate);
    return {
      event_id: p.event_id,
      entry_type: p.entry_type,
      params: { net, gst, refund_id: 0 },
    };
  });

  return { resolution, rationale: judgment.rationale };
}
