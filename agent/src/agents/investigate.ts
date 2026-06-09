// investigate.ts is the async investigate WORK stage as an agent CLI: read the
// parked break queue (breaks.json, emitted by a close run that ended with breaks),
// resolve each break through the brain, and write resolutions.json for the Go
// `investigate apply` stage to validate + book.

import { readBreaks, writeResolutions } from "../harness/io.ts";
import { loadTools } from "../harness/tools.ts";
import { selectInvestigateBrain } from "../harness/brain_investigate.ts";
import { SchemaVersion } from "../harness/types.ts";
import type { Resolution, ResolutionsFile } from "../harness/types.ts";

export interface InvestigateOpts {
  root: string;
  world: string;
  period: string;
  live: boolean;
}

export async function runInvestigate(opts: InvestigateOpts): Promise<{ path: string; resolved: number; escalated: number; brain: string }> {
  const { root, world, period, live } = opts;
  const breaks = readBreaks(root, world, period);
  const tools = loadTools(root, world, period);
  const brain = await selectInvestigateBrain(live);

  const resolutions: Resolution[] = [];
  let resolved = 0;
  let escalated = 0;
  for (const work of breaks.breaks) {
    const r = await brain.investigate(work, tools);
    resolutions.push(r);
    if (r.status === "resolved") resolved++;
    else escalated++;
  }

  const file: ResolutionsFile = { schema_version: SchemaVersion, world, period, resolutions };
  const path = writeResolutions(root, world, period, file);
  return { path, resolved, escalated, brain: brain.name };
}
