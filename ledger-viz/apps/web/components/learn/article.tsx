import * as React from "react";
import type { Block, Section } from "@/lib/explainer";
import {
  EvidencePathDiagram,
  FlowDiagram,
  GstPolicyDiagram,
  HarnessContextDiagram,
  PartialRefundPolicyDiagram,
  SelfImprovementEvidenceDiagram,
  SelfImprovementFlowDiagram,
} from "@/components/learn/flow-diagram";

/**
 * The Learn-page renderer: turns the explainer's data blocks into prose. Kept
 * deliberately small — one component per block kind — so the content stays in
 * lib/explainer.ts and this file rarely changes.
 */

function BlockView({ block }: { block: Block }) {
  switch (block.kind) {
    case "p":
      return (
        <p className="text-pretty text-[15px] leading-relaxed text-foreground/80">
          {block.text}
        </p>
      );
    case "note":
      return (
        <p className="w-full max-w-full break-words border-l-2 border-brand bg-brand-soft px-4 py-3 text-[14px] leading-relaxed text-foreground/85">
          {block.text}
        </p>
      );
    case "code":
      return (
        <pre className="overflow-x-auto border border-border bg-muted/40 px-4 py-3 font-mono text-[12.5px] leading-relaxed text-foreground/90">
          {block.text}
        </pre>
      );
    case "steps":
      return (
        <ol className="space-y-2">
          {block.items.map((it, i) => (
            <li key={i} className="flex gap-3 text-[15px] leading-relaxed text-foreground/80">
              <span className="mt-0.5 shrink-0 font-mono text-[12px] tabular-nums text-brand-foreground dark:text-brand">
                {String(i + 1).padStart(2, "0")}
              </span>
              <span>{it}</span>
            </li>
          ))}
        </ol>
      );
    case "flow":
      return <FlowDiagram />;
    case "evidence":
      return <EvidencePathDiagram />;
    case "context":
      return <HarnessContextDiagram />;
    case "gst-policy":
      return <GstPolicyDiagram />;
    case "partial-refund-policy":
      return <PartialRefundPolicyDiagram />;
    case "self-improvement-evidence":
      return <SelfImprovementEvidenceDiagram />;
    case "self-improvement-flow":
      return <SelfImprovementFlowDiagram />;
  }
}

export function SectionView({ section }: { section: Section }) {
  return (
    <section id={section.id} className="min-w-0 max-w-full scroll-mt-24 space-y-4">
      <div className="space-y-1.5">
        <p className="flex items-center gap-2 font-mono text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
          <span aria-hidden className="h-px w-6 bg-brand" />
          {section.eyebrow}
        </p>
        <h2 className="text-balance text-xl font-semibold tracking-tight sm:text-2xl">
          {section.title}
        </h2>
      </div>
      <div className="min-w-0 max-w-full space-y-4">
        {section.blocks.map((b, i) => (
          <BlockView key={i} block={b} />
        ))}
      </div>
    </section>
  );
}
