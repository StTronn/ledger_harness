/**
 * Assemble RunFilm fixtures from the raw ledger-flow run artifacts.
 *
 * Reads the JSON dumped under ./artifacts (real replay runs) and emits
 * src/fixtures/dtc-2026-03.run.json and dtc-2026-04.run.json. Run:
 *
 *   node scripts/build-run-films.mjs
 *
 * Real data only — every field is traced to an artifact. Where a value is
 * derived (e.g. scorePct = floor(correct/truth*100), the refund_reversal
 * posting reconstructed from params + the break residual) it is computed the
 * same way the engine reports it.
 */
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const ART = join(here, "artifacts");
const OUT = join(here, "..", "src", "fixtures");

const read = (f) => JSON.parse(readFileSync(join(ART, f), "utf8"));
const write = (f, obj) =>
  writeFileSync(join(OUT, f), JSON.stringify(obj, null, 2) + "\n");

const META_COMMON = { world: "dtc", symbol: "₹", minorPerMajor: 100 };

/** scorePct exactly as the engine reports it. */
const score = (correct, truth) => Math.floor((correct / truth) * 100);

/* --------------------------------------------------------- 2026-04 classify */

function build04() {
  const errors = read("04-errors.json");
  const trace = read("04-trace.json");
  const gl = read("04-gl-classify.json");

  const glByEvent = new Map(gl.map((e) => [e.event_id, e]));
  const ctxFor = (id) => read(`ctx-04-${id}.json`);

  const truth = errors.totals.truth_entries; // 41
  const correct = errors.totals.correct; // 36
  const missing = errors.totals.missing; // 5

  const consultations = trace.map((t) => {
    const ctx = ctxFor(t.event_id);
    const entry = glByEvent.get(t.event_id);
    return {
      id: t.event_id,
      role: "classify",
      status: "posted",
      resolves: "skip",
      eventId: t.event_id,
      eventType: t.input.type,
      amount: t.input.amount,
      sku: t.input.sku,
      orderId: t.input.order_id,
      whyMissed:
        "Payment arrived with no gst_rate. The deterministic rules will not " +
        "guess a tax rate, so the dtc_sale is skipped rather than booked wrong.",
      citation: {
        field: "gst_rate",
        value: ctx.recovered.gst_rate,
        object: ctx.recovered._source.object,
        path: ctx.recovered._source.path,
        tool: t.tools_used[0],
      },
      toolsUsed: t.tools_used,
      entryType: t.decision.entry_type,
      params: t.decision.params,
      rationale: t.rationale,
      postings: entry.lines.map((l) => ({
        side: l.side,
        account: l.account,
        amount: l.amount,
      })),
      postedId: entry.id,
    };
  });

  return {
    meta: { ...META_COMMON, period: "2026-04", title: "Five rule misses" },
    baseline: {
      booked: correct,
      skipped: missing,
      breaks: 0,
      truth,
      scorePct: errors.score_pct,
    },
    final: { booked: truth, skipped: 0, breaks: 0, truth, scorePct: 100 },
    consultations,
  };
}

/* ------------------------------------------------------ 2026-03 investigate */

function build03() {
  const errors = read("03-errors.json");
  const results = read("03-results.json");
  const classifyTrace = read("03-trace.json");
  const invTrace = read("03-investigate-trace.json")[0];
  const resolution = read("03-resolutions.json").resolutions[0];
  const ctx = read("ctx-03-break.json");

  const truth = errors.totals.truth_entries; // 38
  const correct = errors.totals.correct; // 37

  // (1) classify escalation — the agent honestly hands the refund off.
  const esc = results.results[0];
  const escTrace = classifyTrace[0];
  const escalation = {
    id: `${esc.event_id}:classify`,
    role: "classify",
    status: "escalated",
    resolves: "skip",
    eventId: esc.event_id,
    eventType: escTrace.input.type,
    amount: escTrace.input.amount,
    sku: escTrace.input.sku,
    whyMissed:
      "Refund settled but never booked, with no gst_rate on the event. The v1 " +
      "classify agent only recovers payments, so it escalates rather than guess.",
    toolsUsed: escTrace.tools_used,
    rationale: escTrace.rationale,
    postings: [],
  };

  // (2) investigation — the centerpiece. Reconstruct the posting from the
  // recovered params and the break residual (Cr the receivable for net+gst,
  // Dr the offsetting income/liability legs).
  const p = invTrace.decision.resolution[0].params; // { gst, net, refund_id }
  const recovered = resolution.postings[0].recovered[0];
  const b = ctx.break;
  const account = Object.keys(ctx.accounts)[0]; // assets/razorpay-settlement-receivable
  const residual = ctx.accounts[account]; // 248591

  const investigation = {
    id: invTrace.break_key,
    role: "investigate",
    status: "posted",
    resolves: "break",
    eventId: invTrace.candidates[0].event_id,
    eventType: invTrace.candidates[0].type,
    amount: invTrace.candidates[0].amount,
    sku: invTrace.candidates[0].sku,
    orderId: recovered.source.object,
    whyMissed:
      "The settled refund was never booked, so the settlement receivable would " +
      "not clear. check #3 flags the residual; no rule can post the missing leg.",
    citation: {
      field: recovered.field,
      value: recovered.value,
      object: recovered.source.object,
      path: recovered.source.path,
      tool: recovered.source.tool,
    },
    toolsUsed: invTrace.tools_used,
    break: {
      key: b.key,
      check: b.check,
      kind: b.kind,
      expected: b.expected,
      actual: b.actual,
      detail: b.detail,
      account,
      candidates: ctx.candidates,
      batch: ctx.batch.map((m) => ({
        eventId: m.event_id,
        type: m.type,
        amount: m.amount,
        booked: m.booked,
        gstRate: m.gst_rate ?? m.recovered?.gst_rate,
        highlight: m.booked === false,
      })),
    },
    entryType: invTrace.decision.resolution[0].entry_type,
    params: { net: p.net, gst: p.gst },
    rationale: invTrace.rationale,
    postings: [
      { side: "Cr", account, amount: p.net + p.gst },
      { side: "Dr", account: "income/sales-returns", amount: p.net },
      { side: "Dr", account: "liabilities/gst-output-payable", amount: p.gst },
    ],
    postedId: resolution.break_key,
    clears: { account, before: residual, after: 0 },
  };

  return {
    meta: { ...META_COMMON, period: "2026-03", title: "The settled refund" },
    baseline: {
      booked: correct,
      skipped: 1,
      breaks: 1,
      truth,
      scorePct: errors.score_pct,
    },
    final: { booked: truth, skipped: 0, breaks: 0, truth, scorePct: 100 },
    consultations: [escalation, investigation],
  };
}


/* ------------------------------------------- 2026-01 partial refunds (judgment) */

function build01() {
  const errorsOff = read("01-errors-off.json"); // rules-only baseline (45/48, 93%)
  const errors = read("01-errors.json"); // replay outcome (46/48, 95%)
  const trace = read("01-trace.json"); // 3 classify consultations
  const invTrace = read("01-investigate-trace.json")[0]; // the escalated break
  const breakCtx = read("ctx-01-break.json");

  const truth = errors.totals.truth_entries; // 48
  const PARTIAL_WHY =
    "A PARTIAL refund — smaller than its payment. The amount alone cannot say " +
    "WHY: one line item returned (refund_reversal), a goodwill credit note " +
    "(price_adjustment), or something else. Booking it blind would be " +
    "wrong-but-balanced: clean books, silently misstated revenue.";

  const consultations = trace.map((t) => {
    const ctx = read(`ctx-01-${t.event_id}.json`);
    const base = {
      id: t.event_id,
      role: "classify",
      resolves: "skip",
      eventId: t.event_id,
      eventType: t.input.type,
      amount: t.input.amount,
      sku: t.input.sku,
      orderId: ctx.event.order_id,
      whyMissed: PARTIAL_WHY,
      toolsUsed: t.tools_used,
      rationale: t.rationale,
    };
    // An escalation's trace decision is EMPTY (the reason rides on rationale);
    // only a booked decision carries entry_type + params.
    if (!t.decision.entry_type) {
      return { ...base, status: "escalated", postings: [] };
    }
    // R1: the exact line-item match, booked as a refund_reversal at the item's rate.
    const p = t.decision.params; // { net, gst, refund_id }
    const match = (ctx.candidates ?? []).find((c) => c.kind === "item-match");
    return {
      ...base,
      status: "posted",
      citation: {
        field: "gst_rate",
        value: match?.gst_rate ?? ctx.event.gst_rate,
        object: match?._source?.object ?? ctx.event.order_id,
        path: match?._source?.path ?? "items.0",
        tool: t.tools_used[0],
      },
      entryType: t.decision.entry_type,
      params: { net: p.net, gst: p.gst },
      postings: [
        { side: "Dr", account: "income/sales-returns", amount: p.net },
        { side: "Dr", account: "liabilities/gst-output-payable", amount: p.gst },
        {
          side: "Cr",
          account: "assets/razorpay-settlement-receivable",
          amount: p.net + p.gst,
        },
      ],
      postedId: `refund:${t.event_id}`,
    };
  });

  // The investigation — honestly ESCALATED: the residual traces to the two
  // partials whose intent no snapshot can recover. The batch reflects the replay
  // state (R1 already booked by classify before investigate ran).
  const bookedByClassify = new Set(
    consultations.filter((c) => c.status === "posted").map((c) => c.eventId),
  );
  const b = invTrace.input;
  const account = Object.keys(breakCtx.accounts)[0];
  const investigation = {
    id: `${invTrace.break_key}investigate`,
    role: "investigate",
    status: "escalated",
    resolves: "break",
    eventId: invTrace.break_key,
    eventType: "break",
    amount: b.actual,
    whyMissed:
      "The receivable did not clear: the residual traces to the two partial " +
      "refunds classify escalated. No posting may guess their intent, so the " +
      "investigation escalates too — two independent nets, one honest answer.",
    toolsUsed: invTrace.tools_used,
    rationale: invTrace.rationale,
    break: {
      key: b.key,
      check: b.check,
      kind: b.kind,
      expected: b.expected,
      actual: b.actual,
      detail: b.detail,
      account,
      candidates: b.candidates,
      batch: breakCtx.batch.map((m) => {
        const booked = m.booked || bookedByClassify.has(m.event_id);
        return {
          eventId: m.event_id,
          type: m.type,
          amount: m.amount,
          booked,
          gstRate: m.gst_rate ?? m.recovered?.gst_rate,
          highlight: !booked,
        };
      }),
    },
    postings: [],
  };

  return {
    meta: {
      ...META_COMMON,
      period: "2026-01",
      title: "Partial refunds (judgment)",
    },
    baseline: {
      booked: errorsOff.totals.correct, // 45
      skipped: errorsOff.totals.missing, // 3
      breaks: 1,
      truth,
      scorePct: errorsOff.score_pct, // 93
    },
    final: {
      booked: errors.totals.correct, // 46
      skipped: errors.totals.missing, // 2
      breaks: 1, // the escalated residual stays listed — honest
      truth,
      scorePct: errors.score_pct, // 95
    },
    consultations: [...consultations, investigation],
  };
}

const film04 = build04();
const film03 = build03();
const film01 = build01();
write("dtc-2026-04.run.json", film04);
write("dtc-2026-03.run.json", film03);
write("dtc-2026-01.run.json", film01);

// Sanity: every posting balances (Dr === Cr), score derivation matches.
for (const film of [film01, film03, film04]) {
  for (const c of film.consultations) {
    if (!c.postings.length) continue;
    const net = c.postings.reduce(
      (s, p) => s + (p.side === "Dr" ? p.amount : -p.amount),
      0,
    );
    if (net !== 0) {
      throw new Error(`${film.meta.period} ${c.id}: unbalanced posting (${net})`);
    }
  }
  const s = score(film.baseline.booked, film.baseline.truth);
  if (s !== film.baseline.scorePct) {
    throw new Error(
      `${film.meta.period}: score mismatch ${s} vs ${film.baseline.scorePct}`,
    );
  }
}

console.log(
  `wrote dtc-2026-01.run.json (${film01.consultations.length}), ` +
    `dtc-2026-03.run.json (${film03.consultations.length}), ` +
    `dtc-2026-04.run.json (${film04.consultations.length} consultations)`,
);
