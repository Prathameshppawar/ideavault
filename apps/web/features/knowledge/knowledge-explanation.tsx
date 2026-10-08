"use client";

import { useState, type ReactNode } from "react";
import Link from "next/link";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Check, CornerDownRight, ExternalLink, GitBranch, History, MessagesSquare, X } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { KnowledgeExplanation, KnowledgeItem, Message, Relationship } from "@/lib/types";
import { entityMeta, tone } from "@/lib/entities";
import { cn, fullDate, humanize, shortDate, truncate } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { EntityChip, ItemStatus, OriginTag } from "@/components/ui/badges";
import { RefLabel } from "@/components/domain/knowledge";
import { ErrorState, Input, SectionTitle, SkeletonLines } from "@/components/ui/primitives";
import { DetailRow, SourceQuote, UntrustedLabel } from "@/features/ideas/ui";
import { useKnowledgeExplanation } from "@/features/ideas/hooks";
import { buildKnowledgePayload, emptyKnowledgeForm, isLive, statusActions, type BuildResult, type KnowledgeFormState } from "./lib";
import { KnowledgeFields } from "./knowledge-form";

/**
 * Full explanation of one knowledge item: statement, reasoning, where it came from (Source vs
 * Interpretation), evidence, supersession history and the checkpoints that captured it.
 * Used in the idea page drawer and on /knowledge/[id].
 */
export function KnowledgeExplanationView({
  id,
  onNavigate,
  readOnly,
  readOnlyReason,
  headerAction,
}: {
  id: string;
  /** Open another item (drawer navigation). Falls back to links to /knowledge/[id]. */
  onNavigate?: (id: string) => void;
  /** Viewing a checkpoint snapshot: no status changes. */
  readOnly?: boolean;
  readOnlyReason?: string;
  headerAction?: ReactNode;
}) {
  const { data, isLoading, error, refetch } = useKnowledgeExplanation(id);
  if (isLoading)
    return (
      <div className="space-y-6 p-5">
        <SkeletonLines lines={3} />
        <SkeletonLines lines={5} />
      </div>
    );
  if (error || !data?.item)
    return (
      <div className="p-5">
        <ErrorState error={error ?? new Error("Item not found")} onRetry={() => void refetch()} />
      </div>
    );
  return <Explanation ex={data} item={data.item} onNavigate={onNavigate} readOnly={readOnly} readOnlyReason={readOnlyReason} headerAction={headerAction} />;
}

function ItemLink({ item, onNavigate, children, className }: { item: Pick<KnowledgeItem, "id">; onNavigate?: (id: string) => void; children: ReactNode; className?: string }) {
  if (onNavigate)
    return (
      <button type="button" onClick={() => onNavigate(item.id)} className={cn("text-left", className)}>
        {children}
      </button>
    );
  return (
    <Link href={`/knowledge/${item.id}`} className={className}>
      {children}
    </Link>
  );
}

function Section({ title, children, action }: { title: string; children: ReactNode; action?: ReactNode }) {
  return (
    <section className="border-t border-border px-5 py-5">
      <SectionTitle action={action}>{title}</SectionTitle>
      {children}
    </section>
  );
}

function Explanation({
  ex,
  item,
  onNavigate,
  readOnly,
  readOnlyReason,
  headerAction,
}: {
  ex: KnowledgeExplanation;
  item: KnowledgeItem;
  onNavigate?: (id: string) => void;
  readOnly?: boolean;
  readOnlyReason?: string;
  headerAction?: ReactNode;
}) {
  const dead = !isLive(item);
  const chain = ex.chain ?? [];
  const replacement = item.superseded_by_id ? chain.find((c) => c.id === item.superseded_by_id) : undefined;
  const untrusted = Boolean(ex.source_message?.untrusted || (ex.conversation && ex.conversation.origin !== "native") || (ex.source && !ex.source.trusted));
  const otherRels = (ex.relationships ?? []).filter((r) => !["supports", "challenges", "contradicts", "supersedes", "inherited_from"].includes(r.rel_type));
  const supporting = ex.supporting ?? [];
  const challenging = ex.challenging ?? [];

  return (
    <article className="animate-fade-in">
      <header className="px-5 pb-5 pt-5">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <RefLabel kind={item.kind} label={item.label} />
          <span className="text-[11px] font-medium uppercase tracking-wide" style={{ color: tone(item.kind) }}>
            {entityMeta(item.kind).label}
          </span>
          <ItemStatus status={item.status} />
          <OriginTag origin={item.origin} />
          {item.review_state === "PROPOSED" && <span className="text-[11px] font-medium uppercase tracking-wide text-warning">Awaiting review</span>}
          {item.review_state === "REJECTED" && <span className="text-[11px] font-medium uppercase tracking-wide text-faint">Rejected in review</span>}
          {item.inherited_from_id && (
            <span className="inline-flex items-center gap-1 text-[11px] text-faint">
              <GitBranch className="h-3 w-3" /> inherited
            </span>
          )}
          {headerAction && <span className="ml-auto">{headerAction}</span>}
        </div>
        <h2 className={cn("font-display text-[1.6rem] leading-[1.2] text-fg [overflow-wrap:anywhere]", dead && "text-muted line-through decoration-faint/60")}>{item.statement}</h2>
        {item.details && <p className="mt-2 whitespace-pre-wrap text-[14px] leading-relaxed text-muted">{item.details}</p>}

        {item.superseded_by_id && (
          <div className="mt-4 flex flex-wrap items-center gap-2 rounded-[var(--radius-md)] border border-border bg-surface-2 px-3 py-2 text-[13px]">
            <History className="h-3.5 w-3.5 text-faint" />
            <span className="text-muted">{item.status === "REVERSED" ? "Reversed by" : "Superseded by"}</span>
            {replacement ? (
              <ItemLink item={replacement} onNavigate={onNavigate} className="inline-flex min-w-0 items-center gap-1.5 font-medium text-fg hover:text-accent">
                <RefLabel kind={replacement.kind} label={replacement.label} />
                <span className="truncate">{truncate(replacement.statement, 80)}</span>
              </ItemLink>
            ) : (
              <ItemLink item={{ id: item.superseded_by_id }} onNavigate={onNavigate} className="font-medium text-fg hover:text-accent">
                the newer version
              </ItemLink>
            )}
          </div>
        )}

        <KindDetails item={item} onNavigate={onNavigate} resolve={(rid) => resolveRef(ex, rid)} />
      </header>

      {!readOnly && <ItemActions item={item} />}
      {readOnly && readOnlyReason && <p className="border-t border-border px-5 py-3 text-[12px] text-faint">{readOnlyReason}</p>}

      <Section title="Where it came from">
        <div className="space-y-4">
          {item.source_excerpt ? (
            <SourceQuote untrusted={untrusted} label={ex.conversation ? `from “${truncate(ex.conversation.title || "conversation", 48)}”` : "verbatim"}>
              {item.source_excerpt}
            </SourceQuote>
          ) : (
            <p className="text-[13px] text-muted">
              {item.origin === "INTERPRETATION"
                ? "This is IdeaVault's interpretation — there's no verbatim source text for it."
                : "Recorded directly, without a quoted source message."}
            </p>
          )}
          {ex.source_window && ex.source_window.length > 0 && <MessageWindow messages={ex.source_window} highlight={ex.source_message?.id} provider={ex.conversation?.provider} />}
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-[12.5px]">
            {ex.conversation && (
              <Link
                href={`/conversations/${ex.conversation.id}${ex.source_message ? `#m-${ex.source_message.id}` : ""}`}
                className="inline-flex items-center gap-1.5 text-muted hover:text-fg"
              >
                <MessagesSquare className="h-3.5 w-3.5" /> Open conversation
                <span className="text-faint">· {humanize(ex.conversation.origin)}</span>
              </Link>
            )}
            {ex.source && (
              <span className="inline-flex items-center gap-1.5 text-muted">
                Source: {ex.source.title || ex.source.provider}
                {ex.source.uri && /^https?:\/\//.test(ex.source.uri) && (
                  <a href={ex.source.uri} target="_blank" rel="noopener noreferrer" className="text-faint hover:text-fg" aria-label="Open source link">
                    <ExternalLink className="h-3 w-3" />
                  </a>
                )}
                {!ex.source.trusted && <UntrustedLabel />}
              </span>
            )}
          </div>
        </div>
      </Section>

      {(supporting.length > 0 || challenging.length > 0) && (
        <Section title="Evidence">
          <div className="space-y-4">
            {supporting.length > 0 && <EvidenceList title="Supports it" items={supporting} onNavigate={onNavigate} tone="action" />}
            {challenging.length > 0 && <EvidenceList title="Challenges it" items={challenging} onNavigate={onNavigate} tone="question" />}
          </div>
        </Section>
      )}

      {chain.length > 1 && (
        <Section title="History">
          <ol className="relative ml-1.5">
            {chain.map((c, i) => {
              const current = c.id === item.id;
              const live = isLive(c);
              return (
                <li key={c.id} className="relative pb-4 pl-6 last:pb-0">
                  {i < chain.length - 1 && <span className="absolute left-[4.5px] top-3 h-full w-px bg-border" aria-hidden />}
                  <span
                    className={cn("absolute left-0 top-1.5 h-2.5 w-2.5 rounded-full border-2", current && "ring-4 ring-accent/15")}
                    style={{ borderColor: tone(c.kind), background: live ? tone(c.kind) : "var(--surface)" }}
                    aria-hidden
                  />
                  <ItemLink item={c} onNavigate={current ? undefined : onNavigate} className={cn("block rounded-[var(--radius-md)]", !current && "hover:opacity-80")}>
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-mono text-[12px] font-semibold" style={{ color: tone(c.kind) }}>
                        {c.label}
                      </span>
                      <ItemStatus status={c.status} />
                      <span className="text-[11px] text-faint">{fullDate(c.created_at)}</span>
                      {current && <span className="text-[11px] font-medium text-accent">viewing</span>}
                    </div>
                    <p className={cn("mt-0.5 text-[13.5px] leading-snug", live ? "text-fg" : "text-faint line-through decoration-faint/60")}>{c.statement}</p>
                  </ItemLink>
                </li>
              );
            })}
          </ol>
        </Section>
      )}

      {(ex.checkpoints?.length ?? 0) > 0 && (
        <Section title="Captured in checkpoints">
          <div className="flex flex-wrap gap-1.5">
            {ex.checkpoints.map((c) => (
              <EntityChip key={c.id} type="checkpoint" id={c.id} label={`${c.label} · ${truncate(c.title, 40)}`} title={`${c.label} on ${c.branch_name ?? "branch"} · ${fullDate(c.created_at)}`} />
            ))}
          </div>
        </Section>
      )}

      {ex.inherited_from && (
        <Section title="Inherited from">
          <ItemLink item={ex.inherited_from} onNavigate={onNavigate} className="flex items-start gap-2.5 rounded-[var(--radius-md)] px-1 py-1 hover:bg-surface-2">
            <CornerDownRight className="mt-0.5 h-3.5 w-3.5 shrink-0 text-faint" />
            <RefLabel kind={ex.inherited_from.kind} label={ex.inherited_from.label} />
            <span className="min-w-0 text-[13.5px] text-fg">{ex.inherited_from.statement}</span>
          </ItemLink>
          <p className="mt-2 text-[12px] text-faint">Copied when this branch was forked. The original keeps its own history on its branch.</p>
        </Section>
      )}

      {otherRels.length > 0 && (
        <Section title="Related">
          <ul className="space-y-1.5">
            {otherRels.map((r) => (
              <RelationshipRow key={r.id} rel={r} selfId={item.id} />
            ))}
          </ul>
        </Section>
      )}

      <Section title="Provenance">
        <dl>
          <DetailRow label="Recorded by">{humanize(item.created_by)}</DetailRow>
          <DetailRow label="Recorded">{fullDate(item.created_at)}</DetailRow>
          {item.updated_at !== item.created_at && <DetailRow label="Status changed">{fullDate(item.updated_at)}</DetailRow>}
          <DetailRow label="Origin">{item.origin === "SOURCE" ? "Source — stated in conversation" : "Interpretation — inferred by IdeaVault"}</DetailRow>
          {typeof item.confidence === "number" && <DetailRow label="Confidence">{Math.round(item.confidence * 100)}%</DetailRow>}
          <DetailRow label="Review">{humanize(item.review_state)}</DetailRow>
        </dl>
      </Section>
    </article>
  );
}

type Resolved = { id: string; kind: string; label: string; statement: string };

/** Find a label/statement for an item id among everything the explanation already returned. */
function resolveRef(ex: KnowledgeExplanation, id: string): Resolved | null {
  const pools = [ex.chain, ex.supporting, ex.challenging, ex.inherited_from ? [ex.inherited_from] : []];
  for (const pool of pools) {
    const hit = (pool ?? []).find((i) => i.id === id);
    if (hit) return { id, kind: hit.kind, label: hit.label, statement: hit.statement };
  }
  for (const r of ex.relationships ?? []) {
    const [type, raw] = r.from_id === id ? [r.from_type, r.from_label] : r.to_id === id ? [r.to_type, r.to_label] : [null, null];
    if (type && raw) {
      const m = /^([A-Z]{1,2}\d+)\s+([\s\S]*)$/.exec(raw);
      return { id, kind: type, label: m ? m[1] : entityMeta(type).label, statement: m ? m[2] : raw };
    }
  }
  return null;
}

function RefLink({ id, resolve, onNavigate, fallback }: { id: string; resolve: (id: string) => Resolved | null; onNavigate?: (id: string) => void; fallback: string }) {
  const r = resolve(id);
  return (
    <ItemLink item={{ id }} onNavigate={onNavigate} className="inline-flex max-w-full items-start gap-1.5 text-left hover:text-accent">
      {r ? (
        <>
          <RefLabel kind={r.kind} label={r.label} />
          <span className="min-w-0">{truncate(r.statement, 90)}</span>
        </>
      ) : (
        <span className="text-accent underline-offset-2 hover:underline">{fallback}</span>
      )}
    </ItemLink>
  );
}

function KindDetails({ item, onNavigate, resolve }: { item: KnowledgeItem; onNavigate?: (id: string) => void; resolve: (id: string) => Resolved | null }) {
  const rows: ReactNode[] = [];
  if (item.decision) {
    if (item.decision.rationale)
      rows.push(
        <div key="why" className="mt-4">
          <p className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-faint">Why</p>
          <p className="whitespace-pre-wrap text-[14.5px] leading-relaxed text-fg">{item.decision.rationale}</p>
        </div>,
      );
    if (item.decision.alternatives?.length)
      rows.push(
        <div key="alts" className="mt-4">
          <p className="mb-1.5 text-[11px] font-semibold uppercase tracking-wider text-faint">Alternatives considered</p>
          <ul className="space-y-1.5">
            {item.decision.alternatives.map((a, i) => (
              <li key={i} className="flex gap-2 text-[13.5px]">
                <X className="mt-0.5 h-3.5 w-3.5 shrink-0 text-faint" aria-hidden />
                <span className="min-w-0">
                  <span className="text-fg">{a.option}</span>
                  {a.reason_rejected && <span className="text-muted"> — {a.reason_rejected}</span>}
                </span>
              </li>
            ))}
          </ul>
        </div>,
      );
  }
  const meta: [string, ReactNode][] = [];
  if (item.decision?.decided_at) meta.push(["Decided", fullDate(item.decision.decided_at)]);
  if (item.decision?.reverses_id)
    meta.push(["Reverses", <RefLink key="rev" id={item.decision.reverses_id} resolve={resolve} onNavigate={onNavigate} fallback="the earlier decision" />]);
  if (item.assumption) {
    meta.push(["Risk if wrong", <RiskText key="r" level={item.assumption.risk} />]);
    if (item.assumption.validation_method) meta.push(["Validate by", item.assumption.validation_method]);
    if (item.assumption.validated_at) meta.push(["Validated", fullDate(item.assumption.validated_at)]);
  }
  if (item.evidence) {
    meta.push(["Stance", humanize(item.evidence.stance)]);
    meta.push(["Strength", humanize(item.evidence.strength)]);
    if (item.evidence.url)
      meta.push([
        "Link",
        /^https?:\/\//.test(item.evidence.url) ? (
          <a key="u" href={item.evidence.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 break-all text-accent hover:underline">
            {item.evidence.url} <ExternalLink className="h-3 w-3 shrink-0" />
          </a>
        ) : (
          item.evidence.url
        ),
      ]);
    if (item.evidence.target_item_id)
      meta.push([
        item.evidence.stance === "CHALLENGES" ? "Challenges" : "Supports",
        <RefLink key="t" id={item.evidence.target_item_id} resolve={resolve} onNavigate={onNavigate} fallback="the linked item" />,
      ]);
  }
  if (item.insight) meta.push(["Importance", humanize(item.insight.importance)]);
  if (item.question) {
    if (item.question.answer)
      rows.push(
        <div key="ans" className="mt-4 rounded-[var(--radius-md)] border border-border bg-surface-2 px-3 py-2.5">
          <p className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-faint">Answer</p>
          <p className="whitespace-pre-wrap text-[14px] text-fg">{item.question.answer}</p>
        </div>,
      );
    if (item.question.answered_at) meta.push(["Answered", fullDate(item.question.answered_at)]);
    if (item.question.answered_by_item_id)
      meta.push([
        "Answered by",
        <RefLink key="ab" id={item.question.answered_by_item_id} resolve={resolve} onNavigate={onNavigate} fallback="linked item" />,
      ]);
  }
  if (item.action) {
    meta.push(["Priority", humanize(item.action.priority)]);
    if (item.action.due_at) meta.push(["Due", shortDate(item.action.due_at)]);
    if (item.action.completed_at) meta.push(["Completed", fullDate(item.action.completed_at)]);
  }
  return (
    <>
      {rows}
      {meta.length > 0 && (
        <dl className="mt-4">
          {meta.map(([k, v]) => (
            <DetailRow key={k} label={k}>
              {v}
            </DetailRow>
          ))}
        </dl>
      )}
    </>
  );
}

function RiskText({ level }: { level: string }) {
  return <span className={cn(level === "HIGH" ? "font-medium text-k-question" : level === "MEDIUM" ? "text-k-assumption" : "text-muted")}>{humanize(level)}</span>;
}

function EvidenceList({ title, items, onNavigate, tone: t }: { title: string; items: KnowledgeItem[]; onNavigate?: (id: string) => void; tone: string }) {
  return (
    <div>
      <p className="mb-1.5 flex items-center gap-1.5 text-[12px] font-medium" style={{ color: `var(--k-${t})` }}>
        <span className="h-1.5 w-1.5 rounded-full" style={{ background: `var(--k-${t})` }} aria-hidden />
        {title}
      </p>
      <ul className="space-y-0.5">
        {items.map((e) => (
          <li key={e.id}>
            <ItemLink item={e} onNavigate={onNavigate} className="flex w-full items-start gap-2.5 rounded-[var(--radius-md)] px-1.5 py-1.5 hover:bg-surface-2">
              <RefLabel kind={e.kind} label={e.label} className={cn(!isLive(e) && "opacity-50")} />
              <span className={cn("min-w-0 text-[13.5px] leading-snug", isLive(e) ? "text-fg" : "text-faint line-through")}>{e.statement}</span>
            </ItemLink>
          </li>
        ))}
      </ul>
    </div>
  );
}

function RelationshipRow({ rel, selfId }: { rel: Relationship; selfId: string }) {
  const outgoing = rel.from_id === selfId;
  const otherType = outgoing ? rel.to_type : rel.from_type;
  const otherId = outgoing ? rel.to_id : rel.from_id;
  const otherLabel = outgoing ? rel.to_label : rel.from_label;
  return (
    <li className="flex flex-wrap items-center gap-2 text-[13px]">
      <span className="text-muted">{outgoing ? humanize(rel.rel_type) : `${humanize(rel.rel_type)} (from)`}</span>
      <ArrowRight className="h-3 w-3 text-faint" aria-hidden />
      <EntityChip type={otherType} id={otherId} label={truncate(otherLabel || entityMeta(otherType).label, 60)} />
      {rel.origin === "INTERPRETATION" && <OriginTag origin="INTERPRETATION" />}
    </li>
  );
}

function MessageWindow({ messages, highlight, provider }: { messages: Message[]; highlight?: string; provider?: string }) {
  return (
    <div className="rounded-[var(--radius-lg)] border border-border">
      <p className="border-b border-border px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-faint">Conversation around it</p>
      <ol className="divide-y divide-border">
        {messages.map((m) => {
          const on = m.id === highlight;
          return (
            <li key={m.id} className={cn("px-3 py-2.5", on && "bg-accent-soft/50")}>
              <div className="mb-0.5 flex items-center gap-2 text-[11px] font-medium uppercase tracking-wide text-faint">
                <span>{m.role === "user" ? "You" : m.role === "assistant" ? provider || "Assistant" : m.role}</span>
                <span className="font-normal normal-case tracking-normal">#{m.position}</span>
                {m.untrusted && <UntrustedLabel />}
                {on && <span className="text-accent">source</span>}
              </div>
              <p className="line-clamp-6 whitespace-pre-wrap break-words text-[13px] leading-relaxed text-fg">{m.content}</p>
            </li>
          );
        })}
      </ol>
    </div>
  );
}

// ---------- Actions ----------

function useItemInvalidation(item: KnowledgeItem) {
  const qc = useQueryClient();
  return () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: ["knowledge"] }),
      qc.invalidateQueries({ queryKey: ["idea", item.idea_id] }),
      qc.invalidateQueries({ queryKey: ["ideas"] }),
    ]);
}

function ItemActions({ item }: { item: KnowledgeItem }) {
  const refresh = useItemInvalidation(item);
  const [answering, setAnswering] = useState(false);
  const [answer, setAnswer] = useState("");
  const [superseding, setSuperseding] = useState(false);

  const setStatus = useMutation({
    mutationFn: (body: { status: string; answer?: string }) => api.patch<KnowledgeItem>(`/v1/knowledge/${item.id}/status`, body),
    onSuccess: (it) => {
      toast.success(`${it.label} → ${humanize(it.status).toLowerCase()}`);
      setAnswering(false);
      setAnswer("");
      void refresh();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const review = useMutation({
    mutationFn: (accept: boolean) => api.post(`/v1/knowledge/review`, { ids: [item.id], accept }),
    onSuccess: (_, accept) => {
      toast.success(accept ? `${item.label} accepted into the idea` : `${item.label} rejected`);
      void refresh();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  if (item.review_state === "PROPOSED") {
    return (
      <div className="flex flex-wrap items-center gap-2 border-t border-border bg-warning-soft/40 px-5 py-3">
        <p className="mr-auto text-[13px] text-muted">Proposed from an import or extraction — it doesn&apos;t count until you accept it.</p>
        <Button size="sm" variant="primary" onClick={() => review.mutate(true)} loading={review.isPending && review.variables === true}>
          <Check className="h-3.5 w-3.5" /> Accept
        </Button>
        <Button size="sm" variant="secondary" onClick={() => review.mutate(false)} loading={review.isPending && review.variables === false}>
          Reject
        </Button>
      </div>
    );
  }
  if (item.review_state !== "ACCEPTED" || item.superseded_by_id) return null;

  const actions = statusActions(item.kind, item.status);
  return (
    <div className="border-t border-border px-5 py-3">
      <div className="flex flex-wrap items-center gap-1.5">
        {actions.map((a) => (
          <Button
            key={a.status}
            size="sm"
            variant={a.tone === "positive" ? "primary" : "secondary"}
            className={cn(a.tone === "negative" && "text-danger")}
            loading={setStatus.isPending && setStatus.variables?.status === a.status}
            onClick={() => (a.needsAnswer ? setAnswering((v) => !v) : setStatus.mutate({ status: a.status }))}
            aria-expanded={a.needsAnswer ? answering : undefined}
          >
            {a.label}
          </Button>
        ))}
        <Button size="sm" variant="ghost" onClick={() => setSuperseding((v) => !v)} aria-expanded={superseding}>
          <History className="h-3.5 w-3.5" /> Supersede…
        </Button>
      </div>
      {answering && (
        <form
          className="mt-3 flex gap-2 animate-slide-up"
          onSubmit={(e) => {
            e.preventDefault();
            if (answer.trim()) setStatus.mutate({ status: "ANSWERED", answer: answer.trim() });
          }}
        >
          <Input value={answer} onChange={(e) => setAnswer(e.target.value)} placeholder="The answer is…" autoFocus aria-label="Answer" />
          <Button type="submit" size="md" variant="primary" disabled={!answer.trim()} loading={setStatus.isPending}>
            Save
          </Button>
        </form>
      )}
      {superseding && <SupersedeForm item={item} onDone={() => setSuperseding(false)} />}
    </div>
  );
}

function SupersedeForm({ item, onDone }: { item: KnowledgeItem; onDone: () => void }) {
  const refresh = useItemInvalidation(item);
  const [form, setForm] = useState<KnowledgeFormState>(() => ({
    ...emptyKnowledgeForm(item.kind as KnowledgeFormState["kind"]),
    risk: (item.assumption?.risk as KnowledgeFormState["risk"]) ?? "MEDIUM",
    priority: (item.action?.priority as KnowledgeFormState["priority"]) ?? "MEDIUM",
    importance: (item.insight?.importance as KnowledgeFormState["importance"]) ?? "MEDIUM",
    stance: (item.evidence?.stance as KnowledgeFormState["stance"]) ?? "SUPPORTS",
    strength: (item.evidence?.strength as KnowledgeFormState["strength"]) ?? "MODERATE",
  }));
  const [reverses, setReverses] = useState(false);
  const [errors, setErrors] = useState<BuildResult["errors"]>({});
  const save = useMutation({
    mutationFn: (body: unknown) => api.post<KnowledgeItem>("/v1/knowledge", body),
    onSuccess: (it) => {
      toast.success(`${it.label} ${reverses ? "reverses" : "supersedes"} ${item.label}`);
      onDone();
      void refresh();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <form
      className="mt-4 rounded-[var(--radius-lg)] border border-border bg-surface-2/50 p-4 animate-slide-up"
      onSubmit={(e) => {
        e.preventDefault();
        const r = buildKnowledgePayload(form, { ideaId: item.idea_id, branchId: item.branch_id, supersedes: item.id, reverses });
        setErrors(r.errors);
        if (r.payload) save.mutate(r.payload);
      }}
    >
      <p className="mb-4 text-[13px] text-muted">
        Record what replaces <span className="font-medium text-fg">{item.label}</span>. The original stays on record, marked {reverses ? "reversed" : "superseded"}, and links to the new
        version.
      </p>
      <KnowledgeFields form={form} set={(p) => setForm((f) => ({ ...f, ...p }))} errors={errors} kindLocked supersede idPrefix={`sup-${item.id.slice(0, 6)}`} />
      {item.kind === "decision" && (
        <label className="mt-4 flex items-start gap-2 text-[13px] text-fg">
          <input type="checkbox" checked={reverses} onChange={(e) => setReverses(e.target.checked)} className="mt-0.5 accent-[var(--accent)]" />
          <span>
            This <span className="font-medium">reverses</span> the decision (not just refines it)
          </span>
        </label>
      )}
      <div className="mt-4 flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" size="sm" loading={save.isPending}>
          {reverses ? "Record reversal" : "Record new version"}
        </Button>
      </div>
    </form>
  );
}
