import type { ColumnNode } from "@ledger-viz/model";

/** Fixed, shared layout metrics so header / step / balance rows align. */
export const GUTTER_W = 264;
export const LEAF_W = 136;

/** Flatten the column tree to its ordered leaf account columns. */
export function leafColumns(columns: ColumnNode[]): ColumnNode[] {
  return columns.flatMap((group) => group.children ?? []);
}

/** A leaf column annotated with whether it begins a new account group. */
export interface LeafCol {
  leaf: ColumnNode;
  /** First leaf of a group (not the very first column) — gets a stronger guide. */
  groupStart: boolean;
}

/**
 * Flatten to leaves, marking group boundaries so every row (header, step,
 * balance) can draw the SAME faint column guides — a stronger rule between
 * account groups, a hairline between leaves within a group.
 */
export function leafColumnsMarked(columns: ColumnNode[]): LeafCol[] {
  const out: LeafCol[] = [];
  columns.forEach((group, gi) => {
    (group.children ?? []).forEach((leaf, li) => {
      out.push({ leaf, groupStart: li === 0 && gi > 0 });
    });
  });
  return out;
}

/** Shared column-guide classes, keyed off whether the leaf starts a group. */
export function guideClass(groupStart: boolean): string {
  return groupStart ? "border-l border-border/55" : "border-l border-border/[0.12]";
}
