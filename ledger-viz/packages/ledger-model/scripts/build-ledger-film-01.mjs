/**
 * Assemble the 2026-01 MATRIX fixture (LedgerFilm) — the partial-refunds world's
 * REPLAY books.
 *
 * The produced ledger of the replay run equals truth MINUS the two escalated
 * partial refunds (errors.json: wrong=0, extra=0, missing=2), so the steps are
 * derived from worlds/dtc/2026-01/truth/gl.json with the missing event_ids
 * dropped — byte-accurate to what the close actually posted. The one entry the
 * AGENT booked (the line-item return) is marked kind="agent" so the matrix can
 * tell it apart from rule-booked entries. The chart includes the (deliberately
 * untouched) income/discounts-allowances column: goodwill was escalated, never
 * booked — an empty column that says so.
 *
 *   node scripts/build-ledger-film-01.mjs
 */
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const REPO = join(here, "..", "..", "..", "..");
const OUT = join(here, "..", "src", "fixtures");

const truth = JSON.parse(
  readFileSync(join(REPO, "worlds", "dtc", "2026-01", "truth", "gl.json"), "utf8"),
);
const errors = JSON.parse(readFileSync(join(here, "artifacts", "01-errors.json"), "utf8"));
const playbook = JSON.parse(readFileSync(join(REPO, "config", "playbook.json"), "utf8"));
const classifyTrace = JSON.parse(
  readFileSync(join(here, "artifacts", "01-trace.json"), "utf8"),
);

// Sanity: the produced-equals-truth-minus-missing derivation only holds when the
// run had no wrong/extra entries.
if (errors.totals.wrong !== 0 || errors.totals.extra !== 0) {
  throw new Error("2026-01 replay has wrong/extra entries; cannot derive the film from truth");
}
const missing = new Set(errors.errors.map((e) => e.event_id));

// The agent-booked event(s): classify decisions that carried an entry type.
const agentBooked = new Set(
  classifyTrace.filter((t) => t.decision.entry_type).map((t) => t.event_id),
);

const accounts = playbook.accounts.map((a) => {
  const [group, ...rest] = a.path.split("/");
  const leaf = rest.join("/");
  return {
    path: a.path,
    group,
    leaf,
    label: leaf.replace(/-/g, " "),
    note: a.note,
  };
});

let i = 0;
const steps = truth.entries
  .filter((e) => !missing.has(e.event_id))
  .map((e) => {
    const agent = agentBooked.has(e.event_id);
    const step = {
      id: `s${String(++i).padStart(3, "0")}`,
      index: i - 1,
      label: e.event_id,
      sublabel: agent ? `${e.entry_type} · agent` : e.entry_type,
      kind: agent ? "agent" : "event",
      eventId: e.event_id,
      entryType: e.entry_type,
      postings: e.lines.map((l) => ({
        account: l.account,
        side: l.side,
        amount: l.amount,
      })),
    };
    return step;
  });

const film = {
  meta: {
    world: "dtc",
    period: "2026-01",
    title: "Partial refunds (replay)",
    currency: "INR",
    minorPerMajor: 100,
    symbol: "₹",
  },
  accounts,
  steps,
};

// Sanity: every step balances and the count matches the replay run (46 posted).
for (const s of steps) {
  const net = s.postings.reduce((sum, p) => sum + (p.side === "Dr" ? p.amount : -p.amount), 0);
  if (net !== 0) throw new Error(`step ${s.id} (${s.label}) does not balance: ${net}`);
}
if (steps.length !== errors.totals.correct) {
  throw new Error(`steps=${steps.length} != correct=${errors.totals.correct}`);
}

writeFileSync(join(OUT, "dtc-2026-01.film.json"), JSON.stringify(film, null, 2) + "\n");
console.log(
  `wrote dtc-2026-01.film.json (${steps.length} steps, ${accounts.length} accounts, ` +
    `${steps.filter((s) => s.kind === "agent").length} agent-booked)`,
);
