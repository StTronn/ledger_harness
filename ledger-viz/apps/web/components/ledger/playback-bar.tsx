"use client";

import * as React from "react";
import { Pause, Play, SkipBack, SkipForward } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";

export interface PlaybackBarProps {
  /** Number of steps (N). Playhead domain is [0 .. length]. */
  length: number;
  playhead: number;
  onSeek: (i: number) => void;
  playing: boolean;
  onTogglePlay: () => void;
  /** Label of the current step (empty at the opening frame). */
  label?: string;
}

/**
 * Transport controls: skip-to-start, play/pause, skip-to-end, a scrubber,
 * and a "step k / N" readout with the active step's label.
 */
export function PlaybackBar({
  length,
  playhead,
  onSeek,
  playing,
  onTogglePlay,
  label,
}: PlaybackBarProps) {
  const atStart = playhead <= 0;
  const atEnd = playhead >= length;

  return (
    <div className="flex flex-col gap-3.5 rounded-lg border border-border bg-card p-4">
      <div className="flex items-center gap-2.5">
        <Button
          variant="outline"
          size="icon"
          onClick={() => onSeek(0)}
          disabled={atStart}
          aria-label="Skip to start"
        >
          <SkipBack />
        </Button>
        <Button
          size="icon"
          onClick={onTogglePlay}
          disabled={atEnd && !playing}
          aria-label={playing ? "Pause" : "Play"}
        >
          {playing ? <Pause /> : <Play />}
        </Button>
        <Button
          variant="outline"
          size="icon"
          onClick={() => onSeek(length)}
          disabled={atEnd}
          aria-label="Skip to end"
        >
          <SkipForward />
        </Button>

        <div className="flex-1 px-2">
          <Slider
            value={playhead}
            min={0}
            max={length}
            step={1}
            onValueChange={onSeek}
            aria-label="Playhead"
          />
        </div>

        <div className="shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">
          <span className="text-foreground">
            {String(playhead).padStart(2, "0")}
          </span>{" "}
          / {String(length).padStart(2, "0")}
        </div>
      </div>

      <div className="min-h-4 truncate font-mono text-[11px] text-muted-foreground">
        {playhead === 0 ? "Opening balances" : (label ?? "")}
      </div>
    </div>
  );
}
