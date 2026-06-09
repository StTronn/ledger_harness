// classify.ts is the async WORK stage as an agent CLI: read the parked queue
// (proposals.json, emitted by `close --agent off`), run each work item through the
// brain (deterministic by default, Flue/LLM with --live), and write results.json
// for the Go `classify apply` stage to validate + book. One process handles the
// whole batch (amortizes any model startup).

import { readProposals, writeResults } from "../harness/io.ts";
import { loadTools } from "../harness/tools.ts";
import { generateSkill } from "../harness/skill.ts";
import { selectClassifyBrain } from "../harness/brain.ts";
import { SchemaVersion } from "../harness/types.ts";
import type { Result, ResultsFile } from "../harness/types.ts";

export interface ClassifyOpts {
  root: string;
  world: string;
  period: string;
  live: boolean;
}

// runClassify processes the proposals queue into the results store and returns the
// path written plus per-status counts (for the CLI summary).
export async function runClassify(opts: ClassifyOpts): Promise<{ path: string; proposed: number; escalated: number; brain: string }> {
  const { root, world, period, live } = opts;
  const proposals = readProposals(root, world, period);
  const tools = loadTools(root, world, period);
  const skill = generateSkill(root);
  const brain = await selectClassifyBrain(live);

  const results: Result[] = [];
  let proposed = 0;
  let escalated = 0;
  for (const item of proposals.items) {
    const r = await brain.classify(item.event, tools, skill);
    results.push(r);
    if (r.status === "proposed") proposed++;
    else escalated++;
  }

  const file: ResultsFile = { schema_version: SchemaVersion, world, period, results };
  const path = writeResults(root, world, period, file);
  return { path, proposed, escalated, brain: brain.name };
}
