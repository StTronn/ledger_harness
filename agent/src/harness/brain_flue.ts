// brain_flue.ts is the LIVE Flue/LLM brain — loaded lazily (only with --live), so
// the committed code and the deterministic path need NO installed dependencies.
// To enable it:  pnpm add flue @ai-sdk/anthropic   and set ANTHROPIC_API_KEY.
//
// It builds a Flue agent whose instructions are the generated SKILL.md, whose only
// tool is a read-only getOrder over the snapshot, and whose structured result is the
// recovered gst_rate + citation. The agent NEVER returns money — the Go apply stage
// derives net/gst and re-verifies the citation, so a wrong rate is caught.
//
// NOTE: `flue` is imported through a variable specifier so this file typechecks even
// when flue is not installed; the import only executes under --live.

import type { EventSummary, Result } from "./types.ts";
import { StatusEscalated, StatusProposed } from "./types.ts";
import type { Tools } from "./tools.ts";
import type { ClassifyBrain } from "./brain.ts";

const orderFetchTool = "getOrder";

// flueClassifyBrain constructs the live brain. The dynamic import keeps `flue` out
// of the static dependency graph.
export function flueClassifyBrain(): ClassifyBrain {
  return {
    name: "flue",
    async classify(event: EventSummary, tools: Tools, skill: string): Promise<Result> {
      if (!process.env.ANTHROPIC_API_KEY) {
        throw new Error("flue-agent --live requires ANTHROPIC_API_KEY (the default deterministic brain needs no key)");
      }
      const spec = "flue"; // variable specifier → not statically resolved by tsc
      let flue: any;
      try {
        flue = await import(spec);
      } catch {
        throw new Error("flue-agent --live needs the Flue SDK installed: run `pnpm add flue @ai-sdk/anthropic` in agent/");
      }

      const agent = flue.createAgent({
        model: process.env.CLOSE_AGENT_MODEL ?? "anthropic/claude-sonnet-4-6",
        instructions: skill,
        // One read-only tool: fetch an order's notes from the snapshot. The agent
        // cannot reach anything else (no truth, no live Razorpay).
        tools: {
          getOrder: {
            description: "Fetch a snapshotted order by id; returns its notes (sku, gst_rate).",
            parameters: { type: "object", properties: { order_id: { type: "string" } }, required: ["order_id"] },
            execute: async ({ order_id }: { order_id: string }) => {
              const o = tools.getOrder(order_id);
              return o ? { id: o.id, notes: o.notes } : { error: "not found" };
            },
          },
        },
      });

      // Ask for the structured recovery: { gst_rate, order_id } or { unclassifiable }.
      const session = agent.session();
      const out = await session.prompt(
        `Classify this event into a playbook entry type. If it is a payment missing its gst_rate, ` +
          `fetch its order (order_id=${event.order_id}) and recover the rate. ` +
          `Event: ${JSON.stringify(event)}`,
        {
          result: {
            type: "object",
            properties: {
              unclassifiable: { type: "boolean" },
              reason: { type: "string" },
              entry_type: { type: "string" },
              gst_rate: { type: "string" },
              order_id: { type: "string" },
            },
          },
        },
      );

      if (out.unclassifiable || !out.entry_type) {
        return { event_id: event.event_id, status: StatusEscalated, reason: out.reason ?? "agent declined", tools_used: [orderFetchTool] };
      }
      // The agent returns the recovered FACT + where it found it; the Go apply stage
      // re-verifies the citation and derives the money.
      return {
        event_id: event.event_id,
        status: StatusProposed,
        entry_type: out.entry_type,
        recovered: [
          { field: "gst_rate", value: String(out.gst_rate), source: { tool: orderFetchTool, object: out.order_id ?? event.order_id ?? "", path: "notes.gst_rate" } },
        ],
        tools_used: [orderFetchTool],
        rationale: out.reason ?? `recovered gst_rate=${out.gst_rate}% from order ${out.order_id}`,
      };
    },
  };
}
