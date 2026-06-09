// skill.ts GENERATES the agent's instructions (SKILL.md text) from
// config/playbook.json — the SAME source the Go rule engine and ledger use. The
// agent therefore can never drift from the playbook: add an entry type to the JSON
// and the agent's instructions update automatically. This is the SPEC §8 rule
// "generate the skill text from the schema file so they can't drift."

import { join } from "node:path";
import { readJSON } from "./io.ts";

interface Line {
  side: string;
  account: string;
  amount: string;
}
interface EntryType {
  name: string;
  doc: string;
  params: string[];
  tx_param: string;
  lines: Line[];
}
interface Playbook {
  accounts: { path: string; note: string }[];
  entry_types: EntryType[];
}

// generateSkill builds the instruction text from the playbook at root/config.
export function generateSkill(root: string): string {
  const pb = readJSON<Playbook>(join(root, "config", "playbook.json"));
  const lines: string[] = [];
  lines.push("# close-agent playbook (generated from config/playbook.json)");
  lines.push("");
  lines.push(
    "You map one event (or reconcile break) to a playbook ENTRY TYPE and the paise",
    "params it needs. You return ONLY {entry_type, recovered facts + citations} —",
    "never raw debits/credits, never money you invented. Recover any missing fact",
    "from a read-only tool and CITE where it came from (object id + field path).",
    "If you cannot recover it, escalate honestly — never guess.",
    "",
    "## Entry types",
    "",
  );
  for (const e of pb.entry_types) {
    lines.push(`### ${e.name}`);
    lines.push(e.doc);
    lines.push(`- params: ${e.params.join(", ")}`);
    lines.push(`- lines:`);
    for (const l of e.lines) lines.push(`    ${l.side} ${l.account}  {${l.amount}}`);
    lines.push("");
  }
  lines.push("## Chart of accounts");
  for (const a of pb.accounts) lines.push(`- ${a.path} — ${a.note}`);
  lines.push("");
  return lines.join("\n");
}
