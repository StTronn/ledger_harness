import type { FilmMeta } from "./types";

/**
 * Format an integer minor-unit amount for display.
 *
 * The sign (for negative amounts) is placed BEFORE the currency symbol.
 * Thousands separators are applied to the major part. The number of
 * decimal places is derived from minorPerMajor (e.g. 100 => 2 decimals,
 * 1000 => 3, 1 => 0).
 *
 * e.g. formatMoney(-278114, { symbol: "₹", minorPerMajor: 100 }) => "-₹2,781.14"
 */
export function formatMoney(
  minor: number,
  meta: Pick<FilmMeta, "symbol" | "minorPerMajor">,
): string {
  const minorPerMajor = meta.minorPerMajor > 0 ? meta.minorPerMajor : 1;
  const decimals = Math.max(0, Math.round(Math.log10(minorPerMajor)));

  const negative = minor < 0;
  const abs = Math.abs(minor);

  const majorPart = Math.floor(abs / minorPerMajor);
  const minorPart = abs % minorPerMajor;

  const majorStr = majorPart.toLocaleString("en-US");

  let numberStr = majorStr;
  if (decimals > 0) {
    const fractional = String(minorPart).padStart(decimals, "0");
    numberStr = `${majorStr}.${fractional}`;
  }

  return `${negative ? "-" : ""}${meta.symbol}${numberStr}`;
}
