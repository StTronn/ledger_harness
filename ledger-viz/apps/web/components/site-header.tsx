"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ModeToggle } from "@/components/mode-toggle";
import { cn } from "@/lib/utils";

const TABS = [
  { href: "/", label: "Harness" },
  { href: "/self-improving", label: "Self-improving" },
  { href: "/ledger", label: "Ledger" },
  { href: "/run", label: "Example" },
  { href: "/rto", label: "RTO" },
] as const;

/**
 * Shared chrome for every view: the wordmark, the view tabs, and the theme
 * toggle. The tabs are the only navigation in the app.
 */
export function SiteHeader() {
  const pathname = usePathname();

  return (
    <header className="sticky top-0 z-40 border-b border-border/70 bg-background/80 backdrop-blur-md">
      <div className="mx-auto flex w-full max-w-[1320px] flex-wrap items-center gap-x-3 gap-y-3 px-4 py-3 sm:flex-nowrap sm:gap-6 sm:px-8 sm:py-3.5">
        <Link href="/" className="flex shrink-0 items-center gap-2.5">
          <div
            aria-hidden
            className="grid size-7 place-items-center rounded-md bg-foreground font-mono text-[13px] font-semibold leading-none tracking-tight text-background"
          >
            <span className="text-brand">§</span>
          </div>
          <span className="text-[14px] font-semibold tracking-tight">
            ledger
            <span className="font-normal text-muted-foreground">/viz</span>
          </span>
        </Link>

        <div className="order-3 basis-full sm:order-none sm:basis-auto">
          <nav
            aria-label="Views"
            className="flex w-fit max-w-full items-center gap-0.5 overflow-x-auto border border-border bg-muted/50 p-0.5"
          >
            {TABS.map((t) => {
              const active = pathname === t.href;
              return (
                <Link
                  key={t.href}
                  href={t.href}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "shrink-0 whitespace-nowrap px-3 py-1 text-[12.5px] font-medium outline-none transition-colors",
                    "focus-visible:ring-2 focus-visible:ring-ring/60",
                    active
                      ? "bg-card text-foreground ring-1 ring-border"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {t.label}
                </Link>
              );
            })}
          </nav>
        </div>

        <div className="ml-auto shrink-0">
          <ModeToggle />
        </div>
      </div>
    </header>
  );
}
