"use client";

import * as React from "react";
import {
  buildRunFrames,
  type RunFilm,
  type ConsultStage,
} from "@ledger-viz/model";
import type { RunSample } from "@ledger-viz/model/fixtures";
import { PlaybackBar } from "@/components/ledger/playback-bar";
import { PipelineStrip } from "./pipeline-strip";
import { ExceptionRow } from "./exception-row";
import { DecisionCard } from "./decision-card";
import { cn } from "@/lib/utils";

const TICK_MS = 1100;

/** Human label for the beat the playhead is on. */
function frameLabel(film: RunFilm, active: number | null, stage: ConsultStage | null): string {
  if (active === null || stage === null) return "Rules-only baseline";
  const c = film.consultations[active];
  const what: Record<ConsultStage, string> = {
    flagged: "flagged",
    agent: `agent reading context for ${c.eventId}`,
    decision:
      c.status === "recommended" && c.entryType
        ? `agent proposes ${c.entryType}`
        : "agent declines — escalates",
    reviewed: `recommendation logged — no posting`,
    escalated: `escalated to a human`,
  };
  return what[stage];
}

/**
 * The Close Run — playback of the review story. Pure function of (film, index):
 * the pipeline counters, each exception's stage, and the score all derive from
 * the current frame; the UI holds a single number.
 */
export function RunViewer({ films }: { films: RunSample[] }) {
  const [selectedId, setSelectedId] = React.useState(films[0]?.id ?? "");
  const [playhead, setPlayhead] = React.useState(0);
  const [playing, setPlaying] = React.useState(false);
  const [selected, setSelected] = React.useState(0);

  const sample = React.useMemo(
    () => films.find((f) => f.id === selectedId) ?? films[0],
    [films, selectedId],
  );
  const film = sample.film;

  const frames = React.useMemo(() => buildRunFrames(film), [film]);
  const length = frames.length - 1;
  const clamped = Math.min(Math.max(playhead, 0), length);
  const frame = frames[clamped];

  // Follow the animation: while playing, keep the active consultation selected.
  React.useEffect(() => {
    if (frame.active !== null) setSelected(frame.active);
  }, [frame.active]);

  React.useEffect(() => {
    // Switching periods restarts the story.
    setPlayhead(0);
    setSelected(0);
    setPlaying(false);
  }, [selectedId]);

  React.useEffect(() => {
    if (!playing) return;
    const t = setInterval(() => {
      setPlayhead((p) => {
        if (p >= length) {
          setPlaying(false);
          return p;
        }
        return p + 1;
      });
    }, TICK_MS);
    return () => clearInterval(t);
  }, [playing, length]);

  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowRight") setPlayhead((p) => Math.min(p + 1, length));
      else if (e.key === "ArrowLeft") setPlayhead((p) => Math.max(p - 1, 0));
      else if (e.key === " ") {
        e.preventDefault();
        setPlaying((v) => !v);
      } else return;
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [length]);

  const current = film.consultations[selected] ?? film.consultations[0];

  return (
    <div className="space-y-4">
      {/* period picker */}
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
          Period
        </span>
        <div
          role="tablist"
          aria-label="Close run"
          className="inline-flex items-center gap-0.5 border border-border bg-muted/50 p-0.5"
        >
          {films.map((f) => {
            const active = f.id === selectedId;
            const story = f.film.meta.title;
            return (
              <button
                key={f.id}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setSelectedId(f.id)}
                className={cn(
                  "px-3 py-1.5 text-[13px] font-medium outline-none transition-colors",
                  "focus-visible:ring-2 focus-visible:ring-ring/60",
                  active
                    ? "bg-card text-foreground ring-1 ring-border"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {f.label}
                <span className="ml-1.5 font-mono text-[10.5px] uppercase tracking-wide text-muted-foreground">
                  {story}
                </span>
              </button>
            );
          })}
        </div>
      </div>

      <PipelineStrip film={film} frame={frame} />

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_360px]">
        <div className="space-y-4">
          {/* exceptions lane */}
          <div className="border border-border bg-card">
            <div className="flex items-baseline justify-between border-b border-border px-4 py-2.5">
              <span className="font-mono text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
                Exceptions lane
              </span>
              <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
                {frame.reviewed}/{film.consultations.length} reviewed · score{" "}
                <span
                  className={cn(
                    "font-semibold",
                      frame.reviewed > 0 &&
                      "bg-brand-soft px-1 text-brand-foreground dark:text-brand",
                  )}
                >
                  {frame.scorePct}%
                </span>
              </span>
            </div>
            <div>
              {film.consultations.map((c, i) => (
                <ExceptionRow
                  key={c.id}
                  consultation={c}
                  meta={film.meta}
                  stage={frame.stages[i]}
                  active={frame.active === i}
                  selected={selected === i}
                  onSelect={() => setSelected(i)}
                />
              ))}
            </div>
          </div>

          <PlaybackBar
            length={length}
            playhead={clamped}
            onSeek={(i) => setPlayhead(i)}
            playing={playing}
            onTogglePlay={() => setPlaying((v) => !v)}
            label={frameLabel(film, frame.active, frame.activeStage)}
          />
        </div>

        {/* decision card for the selected exception */}
        {current ? (
          <DecisionCard
            consultation={current}
            meta={film.meta}
            stage={frame.stages[selected] ?? "flagged"}
          />
        ) : null}
      </div>
    </div>
  );
}
