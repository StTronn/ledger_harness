"use client";

import * as React from "react";
import {
  formatMoney,
  signedDelta,
  type FilmMeta,
  type Step,
} from "@ledger-viz/model";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";

export interface StepInspectorProps {
  step: Step | null;
  meta: FilmMeta;
}

/** Detail panel for the active step: Dr/Cr posting lines + a balanced check. */
export function StepInspector({ step, meta }: StepInspectorProps) {
  if (!step) {
    return (
      <Card className="lg:sticky lg:top-20">
        <CardHeader>
          <CardTitle className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
            Inspector
          </CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          No step selected. Scrub or press play to post a transaction.
        </CardContent>
      </Card>
    );
  }

  const net = step.postings.reduce((sum, p) => sum + signedDelta(p), 0);
  const balanced = net === 0;
  const tag = step.entryType ?? step.kind;

  return (
    <Card className="lg:sticky lg:top-20">
      <CardHeader className="gap-2">
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="truncate font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
            Inspector
          </CardTitle>
          <Badge variant="outline" className="shrink-0 font-mono font-normal">
            {tag}
          </Badge>
        </div>
        <p className="truncate text-[15px] font-medium tracking-tight text-foreground">
          {step.label}
        </p>
        {step.sublabel && (
          <p className="text-xs text-muted-foreground">{step.sublabel}</p>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        <ul className="space-y-2">
          {step.postings.map((p, i) => (
            <li
              key={`${p.account}-${p.side}-${i}`}
              className="flex items-center justify-between gap-3 text-sm"
            >
              <span className="flex min-w-0 items-center gap-2">
                <Badge
                  variant={p.side === "Dr" ? "default" : "secondary"}
                  className="w-7 shrink-0 justify-center font-mono"
                >
                  {p.side}
                </Badge>
                <span className="truncate font-mono text-[12px] text-muted-foreground">
                  {p.account}
                </span>
              </span>
              <span className="shrink-0 font-mono text-[12px] tabular-nums">
                {formatMoney(p.amount, meta)}
              </span>
            </li>
          ))}
        </ul>

        <Separator />

        <div
          className={cn(
            "flex items-center justify-between text-xs font-medium",
            balanced ? "text-muted-foreground" : "text-destructive",
          )}
        >
          <span className="font-mono uppercase tracking-[0.14em]">
            {balanced ? "Balanced" : "Unbalanced"}
          </span>
          <span className="font-mono tabular-nums">
            {balanced ? "= 0" : formatMoney(net, meta)}
          </span>
        </div>
      </CardContent>
    </Card>
  );
}
