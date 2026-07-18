"use client";

import * as React from "react";
import {
  formatMoney,
  type Consultation,
  type ConsultStage,
  type RunMeta,
} from "@ledger-viz/model";
import { cn } from "@/lib/utils";

/** Display order of the stage rail; the terminal column renders reviewed OR escalated. */
const RAIL: { stage: ConsultStage; label: string }[] = [
  { stage: "flagged", label: "flagged" },
  { stage: "agent", label: "agent" },
  { stage: "decision", label: "decision" },
];

const STAGE_RANK: Record<ConsultStage, number> = {
  flagged: 0,
  agent: 1,
  decision: 2,
  reviewed: 3,
  escalated: 3,
};

export interface ExceptionRowProps {
  consultation: Consultation;
  meta: RunMeta;
  /** the furthest stage this consultation has reached at the current frame */
  stage: ConsultStage;
  /** true when this consultation is the one animating on the current frame */
  active: boolean;
  selected: boolean;
  onSelect: () => void;
}

/**
 * One exception in the lane: an event the rules could not book (or a break they
 * could not clear), progressing flagged → agent → decision → reviewed/escalated
 * as the playhead advances. Review is the recorded recommendation; escalation is
 * muted amber — an honest hand-off, not a failure.
 */
export function ExceptionRow({
  consultation: c,
  meta,
  stage,
  active,
  selected,
  onSelect,
}: ExceptionRowProps) {
  const rank = STAGE_RANK[stage];
  const terminal = c.status === "recommended" ? "reviewed" : "escalated";

  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      className={cn(
        "grid w-full grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_auto] items-center gap-3 border-b border-border/70 px-4 py-2.5 text-left outline-none transition-colors last:border-b-0",
        "focus-visible:ring-2 focus-visible:ring-ring/60",
        active && "border-l-2 border-l-brand bg-brand-soft",
        !active && selected && "bg-muted/60",
        !active && !selected && "hover:bg-muted/40",
      )}
    >
      {/* who */}
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span
            className={cn(
              "border border-border px-1 py-px font-mono text-[10px] uppercase tracking-wide",
              c.role === "investigate"
                ? "text-foreground"
                : "text-muted-foreground",
            )}
          >
            {c.role}
          </span>
          <span className="truncate font-mono text-[12.5px] text-foreground">
            {c.eventId}
          </span>
        </div>
        <div className="mt-0.5 truncate font-mono text-[11px] text-muted-foreground">
          {c.eventType} · {formatMoney(c.amount, meta)} ·{" "}
          {c.resolves === "break" ? "reconcile break" : c.whyMissed}
        </div>
      </div>

      {/* stage rail */}
      <div className="flex items-center gap-1.5 justify-self-end font-mono text-[10.5px] uppercase tracking-wide">
        {RAIL.map((r, i) => (
          <React.Fragment key={r.stage}>
            {i > 0 ? (
              <span aria-hidden className="h-px w-2.5 bg-border" />
            ) : null}
            <span
              className={cn(
                rank >= STAGE_RANK[r.stage]
                  ? "text-foreground"
                  : "text-muted-foreground/50",
                active && stage === r.stage && "bg-brand-soft px-1 text-brand-foreground dark:text-brand",
              )}
            >
              {r.label}
            </span>
          </React.Fragment>
        ))}
      </div>

      {/* terminal state */}
      <span
        className={cn(
          "min-w-[88px] border px-1.5 py-0.5 text-center font-mono text-[10.5px] uppercase tracking-wide",
          rank >= 3
            ? c.status === "recommended"
              ? "border-brand/40 bg-brand-soft text-brand-foreground dark:text-brand"
              : "border-amber-600/30 bg-amber-500/10 text-amber-700 dark:text-amber-400"
            : "border-border/60 text-muted-foreground/40",
        )}
      >
        {rank >= 3 ? terminal : "·"}
      </span>
    </button>
  );
}
