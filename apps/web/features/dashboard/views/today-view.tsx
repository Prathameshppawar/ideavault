"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Import, Inbox, MessageSquarePlus, ShieldQuestion } from "lucide-react";
import { api } from "@/lib/api";
import type { AgentRun, Idea, KnowledgeItem, TodayView as TodayData, ToolCallRecord } from "@/lib/types";
import { plural, timeAgo } from "@/lib/format";
import { useSession } from "@/hooks/use-session";
import { KnowledgeRow } from "@/components/domain/knowledge";
import { StatusBadge } from "@/components/ui/badges";
import { ErrorState, Skeleton } from "@/components/ui/primitives";
import { ActivityFeed } from "../components/activity-feed";
import { useNow } from "../lib/hooks";
import { Heading, IdeaLink, LinkButton, QuietEmpty, RowsSkeleton } from "../ui";

function greeting(now: Date | null): string {
  if (!now) return "Hello";
  const h = now.getHours();
  if (h < 5) return "Still thinking";
  if (h < 12) return "Good morning";
  if (h < 18) return "Good afternoon";
  return "Good evening";
}

/** "3 ideas in motion, 5 open loops and 26 proposals to review." */
export function stateSentence(d: Pick<TodayData, "active_ideas" | "open_questions" | "risky_assumptions" | "open_actions" | "pending_proposals">): string {
  const ideas = d.active_ideas?.length ?? 0;
  const loops = (d.open_questions?.length ?? 0) + (d.risky_assumptions?.length ?? 0) + (d.open_actions?.length ?? 0);
  const parts = [ideas === 0 ? "No ideas in motion" : `${plural(ideas, "idea")} in motion`, loops === 0 ? "no open loops" : plural(loops, "open loop")];
  if (d.pending_proposals > 0) parts.push(`${plural(d.pending_proposals, "proposal")} to review`);
  const last = parts.pop()!;
  return `${parts.join(", ")} and ${last}.`;
}

/** The editorial front page: open loops, what changed, ideas in motion. */
export function TodayView() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["dashboard", "today"],
    queryFn: () => api.get<TodayData>("/v1/dashboard/today"),
  });
  const { data: session } = useSession();
  const now = useNow();
  const name = session?.user?.display_name?.split(" ")[0];

  if (isLoading) return <TodaySkeleton />;
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;
  if ((data.counts?.ideas ?? 0) === 0) return <EmptyVault name={name} now={now} />;

  const titles = data.idea_titles ?? {};
  const loops = (data.open_questions?.length ?? 0) + (data.risky_assumptions?.length ?? 0) + (data.open_actions?.length ?? 0);
  const proposalIdeas = (data.active_ideas ?? []).filter((i) => (i.stats?.proposed ?? 0) > 0);
  const confirmations = data.pending_confirmations ?? [];

  return (
    <div className="space-y-10">
      <section aria-label="Summary" className="max-w-3xl">
        <p className="font-display text-[1.9rem] leading-[1.15] text-fg sm:text-[2.15rem]">
          {greeting(now)}
          {name ? `, ${name}` : ""}.{" "}
          <span className="text-muted">{stateSentence(data)}</span>
        </p>
      </section>

      {(data.pending_proposals > 0 || confirmations.length > 0) && (
        <section aria-label="Needs your attention" className="space-y-2">
          {data.pending_proposals > 0 && <ProposalsCallout total={data.pending_proposals} ideas={proposalIdeas} />}
          {confirmations.map((c) => (
            <ConfirmationCallout key={c.id} call={c} />
          ))}
        </section>
      )}

      <div className="grid gap-x-14 gap-y-10 lg:grid-cols-[minmax(0,1fr)_minmax(0,340px)]">
        <div className="min-w-0 space-y-10">
          <section aria-labelledby="open-loops">
            <Heading id="open-loops" count={loops}>Open loops</Heading>
            {loops === 0 ? (
              <QuietEmpty>No open questions, untested assumptions or pending actions. Everything you&apos;re thinking about is settled — for now.</QuietEmpty>
            ) : (
              <div className="space-y-6">
                <LoopGroup title="Questions you haven't answered" items={data.open_questions} titles={titles} />
                <LoopGroup title="Assumptions you haven't tested" hint="riskiest first" items={data.risky_assumptions} titles={titles} />
                <LoopGroup title="Next actions" items={data.open_actions} titles={titles} />
              </div>
            )}
          </section>

          <section aria-labelledby="what-changed">
            <Heading
              id="what-changed"
              action={
                <Link href="/dashboard?view=timeline" scroll={false} className="text-[12px] text-muted hover:text-fg">
                  Full timeline →
                </Link>
              }
            >
              What changed
            </Heading>
            {(data.recent_changes?.length ?? 0) === 0 ? (
              <QuietEmpty>Nothing changed in the last three days.</QuietEmpty>
            ) : now ? (
              <ActivityFeed events={data.recent_changes} now={now} limitPerDay={14} />
            ) : (
              <RowsSkeleton rows={4} />
            )}
          </section>
        </div>

        <aside className="min-w-0 space-y-10" aria-label="Ideas and recent thinking">
          <section aria-labelledby="in-motion">
            <Heading
              id="in-motion"
              count={data.active_ideas?.length ?? 0}
              action={
                <Link href="/dashboard?view=active" scroll={false} className="text-[12px] text-muted hover:text-fg">
                  All active →
                </Link>
              }
            >
              In motion
            </Heading>
            {(data.active_ideas?.length ?? 0) === 0 ? (
              <QuietEmpty>No ideas are being explored right now.</QuietEmpty>
            ) : (
              <ul>
                {data.active_ideas.map((idea) => (
                  <li key={idea.id}>
                    <IdeaLine idea={idea} />
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section aria-labelledby="recently-decided">
            <Heading id="recently-decided">Recently decided</Heading>
            {(data.recent_decisions?.length ?? 0) === 0 ? (
              <QuietEmpty>No decisions recorded yet.</QuietEmpty>
            ) : (
              <ByIdea items={data.recent_decisions} titles={titles} />
            )}
          </section>

          <section aria-labelledby="recently-learned">
            <Heading id="recently-learned">Recently learned</Heading>
            {(data.recent_insights?.length ?? 0) === 0 ? (
              <QuietEmpty>No insights captured yet.</QuietEmpty>
            ) : (
              <ByIdea items={data.recent_insights} titles={titles} />
            )}
          </section>
        </aside>
      </div>
    </div>
  );
}

function LoopGroup({ title, hint, items, titles }: { title: string; hint?: string; items?: KnowledgeItem[] | null; titles: Record<string, string> }) {
  if (!items?.length) return null;
  return (
    <div>
      <h3 className="mb-1 text-[13px] font-medium text-fg">
        {title}
        {hint && <span className="ml-2 font-normal text-faint">{hint}</span>}
      </h3>
      <div className="-mx-2.5">
        {items.map((it) => (
          <KnowledgeRow
            key={it.id}
            item={it}
            href={`/knowledge/${it.id}`}
            trailing={
              titles[it.idea_id] ? (
                <span className="hidden max-w-[11rem] shrink-0 items-center gap-1.5 pt-0.5 text-[12px] text-faint sm:inline-flex" title={titles[it.idea_id]}>
                  <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-k-idea" aria-hidden />
                  <span className="truncate">{titles[it.idea_id]}</span>
                </span>
              ) : undefined
            }
          />
        ))}
      </div>
    </div>
  );
}

/** Knowledge rows grouped under their idea — keeps the narrow aside readable. */
function ByIdea({ items, titles }: { items: KnowledgeItem[]; titles: Record<string, string> }) {
  const groups: { ideaId: string; items: KnowledgeItem[] }[] = [];
  for (const it of items) {
    const g = groups.find((x) => x.ideaId === it.idea_id);
    if (g) g.items.push(it);
    else groups.push({ ideaId: it.idea_id, items: [it] });
  }
  return (
    <div className="space-y-4">
      {groups.map((g) => (
        <div key={g.ideaId}>
          <IdeaLink id={g.ideaId} title={titles[g.ideaId]} className="mb-0.5" />
          <div className="-mx-2.5">
            {g.items.map((it) => (
              <KnowledgeRow key={it.id} item={it} href={`/knowledge/${it.id}`} />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function IdeaLine({ idea }: { idea: Idea }) {
  const s = idea.stats;
  const bits = [
    s?.decisions ? plural(s.decisions, "decision") : null,
    s?.open_questions ? plural(s.open_questions, "open question") : null,
    s?.branches && s.branches > 1 ? plural(s.branches, "branch", "branches") : null,
  ].filter(Boolean);
  return (
    <Link href={`/ideas/${idea.id}`} className="group block border-b border-border py-3 last:border-0">
      <div className="flex items-start justify-between gap-3">
        <h3 className="min-w-0 font-display text-[1.2rem] leading-tight text-fg transition-colors group-hover:text-accent">{idea.title}</h3>
        <StatusBadge status={idea.status} className="mt-0.5 shrink-0" />
      </div>
      <p className="mt-1 text-[12px] text-faint">
        Touched {timeAgo(idea.last_activity_at)}
        {bits.length > 0 && <> · {bits.join(" · ")}</>}
      </p>
    </Link>
  );
}

function ProposalsCallout({ total, ideas }: { total: number; ideas: Idea[] }) {
  return (
    <div className="flex flex-col gap-3 rounded-[var(--radius-lg)] border border-warning/30 bg-warning-soft px-4 py-3 sm:flex-row sm:items-center">
      <Inbox className="hidden h-4 w-4 shrink-0 text-warning sm:block" aria-hidden />
      <div className="min-w-0 flex-1 text-[13px]">
        <p className="text-fg">
          <span className="font-medium">{plural(total, "proposed item")} waiting for review.</span>{" "}
          <span className="text-muted">Extracted or imported knowledge doesn&apos;t count until you accept it.</span>
        </p>
        {ideas.length > 0 && (
          <p className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[12px]">
            {ideas.map((i) => (
              <Link key={i.id} href={`/ideas/${i.id}?tab=review`} className="text-muted underline decoration-border-strong underline-offset-2 hover:text-fg">
                {i.title} · {i.stats?.proposed}
              </Link>
            ))}
          </p>
        )}
      </div>
      <Link href="/imports" className="inline-flex shrink-0 items-center gap-1 text-[13px] font-medium text-fg hover:underline">
        Review <ArrowRight className="h-3.5 w-3.5" aria-hidden />
      </Link>
    </div>
  );
}

function ConfirmationCallout({ call }: { call: ToolCallRecord }) {
  const { data: run } = useQuery({
    queryKey: ["agent-run", call.agent_run_id],
    queryFn: () => api.get<AgentRun>(`/v1/agent/runs/${call.agent_run_id}`),
    staleTime: 60_000,
  });
  const href = run?.conversation_id ? `/conversations/${run.conversation_id}` : null;
  return (
    <div className="flex items-start gap-3 rounded-[var(--radius-lg)] border border-border bg-surface px-4 py-3 text-[13px]">
      <ShieldQuestion className="mt-0.5 h-4 w-4 shrink-0 text-k-question" aria-hidden />
      <div className="min-w-0 flex-1">
        <p className="text-fg">
          <span className="font-medium">IdeaVault is waiting for your go-ahead</span> <span className="text-muted">to run {call.tool_name.replace(/_/g, " ")}</span>
        </p>
        {call.summary && <p className="mt-0.5 line-clamp-2 text-muted">{call.summary}</p>}
        <p className="mt-0.5 text-[12px] text-faint">Asked {timeAgo(call.created_at)} · {call.category.toLowerCase()} action</p>
      </div>
      {href && (
        <Link href={href} className="inline-flex shrink-0 items-center gap-1 text-[13px] font-medium text-fg hover:underline">
          Open chat <ArrowRight className="h-3.5 w-3.5" aria-hidden />
        </Link>
      )}
    </div>
  );
}

function EmptyVault({ name, now }: { name?: string; now: Date | null }) {
  return (
    <section className="mx-auto max-w-2xl py-10 text-center sm:py-16">
      <div className="mx-auto mb-8 flex w-fit items-center gap-1.5" aria-hidden>
        {["idea", "decision", "assumption", "evidence", "insight", "question"].map((k, i) => (
          <span key={k} className="h-2 w-2 rounded-full" style={{ background: `var(--k-${k})`, opacity: 0.35 + i * 0.12 }} />
        ))}
      </div>
      <h2 className="font-display text-[2.1rem] leading-tight text-fg sm:text-[2.5rem]">
        {greeting(now)}
        {name ? `, ${name}` : ""}. Your vault is quiet.
      </h2>
      <p className="mx-auto mt-3 max-w-lg text-[15px] leading-relaxed text-muted">
        Every idea you think through here keeps its decisions, assumptions and evidence — and how they changed. Start with a thought, or bring in a conversation you&apos;ve already had.
      </p>
      <div className="mt-8 flex flex-col items-center justify-center gap-2 sm:flex-row">
        <LinkButton href="/" variant="primary">
          <MessageSquarePlus className="h-4 w-4" aria-hidden /> Start a new idea
        </LinkButton>
        <LinkButton href="/imports">
          <Import className="h-4 w-4" aria-hidden /> Import a conversation
        </LinkButton>
      </div>
      <p className="mt-6 text-[12px] text-faint">Imported conversations are treated as untrusted sources until you review what was extracted.</p>
    </section>
  );
}

function TodaySkeleton() {
  return (
    <div className="space-y-10" aria-busy="true" aria-label="Loading">
      <div className="max-w-3xl space-y-3">
        <Skeleton className="h-8 w-3/4" />
        <Skeleton className="h-8 w-1/2" />
      </div>
      <div className="grid gap-14 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="space-y-4">
          <Skeleton className="h-3 w-24" />
          <RowsSkeleton rows={6} />
        </div>
        <div className="space-y-4">
          <Skeleton className="h-3 w-24" />
          <RowsSkeleton rows={4} />
        </div>
      </div>
    </div>
  );
}
