"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeftRight, ArrowRight, Braces, Download, FileText, Package } from "lucide-react";
import { ApiError, api, downloadFromApi } from "@/lib/api";
import { compactNumber, fullDate, shortDate, timeAgo } from "@/lib/format";
import type { ContextPack, Delta } from "@/lib/types";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { EntityChip } from "@/components/ui/badges";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/overlay";
import { EmptyState, ErrorState, Skeleton, SkeletonLines } from "@/components/ui/primitives";
import { BackLink, CopyButton, LinkButton, Pill } from "@/features/artifacts/kit";
import { deltaStatusMeta } from "@/features/deltas/delta-logic";
import { packMeta, tokenFit } from "./pack-meta";

export function ContextPackScreen() {
  const { id } = useParams<{ id: string }>();
  const { data: pack, isLoading, error, refetch } = useQuery({
    queryKey: ["context-pack", id],
    queryFn: () => api.get<ContextPack>(`/v1/context-packs/${id}`),
  });
  const deltas = useQuery({
    queryKey: ["deltas", { idea_id: pack?.idea_id }],
    queryFn: () => api.get<Delta[]>("/v1/deltas", { query: { idea_id: pack?.idea_id } }),
    enabled: !!pack?.idea_id,
  });

  if (isLoading) {
    return (
      <div className="mx-auto max-w-6xl px-5 pt-8 sm:px-8 sm:pt-10" aria-busy="true">
        <Skeleton className="h-3.5 w-24" />
        <Skeleton className="mt-6 h-10 w-2/3" />
        <Skeleton className="mt-4 h-4 w-1/2" />
        <div className="mt-10 max-w-[70ch]">
          <SkeletonLines lines={10} />
        </div>
      </div>
    );
  }
  if (error || !pack) {
    return (
      <div className="mx-auto max-w-3xl px-5 py-16 sm:px-8">
        {error instanceof ApiError && error.status === 404 ? (
          <EmptyState icon={Package} title="This context pack doesn't exist" action={<LinkButton href="/context-packs">All context packs</LinkButton>} />
        ) : (
          <ErrorState error={error} onRetry={() => void refetch()} />
        )}
      </div>
    );
  }

  const meta = packMeta(pack);
  const fit = tokenFit(pack.token_estimate);
  const json = JSON.stringify(pack.content_json ?? {}, null, 2);
  const related = (deltas.data ?? []).filter((d) => d.context_pack_id === pack.id);
  const bringBack = `/deltas/new?idea=${pack.idea_id}&pack=${pack.id}${pack.branch_id ? `&branch=${pack.branch_id}` : ""}`;

  return (
    <div className="mx-auto max-w-6xl px-5 pb-16 pt-8 sm:px-8 sm:pt-10">
      <BackLink href="/context-packs">Context packs</BackLink>

      <header className="mt-5">
        <p className="mb-2 inline-flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-k-checkpoint">
          <Package className="h-3.5 w-3.5" aria-hidden /> Context pack
        </p>
        <h1 className="max-w-4xl font-display text-[2.1rem] leading-[1.08] text-fg sm:text-[2.6rem]">{pack.title}</h1>
        <p className="mt-3 max-w-3xl border-l-2 border-border-strong pl-3 text-[15px] leading-relaxed text-fg">
          <span className="mr-1.5 text-[11px] font-semibold uppercase tracking-wider text-faint">Objective</span>
          {pack.objective}
        </p>
        <div className="mt-4 flex flex-wrap items-center gap-x-2 gap-y-1.5 text-[13px] text-muted">
          <EntityChip type="idea" id={pack.idea_id} label={meta.ideaTitle || "Idea"} />
          {pack.branch_id && meta.branchName && (
            <span>
              branch <EntityChip type="branch" id={pack.branch_id} ideaId={pack.idea_id} label={meta.branchName} />
            </span>
          )}
          {pack.checkpoint_id && <EntityChip type="checkpoint" id={pack.checkpoint_id} label={meta.checkpointLabel || "Checkpoint"} />}
          <span className="text-faint" aria-hidden>
            ·
          </span>
          <span title={fit.detail}>≈ {compactNumber(pack.token_estimate)} tokens</span>
          <span className="text-faint" aria-hidden>
            ·
          </span>
          <span title={fullDate(pack.created_at)}>Built {timeAgo(pack.created_at)}</span>
        </div>
      </header>

      <div className="mt-6 flex flex-wrap items-center gap-1.5 border-y border-border py-2">
        <CopyButton size="sm" variant="primary" text={pack.content_markdown} label="Copy Markdown" toastMessage="Context pack copied — paste it into your AI chat" />
        <Button size="sm" variant="ghost" onClick={() => downloadFromApi(`/v1/context-packs/${pack.id}/export?format=md`)}>
          <Download className="h-3.5 w-3.5" /> .md
        </Button>
        <Button size="sm" variant="ghost" onClick={() => downloadFromApi(`/v1/context-packs/${pack.id}/export?format=json`)}>
          <Download className="h-3.5 w-3.5" /> .json
        </Button>
        <CopyButton size="sm" variant="ghost" text={json} label="Copy JSON" toastMessage="JSON copied" />
        <span className="ml-auto hidden text-[12px] text-faint sm:inline">{fit.label}</span>
      </div>

      <div className="mt-6 flex flex-wrap items-center gap-x-6 gap-y-3 rounded-[var(--radius-lg)] border border-border bg-surface px-5 py-4">
        <span className="hidden h-9 w-9 shrink-0 items-center justify-center rounded-full bg-k-branch-soft text-k-branch sm:flex">
          <ArrowLeftRight className="h-4 w-4" aria-hidden />
        </span>
        <div className="min-w-[15rem] flex-1">
          <p className="text-[14px] font-medium text-fg">Paste this into ChatGPT, Claude or Gemini.</p>
          <p className="mt-0.5 text-[13px] leading-relaxed text-muted">
            When you come back, bring the conversation into IdeaVault to review what changed — new ideas, revised decisions, rejected assumptions. Nothing is merged until you review it.
          </p>
        </div>
        <LinkButton href={bringBack} variant="primary" size="sm">
          Bring a conversation back <ArrowRight className="h-3.5 w-3.5" />
        </LinkButton>
      </div>

      <div className="mt-8 grid gap-12 xl:grid-cols-[minmax(0,1fr)_17rem]">
        <Tabs defaultValue="preview" className="min-w-0">
          <TabsList>
            <TabsTrigger value="preview">
              <FileText className="h-3.5 w-3.5" /> Preview
            </TabsTrigger>
            <TabsTrigger value="markdown">Markdown</TabsTrigger>
            <TabsTrigger value="json">
              <Braces className="h-3.5 w-3.5" /> JSON
            </TabsTrigger>
          </TabsList>
          <TabsContent value="preview">
            <article className="max-w-[70ch]">
              <Markdown className="text-[15px]">{pack.content_markdown}</Markdown>
            </article>
          </TabsContent>
          <TabsContent value="markdown">
            <pre className="max-h-[70vh] overflow-auto whitespace-pre-wrap break-words rounded-[var(--radius-lg)] border border-border bg-surface p-4 font-mono text-[12.5px] leading-relaxed text-fg">{pack.content_markdown}</pre>
          </TabsContent>
          <TabsContent value="json">
            <pre className="max-h-[70vh] overflow-auto rounded-[var(--radius-lg)] border border-border bg-surface p-4 font-mono text-[12.5px] leading-relaxed text-fg">{json}</pre>
          </TabsContent>
        </Tabs>
        <aside className="xl:pt-14" aria-label="Conversations brought back">
          <h2 className="mb-2 text-[11px] font-semibold uppercase tracking-[0.08em] text-faint">Brought back with this pack</h2>
          {deltas.isLoading ? (
            <SkeletonLines lines={2} />
          ) : related.length ? (
            <ul className="-mx-2">
              {related.map((d) => {
                const s = deltaStatusMeta(d.status);
                return (
                  <li key={d.id}>
                    <Link href={`/deltas/${d.id}`} className="block rounded-[var(--radius-md)] px-2 py-2 transition-colors hover:bg-surface-2">
                      <span className="flex items-center gap-2">
                        <Pill tone={s.tone}>{s.label}</Pill>
                        <span className="ml-auto text-[11px] text-faint">{shortDate(d.created_at)}</span>
                      </span>
                      <span className="mt-1 line-clamp-2 block text-[12.5px] text-muted">{d.summary}</span>
                    </Link>
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="text-[12.5px] leading-relaxed text-faint">Nothing yet. After you&apos;ve used the pack elsewhere, bring the conversation back to review what changed.</p>
          )}
        </aside>
      </div>
    </div>
  );
}
