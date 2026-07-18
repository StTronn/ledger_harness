"use client";

import * as React from "react";
import { formatMoney, type RtoFilm } from "@ledger-viz/model";
import { cn } from "@/lib/utils";

type Meta = RtoFilm["meta"];

function Eyebrow({ children }: { children: React.ReactNode }) {
  return (
    <div className="font-mono text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
      {children}
    </div>
  );
}

function Money({ v, meta, className }: { v: number; meta: Meta; className?: string }) {
  return (
    <span className={cn("font-mono tabular-nums", className)}>
      {formatMoney(v, meta)}
    </span>
  );
}

/**
 * Act 1 — the courier payout, as a waterfall. The courier collected the COD cash
 * at the door, then netted its collection fee + GST and the per-shipment
 * deductions before wiring the remainder. The net matches the bank credit.
 */
function PayoutCard({ film }: { film: RtoFilm }) {
  const { remittance: r, meta } = film;
  const rows: { label: string; v: number; sub?: string; sign: "+" | "−" }[] = [
    { label: "COD cash collected", v: r.grossCollected, sign: "+", sub: `${film.lifecycle.delivered} deliveries` },
    { label: "Collection fee", v: r.collectionFee, sign: "−", sub: "per contract ✓" },
    { label: "GST on fee", v: r.gstOnFee, sign: "−", sub: "per contract ✓" },
    { label: "Per-shipment deductions", v: r.deductionsTotal, sign: "−", sub: `${film.deductions.length} lines — unexplained` },
  ];
  return (
    <div className="border border-border bg-card">
      <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
        <div>
          <Eyebrow>Courier payout · {meta.courier}</Eyebrow>
          <div className="font-mono text-[13.5px] font-semibold tracking-tight">
            {film.remittance.id}
          </div>
        </div>
        <span className="shrink-0 border border-border px-1.5 py-0.5 font-mono text-[10.5px] uppercase tracking-wide text-muted-foreground">
          UTR {r.utr.slice(-8)}
        </span>
      </div>
      <div className="px-4 py-4">
        <div className="space-y-1.5">
          {rows.map((row) => (
            <div key={row.label} className="flex items-baseline justify-between gap-3 text-[12.5px]">
              <span className="flex items-baseline gap-2">
                <span className="w-3 font-mono text-muted-foreground/70">{row.sign}</span>
                <span className="text-muted-foreground">{row.label}</span>
                {row.sub ? (
                  <span className={cn(
                    "font-mono text-[10.5px]",
                    row.label.startsWith("Per-shipment")
                      ? "text-amber-700 dark:text-amber-400"
                      : "text-muted-foreground/60",
                  )}>
                    {row.sub}
                  </span>
                ) : null}
              </span>
              <Money v={row.v} meta={meta} className="text-foreground" />
            </div>
          ))}
        </div>
        <div className="mt-3 flex items-baseline justify-between gap-3 border-t-2 border-foreground/80 pt-3">
          <span className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
            Wired to bank
          </span>
          <span className="flex items-baseline gap-2">
            <Money v={r.netDeposit} meta={meta} className="text-[15px] font-semibold text-foreground" />
            <span className="bg-brand-soft px-1 font-mono text-[10.5px] font-semibold uppercase tracking-wide text-brand-foreground dark:text-brand">
              matches ✓
            </span>
          </span>
        </div>
      </div>
    </div>
  );
}

/** Act 2 — the break: rules cleared everything but the deductions. */
function BreakCard({ film }: { film: RtoFilm }) {
  const { meta } = film;
  return (
    <div className="border border-border bg-card">
      <div className="border-b border-border px-4 py-3">
        <Eyebrow>Reconcile · check #3 (cod-receivable)</Eyebrow>
        <div className="font-mono text-[13.5px] font-semibold tracking-tight text-amber-700 dark:text-amber-400">
          residual {formatMoney(film.residualBefore, meta)}
        </div>
      </div>
      <div className="space-y-3 px-4 py-4">
        <p className="text-pretty text-[12.5px] leading-relaxed text-muted-foreground">
          The deterministic rules booked every delivery as a <code className="font-mono text-[11.5px]">cod_sale</code> and
          the remittance's collection-fee portion. But the courier also netted out
          two per-shipment charges that no per-event rule fits — so the COD
          receivable is left short by exactly their sum. The break reads the
          posted ledger, but the agent review cannot change it. The residual stays
          open until a deterministic or explicitly approved posting handles it.
        </p>
        <p className="border-l-2 border-border pl-2 font-mono text-[11.5px] leading-relaxed text-muted-foreground">
          {film.breakDetail}
        </p>
      </div>
    </div>
  );
}

/**
 * Act 3 — the decomposition. Each deduction, pre-classified against the rate
 * card by the recovery engine's rto-fee policy: a backed RTO fee is SUPPORTED;
 * an unverified charge is ESCALATED. The agent records both outcomes for review;
 * neither recommendation changes the ledger.
 */
function DeductionCard({ d, meta }: { d: RtoFilm["deductions"][number]; meta: Meta }) {
  const supported = d.verdict === "supported";
  return (
    <div className="flex flex-col border border-border bg-card">
      <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
        <div className="min-w-0">
          <Eyebrow>{d.code}</Eyebrow>
          <div className="truncate font-mono text-[13.5px] font-semibold tracking-tight">
            {d.label}
          </div>
        </div>
        <Money v={d.amount} meta={meta} className="shrink-0 text-[15px] font-semibold text-foreground" />
      </div>

      <div className="space-y-3 px-4 py-4">
        <div className="space-y-1 text-[12.5px]">
          <div className="flex items-baseline justify-between gap-3">
            <span className="text-muted-foreground">shipment</span>
            <span className="text-right font-mono text-foreground">{d.shipmentId}</span>
          </div>
          <div className="flex items-baseline justify-between gap-3">
            <span className="text-muted-foreground">lifecycle</span>
            <span className={cn(
              "font-mono text-[11px] uppercase tracking-wide",
              d.shipmentStatus === "rto"
                ? "text-amber-700 dark:text-amber-400"
                : "text-muted-foreground",
            )}>
              {d.shipmentStatus || "—"}
            </span>
          </div>
        </div>

        <div className="space-y-1.5">
          <Eyebrow>Recovery verdict</Eyebrow>
          <p className="text-pretty text-[12px] leading-relaxed text-muted-foreground">
            {d.note}
          </p>
          {d.citation ? (
            <div
              title={`re-read from the snapshot: ${d.citation.object} → ${d.citation.path}`}
              className="inline-block cursor-help border border-border px-1 py-px font-mono text-[10px] text-muted-foreground"
            >
              {d.citation.object}/{d.citation.path} ⓘ
            </div>
          ) : null}
        </div>
      </div>

      {/* REVIEW OUTCOME — the ledger is deliberately untouched by this path. */}
      <div className="mt-auto border-t-2 border-foreground/80 px-4 py-3.5">
        <div className="flex items-center justify-between">
          <span className="font-mono text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
            Review outcome
          </span>
          {supported ? (
            <span className="bg-brand-soft px-1.5 py-0.5 font-mono text-[11px] font-semibold uppercase tracking-wide text-brand-foreground dark:text-brand">
              ✓ recommendation logged
            </span>
          ) : (
            <span className="bg-amber-500/10 px-1.5 py-0.5 font-mono text-[11px] font-semibold uppercase tracking-wide text-amber-700 dark:text-amber-400">
              escalated to a human
            </span>
          )}
        </div>
        {supported ? (
          <p className="mt-2 font-mono text-[12px] leading-relaxed text-muted-foreground">
            {d.entryType} is supported by the rate card. The ledger remains unchanged.
          </p>
        ) : (
          <p className="mt-2 font-mono text-[12px] leading-relaxed text-amber-700 dark:text-amber-400">
            declined — request the courier's reweigh report; held open as money the courier still owes.
          </p>
        )}
      </div>
    </div>
  );
}

/** Act 4 — the evidence accounts for the break; the ledger remains open. */
function ResidualCard({ film }: { film: RtoFilm }) {
  const { meta } = film;
  const supported = film.deductions
    .filter((d) => d.verdict === "supported")
    .reduce((sum, d) => sum + d.amount, 0);
  const unresolved = film.residualBefore - supported;
  return (
    <div className="border border-border bg-card">
      <div className="border-b border-border px-4 py-3">
        <Eyebrow>The residual stays open</Eyebrow>
        <div className="font-mono text-[13.5px] font-semibold tracking-tight">
          {formatMoney(film.residualBefore, meta)} identified for review
        </div>
      </div>
      <div className="space-y-4 px-4 py-4">
        <div className="space-y-1.5 text-[12.5px]">
          <div className="flex items-baseline justify-between gap-3">
            <span className="text-muted-foreground">supported recommendation</span>
            <Money v={supported} meta={meta} className="text-brand-foreground dark:text-brand" />
          </div>
          <div className="flex items-baseline justify-between gap-3">
            <span className="text-muted-foreground">needs evidence</span>
            <Money v={unresolved} meta={meta} className="text-amber-700 dark:text-amber-400" />
          </div>
          <div className="flex items-baseline justify-between gap-3 border-t border-border/70 pt-1.5">
            <span className="text-foreground">reviewed, not posted</span>
            <Money v={film.residualBefore} meta={meta} className="font-semibold text-foreground" />
          </div>
        </div>

        <div className="flex items-baseline justify-between gap-3 border-t border-border/70 pt-3 text-[12.5px]">
          <span className="truncate text-muted-foreground">assets/cod-receivable</span>
          <span className="bg-amber-500/10 px-1 font-mono font-semibold tabular-nums text-amber-700 dark:text-amber-400">
            unchanged · {formatMoney(film.residualAfter, meta)}
          </span>
        </div>

        <div className="flex items-center justify-between gap-3 border-t-2 border-foreground/80 pt-3">
          <span className="font-mono text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
            Score
          </span>
          <span className="font-mono text-[13px] tabular-nums">
            <span className="text-muted-foreground">{film.baseline.scorePct}%</span>
            <span className="px-1.5 text-muted-foreground/60">→</span>
            <span className="font-semibold text-foreground">{film.final.scorePct}%</span>
            <span className="ml-2 text-[11px] text-muted-foreground">
              {film.final.missing} entries still open — review only
            </span>
          </span>
        </div>
      </div>
    </div>
  );
}

/**
 * The cash-on-delivery / RTO view. A four-act narrative: the courier payout
 * arrives netted, reconcile catches the cod-receivable residual, the recovery
 * engine decomposes the remittance, and the agent records supported or escalated
 * recommendations — leaving the ledger unchanged for explicit approval.
 */
export function RtoView({ film }: { film: RtoFilm }) {
  return (
    <div className="space-y-10">
      {/* Acts 1 & 2 side by side: the payout and the break it triggers. */}
      <section className="grid gap-5 lg:grid-cols-2">
        <div className="flex h-full flex-col space-y-2">
          <Eyebrow>1 · The payout lands</Eyebrow>
          <div className="flex-1"><PayoutCard film={film} /></div>
        </div>
        <div className="flex h-full flex-col space-y-2">
          <Eyebrow>2 · Reconcile finds the gap</Eyebrow>
          <div className="flex-1"><BreakCard film={film} /></div>
        </div>
      </section>

      {/* Act 3: the decomposition — the centerpiece. */}
      <section className="space-y-3">
        <div className="flex items-baseline gap-3">
          <Eyebrow>3 · Recovery prepares the review</Eyebrow>
          <span className="font-mono text-[11px] text-muted-foreground/70">
            recommend what the rate card supports · escalate what it cannot
          </span>
        </div>
        <div className="grid gap-5 md:grid-cols-2">
          {film.deductions.map((d) => (
            <DeductionCard key={d.id} d={d} meta={film.meta} />
          ))}
        </div>
      </section>

      {/* Act 4: the review accounts for the evidence. */}
      <section className="space-y-2">
        <Eyebrow>4 · The break is fully accounted for</Eyebrow>
        <div className="max-w-xl">
          <ResidualCard film={film} />
        </div>
      </section>
    </div>
  );
}
