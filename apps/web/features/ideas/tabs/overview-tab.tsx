"use client";

import { useMemo, useState, type ReactNode } from "react";
import Link from "next/link";
import { ArrowRight, ChevronDown, MessagesSquare } from "lucide-react";
import type { ActivityEvent, Checkpoint, ConclusionRecord, IdeaOverview, KnowledgeItem } from "@/lib/types";
import { entityHref, entityMeta, isKnowledge, tone } from "@/lib/entities";
import { cn, fullDate, humanize, shortDate, timeAgo } from "@/lib/format";
import { EntityChip, StatusBadge } from "@/components/ui/badges";
import { KnowledgeRow } from "@/components/domain/knowledge";
import { SectionTitle } from "@/components/ui/primitives";
import { currentState } from "@/features/knowledge/lib";
import { ProposalsReview } from "@/features/knowledge/proposals-review";
import { CheckpointPill, Interpretation, SourceQuote } from "../ui";
import { outcomeInfo } from "../lib/lifecycle";
import { ConclusionSection } from "../conclude-dialog";
import type { IdeaTab } from "../hooks";

export function OverviewTab({
  ov,
  items,
  viewingCp,
  onSelectItem,
  onTab,
}: {
  ov: IdeaOverview;
  items: KnowledgeItem[];
  viewingCp: Checkpoint | null;
  onSelectItem: (id: string) => void;
  onTab: (t: IdeaTab) => void;
}) {
  const idea = ov.idea!;
  const snapIdea = viewingCp?.snapshot?.idea;
  const origin = snapIdea ? snapIdea.origin_text : idea.origin_text;
  const summary = snapIdea ? snapIdea.summary : idea.summary;
  const state = useMemo(() => currentState(items), [items]);
  const conclusions = ov.conclusions ?? [];
  const related = ov.related_ideas ?? [];
  const checkpointById = useMemo(() => new Map((ov.checkpoints ?? []).map((c) => [c.id, c])), [ov.checkpoints]);

  return (
    <div className="space-y-12">
      {!viewingCp && (ov.proposed?.length ?? 0) > 0 && <ProposalsReview ideaId={idea.id} items={ov.proposed} onOpenItem={onSelectItem} />}

      <section className="grid gap-6 md:grid-cols-2">
        <div>
          <SectionTitle>Where it started</SectionTitle>
          {origin ? (
            <SourceQuote
              label={`how you first described it · ${shortDate(idea.created_at)}`}
              meta={
                idea.origin_conversation_id ? (
                  <Link href={`/conversations/${idea.origin_conversation_id}`} className="inline-flex items-center gap-1 text-[12px] text-muted hover:text-fg">
                    <MessagesSquare className="h-3 w-3" /> conversation
                  </Link>
                ) : undefined
              }
            >
              {origin}
            </SourceQuote>
          ) : (
            <p className="text-[13.5px] text-faint">No origin text was recorded for this idea.</p>
          )}
        </div>
        <div>
          <SectionTitle>{viewingCp ? `Understanding at ${viewingCp.label}` : "Current understanding"}</SectionTitle>
          {summary ? (
            <Interpretation>{summary}</Interpretation>
          ) : (
            <p className="text-[13.5px] text-faint">IdeaVault hasn&apos;t summarised this idea yet — it will once there&apos;s more to go on.</p>
          )}
        </div>
      </section>

      {items.length === 0 ? (
        <section className="rounded-[var(--radius-lg)] border border-dashed border-border px-6 py-8 text-center">
          <p className="font-display text-[1.5rem] leading-tight text-fg">{viewingCp ? `${viewingCp.label} captured no knowledge yet` : "Nothing decided yet"}</p>
          <p className="mx-auto mt-1.5 max-w-md text-[13.5px] text-muted">
            {viewingCp
              ? "This snapshot was taken before anything was recorded on its branch."
              : "Think it through in the chat panel — decisions, assumptions, questions and next actions are recorded here as you go. Or add one yourself."}
          </p>
          {!viewingCp && idea.status !== "MERGED" && (
            <button type="button" onClick={() => onTab("knowledge")} className="mt-4 inline-flex items-center gap-1 text-[13px] font-medium text-accent hover:underline">
              Add knowledge <ArrowRight className="h-3.5 w-3.5" />
            </button>
          )}
        </section>
      ) : (
      <section>
        <SectionTitle
          action={
            <button type="button" onClick={() => onTab("knowledge")} className="inline-flex items-center gap-1 text-[12.5px] text-muted hover:text-fg">
              All knowledge <ArrowRight className="h-3 w-3" />
            </button>
          }
        >
          {viewingCp ? `State at ${viewingCp.label}` : "Current state"}
        </SectionTitle>
        <div className="grid gap-x-8 gap-y-7 md:grid-cols-2">
          <StateBlock kind="decision" title="Decisions in force" items={state.decisions} total={state.totals.decisions} empty="No decisions yet." onSelect={onSelectItem} onMore={() => onTab("knowledge")} />
          <StateBlock kind="question" title="Open questions" items={state.questions} total={state.totals.questions} empty="Nothing open." onSelect={onSelectItem} onMore={() => onTab("knowledge")} />
          <StateBlock
            kind="assumption"
            title="Riskiest assumptions"
            items={state.assumptions}
            total={state.totals.assumptions}
            empty="No unvalidated assumptions."
            onSelect={onSelectItem}
            onMore={() => onTab("knowledge")}
            badge={(it) =>
              it.assumption?.risk ? (
                <span className={cn("mt-0.5 shrink-0 text-[10.5px] font-semibold uppercase tracking-wide", it.assumption.risk === "HIGH" ? "text-k-question" : "text-faint")}>
                  {it.assumption.risk === "HIGH" ? "High risk" : humanize(it.assumption.risk)}
                </span>
              ) : null
            }
          />
          <StateBlock
            kind="action"
            title="Next actions"
            items={state.actions}
            total={state.totals.actions}
            empty="No open actions."
            onSelect={onSelectItem}
            onMore={() => onTab("knowledge")}
            badge={(it) =>
              it.status === "IN_PROGRESS" ? (
                <span className="mt-0.5 shrink-0 text-[10.5px] font-semibold uppercase tracking-wide text-k-action">In progress</span>
              ) : it.action?.priority === "HIGH" ? (
                <span className="mt-0.5 shrink-0 text-[10.5px] font-semibold uppercase tracking-wide text-muted">High</span>
              ) : null
            }
          />
        </div>
      </section>
      )}

      {conclusions.length > 0 && (
        <section>
          <SectionTitle>Conclusions</SectionTitle>
          <ol className="space-y-3">
            {conclusions.map((c, i) => (
              <ConclusionItem key={c.id} c={c} checkpoint={c.checkpoint_id ? checkpointById.get(c.checkpoint_id) : undefined} defaultOpen={i === 0} />
            ))}
          </ol>
        </section>
      )}

      {related.length > 0 && (
        <section>
          <SectionTitle>Related ideas</SectionTitle>
          <ul className="flex flex-wrap gap-2">
            {related.map((r) => (
              <li key={`${r.id}-${r.rel_type}`} className="inline-flex items-center gap-1.5 text-[12.5px] text-muted">
                <span>{humanize(r.rel_type)}</span>
                <EntityChip type="idea" id={r.id} label={r.title || "Idea"} />
              </li>
            ))}
          </ul>
        </section>
      )}

      {!viewingCp && (
        <section>
          <SectionTitle>Recent activity</SectionTitle>
          <ActivityTimeline events={ov.recent_activity ?? []} onSelectItem={onSelectItem} />
        </section>
      )}
    </div>
  );
}

function StateBlock({
  kind,
  title,
  items,
  total,
  empty,
  onSelect,
  onMore,
  badge,
}: {
  kind: string;
  title: string;
  items: KnowledgeItem[];
  total: number;
  empty: string;
  onSelect: (id: string) => void;
  onMore: () => void;
  badge?: (it: KnowledgeItem) => ReactNode;
}) {
  return (
    <div className="min-w-0">
      <div className="mb-1.5 flex items-baseline gap-2 border-b border-border pb-1.5">
        <span className="h-1.5 w-1.5 shrink-0 -translate-y-0.5 rounded-full" style={{ background: tone(kind) }} aria-hidden />
        <h3 className="text-[13.5px] font-medium text-fg">{title}</h3>
        <span className="text-[12.5px] tabular-nums text-faint">{total}</span>
        {total > items.length && (
          <button type="button" onClick={onMore} className="ml-auto text-[12px] text-muted hover:text-fg">
            +{total - items.length} more
          </button>
        )}
      </div>
      {items.length === 0 ? (
        <p className="px-2.5 py-1.5 text-[13px] text-faint">{empty}</p>
      ) : (
        <div className="-mx-1">
          {items.map((it) => (
            <KnowledgeRow key={it.id} item={it} compact onClick={() => onSelect(it.id)} trailing={badge?.(it)} />
          ))}
        </div>
      )}
    </div>
  );
}

function ConclusionItem({ c, checkpoint, defaultOpen }: { c: ConclusionRecord; checkpoint?: Checkpoint; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(Boolean(defaultOpen));
  const info = outcomeInfo(c.outcome);
  return (
    <li className="rounded-[var(--radius-lg)] border border-border">
      <button type="button" onClick={() => setOpen((v) => !v)} aria-expanded={open} className="flex w-full flex-wrap items-center gap-x-3 gap-y-1.5 px-4 py-3 text-left">
        <span className="text-[14px] font-medium text-fg">{info?.label ?? humanize(c.outcome)}</span>
        <span className="inline-flex items-center gap-1.5">
          <StatusBadge status={c.previous_status} />
          <ArrowRight className="h-3 w-3 text-faint" aria-hidden />
          <StatusBadge status={c.new_status} />
        </span>
        {checkpoint && <CheckpointPill label={checkpoint.label} />}
        <span className="ml-auto text-[12px] text-faint" title={fullDate(c.created_at)}>
          {shortDate(c.created_at)}
        </span>
        <ChevronDown className={cn("h-3.5 w-3.5 text-faint transition-transform", open && "rotate-180")} aria-hidden />
      </button>
      {open && (
        <div className="space-y-4 border-t border-border px-4 py-4 animate-fade-in">
          {c.note && <SourceQuote label="your closing note">{c.note}</SourceQuote>}
          <ConclusionSection title="What was learned" md={c.learned} />
          <ConclusionSection title="Decisions" md={c.decisions} />
          <ConclusionSection title="Still unresolved" md={c.unresolved} />
          {checkpoint && (
            <Link href={`/checkpoints/${checkpoint.id}`} className="inline-flex items-center gap-1 text-[12.5px] text-muted hover:text-fg">
              Open final snapshot {checkpoint.label} <ArrowRight className="h-3 w-3" />
            </Link>
          )}
        </div>
      )}
    </li>
  );
}

const ACTOR_LABEL: Record<string, string> = { user: "You", agent: "IdeaVault", import: "Import", system: "System" };

export function ActivityTimeline({ events, onSelectItem, limit = 12 }: { events: ActivityEvent[]; onSelectItem: (id: string) => void; limit?: number }) {
  const [all, setAll] = useState(false);
  if (!events.length) return <p className="text-[13px] text-faint">Nothing has happened here yet.</p>;
  const shown = all ? events : events.slice(0, limit);
  return (
    <>
      <ol className="relative">
        {shown.map((e, i) => {
          const t = e.entity_type || "idea";
          const knowledge = isKnowledge(t) && e.entity_id;
          const content = (
            <>
              <span className="min-w-0 flex-1 text-[13.5px] leading-snug text-fg [overflow-wrap:anywhere]">{e.summary}</span>
              <span className="shrink-0 text-[11.5px] text-faint" title={fullDate(e.created_at)}>
                {timeAgo(e.created_at)}
              </span>
            </>
          );
          return (
            <li key={e.id} className="relative flex gap-3 pb-3.5 last:pb-0">
              {i < shown.length - 1 && <span className="absolute left-[3.5px] top-3 h-full w-px bg-border" aria-hidden />}
              <span className="relative mt-1.5 h-2 w-2 shrink-0 rounded-full" style={{ background: tone(t) }} aria-hidden />
              <div className="min-w-0 flex-1">
                {knowledge ? (
                  <button type="button" onClick={() => onSelectItem(e.entity_id!)} className="flex w-full items-baseline gap-3 text-left hover:opacity-80">
                    {content}
                  </button>
                ) : e.entity_id && t !== "idea" ? (
                  <Link href={entityHref(t, e.entity_id, e.idea_id)} className="flex items-baseline gap-3 hover:opacity-80">
                    {content}
                  </Link>
                ) : (
                  <div className="flex items-baseline gap-3">{content}</div>
                )}
                <p className="mt-0.5 text-[11.5px] text-faint">
                  {ACTOR_LABEL[e.actor] ?? humanize(e.actor)} · {entityMeta(t).label.toLowerCase()} {humanize(e.event_type.split(".").pop()).toLowerCase()}
                </p>
              </div>
            </li>
          );
        })}
      </ol>
      {events.length > limit && (
        <button type="button" onClick={() => setAll((v) => !v)} className="mt-3 text-[12.5px] text-muted hover:text-fg">
          {all ? "Show less" : `Show all ${events.length}`}
        </button>
      )}
    </>
  );
}
