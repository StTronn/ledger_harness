// split.ts — the canonical inclusive-GST split, the ONE arithmetic the §8 agent
// owns. It MUST be byte-for-byte identical to the Go seeder/rule-engine formula
// (internal/gstsplit.SplitInclusive): the agent only ever recovers the *rate*;
// the paise split itself is mechanical and must agree with Go to the paise, or
// the orchestrator's balance-or-reject ledger will refuse the entry.
//
// Given an integer-paise `gross` and an integer percentage `ratePercent` (>0):
//   net = floor(gross * 100 / (100 + ratePercent))   // truncate toward zero
//   gst = gross - net                                  // remainder folds into GST
//
// gross*100 stays well within JS's 2^53 safe-integer range for these magnitudes
// (lakhs of paise), so Number arithmetic is exact here.

export interface Split {
  net: number;
  gst: number;
}

export function split(gross: number, ratePercent: number): Split {
  if (!Number.isInteger(gross)) {
    throw new Error(`split: gross must be integer paise, got ${gross}`);
  }
  if (!Number.isInteger(ratePercent) || ratePercent <= 0) {
    throw new Error(`split: ratePercent must be a positive integer, got ${ratePercent}`);
  }
  const net = Math.floor((gross * 100) / (100 + ratePercent));
  const gst = gross - net;
  return { net, gst };
}
