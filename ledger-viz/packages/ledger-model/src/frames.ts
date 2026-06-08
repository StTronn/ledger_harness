import type { Frame, LedgerFilm, Posting } from "./types";

/** Debit-positive convention: Dr => +amount, Cr => -amount. */
export function signedDelta(p: Posting): number {
  return p.side === "Dr" ? p.amount : -p.amount;
}

/**
 * Build the frame timeline for a film.
 *
 * Length = steps.length + 1.
 *  - frames[0] is the opening frame: index -1, step null, all balances 0,
 *    empty deltas.
 *  - frames[i] for i >= 1 is the state AFTER steps[i-1]: index i-1,
 *    step steps[i-1], balances = cumulative signedDelta over steps[0..i-1],
 *    deltas = signedDelta per account for steps[i-1] only.
 */
export function buildFrames(film: LedgerFilm): Frame[] {
  const frames: Frame[] = [];

  const opening: Frame = {
    index: -1,
    step: null,
    balances: {},
    deltas: {},
  };
  for (const account of film.accounts) {
    opening.balances[account.path] = 0;
  }
  frames.push(opening);

  const running: Record<string, number> = { ...opening.balances };

  film.steps.forEach((step, i) => {
    const deltas: Record<string, number> = {};
    for (const posting of step.postings) {
      const d = signedDelta(posting);
      deltas[posting.account] = (deltas[posting.account] ?? 0) + d;
      running[posting.account] = (running[posting.account] ?? 0) + d;
    }
    frames.push({
      index: i,
      step,
      balances: { ...running },
      deltas,
    });
  });

  return frames;
}
