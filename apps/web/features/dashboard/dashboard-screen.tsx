"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { format } from "date-fns";
import { ArrowUpRight, Orbit } from "lucide-react";
import { cn } from "@/lib/format";
import { useNow } from "./lib/hooks";
import { TodayView } from "./views/today-view";
import { TimelineView } from "./views/timeline-view";
import { ActiveView } from "./views/active-view";
import { DecisionsView } from "./views/decisions-view";
import { LearningView } from "./views/learning-view";
import { MomentumView } from "./views/momentum-view";
import { ArchiveView } from "./views/archive-view";

export const DASHBOARD_VIEWS = [
  { key: "today", label: "Today", blurb: "Open loops and what moved recently." },
  { key: "timeline", label: "Timeline", blurb: "How your thinking moved over time." },
  { key: "active", label: "Active", blurb: "Ideas you're still shaping." },
  { key: "decisions", label: "Decisions", blurb: "What you decided — and what you changed your mind about." },
  { key: "learning", label: "Learning", blurb: "Insights, and assumptions that turned out true or false." },
  { key: "momentum", label: "Momentum", blurb: "Ideas changing fastest right now." },
  { key: "archive", label: "Archive", blurb: "Ideas at rest." },
] as const;

export type DashboardViewKey = (typeof DASHBOARD_VIEWS)[number]["key"];

function isView(v: string | null): v is DashboardViewKey {
  return !!v && DASHBOARD_VIEWS.some((x) => x.key === v);
}

/** "Thinking": a living picture of the user's ideas, switched by ?view=. */
export function DashboardScreen() {
  const params = useSearchParams();
  const raw = params.get("view");
  const view: DashboardViewKey = isView(raw) ? raw : "today";
  const now = useNow();
  const meta = DASHBOARD_VIEWS.find((v) => v.key === view)!;

  return (
    <div className="mx-auto w-full max-w-6xl px-4 pb-16 pt-8 sm:px-8 sm:pt-10">
      <header className="mb-6">
        <p className="mb-1.5 h-5 text-[13px] text-muted">{now ? format(now, "EEEE, d MMMM") : ""}</p>
        <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-2">
          <h1 className="font-display text-[2.4rem] leading-[1.05] text-fg">Thinking</h1>
          <p className="pb-1 text-[14px] text-muted">{meta.blurb}</p>
        </div>
      </header>

      <nav aria-label="Thinking views" className="-mx-4 mb-8 overflow-x-auto border-b border-border px-4 sm:mx-0 sm:px-0">
        <ul className="flex min-w-max items-center gap-1">
          {DASHBOARD_VIEWS.map((v) => {
            const active = v.key === view;
            return (
              <li key={v.key}>
                <Link
                  href={v.key === "today" ? "/dashboard" : `/dashboard?view=${v.key}`}
                  scroll={false}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "relative -mb-px inline-flex h-10 items-center border-b-2 px-3 text-[13px] font-medium transition-colors",
                    active ? "border-fg text-fg" : "border-transparent text-muted hover:text-fg",
                  )}
                >
                  {v.label}
                </Link>
              </li>
            );
          })}
          <li aria-hidden className="mx-2 h-4 w-px bg-border" />
          <li>
            <Link href="/universe" className="inline-flex h-10 items-center gap-1.5 px-3 text-[13px] font-medium text-muted transition-colors hover:text-fg">
              <Orbit className="h-3.5 w-3.5" aria-hidden /> Universe <ArrowUpRight className="h-3 w-3 text-faint" aria-hidden />
            </Link>
          </li>
        </ul>
      </nav>

      <div key={view} className="animate-fade-in">
        {view === "today" && <TodayView />}
        {view === "timeline" && <TimelineView />}
        {view === "active" && <ActiveView />}
        {view === "decisions" && <DecisionsView />}
        {view === "learning" && <LearningView />}
        {view === "momentum" && <MomentumView />}
        {view === "archive" && <ArchiveView />}
      </div>
    </div>
  );
}
