export type Side = "Dr" | "Cr";

export interface Posting {
  account: string;
  side: Side;
  /** minor units (paise), >= 0 */
  amount: number;
}

export interface Account {
  path: string;
  group: string;
  leaf: string;
  label: string;
  note?: string;
}

export type StepKind = "event" | "agent" | "reconcile" | "opening" | "closing";

export interface Step {
  id: string;
  index: number;
  label: string;
  sublabel?: string;
  kind: StepKind;
  eventId?: string;
  entryType?: string;
  postings: Posting[];
}

export interface FilmMeta {
  world: string;
  period: string;
  title?: string;
  currency?: string;
  minorPerMajor: number;
  symbol: string;
}

export interface LedgerFilm {
  meta: FilmMeta;
  accounts: Account[];
  steps: Step[];
}

export interface ColumnNode {
  key: string;
  label: string;
  account?: string;
  children?: ColumnNode[];
}

export interface Frame {
  index: number;
  step: Step | null;
  balances: Record<string, number>;
  deltas: Record<string, number>;
}
