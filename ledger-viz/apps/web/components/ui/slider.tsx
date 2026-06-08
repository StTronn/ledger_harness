"use client";

import * as React from "react";
import { cn } from "@/lib/utils";

export interface SliderProps
  extends Omit<
    React.InputHTMLAttributes<HTMLInputElement>,
    "value" | "onChange" | "type"
  > {
  value: number;
  onValueChange?: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
}

const Slider = React.forwardRef<HTMLInputElement, SliderProps>(
  (
    { className, value, onValueChange, min = 0, max = 100, step = 1, ...props },
    ref,
  ) => {
    const range = max - min || 1;
    const pct = ((value - min) / range) * 100;
    return (
      <input
        ref={ref}
        type="range"
        value={value}
        min={min}
        max={max}
        step={step}
        onChange={(e) => onValueChange?.(Number(e.target.value))}
        style={{
          background: `linear-gradient(to right, var(--foreground) 0%, var(--foreground) ${pct}%, var(--border) ${pct}%, var(--border) 100%)`,
        }}
        data-slot="slider"
        className={cn(
          "h-1 w-full cursor-pointer appearance-none rounded-none outline-none transition-[background]",
          "focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-offset-2 focus-visible:ring-offset-background",
          // WebKit thumb — boxy (Lyra)
          "[&::-webkit-slider-thumb]:size-3.5 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-none [&::-webkit-slider-thumb]:border [&::-webkit-slider-thumb]:border-foreground/60 [&::-webkit-slider-thumb]:bg-foreground [&::-webkit-slider-thumb]:transition-transform [&::-webkit-slider-thumb]:hover:scale-110",
          // Firefox thumb — boxy (Lyra)
          "[&::-moz-range-thumb]:size-3.5 [&::-moz-range-thumb]:appearance-none [&::-moz-range-thumb]:rounded-none [&::-moz-range-thumb]:border [&::-moz-range-thumb]:border-foreground/60 [&::-moz-range-thumb]:bg-foreground",
          className,
        )}
        {...props}
      />
    );
  },
);
Slider.displayName = "Slider";

export { Slider };
