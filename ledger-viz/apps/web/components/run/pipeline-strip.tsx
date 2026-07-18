"use client";

import * as React from "react";
import type { RunFilm, RunFrame } from "@ledger-viz/model";
import { cn } from "@/lib/utils";

/**
 * The close pipeline as a counter strip: ingest → normalize → post → reconcile →
 * review → score. Review counters are live; ledger counts and score stay fixed
 * while the agent recommendation is recorded.
 */
export function PipelineStrip({
  film,
  frame,
}: {
  film: RunFilm;
  frame: RunFrame;
}) {
  const events = film.baseline.truth;
  const done = frame.reviewed === film.consultations.length;

  const stages: { name: string; value: string; note?: string; live?: boolean }[] = [
    { name: "ingest", value: `${events} ev` },
    { name: "normalize", value: `${events} ev` },
    {
      name: "classify",
      value: `${film.baseline.booked} ✓`,
      note: frame.skipped > 0 ? `${frame.skipped} miss` : "0 miss",
      live: frame.skipped !== film.baseline.skipped,
    },
    {
      name: "post",
      value: `${frame.booked} ▤`,
      live: false,
    },
    {
      name: "reconcile",
      value: `${frame.breaks} break${frame.breaks === 1 ? "" : "s"}`,
      live: frame.breaks !== film.baseline.breaks,
    },
    {
      name: "review",
      value: `${frame.reviewed}/${film.consultations.length}`,
      note: "recommendations",
      live: frame.reviewed > 0,
    },
    {
      name: "score",
      value: `${frame.scorePct}%`,
      live: frame.scorePct > film.baseline.scorePct,
    },
  ];

  return (
    <div className="border border-border bg-card">
      <div className="grid grid-cols-3 divide-x divide-border sm:grid-cols-7">
        {stages.map((s) => (
          <div key={s.name} className="px-4 py-3">
            <div className="font-mono text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
              {s.name}
            </div>
            <div className="mt-1">
              <span
                className={cn(
                  "font-mono text-[15px] font-semibold tabular-nums tracking-tight",
                  (s.live || (s.name === "score" && done)) &&
                    "bg-brand-soft px-1 text-brand-foreground dark:text-brand",
                )}
              >
                {s.value}
              </span>
            </div>
            {s.note ? (
              <div className="font-mono text-[11px] tabular-nums text-muted-foreground">
                {s.note}
              </div>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  );
}
