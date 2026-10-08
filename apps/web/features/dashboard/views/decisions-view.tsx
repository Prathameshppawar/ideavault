"use client";

import Link from "next/link";
import { useMemo } from "react";
import { useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, FileText, GitBranch, Scale } from "lucide-react";
import { api } from "@/lib/api";
import type { DecisionEntry, DecisionsView as DecisionsData } from "@/lib/types";
import { cn, plural, shortDate } from "@/lib/format";
import { RefLabel } from "@/components/domain/knowledge";
import { EntityChip, ItemStatus, OriginTag } from "@/components/ui/badges";
import { EmptyState, ErrorState } from "@/components/ui/primitives";
import { useParamSetter } from "../lib/url";
import { QuietEmpty, RowsSkeleton, Segmented } from "../ui";

type Filter = "all" | "active" | "changed";
const CHANGED = new Set(["SUPERSEDED", "REVERSED"]);
const DEAD = new Set(["SUPERSEDED", "REVERSED", "REJECTED", "RETRACTED", "INVALIDATED", "DROPPED"]);

export const isChanged = (d: Pick<DecisionEntry, "status" | "replaced_by">) => CHANGED.has(d.status) || !!d.replaced_by;

/** Filter, then group decisions by idea (ideas by most recent decision; decisions oldest → newest so they read as a story). */
export function groupDecisions(decisions: DecisionEntry[], filter: Filter) {
  const kept = decisions.filter((d) => (filter === "all" ? true : filter === "active" ? d.status === "ACTIVE" : isChanged(d)));
  const groups = new Map<string, { ideaId: string; title: string; latest: number; items: DecisionEntry[] }>();
  for (const d of kept) {
    const g = groups.get(d.idea_id) ?? { ideaId: d.idea_id, title: d.idea_title, latest: 0, items: [] };
    g.items.push(d);
    g.latest = Math.max(g.latest, new Date(d.created_at).getTime());
    groups.set(d.idea_id, g);
  }
  const out = [...groups.values()].sort((a, b) => b.latest - a.latest);
  for (const g of out) g.items.sort((a, b) => a.ref_number - b.ref_number || a.created_at.localeCompare(b.created_at));
  return out;
}

function summary(v: DecisionsData): string {
  const n = v.decisions.length;
  const ideas = new Set(v.decisions.map((d) => d.idea_id)).size;
  const changes: string[] = [];
  changes.push(v.reversals === 0 ? "reversed none" : `reversed ${v.reversals}`);
  changes.push(v.supersessions === 0 ? "superseded none" : `superseded ${v.supersessions}`);
  const lead = `${plural(n, "decision")} across ${plural(ideas, "idea")}.`;
  if (v.reversals === 0 && v.supersessions === 0) return `${lead} None of them has been reversed or superseded yet.`;
  return `${lead} You ${changes.join(" and ")} — the originals stay on record, linked to what replaced them.`;
}

export function DecisionsView() {
  const params = useSearchParams();
  const setParams = useParamSetter();
  const rawFilter = params.get("filter");
  const filter: Filter = rawFilter === "active" || rawFilter === "changed" ? rawFilter : "all";
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["dashboard", "decisions"],
    queryFn: () => api.get<DecisionsData>("/v1/dashboard/decisions"),
  });
  const groups = useMemo(() => (data ? groupDecisions(data.decisions ?? [], filter) : []), [data, filter]);

  if (isLoading) return <RowsSkeleton rows={6} />;
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;
  const all = data.decisions ?? [];
  if (!all.length) {
    return (
      <EmptyState
        icon={Scale}
        title="No decisions yet"
        description="When you settle something in a conversation — “we'll start with scheduling” — IdeaVault records it here with its rationale and evidence."
      />
    );
  }
  const counts = { all: all.length, active: all.filter((d) => d.status === "ACTIVE").length, changed: all.filter(isChanged).length };

  return (
    <div className="space-y-8">
      <p className="max-w-2xl font-display text-[1.45rem] leading-snug text-fg">{summary({ ...data, decisions: all })}</p>
      <Segmented<Filter>
        label="Filter decisions"
        value={filter}
        onChange={(f) => setParams({ filter: f === "all" ? null : f })}
        options={[
          { value: "all", label: <>All <span className="tabular-nums text-faint">{counts.all}</span></> },
          { value: "active", label: <>Standing <span className="tabular-nums text-faint">{counts.active}</span></> },
          { value: "changed", label: <>Changed <span className="tabular-nums text-faint">{counts.changed}</span></> },
        ]}
      />
      {groups.length === 0 ? (
        <QuietEmpty>{filter === "changed" ? "You haven't reversed or superseded any decision." : "No standing decisions."}</QuietEmpty>
      ) : (
        <div className="space-y-10">
          {groups.map((g) => (
            <section key={g.ideaId} aria-label={g.title}>
              <div className="mb-1 flex flex-wrap items-baseline justify-between gap-x-4 border-b border-border pb-2">
                <Link href={`/ideas/${g.ideaId}`} className="font-display text-[1.45rem] leading-tight text-fg hover:text-accent">
                  {g.title || "Untitled idea"}
                </Link>
                <span className="text-[12px] text-faint">{plural(g.items.length, "decision")}</span>
              </div>
              <ol>
                {g.items.map((d) => (
                  <DecisionRow key={d.id} d={d} />
                ))}
              </ol>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}

function DecisionRow({ d }: { d: DecisionEntry }) {
  const dead = DEAD.has(d.status);
  return (
    <li className="group relative -mx-2.5 flex items-start gap-3 rounded-[var(--radius-md)] px-2.5 py-3 transition-colors hover:bg-surface-2">
      <RefLabel kind="decision" label={d.label} className={cn("mt-px", dead && "opacity-50")} />
      <div className="min-w-0 flex-1">
        <p className={cn("text-[15px] leading-snug text-fg", dead && "text-faint line-through decoration-faint/60")}>
          <Link href={`/knowledge/${d.id}`} className="after:absolute after:inset-0 after:content-[''] focus-visible:outline-none focus-visible:after:rounded-[var(--radius-md)] focus-visible:after:ring-2 focus-visible:after:ring-accent">
            {d.statement}
          </Link>
        </p>
        {d.decision?.rationale && <p className={cn("mt-0.5 line-clamp-2 text-[13px] text-muted", dead && "text-faint")}>because {d.decision.rationale}</p>}
        <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-faint">
          <ItemStatus status={d.status} />
          <OriginTag origin={d.origin} />
          {d.branch_name && (
            <span className="inline-flex items-center gap-1">
              <GitBranch className="h-3 w-3" aria-hidden /> {d.branch_name}
            </span>
          )}
          <span className="inline-flex items-center gap-1">
            <FileText className="h-3 w-3" aria-hidden /> {d.evidence_count ? plural(d.evidence_count, "piece of evidence", "pieces of evidence") : "no evidence linked"}
          </span>
          <time dateTime={d.created_at}>{shortDate(d.created_at)}</time>
        </div>
        {d.replaced_by && (
          <p className="relative z-10 mt-2 inline-flex flex-wrap items-center gap-1.5 text-[13px] text-muted">
            <ArrowRight className="h-3.5 w-3.5 text-faint" aria-hidden />
            {d.status === "REVERSED" ? "Reversed — replaced by" : "Replaced by"}
            <EntityChip type={d.replaced_by.type || "decision"} id={d.replaced_by.id} label={d.replaced_by.label} title={d.replaced_by.title} />
            {d.replaced_by.title && <span className="line-clamp-1 max-w-md text-faint">{d.replaced_by.title}</span>}
          </p>
        )}
      </div>
    </li>
  );
}
