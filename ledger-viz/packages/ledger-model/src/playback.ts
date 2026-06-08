import { buildFrames } from "./frames";
import type { Frame, LedgerFilm } from "./types";

/**
 * Playback drives a movable playhead across a film's frames.
 *
 * The index domain is [0 .. steps.length]:
 *  - 0      => opening frame (frames[0])
 *  - i >= 1 => state after steps[i-1] (frames[i])
 *
 * frameAt(i) clamps the index and maps 1:1 to frames[i].
 */
export class Playback {
  readonly frames: Frame[];
  readonly length: number;

  constructor(film: LedgerFilm) {
    this.frames = buildFrames(film);
    this.length = this.frames.length;
  }

  clamp(i: number): number {
    const max = this.last();
    if (Number.isNaN(i)) return this.first();
    if (i < 0) return 0;
    if (i > max) return max;
    return Math.floor(i);
  }

  frameAt(i: number): Frame {
    return this.frames[this.clamp(i)]!;
  }

  next(i: number): number {
    return this.clamp(i + 1);
  }

  prev(i: number): number {
    return this.clamp(i - 1);
  }

  first(): number {
    return 0;
  }

  last(): number {
    return this.length - 1;
  }
}
