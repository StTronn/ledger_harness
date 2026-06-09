// brain.ts defines the swappable BRAIN behind the classify agent and ships the
// DEFAULT deterministic brain (no LLM, no key, no deps — runs anywhere on Node).
// The live Flue/LLM brain lives in brain_flue.ts and is loaded lazily only with
// --live, so the committed code runs with zero installed dependencies. Whichever
// brain runs, it produces the SAME Result shape (recovered facts + citations).

import type { EventSummary, Result } from "./types.ts";
import { StatusEscalated, StatusProposed } from "./types.ts";
import type { Tools } from "./tools.ts";

// ClassifyBrain turns one event into a Result. The deterministic and Flue brains
// both satisfy it, so the runner depends only on this interface.
export interface ClassifyBrain {
  readonly name: string;
  classify(event: EventSummary, tools: Tools, skill: string): Promise<Result>;
}

const orderFetchTool = "getOrder";

// escalate builds an escalated Result (the brain still consulted the order tool).
function escalate(eventID: string, reason: string): Result {
  return { event_id: eventID, status: StatusEscalated, reason, tools_used: [orderFetchTool] };
}

// deterministicClassifyBrain models exactly what the live agent does for the v1 long
// tail, with no LLM: a payment arrived without a gst_rate, so fetch its order,
// recover the rate, and propose a dtc_sale citing the source. Non-payments / missing
// order / missing rate escalate honestly.
export const deterministicClassifyBrain: ClassifyBrain = {
  name: "deterministic",
  async classify(event: EventSummary, tools: Tools): Promise<Result> {
    if (event.type !== "payment") {
      return escalate(event.event_id, `v1 classify recovers only payments; ${event.type} is not supported`);
    }
    const orderID = event.order_id ?? "";
    if (orderID === "") return escalate(event.event_id, "payment has no order_id to recover the gst_rate from");
    const order = tools.getOrder(orderID);
    if (order === undefined) return escalate(event.event_id, `no order ${orderID} to recover the gst_rate from`);
    const rate = order.notes.gst_rate ?? "";
    if (rate === "") return escalate(event.event_id, `order ${orderID} has no gst_rate`);
    return {
      event_id: event.event_id,
      status: StatusProposed,
      entry_type: "dtc_sale",
      recovered: [
        { field: "gst_rate", value: rate, source: { tool: orderFetchTool, object: orderID, path: "notes.gst_rate" } },
      ],
      tools_used: [orderFetchTool],
      rationale: `payment ${event.event_id} arrived with no gst_rate; fetched order ${orderID}, recovered gst_rate=${rate}% and proposed a dtc_sale`,
    };
  },
};

// selectClassifyBrain returns the deterministic brain by default, or the live
// Flue/LLM brain when live is set (lazily imported so the default path needs no deps).
export async function selectClassifyBrain(live: boolean): Promise<ClassifyBrain> {
  if (!live) return deterministicClassifyBrain;
  const mod = await import("./brain_flue.ts");
  return mod.flueClassifyBrain();
}
