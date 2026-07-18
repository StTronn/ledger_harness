/**
 * Assemble the 2026-02 RTO fixture (RtoFilm) — the cash-on-delivery story.
 *
 * Real data only: reads the harness break bundle (02-break.json, which already
 * carries each deduction's rate-card verdict from the rto-fee policy), the
 * recorded investigate resolution (02-resolutions.json, the rto_fee postings the
 * agent booked + the escalation), the courier feed (02-courier-feed.json), and
 * the agent-off errors (02-errors-off.json, the baseline score). Every number is
 * traced; the replay score is derived the way the engine reports it
 * (floor(correct/truth*100), where correct rises by the deductions booked).
 *
 *   node scripts/build-rto-film.mjs
 */
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const ART = join(here, "artifacts");
const OUT = join(here, "..", "src", "fixtures");
const read = (f) => JSON.parse(readFileSync(join(ART, f), "utf8"));

const bundle = read("02-break.json");
const recorded = read("02-resolutions.json").resolutions[0];
const courier = read("02-courier-feed.json");
const errorsOff = read("02-errors-off.json");

const rem = courier.remittances[0];
const deductionsTotal = rem.deductions.reduce((s, d) => s + d.amount, 0);

// The agent's booked postings, keyed by the deduction (event) id, reconstructed
// from the recorded resolution params + the rto_fee playbook template
// (Dr reverse-logistics {net}, Dr expense/gst-input {gst}, Cr cod-receivable {net+gst}).
const bookedByEvent = new Map();
for (const p of recorded.resolution ?? []) {
  if (p.entry_type !== "rto_fee") continue;
  const { net, gst } = p.params;
  bookedByEvent.set(p.event_id, [
    { side: "Dr", account: "expense/reverse-logistics", amount: net },
    { side: "Dr", account: "expense/gst-input", amount: gst },
    { side: "Cr", account: "assets/cod-receivable", amount: net + gst },
  ]);
}

const LABELS = {
  RTO_CHG: "Return-to-origin fee",
  WT_ADJ: "Weight-dispute adjustment",
};

const deductions = bundle.deductions.map((d) => {
  const backed = d.rate_card_backed === true;
  return {
    id: d.id,
    code: d.code,
    label: LABELS[d.code] ?? d.code,
    shipmentId: d.shipment_id,
    shipmentStatus: d.shipment_status ?? "",
    amount: d.amount,
    backed,
    verdict: backed ? "book" : "escalate",
    entryType: backed ? d.entry_type : undefined,
    gstRate: backed ? d.gst_rate : undefined,
    citation:
      backed && d._source?.object
        ? { object: d._source.object, path: d._source.path }
        : undefined,
    note: d.note ?? "",
    postings: bookedByEvent.get(d.id) ?? [],
  };
});

const bookedCount = deductions.filter((d) => d.backed).length;
const truth = errorsOff.totals.truth_entries;
const correctOff = errorsOff.totals.correct;
const correctFinal = correctOff + bookedCount; // each booked deduction clears one missing entry
const pct = (correct) => Math.floor((correct / (truth || 1)) * 100);

const film = {
  meta: {
    world: courier.channel ? "dtc" : "dtc",
    period: courier.period,
    title: "The courier payout",
    symbol: "₹",
    minorPerMajor: 100,
    courier: courier.channel,
  },
  remittance: {
    id: rem.id,
    utr: rem.utr,
    grossCollected: rem.gross_collected,
    collectionFee: rem.collection_fee,
    gstOnFee: rem.gst_on_fee,
    netDeposit: rem.net_deposit,
    deductionsTotal,
  },
  lifecycle: {
    delivered: courier.shipments.filter((s) => s.status === "delivered").length,
    rto: courier.shipments.filter((s) => s.status === "rto").length,
  },
  residualBefore: bundle.break.actual,
  residualAfter: deductions
    .filter((d) => !d.backed)
    .reduce((s, d) => s + d.amount, 0),
  breakDetail: bundle.break.detail,
  deductions,
  baseline: { scorePct: errorsOff.score_pct ?? pct(correctOff), missing: errorsOff.totals.missing },
  final: { scorePct: pct(correctFinal), missing: errorsOff.totals.missing - bookedCount },
};

writeFileSync(
  join(OUT, "dtc-2026-02.rto.json"),
  JSON.stringify(film, null, 2) + "\n",
);
console.log(
  `wrote dtc-2026-02.rto.json — residual ${film.residualBefore} → ${film.residualAfter}, ` +
    `score ${film.baseline.scorePct}% → ${film.final.scorePct}%, ${deductions.length} deductions`,
);
