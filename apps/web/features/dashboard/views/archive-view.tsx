"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { ArrowUpRight, Moon, RotateCcw } from "lucide-react";
import { api } from "@/lib/api";
import type { Idea } from "@/lib/types";
import { fullDate, humanize, plural } from "@/lib/format";
import { StatusBadge } from "@/components/ui/badges";
import { EmptyState, ErrorState } from "@/components/ui/primitives";
import { Heading, RowsSkeleton } from "../ui";

const GROUPS: { status: string; title: string; note: string; verb: string }[] = [
  { status: "CONCLUDED", title: "Concluded", note: "Seen through to an outcome.", verb: "Concluded" },
  { status: "PARKED", title: "Parked", note: "Set aside on purpose, ready to resume.", verb: "Parked" },
  { status: "MERGED", title: "Merged", note: "Folded into another idea.", verb: "Merged" },
  { status: "ABANDONED", title: "Let go", note: "Stopped — and the reasons are kept.", verb: "Let go" },
];

export function ArchiveView() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["dashboard", "archive"],
    queryFn: () => api.get<Idea[]>("/v1/dashboard/archive"),
  });
  if (isLoading) return <RowsSkeleton rows={4} />;
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;
  if (!data.length) {
    return <EmptyState icon={Moon} title="No ideas at rest" description="Ideas you park, conclude, merge or let go will rest here — with their outcome and everything that led to it." />;
  }
  const parts = GROUPS.map((g) => ({ ...g, n: data.filter((i) => i.status === g.status).length })).filter((g) => g.n > 0);
  return (
    <div className="space-y-10">
      <div className="max-w-2xl">
        <h2 className="font-display text-[1.9rem] leading-tight text-fg">Ideas at rest</h2>
        <p className="mt-2 text-[15px] text-muted">
          {parts.map((p) => `${p.n} ${p.title.toLowerCase()}`).join(", ")}. Nothing here is lost — every decision, and the reasons behind it, is still on record if you want to pick one back up.
        </p>
      </div>
      {GROUPS.map((g) => {
        const ideas = data.filter((i) => i.status === g.status);
        if (!ideas.length) return null;
        return (
          <section key={g.status} aria-labelledby={`rest-${g.status}`}>
            <Heading id={`rest-${g.status}`} count={ideas.length} action={<span className="hidden text-[12px] text-faint sm:inline">{g.note}</span>}>
              {g.title}
            </Heading>
            <ul>
              {ideas.map((idea) => (
                <RestingIdea key={idea.id} idea={idea} verb={g.verb} />
              ))}
            </ul>
          </section>
        );
      })}
    </div>
  );
}

function RestingIdea({ idea, verb }: { idea: Idea; verb: string }) {
  const when = idea.concluded_at || idea.updated_at;
  const s = idea.stats;
  return (
    <li className="border-b border-border py-5 last:border-0">
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-2">
        <div className="min-w-0 flex-1">
          <Link href={`/ideas/${idea.id}`} className="font-display text-[1.45rem] leading-tight text-fg hover:text-accent">
            {idea.title}
          </Link>
          {idea.summary && <p className="mt-1 max-w-2xl text-[14px] text-muted">{idea.summary}</p>}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {idea.outcome && (
            <span className="inline-flex h-5 items-center rounded-[4px] border border-border px-1.5 text-[11px] font-medium text-muted" title="Outcome">
              {humanize(idea.outcome)}
            </span>
          )}
          <StatusBadge status={idea.status} />
        </div>
      </div>
      {idea.outcome_note && (
        <blockquote className="mt-3 max-w-2xl border-l-2 border-border-strong pl-3 font-display text-[1.1rem] italic leading-snug text-muted">“{idea.outcome_note}”</blockquote>
      )}
      <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-faint">
        <span>
          {verb} {fullDate(when).replace(/, \d\d:\d\d$/, "")}
        </span>
        {s && (
          <span>
            {plural(s.decisions, "decision")} · {plural(s.checkpoints, "checkpoint")}
            {s.artifacts > 0 && <> · {plural(s.artifacts, "artifact")}</>}
          </span>
        )}
        {idea.merged_into_idea_id && (
          <Link href={`/ideas/${idea.merged_into_idea_id}`} className="inline-flex items-center gap-1 text-muted hover:text-fg">
            Open the idea it merged into <ArrowUpRight className="h-3 w-3" aria-hidden />
          </Link>
        )}
        <Link href={`/?idea=${idea.id}`} className="inline-flex items-center gap-1 text-muted hover:text-fg">
          <RotateCcw className="h-3 w-3" aria-hidden /> Pick it back up
        </Link>
      </div>
    </li>
  );
}
