// classify.ts — orchestrates POST /agents/classify: ask the brain for the judgment,
// apply the SHARED split, and shape the §8 response byte-for-byte. The Go LiveClient
// JSON-decodes the body into ClassifyResult, so the two response shapes here must
// match exactly: { entry_type, params, rationale } OR { unclassifiable, reason }.
import type { Brain, ClassifyEvent } from "./brain.ts";
import type { EventContext } from "./tools.ts";
import { split } from "./split.ts";

export interface ClassifyRequest {
  event: ClassifyEvent;
  context: EventContext;
  world: string;
  period: string;
}

// The two §8 response shapes (exactly one is returned).
export type ClassifyResponse =
  | { entry_type: string; params: Record<string, number>; rationale: string }
  | { unclassifiable: true; reason: string };

export async function handleClassify(
  brain: Brain,
  req: ClassifyRequest,
): Promise<ClassifyResponse> {
  const judgment = await brain.classify(req.event, req.context, req.world, req.period);

  if (judgment.kind === "unclassifiable") {
    return { unclassifiable: true, reason: judgment.reason };
  }

  // Shared param computation, keyed by entry type (the id params are always 0 —
  // placeholders; the Go ledger carries the real id on the entry's TxID):
  //   dtc_sale        -> { gross, net, gst, payment_id: 0 }
  //   refund_reversal -> { net, gst, refund_id: 0 }   (a partial line-item return)
  const { net, gst } = split(judgment.gross, judgment.rate);
  const params: Record<string, number> =
    judgment.entry_type === "refund_reversal"
      ? { net, gst, refund_id: 0 }
      : { gross: judgment.gross, net, gst, payment_id: 0 };
  return {
    entry_type: judgment.entry_type,
    params,
    rationale: judgment.rationale,
  };
}
