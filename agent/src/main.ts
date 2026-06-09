// main.ts is the flue-agent CLI entrypoint. It dispatches the agent stages of the
// async pipeline:
//
//   flue-agent classify    --world <w> --period <p> [--root <dir>] [--live]
//   flue-agent investigate --world <w> --period <p> [--root <dir>] [--live]   (added next)
//
// Default brain is DETERMINISTIC (no LLM, no key); --live uses the Flue/LLM brain
// (needs ANTHROPIC_API_KEY + `pnpm add flue`). It reads the keyed stores under
// runs/<world>-<period>/ and the snapshot under worlds/<world>/<period>/.

import { runClassify } from "./agents/classify.ts";
import { runInvestigate } from "./agents/investigate.ts";

interface Flags {
  world: string;
  period: string;
  root: string;
  live: boolean;
}

function parseFlags(argv: string[]): Flags {
  const f: Flags = { world: "", period: "", root: process.cwd(), live: false };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--world") f.world = argv[++i] ?? "";
    else if (a === "--period") f.period = argv[++i] ?? "";
    else if (a === "--root") f.root = argv[++i] ?? "";
    else if (a === "--live") f.live = true;
  }
  if (!f.world || !f.period) {
    throw new Error("flue-agent: --world and --period are required");
  }
  return f;
}

async function main(): Promise<void> {
  const [cmd, ...rest] = process.argv.slice(2);
  if (cmd === "classify") {
    const f = parseFlags(rest);
    const out = await runClassify(f);
    process.stdout.write(
      `classified ${out.proposed} proposed, ${out.escalated} escalated (brain: ${out.brain}) for world "${f.world}" period "${f.period}" -> ${out.path}\n`,
    );
    return;
  }
  if (cmd === "investigate") {
    const f = parseFlags(rest);
    const out = await runInvestigate(f);
    process.stdout.write(
      `investigated ${out.resolved} resolved, ${out.escalated} escalated (brain: ${out.brain}) for world "${f.world}" period "${f.period}" -> ${out.path}\n`,
    );
    return;
  }
  process.stderr.write("usage: flue-agent <classify|investigate> --world <w> --period <p> [--root <dir>] [--live]\n");
  process.exit(2);
}

main().catch((err: unknown) => {
  process.stderr.write(`flue-agent: ${err instanceof Error ? err.message : String(err)}\n`);
  process.exit(1);
});
