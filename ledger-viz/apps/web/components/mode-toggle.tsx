"use client";

import * as React from "react";
import { Monitor, Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { cn } from "@/lib/utils";

type Mode = "light" | "dark" | "system";

const ORDER: Mode[] = ["light", "dark", "system"];
const ICONS: Record<Mode, React.ComponentType<{ className?: string }>> = {
  light: Sun,
  dark: Moon,
  system: Monitor,
};
const LABEL: Record<Mode, string> = {
  light: "Light",
  dark: "Dark",
  system: "System",
};

/**
 * Compact three-way theme segmented control (light / dark / system) wired to
 * next-themes. Renders a stable skeleton until mounted to avoid hydration
 * mismatches.
 */
export function ModeToggle() {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = React.useState(false);
  React.useEffect(() => setMounted(true), []);

  const active = (mounted ? theme : "system") as Mode;

  return (
    <div
      role="radiogroup"
      aria-label="Theme"
      className="inline-flex items-center gap-0.5 border border-border bg-card p-0.5"
    >
      {ORDER.map((mode) => {
        const Icon = ICONS[mode];
        const selected = active === mode;
        return (
          <button
            key={mode}
            type="button"
            role="radio"
            aria-checked={selected}
            aria-label={LABEL[mode]}
            title={LABEL[mode]}
            onClick={() => setTheme(mode)}
            className={cn(
              "inline-flex size-7 items-center justify-center rounded-none text-muted-foreground transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              "hover:text-foreground",
              selected && "bg-secondary text-foreground",
            )}
          >
            <Icon className="size-3.5" />
          </button>
        );
      })}
    </div>
  );
}
