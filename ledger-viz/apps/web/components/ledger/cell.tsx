import * as React from "react";
import { formatMoney, type FilmMeta } from "@ledger-viz/model";
import { cn } from "@/lib/utils";

export interface CellProps {
  value?: number;
  meta: FilmMeta;
  /** Grey the number regardless of sign (used by the Starting/Ending rows). */
  muted?: boolean;
  className?: string;
}

/**
 * A single matrix cell rendering a signed Dr/Cr amount in monospace — no
 * borders, generous padding, so the grid reads as an airy matrix rather than a
 * spreadsheet.
 *
 * Sign treatment is color-coded: a positive (Dr / debit) delta renders emerald,
 * a negative (Cr / credit) delta renders rose, with the minus carried by
 * formatMoney. An undefined value renders a faint centre dot; `muted` greys the
 * value (used by the closing-balance row).
 */
export function Cell({ value, meta, muted, className }: CellProps) {
  const empty = value === undefined;
  const positive = !empty && (value as number) > 0;
  const negative = !empty && (value as number) < 0;

  return (
    <div
      className={cn(
        "flex h-10 items-center justify-end px-4 text-right font-mono text-[12px] tabular-nums",
        className,
      )}
    >
      {empty ? (
        <span className="select-none text-muted-foreground/30">·</span>
      ) : (
        <span
          className={cn(
            muted
              ? "text-muted-foreground/75"
              : positive
                ? "text-emerald-600 dark:text-emerald-400"
                : negative
                  ? "text-rose-600 dark:text-rose-400"
                  : "text-muted-foreground/60",
          )}
        >
          {formatMoney(value as number, meta)}
        </span>
      )}
    </div>
  );
}
