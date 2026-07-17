// main.ts — process entry point. On startup it (1) regenerates agent/SKILL.md from
// the playbook so the Skill never drifts from config, (2) selects the brain, and
// (3) starts the §8 HTTP server. The service is meant to run with cwd = repo root
// so the CLI-as-tool can default --root to cwd.
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { generateSkill } from "./skill.ts";
import { stubBrain, type Brain } from "./brain.ts";
import { makeAiBrain } from "./brain_ai.ts";
import { createAgentServer } from "./server.ts";

const PORT = Number(process.env.PORT ?? 8787);

// The OPENAI_API_KEY lives in the ledger-flow repo's PARENT .env, outside this repo.
// Load it on startup (Node 22 process.loadEnvFile) BEFORE brain selection so the key
// is visible. Tolerate its absence — without a key we simply fall back to the stub.
const PARENT_ENV = "/Users/rishav/projects/razorpay/.env";
function loadParentEnv(): void {
  try {
    process.loadEnvFile(PARENT_ENV);
  } catch {
    // No parent .env (or unreadable) — fine; selectBrain falls back to the stub.
  }
}

// Brain selection: explicit LEDGER_FLOW_BRAIN wins; otherwise prefer the live LLM
// brain ("flue"/"ai" both map to it) when an OPENAI_API_KEY is present, else the
// deterministic stub. The brain is the §8 swap point (SPEC §14) — the framework
// behind it (Vercel AI SDK over OpenAI) lives in brain_ai.ts.
function selectBrain(skill: string): Brain {
  const explicit = process.env.LEDGER_FLOW_BRAIN;
  const choice = explicit ?? (process.env.OPENAI_API_KEY ? "flue" : "stub");
  if (choice === "flue" || choice === "ai") return makeAiBrain(skill);
  if (choice === "stub") return stubBrain;
  throw new Error(`unknown LEDGER_FLOW_BRAIN=${choice} (expected "stub", "ai", or "flue")`);
}

function main(): void {
  loadParentEnv();
  const skill = generateSkill();
  const skillPath = resolve(import.meta.dirname, "..", "SKILL.md");
  writeFileSync(skillPath, skill, "utf8");

  const brain = selectBrain(skill);
  const server = createAgentServer(brain);
  server.listen(PORT, () => {
    console.log(
      `ledger-flow-flue: §8 agent listening on http://localhost:${PORT} ` +
        `(brain=${brain.name}, SKILL.md written to ${skillPath})`,
    );
  });
}

main();
