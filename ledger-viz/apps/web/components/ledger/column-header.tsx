import * as React from "react";
import type { ColumnNode } from "@ledger-viz/model";
import { cn } from "@/lib/utils";
import { GUTTER_W, LEAF_W, leafColumnsMarked, guideClass } from "./layout";

export interface ColumnHeaderProps {
  columns: ColumnNode[];
}

/**
 * Two stacked, sticky header rows: group labels on top, leaf account labels
 * below, with a thin sticky "Step" gutter on the left. Borderless except a
 * single hairline under the leaf row and the shared faint column guides.
 */
export function ColumnHeader({ columns }: ColumnHeaderProps) {
  const leaves = leafColumnsMarked(columns);
  return (
    <div className="sticky top-0 z-20 bg-card/95 backdrop-blur-sm">
      {/* Group row */}
      <div className="flex">
        <div
          className="sticky left-0 z-10 shrink-0 bg-card/95"
          style={{ width: GUTTER_W }}
        />
        {columns.map((group, gi) => {
          const span = group.children?.length ?? 0;
          if (span === 0) return null;
          return (
            <div
              key={group.key}
              className={cn(
                "shrink-0 px-4 pb-1.5 pt-4 text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/70",
                gi > 0 && "border-l border-border/55",
              )}
              style={{ width: span * LEAF_W }}
            >
              {group.label}
            </div>
          );
        })}
      </div>
      {/* Leaf row */}
      <div className="flex border-b border-border/70">
        <div
          className="sticky left-0 z-10 flex shrink-0 items-center bg-card/95 px-4 pb-2.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground/50"
          style={{ width: GUTTER_W }}
        >
          Step
        </div>
        {leaves.map((lc) => (
          <div
            key={lc.leaf.key}
            title={lc.leaf.label}
            className={cn(
              "flex shrink-0 items-end justify-end px-4 pb-2.5 text-right text-[11px] font-medium leading-tight text-foreground/65",
              guideClass(lc.groupStart),
            )}
            style={{ width: LEAF_W }}
          >
            <span className="line-clamp-2">{lc.leaf.label}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
