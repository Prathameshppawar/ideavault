"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, CircleHelp, Download, History, Layers, Link2, ListTree, MoreHorizontal, Pencil, PenLine, RefreshCw, Sparkles, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api, downloadFromApi, errorMessage } from "@/lib/api";
import { cn, fullDate, plural, timeAgo } from "@/lib/format";
import type { Artifact, ArtifactResult } from "@/lib/types";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { EntityChip } from "@/components/ui/badges";
import { Dialog, Dropdown, DropdownContent, DropdownItem, DropdownLabel, DropdownSeparator, DropdownTrigger, SheetContent } from "@/components/ui/overlay";
import { EmptyState, ErrorState, Skeleton, SkeletonLines } from "@/components/ui/primitives";
import { ARTIFACT_STATUSES, ARTIFACT_STATUS_TONE, artifactTypeName, generatorInfo, groupProvenance, isKnowledgeType, linkifyLabels, stripLeadingTitle, type ProvenanceGroups } from "./artifact-meta";
import { ArtifactEditor } from "./artifact-editor";
import { ArtifactHistory } from "./artifact-history";
import { DeleteArtifactDialog } from "./delete-dialog";
import { ExplainPanel, type ExplainTurn } from "./explain-panel";
import { BackLink, CopyButton, LinkButton, Pill } from "./kit";
import { ProvenancePanel, ProvenanceRow, StaleProvenanceCallout, useProvenanceHealth, type DeadInfo } from "./provenance";
import { artifactKeys, useArtifact, useArtifactVersions, useProvenance } from "./queries";
import { RegenerateDialog } from "./regenerate-dialog";

type Mode = "read" | "edit" | "history";
type Panel = "sources" | "why" | null;

const STATUS_HELP: Record<string, string> = {
  DRAFT: "Still taking shape",
  FINAL: "Ready to share or act on",
  ARCHIVED: "Kept for the record",
};

const statusName = (s: string) => s.charAt(0) + s.slice(1).toLowerCase();

export function ArtifactScreen() {
  const { id } = useParams<{ id: string }>();
  const { data: artifact, isLoading, error, refetch } = useArtifact(id);
  const versions = useArtifactVersions(id);
  const provenance = useProvenance(id);
  const groups = useMemo(() => groupProvenance(provenance.data), [provenance.data]);
  const currentVersion = versions.data?.find((v) => v.version === artifact?.current_version);
  const health = useProvenanceHealth(groups, currentVersion?.created_at);

  const [mode, setMode] = useState<Mode>("read");
  const [panel, setPanel] = useState<Panel>(null);
  const [provVersion, setProvVersion] = useState(0);
  const [regenOpen, setRegenOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [thread, setThread] = useState<ExplainTurn[]>([]);

  if (isLoading) return <ArtifactSkeleton />;
  if (error || !artifact) {
    const notFound = error instanceof ApiError && error.status === 404;
    return (
      <div className="mx-auto max-w-3xl px-5 py-16 sm:px-8">
        {notFound ? (
          <EmptyState title="This artifact doesn't exist" description="It may have been deleted." action={<LinkButton href="/artifacts">All artifacts</LinkButton>} />
        ) : (
          <ErrorState error={error} onRetry={() => void refetch()} />
        )}
      </div>
    );
  }

  const a = artifact;
  const gen = generatorInfo(a.generator);
  const openPanel = (p: Panel) => {
    setPanel(p);
    if (p === "sources") setProvVersion(0);
  };

  return (
    <div className="mx-auto max-w-6xl px-5 pb-16 pt-8 sm:px-8 sm:pt-10">
      <BackLink href="/artifacts">Artifacts</BackLink>

      <header className="mt-5">
        <div className="mb-2 flex flex-wrap items-center gap-2.5">
          <span className="inline-flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-k-artifact">
            <span className="h-1.5 w-1.5 rounded-full bg-k-artifact" />
            {artifactTypeName(a.type)}
          </span>
          <StatusMenu artifact={a} />
        </div>
        <h1 className="max-w-4xl font-display text-[2.1rem] leading-[1.08] text-fg sm:text-[2.6rem]">{a.title}</h1>
        <ProvenanceSummary artifact={a} groups={groups} loading={provenance.isLoading} onOpen={() => openPanel("sources")} />
        <p className="mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[12px] text-faint">
          <button type="button" onClick={() => setMode("history")} className="font-mono text-muted hover:text-fg" title="Version history">
            v{a.current_version}
          </button>
          <span aria-hidden>·</span>
          <span className="inline-flex items-center gap-1" title={gen.detail}>
            {gen.kind === "ai" ? <Sparkles className="h-3 w-3" aria-hidden /> : gen.kind === "user" ? <PenLine className="h-3 w-3" aria-hidden /> : <Layers className="h-3 w-3" aria-hidden />}
            {gen.label}
          </span>
          <span aria-hidden>·</span>
          <span title={fullDate(a.created_at)}>Created {fullDate(a.created_at)}</span>
          {a.updated_at !== a.created_at && (
            <>
              <span aria-hidden>·</span>
              <span title={fullDate(a.updated_at)}>Updated {timeAgo(a.updated_at)}</span>
            </>
          )}
        </p>
      </header>

      {mode !== "edit" && (
        <div className="sticky top-0 z-20 -mx-2 mt-6 flex flex-wrap items-center gap-1 border-b border-border bg-bg px-2 py-2">
          {mode === "history" ? (
            <>
              <Button size="sm" variant="secondary" onClick={() => setMode("read")}>
                <X className="h-3.5 w-3.5" /> Close history
              </Button>
              <span className="ml-2 text-[13px] text-muted">{versions.data ? plural(versions.data.length, "version") : "Loading versions…"}</span>
            </>
          ) : (
            <>
              <Button size="sm" variant="secondary" onClick={() => setMode("edit")}>
                <Pencil className="h-3.5 w-3.5" /> Edit
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setRegenOpen(true)}>
                <RefreshCw className="h-3.5 w-3.5" /> <span className="hidden sm:inline">Regenerate</span>
                <span className="sr-only sm:hidden">Regenerate</span>
              </Button>
              <CopyButton size="sm" variant="ghost" text={a.content_markdown} label={<span className="hidden sm:inline">Copy</span>} copiedLabel={<span className="hidden sm:inline">Copied</span>} toastMessage="Markdown copied to clipboard" aria-label="Copy Markdown" />
              <Button size="sm" variant="ghost" onClick={() => downloadFromApi(`/v1/artifacts/${a.id}/download`)} aria-label="Download .md">
                <Download className="h-3.5 w-3.5" /> <span className="hidden sm:inline">.md</span>
              </Button>
              <span className="mx-1 hidden h-5 w-px bg-border sm:block" aria-hidden />
              <Button size="sm" variant="ghost" onClick={() => openPanel("sources")} aria-label="View sources">
                <ListTree className="h-3.5 w-3.5" /> <span className="hidden sm:inline">Sources</span>
                {groups.itemCount > 0 && <span className="rounded-full bg-surface-3 px-1.5 text-[11px] text-muted">{groups.itemCount}</span>}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setMode("history")} aria-label="Version history">
                <History className="h-3.5 w-3.5" /> <span className="hidden sm:inline">History</span>
              </Button>
              <Button size="sm" variant="ghost" onClick={() => openPanel("why")} aria-label="Why does it say this?">
                <CircleHelp className="h-3.5 w-3.5" /> <span className="hidden md:inline">Why does it say this?</span>
              </Button>
            </>
          )}
          <div className="ml-auto">
            <Dropdown>
              <DropdownTrigger asChild>
                <Button size="icon-sm" variant="ghost" aria-label="More actions">
                  <MoreHorizontal className="h-4 w-4" />
                </Button>
              </DropdownTrigger>
              <DropdownContent>
                <DropdownItem
                  onSelect={() => {
                    void navigator.clipboard.writeText(window.location.href).then(() => toast.success("Link copied"));
                  }}
                >
                  <Link2 className="h-3.5 w-3.5 text-muted" /> Copy link
                </DropdownItem>
                <DropdownItem asChild>
                  <Link href={`/artifacts?idea_id=${a.idea_id}`}>
                    <ListTree className="h-3.5 w-3.5 text-muted" /> Other artifacts for this idea
                  </Link>
                </DropdownItem>
                <DropdownSeparator />
                <DropdownItem danger onSelect={() => setDeleteOpen(true)}>
                  <Trash2 className="h-3.5 w-3.5" /> Delete artifact…
                </DropdownItem>
              </DropdownContent>
            </Dropdown>
          </div>
        </div>
      )}

      <div className={cn(mode === "edit" ? "mt-6" : "mt-8")}>
        {mode === "edit" ? (
          <ArtifactEditor key={`${a.id}-${a.current_version}`} artifact={a} onDone={() => setMode("read")} />
        ) : mode === "history" ? (
          versions.isLoading ? (
            <SkeletonLines lines={6} />
          ) : versions.error ? (
            <ErrorState error={versions.error} onRetry={() => void versions.refetch()} />
          ) : (
            <ArtifactHistory artifact={a} versions={versions.data ?? []} />
          )
        ) : (
          <ReadView artifact={a} groups={groups} health={health} onRegenerate={() => setRegenOpen(true)} onOpenSources={() => openPanel("sources")} />
        )}
      </div>

      <Dialog open={panel !== null} onOpenChange={(o) => !o && setPanel(null)}>
        {panel === "sources" && (
          <SheetContent title="Sources & provenance">
            <ProvenancePanel artifactId={a.id} versions={versions.data ?? []} version={provVersion} onVersionChange={setProvVersion} />
          </SheetContent>
        )}
        {panel === "why" && (
          <SheetContent title="Why does it say this?">
            <ExplainPanel artifactId={a.id} cited={groups.cited} thread={thread} setThread={setThread} />
          </SheetContent>
        )}
      </Dialog>
      <RegenerateDialog artifact={a} open={regenOpen} onOpenChange={setRegenOpen} />
      <DeleteArtifactDialog artifact={a} open={deleteOpen} onOpenChange={setDeleteOpen} />
    </div>
  );
}

function StatusMenu({ artifact: a }: { artifact: Artifact }) {
  const qc = useQueryClient();
  const set = useMutation({
    mutationFn: (status: string) => api.patch<ArtifactResult>(`/v1/artifacts/${a.id}`, { status }),
    onMutate: async (status) => {
      await qc.cancelQueries({ queryKey: artifactKeys.one(a.id) });
      const prev = qc.getQueryData<Artifact>(artifactKeys.one(a.id));
      if (prev) qc.setQueryData(artifactKeys.one(a.id), { ...prev, status });
      return { prev };
    },
    onError: (e, _s, ctx) => {
      if (ctx?.prev) qc.setQueryData(artifactKeys.one(a.id), ctx.prev);
      toast.error(errorMessage(e));
    },
    onSuccess: (_r, status) => toast.success(`Marked as ${statusName(status).toLowerCase()}`),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: artifactKeys.one(a.id) });
      void qc.invalidateQueries({ queryKey: ["artifacts"] });
    },
  });
  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <button type="button" className="inline-flex items-center gap-0.5 rounded-full transition-opacity hover:opacity-80" aria-label={`Status: ${statusName(a.status)}. Change status`}>
          <Pill tone={ARTIFACT_STATUS_TONE[a.status] ?? "muted"}>
            {statusName(a.status)}
            <ChevronDown className="-mr-0.5 h-3 w-3" aria-hidden />
          </Pill>
        </button>
      </DropdownTrigger>
      <DropdownContent align="start" className="w-60">
        <DropdownLabel>Status</DropdownLabel>
        {ARTIFACT_STATUSES.map((s) => (
          <DropdownItem key={s} onSelect={() => s !== a.status && set.mutate(s)} className="items-start">
            <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: s === "FINAL" ? "var(--success)" : s === "DRAFT" ? "var(--text-muted)" : "var(--text-faint)" }} />
            <span className="min-w-0 flex-1">
              <span className="block font-medium">{statusName(s)}</span>
              <span className="block text-[12px] text-muted">{STATUS_HELP[s]}</span>
            </span>
            {s === a.status && <span className="text-[11px] text-faint">current</span>}
          </DropdownItem>
        ))}
      </DropdownContent>
    </Dropdown>
  );
}

/** "Generated from <idea> · branch Main · CP3 · 12 recorded items" */
function ProvenanceSummary({ artifact: a, groups, loading, onOpen }: { artifact: Artifact; groups: ProvenanceGroups; loading: boolean; onOpen: () => void }) {
  const handWritten = a.generator === "user" && a.current_version === 1;
  return (
    <div className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1.5 text-[13px] text-muted">
      <span>{handWritten ? "Written for" : "Generated from"}</span>
      <EntityChip type="idea" id={a.idea_id} label={a.idea_title || "Idea"} />
      {a.branch_name && a.branch_id && (
        <>
          <span className="text-faint" aria-hidden>
            ·
          </span>
          <span>
            branch <EntityChip type="branch" id={a.branch_id} ideaId={a.idea_id} label={a.branch_name} />
          </span>
        </>
      )}
      {a.checkpoint_label && a.checkpoint_id && (
        <>
          <span className="text-faint" aria-hidden>
            ·
          </span>
          <EntityChip type="checkpoint" id={a.checkpoint_id} label={a.checkpoint_label} />
        </>
      )}
      <span className="text-faint" aria-hidden>
        ·
      </span>
      {loading ? (
        <Skeleton className="h-3.5 w-24" />
      ) : (
        <button type="button" onClick={onOpen} className="underline decoration-border-strong underline-offset-[3px] transition-colors hover:text-fg hover:decoration-fg">
          {plural(groups.itemCount, "recorded item")}
        </button>
      )}
    </div>
  );
}

function ReadView({
  artifact: a,
  groups,
  health,
  onRegenerate,
  onOpenSources,
}: {
  artifact: Artifact;
  groups: ProvenanceGroups;
  health: Map<string, DeadInfo>;
  onRegenerate: () => void;
  onOpenSources: () => void;
}) {
  // Bare ref labels in the text ([D1], [CP3]) become chips that open the recorded item.
  const body = useMemo(() => {
    const refs = [...groups.baseCheckpoint, ...groups.cited, ...groups.inputs]
      .filter((e) => isKnowledgeType(e.entity_type) || e.entity_type === "checkpoint")
      .map((e) => ({ label: e.label, type: e.entity_type, id: e.entity_id }));
    return linkifyLabels(stripLeadingTitle(a.content_markdown, a.title), refs);
  }, [a.content_markdown, a.title, groups]);
  const cited = groups.cited.filter((e) => e.entity_type !== "checkpoint");
  const shown = cited.slice(0, 10);
  const rest = cited.length - shown.length + groups.inputs.length;
  return (
    <div className="grid animate-fade-in gap-12 xl:grid-cols-[minmax(0,1fr)_17rem]">
      <article className="min-w-0 max-w-[70ch]">
        <StaleProvenanceCallout health={health} onRegenerate={onRegenerate} onOpenSources={onOpenSources} />
        {body.trim() ? <Markdown className="text-[15.5px] leading-[1.75]">{body}</Markdown> : <p className="text-[14px] text-faint">This artifact is empty. Use Edit to write it, or Regenerate.</p>}
      </article>
      {(cited.length > 0 || groups.baseCheckpoint.length > 0) && (
        <aside className="hidden xl:block" aria-label="Built from">
          <div className="sticky top-20">
            <h2 className="mb-2 text-[11px] font-semibold uppercase tracking-[0.08em] text-faint">Built from</h2>
            <ul className="-mx-2">
              {groups.baseCheckpoint.map((e) => (
                <ProvenanceRow key={e.entity_id} entry={e} compact />
              ))}
              {shown.map((e) => (
                <ProvenanceRow key={e.entity_id} entry={e} dead={health.get(e.entity_id)} compact />
              ))}
            </ul>
            <button type="button" onClick={onOpenSources} className="mt-2 text-[12px] text-muted underline decoration-border-strong underline-offset-2 hover:text-fg">
              {rest > 0 ? `+ ${plural(rest, "more input")} · all sources` : "All sources"}
            </button>
          </div>
        </aside>
      )}
    </div>
  );
}

function ArtifactSkeleton() {
  return (
    <div className="mx-auto max-w-6xl px-5 pt-8 sm:px-8 sm:pt-10" aria-busy="true" aria-label="Loading artifact">
      <Skeleton className="h-3.5 w-20" />
      <Skeleton className="mt-6 h-3 w-32" />
      <Skeleton className="mt-3 h-10 w-3/4" />
      <Skeleton className="mt-4 h-4 w-1/2" />
      <Skeleton className="mt-8 h-9 w-full" />
      <div className="mt-8 max-w-[70ch]">
        <SkeletonLines lines={10} />
      </div>
    </div>
  );
}
