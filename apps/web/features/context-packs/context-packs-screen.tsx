"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { MessageSquare, Package } from "lucide-react";
import { api } from "@/lib/api";
import { compactNumber, fullDate, plural, timeAgo } from "@/lib/format";
import type { ContextPack } from "@/lib/types";
import { EntityChip } from "@/components/ui/badges";
import { EmptyState, ErrorState, PageHeader, Skeleton } from "@/components/ui/primitives";
import { LinkButton, useIdeaTitles } from "@/features/artifacts/kit";

export function ContextPacksScreen() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["context-packs"],
    queryFn: () => api.get<ContextPack[]>("/v1/context-packs"),
  });
  const titles = useIdeaTitles();

  return (
    <div className="mx-auto max-w-5xl px-5 py-8 sm:px-8 sm:py-10">
      <PageHeader
        title="Context packs"
        description="Portable snapshots of an idea's thinking. Continue in ChatGPT, Claude or Gemini, then bring the conversation back to merge what changed."
        actions={
          <LinkButton href="/deltas" size="sm">
            Brought-back conversations
          </LinkButton>
        }
      />
      {isLoading ? (
        <div className="border-t border-border">
          {[0, 1].map((i) => (
            <div key={i} className="space-y-2.5 border-b border-border py-5">
              <Skeleton className="h-6 w-1/2" />
              <Skeleton className="h-3.5 w-3/4" />
            </div>
          ))}
        </div>
      ) : error ? (
        <ErrorState error={error} onRetry={() => void refetch()} />
      ) : !data?.length ? (
        <EmptyState
          icon={Package}
          title="No context packs yet"
          description="A context pack captures an idea's decisions, assumptions and open questions so another AI can pick up where you left off. Build one from an idea, or ask in chat."
          action={
            <LinkButton href={`/?q=${encodeURIComponent("Build a context pack for my idea")}`} size="sm" variant="primary">
              <MessageSquare className="h-3.5 w-3.5" /> “Build a context pack for my idea”
            </LinkButton>
          }
        />
      ) : (
        <>
          <p className="mb-2 text-[12px] text-faint">{plural(data.length, "pack")}</p>
          <ul className="animate-fade-in border-t border-border">
            {data.map((p) => (
              <li key={p.id} className="group relative -mx-3 border-b border-border px-3 py-5 transition-colors hover:bg-surface-2/50 sm:rounded-[var(--radius-md)]">
                <h3 className="font-display text-[1.35rem] leading-tight text-fg transition-colors group-hover:text-accent">
                  <Link href={`/context-packs/${p.id}`} className="after:absolute after:inset-0 after:content-[''] focus-visible:outline-none focus-visible:after:rounded-[var(--radius-md)] focus-visible:after:ring-2 focus-visible:after:ring-accent">
                    {p.title}
                  </Link>
                </h3>
                <p className="mt-1 line-clamp-2 max-w-3xl text-[14px] text-muted">{p.objective}</p>
                <div className="mt-2.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-faint">
                  <span className="relative z-10">
                    <EntityChip type="idea" id={p.idea_id} label={titles.get(p.idea_id) ?? "Idea"} />
                  </span>
                  <span>≈ {compactNumber(p.token_estimate)} tokens</span>
                  <span title={fullDate(p.created_at)}>Built {timeAgo(p.created_at)}</span>
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
