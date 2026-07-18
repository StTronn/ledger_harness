import { loadFilm } from "../load";
import type { LedgerFilm } from "../types";
import { loadRunFilm } from "../run-film";
import type { RunFilm } from "../run-film";
import { loadRtoFilm } from "../rto-film";
import type { RtoFilm } from "../rto-film";
import dtc202605 from "./dtc-2026-05.film.json";
import dtc202603 from "./dtc-2026-03.film.json";
import dtc202601 from "./dtc-2026-01.film.json";
import run202601 from "./dtc-2026-01.run.json";
import run202603 from "./dtc-2026-03.run.json";
import run202604 from "./dtc-2026-04.run.json";
import rto202602 from "./dtc-2026-02.rto.json";

export interface Sample {
  id: string;
  label: string;
  film: LedgerFilm;
}

export interface RunSample {
  id: string;
  label: string;
  film: RunFilm;
}

function toLabel(film: LedgerFilm): string {
  const title = film.meta.title ?? film.meta.world;
  return `${title} (${film.meta.period})`;
}

const film202605 = loadFilm(dtc202605);
const film202603 = loadFilm(dtc202603);
const film202601 = loadFilm(dtc202601);

export const samples: Sample[] = [
  { id: "dtc-2026-05", label: toLabel(film202605), film: film202605 },
  { id: "dtc-2026-03", label: toLabel(film202603), film: film202603 },
  { id: "dtc-2026-01", label: toLabel(film202601), film: film202601 },
];

const runFilm202601 = loadRunFilm(run202601);
const runFilm202603 = loadRunFilm(run202603);
const runFilm202604 = loadRunFilm(run202604);

function runLabel(film: RunFilm): string {
  return `${film.meta.world} ${film.meta.period}`;
}

/** Run films ordered with the judgment story (2026-01 partial refunds) first. */
export const runSamples: RunSample[] = [
  { id: "dtc-2026-01", label: runLabel(runFilm202601), film: runFilm202601 },
  { id: "dtc-2026-03", label: runLabel(runFilm202603), film: runFilm202603 },
  { id: "dtc-2026-04", label: runLabel(runFilm202604), film: runFilm202604 },
];

/** The cash-on-delivery / RTO story (2026-02) — the courier-rail investigation. */
export const rtoFilm: RtoFilm = loadRtoFilm(rto202602);
