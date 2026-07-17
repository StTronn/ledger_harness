// server.ts — the §8 HTTP transport. A bare Node http server (no framework) that
// the Go orchestrator's live mode POSTs to. It routes POST /agents/classify and
// POST /agents/investigate to the brain-backed handlers and returns 200 + JSON on
// success. Any failure is a non-200 + JSON {error} — the Go client treats every
// non-200 as an infrastructure error (distinct from a normal {unclassifiable} /
// {escalate} agent decision, which is a 200).
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import type { Brain } from "./brain.ts";
import { handleClassify, type ClassifyRequest } from "./classify.ts";
import { handleInvestigate, type InvestigateRequest } from "./investigate.ts";

// readJsonBody buffers and parses the request body, bounded so a runaway client
// cannot exhaust memory.
const MAX_BODY = 1 << 20; // 1 MiB is ample for a single event/break
function readJsonBody(req: IncomingMessage): Promise<any> {
  return new Promise((resolve, reject) => {
    let raw = "";
    req.on("data", (chunk) => {
      raw += chunk;
      if (raw.length > MAX_BODY) {
        reject(new Error("request body too large"));
        req.destroy();
      }
    });
    req.on("end", () => {
      try {
        resolve(raw.length === 0 ? {} : JSON.parse(raw));
      } catch {
        reject(new Error("invalid JSON request body"));
      }
    });
    req.on("error", reject);
  });
}

function sendJson(res: ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    "content-type": "application/json",
    "content-length": Buffer.byteLength(payload),
  });
  res.end(payload);
}

export function createAgentServer(brain: Brain) {
  return createServer((req, res) => {
    void handle(req, res, brain);
  });
}

async function handle(req: IncomingMessage, res: ServerResponse, brain: Brain): Promise<void> {
  const url = req.url ?? "";

  if (req.method !== "POST") {
    sendJson(res, 405, { error: `method ${req.method} not allowed; use POST` });
    return;
  }

  try {
    if (url === "/agents/classify") {
      const body = (await readJsonBody(req)) as ClassifyRequest;
      if (!body?.event?.event_id) {
        sendJson(res, 400, { error: "classify: missing event.event_id" });
        return;
      }
      if (!body?.context) {
        sendJson(res, 400, { error: "classify: missing recovery context" });
        return;
      }
      const out = await handleClassify(brain, body);
      sendJson(res, 200, out);
      return;
    }

    if (url === "/agents/investigate") {
      const body = (await readJsonBody(req)) as InvestigateRequest;
      if (!body?.break?.key) {
        sendJson(res, 400, { error: "investigate: missing break.key" });
        return;
      }
      if (!body?.context) {
        sendJson(res, 400, { error: "investigate: missing recovery context" });
        return;
      }
      const out = await handleInvestigate(brain, body);
      sendJson(res, 200, out);
      return;
    }

    sendJson(res, 404, { error: `no route for ${url}` });
  } catch (err) {
    // A thrown error is an infrastructure fault (e.g. the CLI tool failed) — a 5xx,
    // NOT an agent escalation. Surfacing {error} lets the Go client log the cause.
    sendJson(res, 500, { error: (err as Error).message });
  }
}
