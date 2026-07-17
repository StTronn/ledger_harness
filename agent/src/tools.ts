// tools.ts — the agent's optional read-only exploration surface (SPEC §8,
// "Option 2: CLI-as-tool"). Primary event/break context is prepared by the Go
// recovery engine and sent in the request. These tools remain available for
// deeper lookups when a novel case needs more evidence.
//
// Both the stub brain and the Flue brain consume these exact helpers: the stub
// calls them directly; the Flue brain wraps them as `execute` closures on its
// tool definitions. The shape returned here is therefore the single source of
// truth for "what the agent can see."
import { spawn } from "node:child_process";
import { resolve } from "node:path";

// Resolve the CLI binary once. LEDGER_FLOW_BIN overrides; otherwise default to
// ./bin/ledger-flow relative to the agent/ package (the README build target),
// resolved against this module's location so it works regardless of cwd.
const BIN =
  process.env.LEDGER_FLOW_BIN ?? resolve(import.meta.dirname, "..", "bin", "ledger-flow");

// runCli spawns the CLI with args, captures stdout, and JSON-parses it. The CLI
// defaults --root to cwd, and the service runs with cwd = repo root, so callers
// omit --root. A non-zero exit or unparseable stdout is an error the endpoint
// surfaces as a 5xx (an infrastructure fault, distinct from an agent escalation).
export function runCli(args: string[]): Promise<any> {
  return new Promise((resolveP, rejectP) => {
    const child = spawn(BIN, args, { stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (d) => (stdout += d));
    child.stderr.on("data", (d) => (stderr += d));
    child.on("error", (err) =>
      rejectP(new Error(`runCli: spawn ${BIN} failed: ${err.message}`)),
    );
    child.on("close", (code) => {
      if (code !== 0) {
        rejectP(new Error(`runCli: ${BIN} ${args.join(" ")} exited ${code}: ${stderr.trim()}`));
        return;
      }
      try {
        resolveP(JSON.parse(stdout));
      } catch (err) {
        rejectP(new Error(`runCli: could not parse stdout as JSON: ${(err as Error).message}`));
      }
    });
  });
}

// ---- Typed views of the CLI's context bundles (the fields the §8 brains read) ----

export interface RecoveredSource {
  object: string;
  path: string;
}
export interface Recovered {
  gst_rate: string;
  _source?: RecoveredSource;
}

// RefundCandidate is one precomputed partial-refund explanation from the bundle:
// an exact line-item (or pair) match implying refund_reversal at the matched
// item's rate, or an explicit no-match.
export interface RefundCandidate {
  kind: "item-match" | "pair-match" | "no-match";
  entry_type?: string;
  gst_rate?: string;
  items?: number[];
  _source?: RecoveredSource;
  note?: string;
}

export interface EventContext {
  event: {
    event_id: string;
    type: string;
    amount: number;
    order_id?: string;
    gst_rate?: string;
    sku?: string;
    reason?: string; // ops annotation (e.g. "goodwill") on a manual partial refund
    parent_amount?: number; // set ONLY on a partial refund: the parent payment's gross
    booked: boolean;
  };
  recovered?: Recovered;
  order_items?: { sku: string; amount: number; gst_rate: string }[];
  candidates?: RefundCandidate[];
  applicable_entry_types: string[];
}

export interface BatchMember {
  event_id: string;
  type: string;
  amount: number;
  booked: boolean;
  gst_rate?: string;
  parent_amount?: number; // set ONLY on a partial refund (do NOT reverse it; escalate)
  recovered?: Recovered;
}

export interface BreakContext {
  break: Record<string, unknown>;
  settlement?: Record<string, unknown>;
  batch: BatchMember[];
  candidates: string[];
  applicable_entry_types: string[];
  accounts: Record<string, number>;
}

// getEntity fetches ANY snapshotted object by id (tier-2 self-directed lookup):
// an event's raw object + booked + edges, an order with line items, a rate-card
// channel (ratecard/<channel>), or an account path's balance.
export async function getEntity(
  id: string,
  world: string,
  period: string,
): Promise<unknown> {
  return runCli(["context", "entity", id, "--world", world, "--period", period]);
}

// getEventContext fetches one event's recovery context for classify.
export async function getEventContext(
  eventId: string,
  world: string,
  period: string,
): Promise<EventContext> {
  return (await runCli([
    "context",
    "event",
    eventId,
    "--world",
    world,
    "--period",
    period,
  ])) as EventContext;
}

// getBreakContext fetches one break's settlement batch + recovery context for
// investigate (the batch carries each member's booked flag and recovered rate).
export async function getBreakContext(
  breakKey: string,
  world: string,
  period: string,
): Promise<BreakContext> {
  return (await runCli([
    "context",
    "break",
    breakKey,
    "--world",
    world,
    "--period",
    period,
  ])) as BreakContext;
}
