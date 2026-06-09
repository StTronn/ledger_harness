// io.ts resolves the on-disk paths (matching the Go layout) and reads/writes the
// keyed stores in the project's canonical stable JSON (2-space indent, sorted by
// key, trailing newline) so a run is byte-stable and the Go side can parse it.

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import type { ProposalsFile, ResultsFile, Result } from "./types.ts";

export function runDir(root: string, world: string, period: string): string {
  return join(root, "runs", `${world}-${period}`);
}
export function razorpayDir(root: string, world: string, period: string): string {
  return join(root, "worlds", world, period, "razorpay");
}
export function proposalsPath(root: string, world: string, period: string): string {
  return join(runDir(root, world, period), "proposals.json");
}
export function resultsPath(root: string, world: string, period: string): string {
  return join(runDir(root, world, period), "results.json");
}

// readJSON parses a JSON file, with a clear error if it is missing.
export function readJSON<T>(path: string): T {
  let data: string;
  try {
    data = readFileSync(path, "utf8");
  } catch {
    throw new Error(`flue-agent: ${path} not found (was the stage run?)`);
  }
  return JSON.parse(data) as T;
}

export function readProposals(root: string, world: string, period: string): ProposalsFile {
  const f = readJSON<ProposalsFile>(proposalsPath(root, world, period));
  if (f.schema_version !== 1) {
    throw new Error(`flue-agent: proposals schema_version=${f.schema_version}, want 1`);
  }
  return f;
}

// writeResults writes the results store as stable JSON (results sorted by event_id,
// 2-space indent, trailing newline) so the Go apply stage parses it and reruns are
// byte-stable.
export function writeResults(root: string, world: string, period: string, file: ResultsFile): string {
  file.results.sort((a: Result, b: Result) => (a.event_id < b.event_id ? -1 : a.event_id > b.event_id ? 1 : 0));
  const path = resultsPath(root, world, period);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify(file, null, 2) + "\n", "utf8");
  return path;
}
