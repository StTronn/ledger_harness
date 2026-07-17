// brain_ai.ts — the LIVE LLM brain. Same Brain interface (same inputs/outputs) as
// the deterministic stub, but the JUDGMENT comes from an LLM driven by the
// generated SKILL.md as its system instructions, with the recovery context bundle
// supplied as the primary input and read-only CLI commands (tools.ts) wired in as
// optional model-callable tools. The shared inclusive-GST
// split + §8 response shaping in classify.ts/investigate.ts are UNCHANGED, so this
// brain swaps ONLY the judgment — never the arithmetic or the wire contract.
//
// ── Why the Vercel AI SDK and not `@flue/runtime` ───────────────────────────────
// The real Flue framework (`@flue/runtime`, by withastro) is a durable
// workflow/dispatch runtime: its ergonomic `createAgent` + `session.prompt(...,
// {result})` surface is only reachable from a `FlueContext` produced inside a
// workflow or HTTP-dispatch harness (`ctx.init()`), and that context is built only
// by internal server plumbing (`createFlueContext` wants a fully-resolved
// `AgentConfig`, a `SessionStore`, and a sandbox `SessionEnv`). Its synchronous
// invocation path is a mounted Hono app (`POST /agents/:name/:id`), which also
// cannot express a structured `result` schema across the HTTP boundary. There is no
// clean inline "prompt with tools + structured output" call. Per SPEC §14 the Brain
// interface is the swap point, so we honor the sanctioned fallback: the Vercel AI
// SDK (`ai` + `@ai-sdk/openai`) behind the exact same Brain. Live deps:
//   npm install ai @ai-sdk/openai zod   (+ OPENAI_API_KEY)
import { generateText, stepCountIs, tool, Output } from "ai";
import { openai } from "@ai-sdk/openai";
import { z } from "zod";
import type {
  Brain,
  ClassifyEvent,
  ClassifyJudgment,
  InvestigateBreak,
  InvestigateJudgment,
  InvestigatePostingJudgment,
} from "./brain.ts";
import type { BreakContext, EventContext } from "./tools.ts";
import { runCli } from "./tools.ts";

// The model is a `provider/model` spec to stay framework-agnostic; the AI SDK's
// OpenAI provider takes the bare model id, so we strip a leading `openai/`.
const MODEL_SPEC = process.env.LEDGER_FLOW_MODEL ?? "openai/gpt-4o-mini";
const OPENAI_MODEL = MODEL_SPEC.replace(/^openai\//, "");

// Cap the agentic tool loop: each judgment needs at most one context fetch plus the
// final structured answer, so a handful of steps is ample headroom.
const MAX_STEPS = 6;

// parseRate mirrors brain.ts: a notes rate string ("18", "18%", " 5 ") or number ->
// positive integer percent, or null when absent/unusable (caller declines/escalates
// rather than guess).
function parseRate(raw: unknown): number | null {
  if (typeof raw !== "string") {
    return typeof raw === "number" && Number.isInteger(raw) && raw > 0 ? raw : null;
  }
  const trimmed = raw.trim().replace(/%$/, "");
  if (trimmed === "") return null;
  const n = Number(trimmed);
  return Number.isInteger(n) && n > 0 ? n : null;
}

// The two read-only CLI tools, bound to a (world, period). Both shell out through
// tools.ts — the single source of truth for "what the agent can see" — and return
// the raw JSON as a string for the model to read. These are the agent's ENTIRE §8
// authority: read-only context over the period's committed fixtures.
function contextTools(world: string, period: string) {
  return {
    getEventContext: tool({
      description:
        "Fetch one event's recovery context (event fields, recovered gst_rate, " +
        "applicable entry types) for the current world/period. Call with the event_id.",
      inputSchema: z.object({ event_id: z.string() }),
      execute: async ({ event_id }) =>
        JSON.stringify(
          await runCli(["context", "event", event_id, "--world", world, "--period", period]),
        ),
    }),
    getEntity: tool({
      description:
        "Tier-2 lookup: fetch ANY snapshotted object by id when the context bundle " +
        "is not enough — pay_/rfnd_/setl_/disp_ (raw object + booked + edges), " +
        "order_ (line items + rate), ratecard/<channel> (contracted fees), or an " +
        "account path like assets/razorpay-settlement-receivable (balance). Read-only.",
      inputSchema: z.object({ id: z.string() }),
      execute: async ({ id }) =>
        JSON.stringify(await runCli(["context", "entity", id, "--world", world, "--period", period])),
    }),
    getBreakContext: tool({
      description:
        "Fetch one reconcile break's settlement batch + recovery context (each " +
        "member's booked flag, type, amount and recovered gst_rate) for the current " +
        "world/period. Call with the break_key.",
      inputSchema: z.object({ break_key: z.string() }),
      execute: async ({ break_key }) =>
        JSON.stringify(
          await runCli(["context", "break", break_key, "--world", world, "--period", period]),
        ),
    }),
  };
}

// ── Structured-output schemas: the brain's judgment, BEFORE the shared split ─────
// These mirror brain.ts's Judgment unions but flattened for the model. The shaper
// downstream recomputes params, so the model never returns arithmetic — only the
// decision (which entry type, which rate, or decline/escalate).
// NOTE: OpenAI's strict structured-output mode requires EVERY property to appear
// in the schema's `required` array — optional keys are rejected. So fields the
// model may omit are modeled as `.nullable()` (always present, possibly null)
// rather than `.optional()`. parseRate / the guards below treat null as "absent".
const classifySchema = z.object({
  unclassifiable: z.boolean(),
  reason: z.string(),
  entry_type: z.string().nullable(),
  gst_rate: z.string().nullable(),
});

const investigateSchema = z.object({
  escalate: z.boolean(),
  reason: z.string(),
  postings: z
    .array(z.object({ event_id: z.string(), gst_rate: z.string().nullable() }))
    .nullable(),
});

// makeAiBrain builds the LLM brain over the generated SKILL.md (the agent's system
// instructions). One brain instance serves every request; the per-(world,period)
// tool binding is created per call because the read-only tools close over them.
export function makeAiBrain(skillMarkdown: string): Brain {
  return {
    name: "ai-sdk",

    async classify(event: ClassifyEvent, context: EventContext, world: string, period: string): Promise<ClassifyJudgment> {
      const { experimental_output: out } = await generateText({
        model: openai(OPENAI_MODEL),
        system: skillMarkdown,
        tools: contextTools(world, period),
        stopWhen: stepCountIs(MAX_STEPS),
        experimental_output: Output.object({ schema: classifySchema }),
        prompt:
          `Classify this event per the SKILL CLASSIFY rule. The recovery engine has already prepared ` +
          `the primary context bundle. Use getEntity only if a deeper read-only lookup is needed.\n` +
          `event = ${JSON.stringify(event)}\ncontext = ${JSON.stringify(context)}\n` +
          `PAYMENT: set unclassifiable=false, entry_type="dtc_sale", gst_rate = the recovered ` +
          `integer percent (e.g. "18").\n` +
          `PARTIAL REFUND (the context's event.parent_amount is set): follow SKILL rule 3 — ` +
          `an annotated reason (e.g. "goodwill") or no/ambiguous line-item match means ` +
          `unclassifiable=true with the reason; EXACTLY ONE item/pair match means ` +
          `unclassifiable=false, entry_type="refund_reversal", gst_rate = the matched item's rate.\n` +
          `Anything else: unclassifiable=true with a reason. Never guess.`,
      });

      if (out.unclassifiable) {
        return { kind: "unclassifiable", reason: out.reason || "agent declined to classify" };
      }
      const rate = parseRate(out.gst_rate);
      if (rate === null) {
        return {
          kind: "unclassifiable",
          reason: `ai brain could not recover a usable gst_rate for ${event.event_id}`,
        };
      }
      const entryType = out.entry_type || "dtc_sale";
      if (entryType !== "dtc_sale" && entryType !== "refund_reversal") {
        // The model may only recommend the two reviewable types; anything else
        // (e.g. price_adjustment, a human-only policy call) becomes an escalation.
        return {
          kind: "unclassifiable",
          reason: `ai brain proposed ${entryType}, which agent policy does not recommend — escalating`,
        };
      }
      return {
        kind: "classify",
        entry_type: entryType,
        // gross is the event's own authoritative amount — the model never invents it.
        gross: event.amount,
        rate,
        rationale:
          out.reason ||
          `recovered gst_rate=${rate}% and booked the dtc_sale at the recovered rate`,
      };
    },

    async investigate(
      brk: InvestigateBreak,
      context: BreakContext,
      world: string,
      period: string,
    ): Promise<InvestigateJudgment> {
      const { experimental_output: out } = await generateText({
        model: openai(OPENAI_MODEL),
        system: skillMarkdown,
        tools: contextTools(world, period),
        stopWhen: stepCountIs(MAX_STEPS),
        experimental_output: Output.object({ schema: investigateSchema }),
        prompt:
          `Investigate this break per the SKILL INVESTIGATE rule. The recovery engine has already ` +
          `prepared the primary context bundle. Use getEntity only for deeper read-only exploration.\n` +
          `break = ${JSON.stringify(brk)}\ncontext = ${JSON.stringify(context)}\n` +
          `For each batch member with booked=false and type="refund", recover its gst_rate and add a ` +
          `posting keyed by the refund's event_id with that integer gst_rate. If none are found, set ` +
          `escalate=true with a reason; otherwise set escalate=false and fill the postings array.`,
      });

      if (out.escalate || !Array.isArray(out.postings) || out.postings.length === 0) {
        return {
          kind: "escalate",
          reason: out.reason || `ai brain found no unbooked refund to reverse for ${brk.key}`,
        };
      }

      // The model returns refund ids + recovered rates; re-fetch the batch to bind
      // each refund's gross AUTHORITATIVELY (the agent never invents amounts) and to
      // discard any hallucinated id that is not a real unbooked refund.
      const byId = new Map(context.batch.map((m) => [m.event_id, m]));
      const postings: InvestigatePostingJudgment[] = [];
      for (const p of out.postings) {
        const member = byId.get(p.event_id);
        if (!member) continue;
        // HARD GUARD (mirrors SKILL rule 3): a PARTIAL refund's intent is a
        // classify-side judgment — never reversed here, even if the model proposed it.
        if (member.parent_amount !== undefined) continue;
        const rate = parseRate(p.gst_rate) ?? parseRate(member.recovered?.gst_rate);
        if (rate === null) continue;
        postings.push({
          event_id: p.event_id,
          entry_type: "refund_reversal",
          gross: member.amount,
          rate,
        });
      }
      if (postings.length === 0) {
        return {
          kind: "escalate",
          reason: `ai brain proposed postings but none resolved to a batch refund with a recoverable rate for ${brk.key}`,
        };
      }
      return {
        kind: "resolve",
        postings,
        rationale: out.reason || "booked refund_reversal(s) so the settlement receivable clears",
      };
    },
  };
}
