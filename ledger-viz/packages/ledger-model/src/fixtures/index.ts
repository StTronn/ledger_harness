import { loadFilm } from "../load";
import type { LedgerFilm } from "../types";
import dtc202605 from "./dtc-2026-05.film.json";
import dtc202603 from "./dtc-2026-03.film.json";

export interface Sample {
  id: string;
  label: string;
  film: LedgerFilm;
}

function toLabel(film: LedgerFilm): string {
  const title = film.meta.title ?? film.meta.world;
  return `${title} (${film.meta.period})`;
}

const film202605 = loadFilm(dtc202605);
const film202603 = loadFilm(dtc202603);

export const samples: Sample[] = [
  { id: "dtc-2026-05", label: toLabel(film202605), film: film202605 },
  { id: "dtc-2026-03", label: toLabel(film202603), film: film202603 },
];
