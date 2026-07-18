"use client";

import * as React from "react";
import {
  formatMoney,
  type Consultation,
  type ConsultStage,
  type RunMeta,
} from "@ledger-viz/model";
import { cn } from "@/lib/utils";

function Section({
  label,
  children,
  className,
}: {
  label: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("space-y-1.5", className)}>
      <div className="font-mono text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
        {label}
      </div>
      {children}
    </div>
  );
}

function Fact({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 text-[12.5px]">
      <span className="text-muted-foreground">{k}</span>
      <span className="text-right font-mono tabular-nums text-foreground">
        {v}
      </span>
    </div>
  );
}

/**
 * One agent consultation, fully explained — the legibility view. The layout
 * makes the trust boundary visible: everything above the rule is the AGENT
 * (what it saw, what it recommended); the review outcome below records whether
 * the recommendation was logged or escalated. No agent recommendation posts.
 */
export function DecisionCard({
  consultation: c,
  meta,
  stage,
}: {
  consultation: Consultation;
  meta: RunMeta;
  stage: ConsultStage;
}) {
  const reached = (s: "agent" | "decision" | "terminal") => {
    const rank = { flagged: 0, agent: 1, decision: 2, reviewed: 3, escalated: 3 }[
      stage
    ];
    return rank >= { agent: 1, decision: 2, terminal: 3 }[s];
  };
  const unbooked = c.break?.batch.filter((m) => !m.booked) ?? [];
  const bookedCount = (c.break?.batch.length ?? 0) - unbooked.length;

  return (
    <div className="flex flex-col border border-border bg-card">
      <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
        <div className="min-w-0">
          <div className="font-mono text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
            {c.role} · {c.status === "recommended" ? "reviewed" : "escalated"}
          </div>
          <div className="truncate font-mono text-[13.5px] font-semibold tracking-tight">
            {c.eventId}
          </div>
        </div>
        <span className="shrink-0 border border-border px-1.5 py-0.5 font-mono text-[10.5px] uppercase tracking-wide text-muted-foreground">
          {c.eventType}
        </span>
      </div>

      <div className="space-y-5 px-4 py-4">
        <Section label="Why the rules missed">
          <p className="text-pretty text-[12.5px] leading-relaxed text-muted-foreground">
            {c.whyMissed}
          </p>
        </Section>

        <Section label="What the agent saw" className={cn(!reached("agent") && "opacity-40")}>
          <div className="space-y-1">
            <Fact k="amount" v={formatMoney(c.amount, meta)} />
            {c.orderId ? <Fact k="order" v={c.orderId} /> : null}
            {c.sku ? <Fact k="sku" v={c.sku} /> : null}
            {c.break ? (
              <>
                <Fact
                  k={c.break.account.split("/").pop() ?? c.break.account}
                  v={formatMoney(c.break.actual, meta)}
                />
                <Fact
                  k="batch"
                  v={`${bookedCount} booked · ${unbooked.length} unbooked`}
                />
              </>
            ) : null}
            {c.citation ? (
              <div className="flex items-baseline justify-between gap-3 text-[12.5px]">
                <span className="text-muted-foreground">
                  recovered {c.citation.field}
                </span>
                <span className="text-right">
                  <span className="bg-brand-soft px-1 font-mono font-semibold tabular-nums text-brand-foreground dark:text-brand">
                    {c.citation.value}%
                  </span>
                  <span
                    title={`re-read from the snapshot: ${c.citation.object} → ${c.citation.path}`}
                    className="ml-1.5 cursor-help border border-border px-1 py-px font-mono text-[10px] text-muted-foreground"
                  >
                    {c.citation.object.slice(0, 12)}…/{c.citation.path} ⓘ
                  </span>
                </span>
              </div>
            ) : null}
          </div>

          {/* the investigate batch: every member with a booked tick, the culprit highlighted */}
          {c.break && unbooked.length > 0 ? (
            <div className="mt-2 max-h-44 overflow-y-auto border border-border/70">
              {c.break.batch.map((m) => (
                <div
                  key={m.eventId}
                  className={cn(
                    "flex items-center justify-between gap-2 border-b border-border/50 px-2 py-1 font-mono text-[11px] last:border-b-0",
                    m.highlight
                      ? "bg-brand-soft text-foreground"
                      : "text-muted-foreground",
                  )}
                >
                  <span className="truncate">{m.eventId}</span>
                  <span className="flex shrink-0 items-center gap-2 tabular-nums">
                    {formatMoney(m.amount, meta)}
                    <span
                      className={cn(
                        "w-14 text-right",
                        m.booked
                          ? "text-muted-foreground/70"
                          : "font-semibold text-amber-700 dark:text-amber-400",
                      )}
                    >
                      {m.booked ? "booked ✓" : "unbooked"}
                    </span>
                  </span>
                </div>
              ))}
            </div>
          ) : null}
        </Section>

        <Section label="Tools used" className={cn(!reached("agent") && "opacity-40")}>
          <div className="flex flex-wrap gap-1.5">
            {c.toolsUsed.map((t) => (
              <span
                key={t}
                className="border border-border px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
              >
                {t} <span className="text-muted-foreground/60">(read-only)</span>
              </span>
            ))}
          </div>
        </Section>

        <Section label="Decision" className={cn(!reached("decision") && "opacity-40")}>
          {c.status === "recommended" && c.entryType ? (
            <>
              <div className="font-mono text-[13px] font-semibold tracking-tight">
                {c.entryType}
              </div>
              <div className="space-y-1">
                {Object.entries(c.params ?? {}).map(([k, v]) => (
                  <Fact key={k} k={k} v={formatMoney(v, meta)} />
                ))}
              </div>
            </>
          ) : (
            <div className="font-mono text-[12.5px] text-amber-700 dark:text-amber-400">
              escalated — hands off, never guesses
            </div>
          )}
          <p className="text-pretty border-l-2 border-border pl-2 text-[12px] italic leading-relaxed text-muted-foreground">
            “{c.rationale}”
          </p>
        </Section>
      </div>

      {/* REVIEW OUTCOME — the ledger is deliberately untouched by this path */}
      <div
        className={cn(
          "border-t-2 border-foreground/80 px-4 py-3.5",
          !reached("terminal") && "opacity-40",
        )}
      >
        <div className="flex items-center justify-between">
          <span className="font-mono text-[10.5px] uppercase tracking-[0.16em] text-muted-foreground">
            Review outcome
          </span>
          {reached("terminal") ? (
            c.status === "recommended" ? (
              <span className="bg-brand-soft px-1.5 py-0.5 font-mono text-[11px] font-semibold uppercase tracking-wide text-brand-foreground dark:text-brand">
                ✓ recommendation logged
              </span>
            ) : (
              <span className="bg-amber-500/10 px-1.5 py-0.5 font-mono text-[11px] font-semibold uppercase tracking-wide text-amber-700 dark:text-amber-400">
                escalated to a human
              </span>
            )
          ) : (
            <span className="font-mono text-[11px] text-muted-foreground/50">
              pending…
            </span>
          )}
        </div>

        {reached("terminal") ? (
          <p className="mt-2 font-mono text-[12px] leading-relaxed text-muted-foreground">
            The ledger is unchanged. Any posting requires a deterministic or explicitly approved path.
          </p>
        ) : null}
      </div>
    </div>
  );
}
