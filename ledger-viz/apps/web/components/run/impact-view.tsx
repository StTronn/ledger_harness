"use client";

import * as React from "react";
import { formatMoney, type RunFilm } from "@ledger-viz/model";
import type { RunSample } from "@ledger-viz/model/fixtures";
import { cn } from "@/lib/utils";

function Bar({ pct, live }: { pct: number; live?: boolean }) {
  return (
    <div className="h-1.5 w-full bg-muted">
      <div
        className={cn("h-full", live ? "bg-brand" : "bg-foreground/30")}
        style={{ width: `${Math.min(100, Math.max(0, pct))}%` }}
      />
    </div>
  );
}

function Column({
  title,
  film,
  side,
}: {
  title: string;
  film: RunFilm;
  side: "off" | "on";
}) {
  const c = side === "off" ? film.baseline : film.final;
  const live = side === "on";
  const rows: { k: string; v: string }[] = [
    { k: "entries booked", v: `${c.booked}/${c.truth}` },
    { k: "skipped", v: String(c.skipped) },
    { k: "breaks", v: String(c.breaks) },
    ...(side === "on"
      ? [{ k: "reviewed", v: String(film.consultations.length) }]
      : []),
  ];

  return (
    <div className="border border-border bg-card">
      <div
        className={cn(
          "border-b border-border px-4 py-2.5 font-mono text-[10.5px] uppercase tracking-[0.16em]",
          live ? "bg-brand-soft text-brand-foreground dark:text-brand" : "text-muted-foreground",
        )}
      >
        {title}
      </div>
      <div className="space-y-3 px-4 py-4">
        {rows.map((r) => (
          <div
            key={r.k}
            className="flex items-baseline justify-between text-[13px]"
          >
            <span className="text-muted-foreground">{r.k}</span>
            <span className="font-mono font-semibold tabular-nums">{r.v}</span>
          </div>
        ))}
        <div className="space-y-1.5 pt-1">
          <div className="flex items-baseline justify-between text-[13px]">
            <span className="text-muted-foreground">score</span>
            <span
              className={cn(
                "font-mono text-[15px] font-semibold tabular-nums",
                live && "bg-brand-soft px-1 text-brand-foreground dark:text-brand",
              )}
            >
              {c.scorePct}%
            </span>
          </div>
          <Bar pct={c.scorePct} live={live} />
        </div>
      </div>
    </div>
  );
}

/**
 * Baseline vs agent, the value proposition in one glance: the same period
 * closed with the agent off (rules only) and on. Below, exactly the entries
 * the agent added — nothing else differs between the two columns.
 */
export function ImpactView({ films }: { films: RunSample[] }) {
  const [selectedId, setSelectedId] = React.useState(films[0]?.id ?? "");
  const sample =
    films.find((f) => f.id === selectedId) ?? films[0];
  const film = sample.film;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
          Period
        </span>
        <div
          role="tablist"
          aria-label="Impact period"
          className="inline-flex items-center gap-0.5 border border-border bg-muted/50 p-0.5"
        >
          {films.map((f) => {
            const active = f.id === selectedId;
            return (
              <button
                key={f.id}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setSelectedId(f.id)}
                className={cn(
                  "px-3 py-1.5 text-[13px] font-medium outline-none transition-colors",
                  "focus-visible:ring-2 focus-visible:ring-ring/60",
                  active
                    ? "bg-card text-foreground ring-1 ring-border"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {f.label}
              </button>
            );
          })}
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <Column title="Rules only — baseline" film={film} side="off" />
        <Column title="With agent review" film={film} side="on" />
      </div>

      <div className="border border-border bg-card">
        <div className="border-b border-border px-4 py-2.5 font-mono text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
          Review queue
        </div>
        <div>
          {film.consultations.map((c) => (
            <div
              key={c.id}
              className="grid grid-cols-[minmax(0,1fr)_auto_auto] items-baseline gap-3 border-b border-border/70 border-l-2 border-l-brand bg-brand-soft/50 px-4 py-2 last:border-b-0"
            >
              <div className="min-w-0">
                <span className="font-mono text-[12.5px]">{c.eventId}</span>
                <span className="ml-2 font-mono text-[11px] text-muted-foreground">
                  {c.role === "investigate"
                    ? `reviewed ${c.break?.kind ?? "break"}`
                    : c.whyMissed}
                </span>
              </div>
              <span className="font-mono text-[12px] text-muted-foreground">
                {c.status === "recommended" ? c.entryType ?? "review" : "escalated"}
              </span>
              <span className="font-mono text-[12.5px] font-semibold tabular-nums">
                {formatMoney(c.amount, film.meta)}
              </span>
            </div>
          ))}
          {film.consultations.length === 0 ? (
            <div className="px-4 py-3 text-[13px] text-muted-foreground">
              No exceptions required review.
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
