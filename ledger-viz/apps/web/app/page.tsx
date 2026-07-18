import { article } from "@/lib/explainer";
import { SectionView } from "@/components/learn/article";
import { SiteHeader } from "@/components/site-header";

export default function Page() {
  return (
    <div className="min-h-screen">
      <SiteHeader />

      <main className="mx-auto w-full max-w-[760px] space-y-12 px-5 py-10 sm:px-8 sm:py-14">
        <div className="max-w-2xl space-y-3">
          <p className="flex items-center gap-2 font-mono text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
            <span aria-hidden className="h-px w-6 bg-brand" />
            How it works
          </p>
          <h1 className="text-balance text-3xl font-semibold tracking-tight sm:text-4xl">
            {article.title}
          </h1>
          <p className="text-pretty text-[15px] leading-relaxed text-muted-foreground">
            {article.lede}
          </p>
        </div>

        <nav className="flex flex-wrap gap-x-4 gap-y-1.5 border-y border-border/70 py-3 font-mono text-[11px] uppercase tracking-[0.14em]">
          {article.sections.map((s) => (
            <a key={s.id} href={`#${s.id}`} className="text-muted-foreground transition-colors hover:text-foreground">
              {s.eyebrow}
            </a>
          ))}
        </nav>

        <div className="space-y-12">
          {article.sections.map((s) => (
            <SectionView key={s.id} section={s} />
          ))}
        </div>
      </main>
    </div>
  );
}
