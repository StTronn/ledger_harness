import { runSamples } from "@ledger-viz/model/fixtures";
import { RunViewer } from "@/components/run/run-viewer";
import { SiteHeader } from "@/components/site-header";

export default function RunPage() {
  return (
    <div className="min-h-screen">
      <SiteHeader />

      <main className="mx-auto w-full max-w-[1320px] space-y-8 px-5 py-10 sm:px-8 sm:py-14">
        <div className="max-w-2xl space-y-3">
          <p className="flex items-center gap-2 font-mono text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
            <span aria-hidden className="h-px w-6 bg-brand" />
            Worked run
          </p>
          <h1 className="text-balance text-3xl font-semibold tracking-tight sm:text-4xl">
            The ledger-flow run
          </h1>
          <p className="text-pretty text-[15px] leading-relaxed text-muted-foreground">
            The deterministic engine books what the rules cover; everything else
            falls into the review queue. Play the run to watch the agent read the
            recovery context, make a recommendation, and leave the ledger
            unchanged for explicit review.
          </p>
        </div>

        <RunViewer films={runSamples} />
      </main>
    </div>
  );
}
