import * as React from "react";
import type { ColumnNode, FilmMeta, Step } from "@ledger-viz/model";
import { cn } from "@/lib/utils";
import { Cell } from "./cell";
import { GUTTER_W, LEAF_W, leafColumnsMarked, guideClass } from "./layout";

export interface StepRowProps {
  step: Step;
  columns: ColumnNode[];
  deltas: Record<string, number>;
  meta: FilmMeta;
  state: "past" | "current" | "future";
}

/**
 * One transaction row: a sticky left gutter (index, label, muted entry-type)
 * + a cell per leaf account. No row borders — only the soft active tint and the
 * shared faint column guides, so the matrix reads airy rather than tabular.
 */
export function StepRow({ step, columns, deltas, meta, state }: StepRowProps) {
  const leaves = leafColumnsMarked(columns);
  const current = state === "current";
  const future = state === "future";
  const tag = step.entryType ?? step.kind;
  const zebra = step.index % 2 === 1;

  return (
    <div
      className={cn(
        "flex border-b border-border/40 transition-opacity",
        current
          ? "bg-brand-soft"
          : zebra
            ? "bg-muted/40"
            : "bg-card",
        future && "opacity-40",
      )}
    >
      <div
        className={cn(
          "sticky left-0 z-10 flex shrink-0 items-center gap-2.5 py-2.5 pr-4",
          current
            ? "border-l-2 border-brand bg-brand-soft pl-[14px]"
            : cn(zebra ? "bg-muted/40" : "bg-card", "pl-4"),
        )}
        style={{ width: GUTTER_W }}
      >
        <span
          className={cn(
            "w-5 shrink-0 text-right font-mono text-[11px] tabular-nums",
            current ? "text-foreground/75" : "text-muted-foreground/45",
          )}
        >
          {step.index + 1}
        </span>
        <span
          className={cn(
            "flex-1 truncate text-[12.5px] tracking-tight",
            current ? "font-medium text-foreground" : "text-foreground/80",
          )}
          title={step.label}
        >
          {step.label}
        </span>
        {tag && (
          <span
            className={cn(
              "shrink-0 rounded-sm border px-1.5 py-px font-mono text-[9.5px] uppercase tracking-wide",
              // Legacy agent-tagged rows retain the brand treatment when present;
              // current review-only agent records are excluded from the ledger film.
              step.kind === "agent"
                ? "border-brand/40 bg-brand-soft text-brand-foreground dark:text-brand"
                : "border-border/70 bg-card text-muted-foreground/65",
            )}
          >
            {step.kind === "agent" ? `${tag} · agent` : tag}
          </span>
        )}
      </div>
      {leaves.map((lc) => (
        <div
          key={lc.leaf.key}
          className={cn("shrink-0", guideClass(lc.groupStart))}
          style={{ width: LEAF_W }}
        >
          <Cell
            value={lc.leaf.account ? deltas[lc.leaf.account] : undefined}
            meta={meta}
          />
        </div>
      ))}
    </div>
  );
}
