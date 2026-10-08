"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useParams, usePathname, useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeftRight, Box, Check, ChevronRight, Copy, ExternalLink, FileText, GitBranch, GitFork, MessagesSquare, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import type { CheckpointDiff, Snapshot } from "@/lib/types";
import { fullDate, humanize, plural, shortDate, truncate } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/ui/badges";
import { Switch } from "@/components/ui/overlay";
import { EmptyState, ErrorState, SectionTitle, Skeleton, SkeletonLines } from "@/components/ui/primitives";
import { CheckpointPill, Interpretation, KindCounts, Select, SourceQuote } from "@/features/ideas/ui";
import { useCheckpoint, useIdeaCheckpoints } from "@/features/ideas/hooks";
import { ForkDialog } from "@/features/ideas/fork-dialog";
import { sortCheckpointsDesc } from "@/features/ideas/lib/fork";
import { KnowledgeSections } from "@/features/knowledge/knowledge-sections";
import { KnowledgeDrawer } from "@/features/knowledge/knowledge-drawer";
import { CheckpointDiffView } from "./checkpoint-diff";

type SnapshotRef = Snapshot["conversations"][number];

const KIND_LABEL: Record<string, string> = {
  manual: "Manual checkpoint",
  auto: "Automatic checkpoint",
  conclusion: "Conclusion snapshot",
  fork_base: "Fork base",
  merge: "Merge snapshot",
};

/** /checkpoints/[id]: an immutable snapshot with its knowledge, captured sources and comparisons. */
export function CheckpointScreen() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const pathname = usePathname();
  const sp = useSearchParams();
  const compareId = sp.get("compare");
  const cp = useCheckpoint(id);
  const all = useIdeaCheckpoints(cp.data?.idea_id);
  const [showHistory, setShowHistory] = useState(true);
  const [item, setItem] = useState<string | null>(null);
  const [forkOpen, setForkOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  const others = useMemo(() => sortCheckpointsDesc(all.data ?? []).filter((c) => c.id !== id), [all.data, id]);
  const other = others.find((c) => c.id === compareId);
  const [from, to] = cp.data && other ? (other.number < cp.data.number ? [other.id, cp.data.id] : [cp.data.id, other.id]) : [null, null];
  const diff = useQuery({
    queryKey: ["checkpoint-compare", from, to],
    queryFn: () => api.get<CheckpointDiff>("/v1/checkpoints/compare", { query: { from, to } }),
    enabled: Boolean(from && to),
    staleTime: Infinity,
  });

  const setCompare = (v: string | null) => {
    const p = new URLSearchParams(window.location.search);
    if (v) p.set("compare", v);
    else p.delete("compare");
    const s = p.toString();
    window.history.replaceState(null, "", `${pathname}${s ? `?${s}` : ""}`);
  };

  if (cp.isLoading)
    return (
      <div className="mx-auto max-w-5xl px-4 py-10 sm:px-8">
        <Skeleton className="mb-4 h-3.5 w-48" />
        <Skeleton className="h-10 w-2/3" />
        <div className="mt-8">
          <SkeletonLines lines={8} />
        </div>
      </div>
    );
  if (cp.error || !cp.data)
    return (
      <div className="mx-auto max-w-3xl px-6 py-16">
        <ErrorState error={cp.error ?? new Error("Checkpoint not found")} onRetry={() => void cp.refetch()} />
      </div>
    );

  const c = cp.data;
  const snap = c.snapshot;
  const items = snap?.items ?? [];
  const ideaHref = `/ideas/${c.idea_id}`;
  const openInIdea = `${ideaHref}?branch=${c.branch_id}&cp=${c.id}`;
  const counts = c.counts ?? countKinds(items);

  return (
    <div className="mx-auto w-full max-w-5xl px-4 pb-24 pt-6 sm:px-8 sm:pt-8">
      <nav aria-label="Breadcrumb" className="mb-6 flex min-w-0 flex-wrap items-center gap-1 text-[13px] text-muted">
        <Link href="/ideas" className="hover:text-fg">
          Ideas
        </Link>
        <ChevronRight className="h-3.5 w-3.5 text-faint" aria-hidden />
        <Link href={ideaHref} className="max-w-[18rem] truncate hover:text-fg">
          {snap?.idea?.title ?? "Idea"}
        </Link>
        <ChevronRight className="h-3.5 w-3.5 text-faint" aria-hidden />
        <span className="text-fg">{c.label}</span>
      </nav>

      <header>
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <CheckpointPill label={c.label} className="h-6 px-2 text-[12px]" />
          <span className="text-[12px] font-medium uppercase tracking-wide text-muted">{KIND_LABEL[c.kind] ?? humanize(c.kind)}</span>
          <span className="text-[12px] text-faint">· immutable</span>
        </div>
        <h1 className="font-display text-[2.1rem] leading-[1.08] text-fg [overflow-wrap:anywhere] sm:text-[2.6rem]">{c.title}</h1>
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[13px] text-muted">
          <Link href={`${ideaHref}?branch=${c.branch_id}`} className="inline-flex items-center gap-1.5 hover:text-fg">
            <GitBranch className="h-3.5 w-3.5 text-k-branch" /> {c.branch_name ?? snap?.branch?.name}
          </Link>
          <span title={fullDate(c.created_at)}>{fullDate(c.created_at)}</span>
          <span>by {humanize(c.created_by).toLowerCase()}</span>
          {snap?.idea && (
            <span className="inline-flex items-center gap-1.5">
              idea was <StatusBadge status={snap.idea.status} /> v{snap.idea.version}
            </span>
          )}
        </div>
        <div className="mt-5 flex flex-wrap items-center gap-2 border-y border-border py-2.5">
          <Button size="sm" variant="primary" onClick={() => router.push(openInIdea)}>
            <ExternalLink className="h-3.5 w-3.5" /> View in idea
          </Button>
          <Button size="sm" variant="secondary" onClick={() => setForkOpen(true)} disabled={!all.data}>
            <GitFork className="h-3.5 w-3.5" /> Fork from here
          </Button>
          <div className="flex items-center gap-1.5">
            <ArrowLeftRight className="ml-1 h-3.5 w-3.5 text-faint" aria-hidden />
            <Select value={compareId ?? ""} onChange={(e) => setCompare(e.target.value || null)} className="w-56" aria-label="Compare with another checkpoint" disabled={!others.length}>
              <option value="">{others.length ? "Compare with…" : "No other checkpoints"}</option>
              {others.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.label} · {truncate(o.title, 40)}
                </option>
              ))}
            </Select>
            {compareId && (
              <Button size="icon-sm" variant="ghost" onClick={() => setCompare(null)} aria-label="Clear comparison">
                <X className="h-3.5 w-3.5" />
              </Button>
            )}
          </div>
        </div>
      </header>

      {compareId && (
        <section className="mt-8 rounded-[var(--radius-lg)] border border-border bg-surface p-5">
          <SectionTitle>Comparison</SectionTitle>
          {!other ? (
            all.isLoading ? (
              <SkeletonLines lines={3} />
            ) : (
              <p className="text-[13px] text-faint">That checkpoint isn&apos;t part of this idea.</p>
            )
          ) : diff.isLoading ? (
            <SkeletonLines lines={5} />
          ) : diff.error ? (
            <ErrorState error={diff.error} onRetry={() => void diff.refetch()} />
          ) : diff.data ? (
            <CheckpointDiffView diff={diff.data} onSelectItem={setItem} />
          ) : null}
        </section>
      )}

      <div className="mt-10 grid gap-8 md:grid-cols-2">
        <div>
          <SectionTitle>Summary</SectionTitle>
          {c.summary ? <Interpretation label="summary of the snapshot">{c.summary}</Interpretation> : <p className="text-[13px] text-faint">No summary.</p>}
        </div>
        <div>
          <SectionTitle>Note</SectionTitle>
          {snap?.context ? <SourceQuote label="kept with this checkpoint">{snap.context}</SourceQuote> : <p className="text-[13px] text-faint">No note was written for this checkpoint.</p>}
        </div>
      </div>

      <section className="mt-10">
        <SectionTitle
          action={
            <label className="flex cursor-pointer items-center gap-2 text-[12.5px] font-normal normal-case tracking-normal text-muted">
              <Switch checked={showHistory} onCheckedChange={setShowHistory} aria-label="Include superseded items" /> Include superseded
            </label>
          }
        >
          Knowledge in this snapshot
        </SectionTitle>
        <div className="mb-6">
          <KindCounts counts={counts} />
        </div>
        {items.length === 0 ? (
          <EmptyState title="Nothing captured" description="This snapshot was taken before any knowledge was recorded on its branch." />
        ) : (
          <KnowledgeSections items={items} showHistory={showHistory} onSelect={(it) => setItem(it.id)} />
        )}
      </section>

      <section className="mt-12 grid gap-8 md:grid-cols-3">
        <RefList title="Conversations" icon={MessagesSquare} refs={snap?.conversations ?? []} href={(r) => `/conversations/${r.id}`} />
        <RefList title="Sources" icon={Box} refs={snap?.sources ?? []} />
        <RefList title="Artifacts" icon={FileText} refs={snap?.artifacts ?? []} href={(r) => `/artifacts/${r.id}`} />
      </section>

      {(snap?.contradictions?.length ?? 0) > 0 && (
        <section className="mt-12">
          <SectionTitle>Tensions noted at the time</SectionTitle>
          <ul className="space-y-2">
            {snap!.contradictions!.map((x, i) => (
              <li key={i} className="text-[13.5px] text-fg">
                <span className="font-mono text-[12px] font-semibold">{x.from_label}</span> ↔ <span className="font-mono text-[12px] font-semibold">{x.to_label}</span>
                {x.rationale && <span className="text-muted"> — {x.rationale}</span>}
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="mt-12 border-t border-border pt-5">
        <dl className="grid gap-x-8 gap-y-2 text-[12.5px] sm:grid-cols-2">
          <div className="flex min-w-0 items-center gap-2">
            <dt className="shrink-0 text-muted">Content hash</dt>
            <dd className="min-w-0 truncate font-mono text-[11.5px] text-faint" title={c.content_hash}>
              {c.content_hash}
            </dd>
            <button
              type="button"
              onClick={() => {
                void navigator.clipboard?.writeText(c.content_hash);
                setCopied(true);
                toast.success("Hash copied");
                setTimeout(() => setCopied(false), 1500);
              }}
              className="shrink-0 rounded p-1 text-faint hover:bg-surface-2 hover:text-fg"
              aria-label="Copy content hash"
            >
              {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
            </button>
          </div>
          <div className="flex gap-2">
            <dt className="text-muted">Captured</dt>
            <dd className="text-faint">{snap?.captured_at ? fullDate(snap.captured_at) : "—"}</dd>
          </div>
          <div className="flex gap-2">
            <dt className="text-muted">Items</dt>
            <dd className="text-faint">
              {plural(items.length, "item")} · {plural(snap?.relationships?.length ?? 0, "relationship")}
            </dd>
          </div>
          <div className="flex gap-2">
            <dt className="text-muted">Schema</dt>
            <dd className="text-faint">v{snap?.schema_version ?? 1}</dd>
          </div>
        </dl>
      </section>

      <KnowledgeDrawer
        itemId={item}
        onOpenChange={(v) => !v && setItem(null)}
        onNavigate={setItem}
        readOnly
        readOnlyReason={`Opened from ${c.label}. Shown below is the item's full current record — open the idea to change it.`}
      />
      {all.data && (
        <ForkDialog
          open={forkOpen}
          onOpenChange={setForkOpen}
          ideaId={c.idea_id}
          checkpoints={all.data}
          defaultSourceId={c.id}
          onDone={(r) => router.push(`${ideaHref}${r.branch ? `?branch=${r.branch.id}` : ""}`)}
        />
      )}
    </div>
  );
}

function countKinds(items: { kind: string }[]): Record<string, number> {
  const m: Record<string, number> = {};
  for (const i of items) m[i.kind] = (m[i.kind] ?? 0) + 1;
  return m;
}

function RefList({ title, icon: Icon, refs, href }: { title: string; icon: React.ComponentType<{ className?: string }>; refs: SnapshotRef[]; href?: (r: SnapshotRef) => string }) {
  return (
    <div className="min-w-0">
      <SectionTitle>
        {title} <span className="ml-1 font-normal tabular-nums text-faint">{refs.length}</span>
      </SectionTitle>
      {refs.length === 0 ? (
        <p className="text-[13px] text-faint">None captured.</p>
      ) : (
        <ul className="-mx-1.5 space-y-0.5">
          {refs.map((r) => {
            const body = (
              <>
                <Icon className="mt-0.5 h-3.5 w-3.5 shrink-0 text-faint" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13.5px] text-fg">{r.title || "Untitled"}</span>
                  <span className="block text-[11.5px] text-faint">
                    {r.kind ? `${humanize(r.kind)} · ` : ""}
                    {shortDate(r.at)}
                  </span>
                </span>
              </>
            );
            return (
              <li key={r.id}>
                {href ? (
                  <Link href={href(r)} className="flex items-start gap-2 rounded-[var(--radius-md)] px-1.5 py-1.5 hover:bg-surface-2">
                    {body}
                  </Link>
                ) : (
                  <div className="flex items-start gap-2 px-1.5 py-1.5">{body}</div>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

