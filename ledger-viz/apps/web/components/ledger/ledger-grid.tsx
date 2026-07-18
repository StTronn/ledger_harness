import * as React from "react";
import {
  buildColumns,
  type Frame,
  type LedgerFilm,
} from "@ledger-viz/model";
import { Card } from "@/components/ui/card";
import { ColumnHeader } from "./column-header";
import { StepRow } from "./step-row";
import { BalanceRow } from "./balance-row";

export interface LedgerGridProps {
  film: LedgerFilm;
  frames: Frame[];
  /** Playhead in [0 .. steps.length]. 0 = opening; n = after step n. */
  playhead: number;
}

/**
 * The ledger view: grouped column headers, a live "Current" balance row
 * (the running balance at the playhead), one row per step (signed deltas), and a
 * static "Ending" balance row, inside a horizontally scrollable card. Column
 * widths are fixed and shared across every row so the grid aligns perfectly.
 */
export function LedgerGrid({ film, frames, playhead }: LedgerGridProps) {
  const columns = buildColumns(film.accounts);
  const current = frames[playhead] ?? frames[0];
  const ending = frames[frames.length - 1];

  return (
    <Card className="overflow-x-auto p-0">
      <div className="min-w-max pb-1">
        <ColumnHeader columns={columns} />

        <BalanceRow
          label="Current"
          columns={columns}
          balances={current.balances}
          meta={film.meta}
          variant="current"
        />

        {film.steps.map((step) => {
          const frame = frames[step.index + 1];
          const state =
            step.index < playhead - 1
              ? "past"
              : step.index === playhead - 1
                ? "current"
                : "future";
          return (
            <StepRow
              key={step.id}
              step={step}
              columns={columns}
              deltas={frame.deltas}
              meta={film.meta}
              state={state}
            />
          );
        })}

        <BalanceRow
          label="Ending"
          columns={columns}
          balances={ending.balances}
          meta={film.meta}
          variant="ending"
        />
      </div>
    </Card>
  );
}
