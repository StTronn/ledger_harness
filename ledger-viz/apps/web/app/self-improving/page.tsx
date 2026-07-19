import { SectionView } from "@/components/learn/article";
import { SiteHeader } from "@/components/site-header";
import { selfImprovingArticle } from "@/lib/self-improving";

export default function SelfImprovingPage() {
  return (
    <div className="min-h-screen overflow-x-clip">
      <SiteHeader />

      <main className="mx-auto w-full min-w-0 max-w-[760px] space-y-12 px-5 py-10 sm:px-8 sm:py-14">
        <div className="max-w-2xl space-y-3">
          <p className="flex items-center gap-2 font-mono text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
            <span aria-hidden className="h-px w-6 bg-brand" />
            How it learns
          </p>
          <h1 className="text-balance text-3xl font-semibold tracking-tight sm:text-4xl">
            {selfImprovingArticle.title}
          </h1>
          <p className="text-pretty text-[15px] leading-relaxed text-muted-foreground">
            {selfImprovingArticle.lede}
          </p>
        </div>

        <nav className="flex flex-wrap gap-x-4 gap-y-1.5 border-y border-border/70 py-3 font-mono text-[11px] uppercase tracking-[0.14em]">
          {selfImprovingArticle.sections.map((section) => (
            <a
              key={section.id}
              href={`#${section.id}`}
              className="text-muted-foreground transition-colors hover:text-foreground"
            >
              {section.eyebrow}
            </a>
          ))}
        </nav>

        <div className="min-w-0 space-y-12">
          {selfImprovingArticle.sections.map((section) => (
            <SectionView key={section.id} section={section} />
          ))}
        </div>
      </main>
    </div>
  );
}
