import type { ReactNode } from "react";
import { ArrowDown, ArrowRight, Check, FileSearch, GitBranch, RefreshCw, Scale, ShieldAlert, Workflow } from "lucide-react";
import type { LucideIcon } from "lucide-react";

function FlowNode({
  label,
  detail,
  icon: Icon,
  tone = "neutral",
}: {
  label: string;
  detail: string;
  icon: LucideIcon;
  tone?: "neutral" | "brand" | "amber";
}) {
  const tones = {
    neutral: "border-border bg-card",
    brand: "border-brand/50 bg-brand-soft",
    amber: "border-amber/50 bg-amber-soft",
  };

  return (
    <div className={`min-w-0 border px-3.5 py-3 ${tones[tone]}`}>
      <div className="flex items-start gap-2.5">
        <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
        <div className="min-w-0">
          <p className="text-[13px] font-semibold leading-tight">{label}</p>
          <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">{detail}</p>
        </div>
      </div>
    </div>
  );
}

function BranchLabel({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-2 font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
      <span className="h-px w-4 bg-border" aria-hidden />
      {children}
    </div>
  );
}

function DownArrow() {
  return <ArrowDown className="size-4 text-muted-foreground" aria-hidden />;
}

function RightArrow() {
  return <ArrowRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />;
}

export function FlowDiagram() {
  return (
    <div className="border border-border bg-muted/20 p-4 sm:p-5" aria-label="ledger-flow decision path">
      <div className="flex flex-col items-center gap-2">
        <FlowNode label="Incoming event" detail="Razorpay payment, refund, settlement, or COD event" icon={FileSearch} />
        <DownArrow />
        <FlowNode label="Posting engine" detail="Apply a known accounting rule" icon={Workflow} tone="brand" />
      </div>

      <div className="my-5 grid gap-4 border-y border-border py-5 sm:grid-cols-2 sm:gap-8">
        <div className="flex items-center justify-center gap-3">
          <BranchLabel>Known rule</BranchLabel>
          <RightArrow />
          <FlowNode label="Double-entry ledger" detail="Validate and record" icon={Scale} tone="brand" />
        </div>
        <div className="flex items-center justify-center gap-3">
          <BranchLabel>No rule</BranchLabel>
          <RightArrow />
          <FlowNode label="Recovery engine" detail="Find and validate missing facts" icon={FileSearch} />
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 sm:gap-8">
        <section className="border border-border/70 p-3.5" aria-labelledby="safe-candidate-path">
          <BranchLabel>
            <span id="safe-candidate-path">Safe candidate</span>
          </BranchLabel>
          <div className="mt-3 flex flex-col items-center gap-2">
            <DownArrow />
            <FlowNode label="Posting engine" detail="Apply recovered template and parameters" icon={Check} tone="brand" />
            <DownArrow />
            <FlowNode label="Double-entry ledger" detail="Balance or reject" icon={Scale} tone="brand" />
          </div>
        </section>

        <section className="border border-border/70 p-3.5" aria-labelledby="judgment-path">
          <BranchLabel>
            <span id="judgment-path">No safe candidate</span>
          </BranchLabel>
          <div className="mt-3 flex flex-col items-center gap-2">
            <DownArrow />
            <FlowNode label="Judgment agent" detail="Review prepared evidence" icon={GitBranch} tone="amber" />
            <DownArrow />
            <FlowNode label="Review or escalation" detail="Recommendation recorded; no automatic post" icon={ShieldAlert} tone="amber" />
          </div>
        </section>
      </div>

      <div className="mt-5 border-t border-border pt-5">
        <div className="flex flex-col items-center gap-2 sm:flex-row sm:justify-center sm:gap-3">
          <FlowNode label="Reconciliation check" detail="Compare the ledger with settlements and bank data" icon={RefreshCw} />
          <RightArrow />
          <div className="flex items-center gap-2">
            <span className="border border-brand/50 bg-brand-soft px-2.5 py-2 font-mono text-[10px] uppercase tracking-[0.12em] text-brand-foreground dark:text-brand">clean</span>
            <span className="border border-amber/50 bg-amber-soft px-2.5 py-2 font-mono text-[10px] uppercase tracking-[0.12em] text-foreground">break → recovery</span>
          </div>
        </div>
      </div>

      <p className="mt-5 border-t border-border pt-4 text-center font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
        Only validated deterministic entries reach the ledger
      </p>
    </div>
  );
}

export function EvidencePathDiagram() {
  const paths = [
    { label: "Missing GST", source: "payment → order → GST rate" },
    { label: "Partial refund", source: "refund → order → line items" },
    { label: "RTO deduction", source: "deduction → shipment → rate card" },
  ];

  return (
    <div className="border border-border bg-muted/20 p-4 sm:p-5" aria-label="recovery engine evidence paths">
      <div className="grid gap-3 sm:grid-cols-[minmax(0,0.8fr)_auto_minmax(0,1.2fr)_auto_minmax(0,0.9fr)] sm:items-center">
        <div className="border border-border bg-card px-3.5 py-3 text-center">
          <p className="text-[13px] font-semibold">Known edge case</p>
          <p className="mt-1 text-[11px] text-muted-foreground">A defined recovery policy applies</p>
        </div>
        <ArrowRight className="mx-auto hidden size-4 text-muted-foreground sm:block" aria-hidden />

        <div className="space-y-2 border border-border/70 p-3">
          {paths.map((path) => (
            <div key={path.label} className="flex items-baseline justify-between gap-3 border-b border-border/70 pb-2 last:border-0 last:pb-0">
              <span className="text-[12px] font-medium">{path.label}</span>
              <span className="text-right font-mono text-[10px] text-muted-foreground">{path.source}</span>
            </div>
          ))}
        </div>

        <ArrowRight className="mx-auto hidden size-4 text-muted-foreground sm:block" aria-hidden />
        <div className="border border-border bg-card px-3.5 py-3 text-center">
          <p className="text-[13px] font-semibold">Policy check</p>
          <p className="mt-1 text-[11px] text-muted-foreground">Validate source and meaning</p>
        </div>
      </div>

      <div className="mt-4 grid gap-3 border-t border-border pt-4 sm:grid-cols-2">
        <div className="border border-brand/50 bg-brand-soft px-3.5 py-3">
          <div className="flex items-center gap-2">
            <Check className="size-4 text-brand-foreground dark:text-brand" aria-hidden />
            <p className="text-[13px] font-semibold">Safe candidate</p>
          </div>
          <p className="mt-1 pl-6 text-[11px] leading-relaxed text-muted-foreground">Return to the posting engine.</p>
        </div>
        <div className="border border-amber/50 bg-amber-soft px-3.5 py-3">
          <div className="flex items-center gap-2">
            <ShieldAlert className="size-4 text-muted-foreground" aria-hidden />
            <p className="text-[13px] font-semibold">Needs judgment</p>
          </div>
          <p className="mt-1 pl-6 text-[11px] leading-relaxed text-muted-foreground">Send prepared evidence to the agent.</p>
        </div>
      </div>
    </div>
  );
}

export function GstPolicyDiagram() {
  return (
    <div className="border border-border bg-muted/20 p-4 sm:p-5" aria-label="GST recovery policy path">
      <div className="flex items-center gap-2 font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
        <span className="h-px w-4 bg-border" aria-hidden />
        GST recovery policy
      </div>

      <div className="mt-4 grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)_auto_minmax(0,1.15fr)] sm:items-center">
        <FlowNode label="Payment miss" detail="gst_rate is empty" icon={FileSearch} />
        <ArrowRight className="mx-auto hidden size-4 text-muted-foreground sm:block" aria-hidden />
        <FlowNode label="Order source" detail="Read notes.gst_rate = 18" icon={FileSearch} />
        <ArrowRight className="mx-auto hidden size-4 text-muted-foreground sm:block" aria-hidden />
        <FlowNode label="Policy check" detail="18 is in the allowed set {5, 12, 18}" icon={Check} tone="brand" />
      </div>

      <div className="mt-4 grid gap-3 border-t border-border pt-4 sm:grid-cols-[minmax(0,1.35fr)_minmax(0,0.85fr)]">
        <div className="border border-brand/50 bg-brand-soft p-3.5">
          <BranchLabel>Validated safe candidate</BranchLabel>
          <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <FlowNode label="Posting engine" detail="Split sale with GST at 18%" icon={Workflow} tone="brand" />
            <RightArrow />
            <FlowNode label="Double-entry ledger" detail="Validate balance and record" icon={Scale} tone="brand" />
          </div>
        </div>

        <div className="border border-dashed border-border bg-card p-3.5">
          <BranchLabel>Agent path</BranchLabel>
          <div className="mt-3 flex items-start gap-2.5">
            <GitBranch className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
            <div>
              <p className="text-[13px] font-semibold">Judgment agent skipped</p>
              <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
                The cited rate passes policy, so no judgment is needed.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export function PartialRefundPolicyDiagram() {
  return (
    <div className="border border-border bg-muted/20 p-4 sm:p-5" aria-label="partial refund recovery policy path">
      <div className="flex items-center gap-2 font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
        <span className="h-px w-4 bg-border" aria-hidden />
        Partial-refund policy
      </div>

      <div className="mt-4 grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)_auto_minmax(0,1.15fr)] sm:items-center">
        <FlowNode label="Refund miss" detail="Partial refund has no complete posting rule" icon={FileSearch} />
        <ArrowRight className="mx-auto hidden size-4 text-muted-foreground sm:block" aria-hidden />
        <FlowNode label="Linked evidence" detail="Payment, order, items, and amounts" icon={FileSearch} />
        <ArrowRight className="mx-auto hidden size-4 text-muted-foreground sm:block" aria-hidden />
        <FlowNode label="Policy check" detail="Evidence does not prove which treatment to reverse" icon={ShieldAlert} tone="amber" />
      </div>

      <div className="mt-4 grid gap-3 border-t border-border pt-4 sm:grid-cols-[minmax(0,1.35fr)_minmax(0,0.85fr)]">
        <div className="border border-amber/50 bg-amber-soft p-3.5">
          <BranchLabel>Review required</BranchLabel>
          <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <FlowNode label="Harness context" detail="Prepared facts, citations, and booked state" icon={FileSearch} tone="amber" />
            <RightArrow />
            <FlowNode label="Judgment agent" detail="Recommend a treatment or escalate" icon={GitBranch} tone="amber" />
          </div>
        </div>

        <div className="border border-dashed border-border bg-card p-3.5">
          <BranchLabel>Posting path</BranchLabel>
          <div className="mt-3 flex items-start gap-2.5">
            <ShieldAlert className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
            <div>
              <p className="text-[13px] font-semibold">Ledger unchanged</p>
              <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
                No safe candidate means no automatic posting.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function SchemaRow({ label, value, source }: { label: string; value: string; source?: string }) {
  return (
    <div className="border-b border-border/70 py-2 last:border-0">
      <div className="flex items-start justify-between gap-3">
        <span className="font-mono text-[10px] text-muted-foreground">{label}</span>
        <span className="text-right text-[11px] leading-relaxed">{value}</span>
      </div>
      {source ? <p className="mt-1 text-right font-mono text-[9px] text-brand-foreground dark:text-brand">source: {source}</p> : null}
    </div>
  );
}

function ContextCard({
  title,
  detail,
  rows,
}: {
  title: string;
  detail: string;
  rows: ReactNode;
}) {
  return (
    <section className="border border-border bg-card" aria-label={`${title} context`}>
      <div className="border-b border-border bg-muted/30 px-3.5 py-3">
        <p className="text-[13px] font-semibold">{title}</p>
        <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">{detail}</p>
      </div>
      <div className="px-3.5">{rows}</div>
    </section>
  );
}

export function HarnessContextDiagram() {
  return (
    <div className="space-y-3 border border-border bg-muted/20 p-4 sm:p-5" aria-label="harness context schemas">
      <div className="flex items-center gap-2 font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
        <span className="h-px w-4 bg-border" aria-hidden />
        Read-only context from the recovery engine
      </div>

      <div className="grid gap-3 lg:grid-cols-3">
        <ContextCard
          title="Event context"
          detail="Why did this event miss a posting rule?"
          rows={
            <>
              <SchemaRow label="event" value="id · type · amount · links" />
              <SchemaRow label="recovered" value="GST rate · policy" source="order.notes.gst_rate" />
              <SchemaRow label="state" value="booked · entry types" source="ledger IK" />
            </>
          }
        />
        <ContextCard
          title="Break context"
          detail="What does the reconciliation mismatch involve?"
          rows={
            <>
              <SchemaRow label="break" value="check · expected · actual" source="reconcile check" />
              <SchemaRow label="batch" value="events · amounts · booked" source="settlement members" />
              <SchemaRow label="accounts" value="receivable balances" source="ledger account" />
            </>
          }
        />
        <ContextCard
          title="Entity lookup"
          detail="What else can be checked for a novel case?"
          rows={
            <>
              <SchemaRow label="order" value="items · GST rate" source="orders.json" />
              <SchemaRow label="deduction" value="code · shipment · amount" source="courier feed" />
              <SchemaRow label="rate card" value="fee · RTO · GST" source="ratecard.json" />
            </>
          }
        />
      </div>

      <div className="flex items-center justify-center gap-2 border-t border-border pt-3 text-center font-mono text-[10px] uppercase tracking-[0.12em] text-muted-foreground">
        <span className="border border-brand/50 bg-brand-soft px-2.5 py-1.5 text-brand-foreground dark:text-brand">object + field citation</span>
        <span>→</span>
        <span>traceable review</span>
      </div>
    </div>
  );
}
