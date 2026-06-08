import type { LedgerFilm } from "./types";

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/**
 * Shallow runtime validation of a parsed JSON value into a LedgerFilm.
 * Throws on missing/invalid meta, accounts, or steps.
 */
export function loadFilm(json: unknown): LedgerFilm {
  if (!isObject(json)) {
    throw new Error("loadFilm: expected an object");
  }

  const { meta, accounts, steps } = json;

  if (!isObject(meta)) {
    throw new Error("loadFilm: missing or invalid 'meta'");
  }
  if (typeof meta.minorPerMajor !== "number") {
    throw new Error("loadFilm: meta.minorPerMajor must be a number");
  }
  if (typeof meta.symbol !== "string") {
    throw new Error("loadFilm: meta.symbol must be a string");
  }

  if (!Array.isArray(accounts)) {
    throw new Error("loadFilm: missing or invalid 'accounts'");
  }
  if (!Array.isArray(steps)) {
    throw new Error("loadFilm: missing or invalid 'steps'");
  }

  return json as unknown as LedgerFilm;
}
