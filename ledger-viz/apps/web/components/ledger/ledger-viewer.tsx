"use client";

import * as React from "react";
import { buildFrames, type LedgerFilm } from "@ledger-viz/model";
import { FilmPicker, type FilmOption } from "./film-picker";
import { LedgerGrid } from "./ledger-grid";
import { PlaybackBar } from "./playback-bar";
import { StepInspector } from "./step-inspector";

export interface LedgerViewerProps {
  films: FilmOption[];
}

const TICK_MS = 900;

/** Stateful shell tying together the film picker, matrix grid, transport, and inspector. */
export function LedgerViewer({ films }: LedgerViewerProps) {
  const [selectedId, setSelectedId] = React.useState(films[0]?.id ?? "");
  // Start on the first transaction so a "current" row is highlighted on load.
  const [playhead, setPlayhead] = React.useState(1);
  const [playing, setPlaying] = React.useState(false);

  const selected = React.useMemo(
    () => films.find((f) => f.id === selectedId) ?? films[0],
    [films, selectedId],
  );
  const film: LedgerFilm = selected.film;

  const frames = React.useMemo(() => buildFrames(film), [film]);
  const length = film.steps.length;

  const clampedPlayhead = Math.min(Math.max(playhead, 0), length);
  const currentFrame = frames[clampedPlayhead];
  const currentStep = currentFrame?.step ?? null;

  const seek = React.useCallback(
    (i: number) => setPlayhead(Math.min(Math.max(i, 0), length)),
    [length],
  );

  // Reset on film change (back to the first transaction, highlighted).
  React.useEffect(() => {
    setPlayhead(1);
    setPlaying(false);
  }, [selectedId]);

  // Auto-play.
  React.useEffect(() => {
    if (!playing) return;
    const timer = setInterval(() => {
      setPlayhead((p) => {
        if (p >= length) {
          setPlaying(false);
          return p;
        }
        return p + 1;
      });
    }, TICK_MS);
    return () => clearInterval(timer);
  }, [playing, length]);

  // Keyboard stepping.
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowRight") {
        e.preventDefault();
        seek(clampedPlayhead + 1);
      } else if (e.key === "ArrowLeft") {
        e.preventDefault();
        seek(clampedPlayhead - 1);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [seek, clampedPlayhead]);

  return (
    <div className="space-y-4">
      <FilmPicker films={films} value={selectedId} onChange={setSelectedId} />

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="space-y-4">
          <LedgerGrid film={film} frames={frames} playhead={clampedPlayhead} />
          <PlaybackBar
            length={length}
            playhead={clampedPlayhead}
            onSeek={seek}
            playing={playing}
            onTogglePlay={() => {
              if (clampedPlayhead >= length) seek(0);
              setPlaying((p) => !p);
            }}
            label={currentStep?.label}
          />
        </div>
        <StepInspector step={currentStep} meta={film.meta} />
      </div>
    </div>
  );
}
