import * as React from "react";
import type { ColumnNode, FilmMeta } from "@ledger-viz/model";
import { cn } from "@/lib/utils";
import { Cell } from "./cell";
import { GUTTER_W, LEAF_W, leafColumnsMarked, guideClass } from "./layout";

export interface BalanceRowProps {
  label: string;
  columns: ColumnNode[];
  balances: Record<string, number>;
  meta: FilmMeta;
  /**
   * "current" is the LIVE running balance at the playhead — emphasised, with
   * sign-coloured numbers and a dashed leader below. "ending" is the static
   * closing balance — muted, with a dashed leader above.
   */
  variant: "current" | "ending";
}

/**
 * A running-balance row. The Current row tracks the playhead and stands out
 * (sign-coloured, subtly tinted); the Ending row is the muted closing total.
 */
export function BalanceRow({
  label,
  columns,
  balances,
  meta,
  variant,
}: BalanceRowProps) {
  const leaves = leafColumnsMarked(columns);
  const isCurrent = variant === "current";
  return (
    <div
      className={cn(
        "flex",
        isCurrent
          ? "border-b border-dashed border-border/70 bg-primary/[0.04]"
          : "border-t border-dashed border-border/60",
      )}
    >
      <div
        className={cn(
          "sticky left-0 z-10 flex shrink-0 items-center py-2.5 pr-4 text-[10px] font-semibold uppercase tracking-[0.16em]",
          isCurrent
            ? "border-l-2 border-primary bg-primary/[0.04] pl-[14px] text-foreground/70"
            : "bg-card pl-4 text-muted-foreground/50",
        )}
        style={{ width: GUTTER_W }}
      >
        {label}
      </div>
      {leaves.map((lc) => (
        <div
          key={lc.leaf.key}
          className={cn("shrink-0", guideClass(lc.groupStart))}
          style={{ width: LEAF_W }}
        >
          <Cell
            value={lc.leaf.account ? (balances[lc.leaf.account] ?? 0) : undefined}
            meta={meta}
            muted={!isCurrent}
          />
        </div>
      ))}
    </div>
  );
}
