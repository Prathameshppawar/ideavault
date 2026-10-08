"use client";

import Link from "next/link";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, GitCompareArrows, Plus } from "lucide-react";
import { api } from "@/lib/api";
import { fullDate, plural, timeAgo } from "@/lib/format";
import type { Delta } from "@/lib/types";
import { EmptyState, ErrorState, PageHeader, Skeleton } from "@/components/ui/primitives";
import { analyzerLabel } from "@/features/artifacts/artifact-meta";
import { LinkButton, Pill, Segmented, useIdeaTitles } from "@/features/artifacts/kit";
import { deltaStatusMeta, isPendingDelta } from "./delta-logic";

type Filter = "all" | "pending" | "resolved";

export function DeltasScreen() {
  const [filter, setFilter] = useState<Filter>("all");
  const { data, isLoading, error, refetch } = useQuery({ queryKey: ["deltas", {}], queryFn: () => api.get<Delta[]>("/v1/deltas") });
  const titles = useIdeaTitles();
  const rows = (data ?? []).filter((d) => (filter === "all" ? true : filter === "pending" ? isPendingDelta(d.status) : !isPendingDelta(d.status)));
  const pendingCount = (data ?? []).filter((d) => isPendingDelta(d.status)).length;

  return (
    <div className="mx-auto max-w-5xl px-5 py-8 sm:px-8 sm:py-10">
      <PageHeader
        title="External conversations"
        description="Conversations you continued in ChatGPT, Claude or Gemini and brought back. Review what changed before anything becomes part of your thinking."
        actions={
          <LinkButton href="/deltas/new" variant="primary" size="sm">
            <Plus className="h-3.5 w-3.5" /> Bring back a conversation
          </LinkButton>
        }
      />
      {isLoading ? (
        <div className="border-t border-border">
          {[0, 1].map((i) => (
            <div key={i} className="space-y-2.5 border-b border-border py-5">
              <Skeleton className="h-3 w-32" />
              <Skeleton className="h-5 w-1/2" />
              <Skeleton className="h-3.5 w-3/4" />
            </div>
          ))}
        </div>
      ) : error ? (
        <ErrorState error={error} onRetry={() => void refetch()} />
      ) : !data?.length ? (
        <EmptyState
          icon={GitCompareArrows}
          title="Nothing brought back yet"
          description="Build a context pack from an idea, continue the conversation in another AI, then bring it back here. IdeaVault shows what's new, changed or rejected — and merges only what you choose."
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <LinkButton href="/context-packs" size="sm">
                Context packs
              </LinkButton>
              <LinkButton href="/deltas/new" size="sm" variant="primary">
                Bring back a conversation
              </LinkButton>
            </div>
          }
        />
      ) : (
        <>
          <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
            <Segmented<Filter>
              label="Filter"
              size="sm"
              value={filter}
              onChange={setFilter}
              options={[
                { value: "all", label: <>All <span className="text-faint">{data.length}</span></> },
                { value: "pending", label: <>Awaiting review <span className="text-faint">{pendingCount}</span></> },
                { value: "resolved", label: "Resolved" },
              ]}
            />
            <span className="text-[12px] text-faint">{plural(rows.length, "conversation")}</span>
          </div>
          {rows.length === 0 ? (
            <p className="rounded-[var(--radius-lg)] border border-dashed border-border px-4 py-8 text-center text-[13px] text-muted">
              {filter === "pending" ? "Nothing is waiting for review." : "No resolved reviews yet."}
            </p>
          ) : (
            <ul className="animate-fade-in border-t border-border">
              {rows.map((d) => {
                const s = deltaStatusMeta(d.status);
                const n = d.items?.length;
                return (
                  <li key={d.id}>
                    <Link href={`/deltas/${d.id}`} className="group -mx-3 flex items-start gap-4 border-b border-border px-3 py-4 transition-colors hover:bg-surface-2/50 sm:rounded-[var(--radius-md)]">
                      <div className="min-w-0 flex-1">
                        <div className="mb-1 flex flex-wrap items-center gap-2">
                          <Pill tone={s.tone} pulse={isPendingDelta(d.status)}>
                            {s.label}
                          </Pill>
                          <span className="text-[12px] text-faint">{analyzerLabel(d.analyzer)}</span>
                        </div>
                        <p className="font-display text-[1.3rem] leading-tight text-fg transition-colors group-hover:text-accent">{titles.get(d.idea_id) ?? "Idea"}</p>
                        <p className="mt-1 line-clamp-2 max-w-3xl text-[13.5px] text-muted">{d.summary}</p>
                      </div>
                      <div className="flex shrink-0 flex-col items-end gap-1.5 pt-0.5 text-[12px] text-faint">
                        <span title={fullDate(d.created_at)}>{timeAgo(d.created_at)}</span>
                        {n !== undefined && <span>{plural(n, "change")}</span>}
                        <ArrowRight className="h-3.5 w-3.5 opacity-0 transition-opacity group-hover:opacity-100" aria-hidden />
                      </div>
                    </Link>
                  </li>
                );
              })}
            </ul>
          )}
        </>
      )}
    </div>
  );
}
