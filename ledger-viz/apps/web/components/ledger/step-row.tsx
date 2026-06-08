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

  return (
    <div
      className={cn(
        "flex transition-opacity",
        current && "bg-primary/[0.07]",
        future && "opacity-35",
      )}
    >
      <div
        className={cn(
          "sticky left-0 z-10 flex shrink-0 items-center gap-2.5 py-2 pr-4",
          current
            ? "border-l-2 border-primary bg-primary/[0.07] pl-[14px]"
            : "bg-card pl-4",
        )}
        style={{ width: GUTTER_W }}
      >
        <span
          className={cn(
            "w-5 shrink-0 text-right font-mono text-[11px] tabular-nums",
            current ? "text-foreground/70" : "text-muted-foreground/45",
          )}
        >
          {step.index + 1}
        </span>
        <span
          className={cn(
            "flex-1 truncate font-mono text-[12px]",
            current ? "font-medium text-foreground" : "text-foreground/75",
          )}
          title={step.label}
        >
          {step.label}
        </span>
        {tag && (
          <span className="shrink-0 font-mono text-[10px] tracking-tight text-muted-foreground/55">
            {tag}
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
