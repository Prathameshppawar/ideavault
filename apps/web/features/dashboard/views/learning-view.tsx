"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Check, Lightbulb, X } from "lucide-react";
import { api } from "@/lib/api";
import type { DecisionEntry, KnowledgeItem, LearningView as LearningData, Relationship } from "@/lib/types";
import { entityHref, entityMeta, tone } from "@/lib/entities";
import { humanize, plural, shortDate } from "@/lib/format";
import { KnowledgeRow } from "@/components/domain/knowledge";
import { EmptyState, ErrorState } from "@/components/ui/primitives";
import { Heading, QuietEmpty, RowsSkeleton } from "../ui";

const UNVALIDATED_QUERY = "/search?q=" + encodeURIComponent("What assumptions have never been validated?");

/** Insight entries are knowledge items flattened with idea/branch context; strip that for KnowledgeRow. */
export function asItem(entry: DecisionEntry): KnowledgeItem {
  const { evidence_count: _count, idea_title: _t, branch_name: _b, replaced_by: _r, ...rest } = entry;
  void _count;
  void _t;
  void _b;
  void _r;
  return rest as KnowledgeItem;
}

export function LearningView() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["dashboard", "learning"],
    queryFn: () => api.get<LearningData>("/v1/dashboard/learning"),
  });
  if (isLoading) return <RowsSkeleton rows={6} />;
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;

  const insights = data.insights ?? [];
  const validated = data.validated_assumptions ?? [];
  const invalidated = data.invalidated_assumptions ?? [];
  const lessons = data.reused_lessons ?? [];

  if (!insights.length && !validated.length && !invalidated.length && !lessons.length) {
    return (
      <EmptyState
        icon={Lightbulb}
        title="Nothing learned — yet"
        description="Insights you capture, and assumptions you prove right or wrong, collect here so the next idea starts smarter."
      />
    );
  }

  const byIdea = new Map<string, { ideaId: string; title: string; items: typeof insights }>();
  for (const it of insights) {
    const g = byIdea.get(it.idea_id) ?? { ideaId: it.idea_id, title: it.idea_title, items: [] };
    g.items.push(it);
    byIdea.set(it.idea_id, g);
  }
  const ideaCount = byIdea.size;

  return (
    <div className="space-y-12">
      <p className="max-w-2xl font-display text-[1.45rem] leading-snug text-fg">
        {plural(insights.length, "insight")} from {plural(ideaCount, "idea")}.{" "}
        <span className="text-muted">
          {validated.length + invalidated.length === 0
            ? "No assumption has been settled either way yet."
            : `${plural(validated.length + invalidated.length, "assumption")} settled: ${validated.length} held up, ${invalidated.length} didn't.`}
        </span>
      </p>

      <section aria-labelledby="insights">
        <Heading id="insights" count={insights.length}>
          Insights
        </Heading>
        {insights.length === 0 ? (
          <QuietEmpty>No insights captured yet.</QuietEmpty>
        ) : (
          <div className="grid gap-x-10 gap-y-8 md:grid-cols-2">
            {[...byIdea.values()].map((g) => (
              <div key={g.ideaId}>
                <Link href={`/ideas/${g.ideaId}`} className="mb-1 block font-display text-[1.25rem] leading-tight text-fg hover:text-accent">
                  {g.title || "Untitled idea"}
                </Link>
                <div className="-mx-2.5">
                  {g.items.map((it) => (
                    <KnowledgeRow
                      key={it.id}
                      item={asItem(it)}
                      href={`/knowledge/${it.id}`}
                      trailing={it.insight?.importance === "HIGH" ? <span className="shrink-0 pt-0.5 text-[11px] font-medium text-k-insight">Key</span> : undefined}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      <section aria-labelledby="outcomes">
        <Heading id="outcomes">What turned out true — and false</Heading>
        <div className="grid gap-x-10 gap-y-8 md:grid-cols-2">
          <Outcome
            title="Held up"
            icon={<Check className="h-3.5 w-3.5" aria-hidden />}
            color="var(--success)"
            items={validated}
            empty={
              <>
                No assumption validated yet.{" "}
                <Link href={UNVALIDATED_QUERY} className="text-muted underline decoration-border-strong underline-offset-2 hover:text-fg">
                  See what&apos;s still untested
                </Link>
                .
              </>
            }
          />
          <Outcome
            title="Turned out false"
            icon={<X className="h-3.5 w-3.5" aria-hidden />}
            color="var(--danger)"
            items={invalidated}
            empty="Nothing has been proven wrong. That's either good news or a sign nothing has been tested."
          />
        </div>
      </section>

      <section aria-labelledby="lessons">
        <Heading id="lessons" count={lessons.length}>
          Lessons carried between ideas
        </Heading>
        {lessons.length === 0 ? (
          <QuietEmpty>No idea has borrowed from another yet. When one evolves from, is inspired by, or reuses a lesson from another, the link shows up here.</QuietEmpty>
        ) : (
          <ul className="divide-y divide-border">
            {lessons.map((r) => (
              <LessonRow key={r.id} r={r} />
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function Outcome({ title, icon, color, items, empty }: { title: string; icon: React.ReactNode; color: string; items: KnowledgeItem[]; empty: React.ReactNode }) {
  return (
    <div>
      <h3 className="mb-2 flex items-center gap-2 text-[13px] font-medium text-fg">
        <span className="flex h-5 w-5 items-center justify-center rounded-full" style={{ color, background: `color-mix(in srgb, ${color} 14%, transparent)` }}>
          {icon}
        </span>
        {title}
        <span className="font-normal tabular-nums text-faint">{items.length}</span>
      </h3>
      {items.length === 0 ? (
        <QuietEmpty>{empty}</QuietEmpty>
      ) : (
        <div className="-mx-2.5">
          {items.map((it) => (
            <KnowledgeRow key={it.id} item={it} href={`/knowledge/${it.id}`} />
          ))}
        </div>
      )}
    </div>
  );
}

function End({ type, id, label, ideaId }: { type: string; id: string; label?: string; ideaId?: string }) {
  const meta = entityMeta(type);
  const Icon = meta.icon;
  return (
    <Link href={entityHref(type, id, ideaId)} className="inline-flex min-w-0 items-center gap-1.5 text-[14px] text-fg hover:underline hover:decoration-border-strong hover:underline-offset-2">
      <Icon className="h-3.5 w-3.5 shrink-0" style={{ color: tone(type) }} aria-hidden />
      <span className="truncate">{label || meta.label}</span>
    </Link>
  );
}

function LessonRow({ r }: { r: Relationship }) {
  return (
    <li className="py-3">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <End type={r.from_type} id={r.from_id} label={r.from_label} ideaId={r.idea_id} />
        <span className="inline-flex items-center gap-1 text-[12px] text-muted">
          <ArrowRight className="h-3 w-3 text-faint" aria-hidden /> {humanize(r.rel_type).toLowerCase()}
        </span>
        <End type={r.to_type} id={r.to_id} label={r.to_label} />
      </div>
      {r.rationale && <p className="mt-1 text-[13px] text-muted">{r.rationale}</p>}
      <p className="mt-1 text-[11px] text-faint">
        {r.origin === "SOURCE" ? "Stated" : "Inferred"} · {shortDate(r.created_at)}
      </p>
    </li>
  );
}
