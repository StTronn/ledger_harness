import { samples } from "@ledger-viz/model/fixtures";
import { LedgerViewer } from "@/components/ledger/ledger-viewer";
import { ModeToggle } from "@/components/mode-toggle";

export default function Page() {
  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-40 border-b border-border/70 bg-background/80 backdrop-blur-md">
        <div className="mx-auto flex w-full max-w-[1320px] items-center justify-between gap-4 px-5 py-3.5 sm:px-8">
          <div className="flex items-center gap-3">
            <div
              aria-hidden
              className="grid size-7 place-items-center border border-border bg-foreground font-mono text-[13px] font-medium leading-none tracking-tight text-background"
            >
              L
            </div>
            <span className="font-mono text-[13px] font-medium tracking-tight">
              ledger<span className="text-muted-foreground">/viz</span>
            </span>
          </div>
          <ModeToggle />
        </div>
      </header>

      <main className="mx-auto w-full max-w-[1320px] space-y-8 px-5 py-10 sm:px-8 sm:py-14">
        <div className="max-w-2xl space-y-3">
          <p className="font-mono text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
            Double-entry ledger
          </p>
          <h1 className="text-balance text-3xl font-semibold tracking-tight sm:text-4xl">
            The transaction matrix
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
