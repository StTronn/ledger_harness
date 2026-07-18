import { samples } from "@ledger-viz/model/fixtures";
import { LedgerViewer } from "@/components/ledger/ledger-viewer";
import { SiteHeader } from "@/components/site-header";

export default function LedgerPage() {
  return (
    <div className="min-h-screen">
      <SiteHeader />

      <main className="mx-auto w-full max-w-[1320px] space-y-8 px-5 py-10 sm:px-8 sm:py-14">
        <div className="max-w-2xl space-y-3">
          <p className="flex items-center gap-2 font-mono text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
            <span aria-hidden className="h-px w-6 bg-brand" />
            Double-entry ledger
          </p>
          <h1 className="text-balance text-3xl font-semibold tracking-tight sm:text-4xl">
            The ledger
          </h1>
          <p className="text-pretty text-[15px] leading-relaxed text-muted-foreground">
            Rows are transactions, columns are accounts grouped by type. Scrub
            the playhead to replay each posting and watch the signed Dr/Cr
            deltas accumulate into running balances.
          </p>
        </div>

        <LedgerViewer films={samples} />
      </main>
    </div>
  );
}
