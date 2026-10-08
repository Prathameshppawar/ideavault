"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ArrowUpRight, CornerDownLeft, Search, ShieldAlert, Sparkles, X } from "lucide-react";
import { api } from "@/lib/api";
import type { IdeaCandidate, SearchHit, SearchResponse } from "@/lib/types";
import { entityHref, entityMeta, isKnowledge, tone } from "@/lib/entities";
import { cn, humanize, plural, shortDate } from "@/lib/format";
import { RefLabel } from "@/components/domain/knowledge";
import { ItemStatus, OriginTag, StatusBadge } from "@/components/ui/badges";
import { EmptyState, ErrorState, Kbd, Skeleton } from "@/components/ui/primitives";
import { useParamSetter } from "@/features/dashboard/lib/url";
import { Heading, IdeaLink, Segmented, ToggleChip } from "@/features/dashboard/ui";
import { cleanSnippet, parseHighlights } from "./highlight";
import { describeInterpretation } from "./interpretation";

export const EXAMPLES = [
  "Where did I first talk about AI automation?",
  "Decisions related to the biker marketplace",
  "Ideas similar to HireHub",
  "What assumptions have never been validated?",
];

const TYPES = ["idea", "decision", "assumption", "evidence", "insight", "question", "action", "artifact", "conversation", "message"] as const;
type Mode = "hybrid" | "keyword" | "semantic";
const API_MODE: Record<Mode, string | undefined> = { hybrid: undefined, keyword: "fts", semantic: "semantic" };
type Order = "auto" | "relevance" | "newest" | "oldest";
/** Superseded/reversed knowledge stays findable but reads as history. */
const DEAD = new Set(["SUPERSEDED", "REVERSED", "REJECTED", "RETRACTED", "INVALIDATED", "DROPPED"]);

/** Highlighted snippet: «term» → <mark>, rendered as React nodes (no HTML injection). */
export function Snippet({ text, className, as: Tag = "p" }: { text: string; className?: string; as?: "p" | "span" }) {
  const segs = useMemo(() => parseHighlights(cleanSnippet(text)), [text]);
  return (
    <Tag className={className}>
      {segs.map((s, i) =>
        s.hit ? (
          <mark key={i} className="rounded-[3px] bg-accent-soft px-0.5 text-fg">
            {s.text}
          </mark>
        ) : (
          <span key={i}>{s.text}</span>
        ),
      )}
    </Tag>
  );
}

/** True when a snippet is just the (possibly truncated) title with match markers. */
export function sameText(snippet: string, title: string): boolean {
  const a = cleanSnippet(snippet).replace(/[«»]/g, "").replace(/…$/, "").trim().toLowerCase();
  const b = cleanSnippet(title).trim().toLowerCase();
  return !!a && !!b && (a === b || (a.length > 24 && b.startsWith(a)));
}

function hitHref(h: SearchHit): string {
  if (h.entity_type === "message" && h.conversation_id) return `/conversations/${h.conversation_id}#m-${h.entity_id}`;
  return entityHref(h.entity_type, h.entity_id, h.idea_id);
}

export function SearchScreen() {
  const params = useSearchParams();
  const setParams = useParamSetter();
  const q = params.get("q") ?? "";
  const modeParam = params.get("mode");
  const mode: Mode = modeParam === "keyword" || modeParam === "semantic" ? modeParam : "hybrid";
  const orderParam = params.get("order");
  const order: Order = orderParam === "relevance" || orderParam === "newest" || orderParam === "oldest" ? orderParam : "auto";
  const types = useMemo(() => (params.get("types") ?? "").split(",").filter((t) => (TYPES as readonly string[]).includes(t)), [params]);
  const [grouped, setGrouped] = useState(false);

  // Input text mirrors ?q= but can run ahead of it while typing.
  const [text, setText] = useState(q);
  const [syncedQ, setSyncedQ] = useState(q);
  if (q !== syncedQ) {
    setSyncedQ(q);
    setText(q);
  }
  const inputRef = useRef<HTMLInputElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const setParamsRef = useRef(setParams);
  useEffect(() => {
    setParamsRef.current = setParams;
  }, [setParams]);
  useEffect(() => () => {
    if (timer.current) clearTimeout(timer.current);
  }, []);

  const commit = (value: string, delay = 0) => {
    if (timer.current) clearTimeout(timer.current);
    const run = () => setParamsRef.current({ q: value.trim() || null });
    if (delay) timer.current = setTimeout(run, delay);
    else run();
  };

  // "/" focuses the search box from anywhere on the page.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable)) return;
      e.preventDefault();
      inputRef.current?.focus();
      inputRef.current?.select();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const enabled = q.trim().length > 0;
  const { data, error, isLoading, isFetching, refetch, isPlaceholderData } = useQuery({
    queryKey: ["search", { q, mode, types: types.join(","), order }],
    queryFn: ({ signal }) =>
      api.get<SearchResponse>("/v1/search", {
        query: { q, mode: API_MODE[mode], types: types.length ? types : undefined, order: order === "auto" ? undefined : order, limit: 40 },
        signal,
      }),
    enabled,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });

  const toggleType = (t: string) => {
    const next = types.includes(t) ? types.filter((x) => x !== t) : [...types, t];
    setParams({ types: next.length ? next.join(",") : null });
  };

  const results = data?.results ?? [];
  const similar = data?.similar_ideas ?? [];

  return (
    <div className="mx-auto w-full max-w-4xl px-4 pb-16 pt-8 sm:px-8 sm:pt-12">
      <h1 className="sr-only">Search</h1>
      <form
        role="search"
        onSubmit={(e) => {
          e.preventDefault();
          commit(text);
        }}
        className="group relative"
      >
        <Search className="pointer-events-none absolute left-4 top-1/2 h-5 w-5 -translate-y-1/2 text-faint transition-colors group-focus-within:text-accent" aria-hidden />
        <input
          ref={inputRef}
          type="search"
          name="q"
          autoFocus
          autoComplete="off"
          spellCheck={false}
          aria-label="Search your vault"
          placeholder="Ask your vault anything…"
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            commit(e.target.value, 450);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape" && text) {
              e.preventDefault();
              setText("");
              commit("");
            }
          }}
          className="h-14 w-full rounded-[var(--radius-xl)] border border-border bg-surface pl-12 pr-24 font-display text-[1.45rem] text-fg shadow-sm transition-colors placeholder:text-faint hover:border-border-strong focus:border-accent focus:outline-none focus:ring-4 focus:ring-accent/10 sm:text-[1.6rem] [&::-webkit-search-cancel-button]:hidden"
        />
        <div className="absolute right-3 top-1/2 flex -translate-y-1/2 items-center gap-1.5">
          {text ? (
            <button
              type="button"
              aria-label="Clear search"
              onClick={() => {
                setText("");
                commit("");
                inputRef.current?.focus();
              }}
              className="rounded p-1 text-faint hover:bg-surface-2 hover:text-fg"
            >
              <X className="h-4 w-4" />
            </button>
          ) : (
            <Kbd className="hidden sm:inline-flex">/</Kbd>
          )}
          <button type="submit" aria-label="Search" className="hidden h-8 w-8 items-center justify-center rounded-[var(--radius-md)] text-muted hover:bg-surface-2 hover:text-fg sm:flex">
            <CornerDownLeft className="h-4 w-4" />
          </button>
        </div>
      </form>

      <div className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-2">
        <Segmented<Mode>
          label="Search mode"
          size="xs"
          value={mode}
          onChange={(m) => setParams({ mode: m === "hybrid" ? null : m })}
          options={[
            { value: "hybrid", label: "Hybrid", title: "Keywords and meaning, fused" },
            { value: "keyword", label: "Keyword", title: "Exact words (full-text)" },
            { value: "semantic", label: "Semantic", title: "Meaning (embeddings)" },
          ]}
        />
        <label className="sr-only" htmlFor="search-order">
          Order
        </label>
        <select
          id="search-order"
          value={order}
          onChange={(e) => setParams({ order: e.target.value === "auto" ? null : e.target.value })}
          className="h-7 rounded-[var(--radius-md)] border border-border bg-surface-2 px-2 text-[12px] font-medium text-muted hover:text-fg focus:border-accent focus:outline-none"
        >
          <option value="auto">Order: as understood</option>
          <option value="relevance">Most relevant</option>
          <option value="newest">Newest first</option>
          <option value="oldest">Oldest first</option>
        </select>
        {types.length > 0 && (
          <button type="button" onClick={() => setParams({ types: null })} className="text-[12px] text-muted underline decoration-border-strong underline-offset-2 hover:text-fg">
            Clear types
          </button>
        )}
      </div>
      <div className="-mx-1 mt-2.5 flex gap-1.5 overflow-x-auto px-1 pb-1 sm:flex-wrap sm:overflow-visible" role="group" aria-label="Filter by type">
        {TYPES.map((t) => (
          <ToggleChip key={t} pressed={types.includes(t)} onClick={() => toggleType(t)} tone={entityMeta(t).tone}>
            {entityMeta(t).plural}
          </ToggleChip>
        ))}
      </div>

      <div className="mt-8">
        {!enabled ? (
          <Suggestions
            onPick={(ex) => {
              setText(ex);
              commit(ex);
            }}
          />
        ) : isLoading && !data ? (
          <ResultsSkeleton />
        ) : error && !data ? (
          <ErrorState error={error} onRetry={() => void refetch()} />
        ) : data ? (
          <div className={cn("transition-opacity", (isPlaceholderData || isFetching) && "opacity-60")} aria-busy={isFetching}>
            <div className="mb-6 flex flex-wrap items-start justify-between gap-x-6 gap-y-2">
              <p className="flex max-w-2xl items-start gap-2 text-[15px] text-muted" aria-live="polite">
                <Sparkles className="mt-1 h-3.5 w-3.5 shrink-0 text-k-insight" aria-hidden />
                <span>
                  {describeInterpretation(data.interpretation)}{" "}
                  <span className="text-faint">
                    {plural(results.length, "result")}
                    {mode === "hybrid" && !data.semantic ? " · keyword match only" : ""}
                    {mode === "semantic" && !data.semantic ? " · meaning-based search isn't available for this query" : ""}
                  </span>
                </span>
              </p>
              {results.length > 1 && (
                <Segmented<"ranked" | "grouped">
                  label="Result layout"
                  size="xs"
                  value={grouped ? "grouped" : "ranked"}
                  onChange={(v) => setGrouped(v === "grouped")}
                  options={[
                    { value: "ranked", label: "Ranked" },
                    { value: "grouped", label: "By type" },
                  ]}
                />
              )}
            </div>

            {similar.length > 0 && <SimilarIdeas items={similar} to={data.interpretation.similar_to} />}

            {results.length === 0 ? (
              similar.length === 0 &&
              (data.interpretation.similar_to ? (
                <EmptyState
                  icon={Search}
                  title="No similar ideas found"
                  description={
                    <>
                      Nothing in your vault resembles “{data.interpretation.similar_to}” closely enough. Try describing it in a few words instead — for example “ideas about hiring marketplaces”.
                    </>
                  }
                />
              ) : (
                <EmptyState
                  icon={Search}
                  title="Nothing matched"
                  description={
                    <>
                      {mode !== "semantic" ? "Try semantic mode to match by meaning rather than exact words" : "Try keyword mode or fewer words"}
                      {types.length ? ", or clear the type filters." : "."}
                    </>
                  }
                  action={
                    <div className="flex flex-wrap justify-center gap-2">
                      {mode !== "semantic" && (
                        <button type="button" onClick={() => setParams({ mode: "semantic" })} className="h-8 rounded-[var(--radius-md)] border border-border bg-surface px-3 text-[13px] font-medium text-fg shadow-sm hover:bg-surface-2">
                          Search by meaning
                        </button>
                      )}
                      {types.length > 0 && (
                        <button type="button" onClick={() => setParams({ types: null })} className="h-8 rounded-[var(--radius-md)] border border-border bg-surface px-3 text-[13px] font-medium text-fg shadow-sm hover:bg-surface-2">
                          Clear type filters
                        </button>
                      )}
                    </div>
                  }
                />
              ))
            ) : grouped ? (
              <GroupedResults results={results} />
            ) : (
              <>
                {similar.length > 0 && <Heading count={results.length}>Mentions</Heading>}
                <ol>
                  {results.map((h) => (
                    <ResultRow key={`${h.entity_type}:${h.entity_id}`} h={h} />
                  ))}
                </ol>
              </>
            )}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function GroupedResults({ results }: { results: SearchHit[] }) {
  const groups = TYPES.map((t) => ({ t, items: results.filter((r) => r.entity_type === t) })).filter((g) => g.items.length);
  const other = results.filter((r) => !(TYPES as readonly string[]).includes(r.entity_type));
  if (other.length) groups.push({ t: "other" as (typeof TYPES)[number], items: other });
  return (
    <div className="space-y-8">
      {groups.map((g) => (
        <section key={g.t} aria-label={entityMeta(g.t).plural}>
          <Heading count={g.items.length}>{g.t === ("other" as string) ? "Other" : entityMeta(g.t).plural}</Heading>
          <ol>
            {g.items.map((h) => (
              <ResultRow key={`${h.entity_type}:${h.entity_id}`} h={h} />
            ))}
          </ol>
        </section>
      ))}
    </div>
  );
}

function ResultRow({ h }: { h: SearchHit }) {
  const meta = entityMeta(h.entity_type);
  const Icon = meta.icon;
  const knowledge = isKnowledge(h.entity_type);
  const isMessage = h.entity_type === "message";
  const isConversation = h.entity_type === "conversation";
  const imported = h.untrusted || ((isConversation || isMessage) && h.origin === "imported");
  const roleText = isMessage ? (h.role === "user" ? "You" : h.role === "assistant" ? "Assistant" : h.role) : null;
  const cleanTitle = cleanSnippet(h.title || "");
  const title = isMessage ? `${roleText ? `${roleText} in ` : ""}“${cleanTitle}”` : cleanTitle || meta.label;
  // Knowledge snippets usually repeat the statement: highlight the title instead of repeating it.
  const snippetIsTitle = !!h.snippet && sameText(h.snippet, h.title);
  return (
    <li className="group relative -mx-3 flex gap-3 rounded-[var(--radius-lg)] px-3 py-3.5 transition-colors hover:bg-surface-2/70">
      <span className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-md)]" style={{ background: tone(h.entity_type, true), color: tone(h.entity_type) }} aria-hidden>
        <Icon className="h-3.5 w-3.5" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-baseline gap-2">
          {knowledge && h.label && <RefLabel kind={h.entity_type} label={h.label} className="self-center" />}
          <Link
            href={hitHref(h)}
            className={cn(
              "min-w-0 text-[15px] font-medium leading-snug text-fg after:absolute after:inset-0 after:rounded-[var(--radius-lg)] after:content-[''] focus-visible:outline-none focus-visible:after:ring-2 focus-visible:after:ring-accent",
              h.entity_type === "idea" && "font-display text-[1.3rem] font-normal",
              knowledge && h.status && DEAD.has(h.status) && "text-faint line-through decoration-faint/60",
            )}
          >
            {snippetIsTitle ? <Snippet text={h.snippet} as="span" /> : title}
          </Link>
        </div>
        {h.snippet && !snippetIsTitle && <Snippet text={h.snippet} className="mt-1 line-clamp-2 break-words text-[14px] leading-relaxed text-muted" />}
        <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-faint">
          <span className="font-medium uppercase tracking-wide" style={{ color: tone(h.entity_type) }}>
            {meta.label}
            {isMessage && h.label ? ` ${h.label}` : ""}
            {h.entity_type === "artifact" && h.label ? ` · ${h.label}` : ""}
          </span>
          {h.entity_type === "idea" && h.status && <StatusBadge status={h.status} />}
          {knowledge && h.status && <ItemStatus status={h.status} />}
          {knowledge && h.origin && <OriginTag origin={h.origin} />}
          {h.entity_type === "artifact" && h.role && <span>{humanize(h.role)}</span>}
          {isConversation && h.role && <span>{h.role === "ideavault" ? "IdeaVault chat" : h.role}</span>}
          {imported && (
            <span className="inline-flex items-center gap-1 rounded-[3px] border border-warning/30 bg-warning-soft px-1 py-px text-[11px] font-medium text-warning" title="Imported from another tool — treated as untrusted data">
              <ShieldAlert className="h-3 w-3" aria-hidden /> Imported · untrusted
            </span>
          )}
          {h.entity_type !== "idea" && (h.idea_id || h.idea_title) && <IdeaLink id={h.idea_id} title={h.idea_title} />}
          <time dateTime={h.created_at}>{shortDate(h.created_at)}</time>
        </div>
      </div>
    </li>
  );
}

function SimilarIdeas({ items, to }: { items: IdeaCandidate[]; to?: string }) {
  return (
    <section aria-labelledby="similar-ideas" className="mb-10">
      <Heading id="similar-ideas" count={items.length}>
        Similar ideas{to ? ` to “${to}”` : ""}
      </Heading>
      <ul>
        {items.map((c) => (
          <li key={c.idea.id} className="group relative -mx-3 flex items-start justify-between gap-4 rounded-[var(--radius-lg)] px-3 py-3 hover:bg-surface-2/70">
            <div className="min-w-0">
              <Link href={`/ideas/${c.idea.id}`} className="font-display text-[1.3rem] leading-tight text-fg after:absolute after:inset-0 after:content-[''] group-hover:text-accent">
                {c.idea.title}
              </Link>
              {(c.idea.summary || c.idea.origin_text) && <p className="mt-0.5 line-clamp-2 text-[13px] text-muted">{c.idea.summary || c.idea.origin_text}</p>}
            </div>
            <div className="flex shrink-0 flex-col items-end gap-1.5 pt-1">
              <span className="text-[13px] font-medium tabular-nums text-fg">{Math.round(Math.min(1, Math.max(0, c.score)) * 100)}% match</span>
              <span className="text-[11px] text-faint">{humanize(c.verdict)}</span>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}

function Suggestions({ onPick }: { onPick: (q: string) => void }) {
  return (
    <div className="animate-fade-in">
      <h2 className="font-display text-[1.6rem] leading-tight text-fg">Ask the way you&apos;d ask yourself.</h2>
      <p className="mt-2 max-w-xl text-[14px] text-muted">
        Search understands time (“first”, “latest”), state (“never validated”, “open questions”, “reversed decisions”) and resemblance (“ideas similar to …”), across ideas, knowledge, artifacts and every conversation — including imported ones.
      </p>
      <ul className="mt-6 divide-y divide-border border-y border-border">
        {EXAMPLES.map((ex) => (
          <li key={ex}>
            <button type="button" onClick={() => onPick(ex)} className="group flex w-full items-center justify-between gap-4 py-3.5 text-left">
              <span className="font-display text-[1.25rem] leading-snug text-fg transition-colors group-hover:text-accent">{ex}</span>
              <ArrowUpRight className="h-4 w-4 shrink-0 text-faint transition-colors group-hover:text-accent" aria-hidden />
            </button>
          </li>
        ))}
      </ul>
      <p className="mt-4 text-[12px] text-faint">
        Press <Kbd>/</Kbd> anywhere on this page to jump to the search box.
      </p>
    </div>
  );
}

function ResultsSkeleton() {
  return (
    <div className="space-y-6" aria-busy="true" aria-label="Searching">
      <Skeleton className="h-4 w-2/3" />
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="flex gap-3">
          <Skeleton className="h-7 w-7 shrink-0" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-3.5 w-full" />
            <Skeleton className="h-3 w-1/3" />
          </div>
        </div>
      ))}
    </div>
  );
}
