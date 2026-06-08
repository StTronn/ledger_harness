"use client";

import * as React from "react";
import type { LedgerFilm } from "@ledger-viz/model";
import { cn } from "@/lib/utils";

export interface FilmOption {
  id: string;
  label: string;
  film: LedgerFilm;
}

export interface FilmPickerProps {
  films: FilmOption[];
  value: string;
  onChange: (id: string) => void;
}

/** Segmented control to switch between sample films. */
export function FilmPicker({ films, value, onChange }: FilmPickerProps) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      <span className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
        Sample
      </span>
      <div
        role="tablist"
        aria-label="Sample ledger"
        className="inline-flex items-center gap-0.5 border border-border bg-muted/40 p-0.5"
      >
        {films.map((f) => {
          const active = f.id === value;
          return (
            <button
              key={f.id}
              type="button"
              role="tab"
              aria-selected={active}
              onClick={() => onChange(f.id)}
              className={cn(
                "rounded-none px-3 py-1.5 text-[13px] font-medium outline-none transition-colors",
                "focus-visible:ring-2 focus-visible:ring-ring/60",
                active
                  ? "bg-card text-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {f.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}
