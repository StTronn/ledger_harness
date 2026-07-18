import { rtoFilm } from "@ledger-viz/model/fixtures";
import { RtoView } from "@/components/rto/rto-view";
import { SiteHeader } from "@/components/site-header";

export default function RtoPage() {
  return (
    <div className="min-h-screen">
      <SiteHeader />

      <main className="mx-auto w-full max-w-[1320px] space-y-8 px-5 py-10 sm:px-8 sm:py-14">
        <div className="max-w-2xl space-y-3">
          <p className="flex items-center gap-2 font-mono text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
            <span aria-hidden className="h-px w-6 bg-brand" />
            The cash-on-delivery rail
          </p>
          <h1 className="text-balance text-3xl font-semibold tracking-tight sm:text-4xl">
            Return to origin
          </h1>
          <p className="text-pretty text-[15px] leading-relaxed text-muted-foreground">
            A third of COD orders bounce. The courier collects the cash at the
            door and wires it back in one netted batch — minus fees the rate card
            explains and deductions it doesn&rsquo;t. This is where the recovery engine
            prepares the evidence for review: it decomposes the payout, identifies
            the return fee it can verify, and hands a human the deduction it can&rsquo;t
            — never guessing or posting from the agent path.
          </p>
        </div>

        <RtoView film={rtoFilm} />
      </main>
    </div>
  );
}
