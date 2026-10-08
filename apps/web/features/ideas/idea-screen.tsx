"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { ArrowLeftRight, Bookmark, GitFork, Lightbulb, MessageSquare, PanelRightClose, RotateCcw, Undo2 } from "lucide-react";
import { ApiError } from "@/lib/api";
import type { Checkpoint, ForkResult, IdeaOverview, KnowledgeItem } from "@/lib/types";
import { cn, fullDate, humanize, plural } from "@/lib/format";
import { useUI } from "@/stores/ui";
import { Button } from "@/components/ui/button";
import { Dialog, Popover, PopoverContent, PopoverTrigger, SheetContent, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/overlay";
import { EmptyState, ErrorState, Skeleton, SkeletonLines } from "@/components/ui/primitives";
import { StatusBadge } from "@/components/ui/badges";
import { ChatView } from "@/features/chat/chat-view";
import { KnowledgeDrawer } from "@/features/knowledge/knowledge-drawer";
import { flattenKnowledge, isLive } from "@/features/knowledge/lib";
import { CheckpointPill } from "./ui";
import { IdeaHeader, useReopen } from "./idea-header";
import { CheckpointDialog, DeleteIdeaDialog } from "./idea-dialogs";
import { ForkDialog } from "./fork-dialog";
import { ConcludeDialog } from "./conclude-dialog";
import { useCheckpoint, useIdeaOverview, useIdeaUrlState, useMediaQuery, type IdeaTab } from "./hooks";
import { defaultForkSource, sortCheckpointsDesc } from "./lib/fork";
import { outcomeInfo } from "./lib/lifecycle";
import { OverviewTab } from "./tabs/overview-tab";
import { JourneyTab } from "./tabs/journey-tab";
import { KnowledgeTab } from "./tabs/knowledge-tab";
import { BranchesTab } from "./tabs/branches-tab";
import { ConversationsTab } from "./tabs/conversations-tab";
import { ArtifactsTab } from "./tabs/artifacts-tab";

type DialogKind = null | "checkpoint" | "fork" | "conclude" | "delete";

/** The Idea page: header, checkpoint/branch context, tabs, and a persistent chat panel. */
export function IdeaScreen() {
  const { id } = useParams<{ id: string }>();
  const url = useIdeaUrlState();
  const overview = useIdeaOverview(id, url.branch);
  const cpQuery = useCheckpoint(url.cp);
  const chatPref = useUI((s) => s.ideaChatOpen);
  const setChatPref = useUI((s) => s.setIdeaChatOpen);
  const wide = useMediaQuery("(min-width: 1280px)");
  const [chatDrawer, setChatDrawer] = useState(false);
  const [dialog, setDialog] = useState<DialogKind>(null);
  const [forkSourceId, setForkSourceId] = useState<string | null>(null);

  const ov = overview.data;
  const liveItems = useMemo(() => flattenKnowledge(ov?.knowledge), [ov]);
  const viewingCp = cpQuery.data && cpQuery.data.idea_id === id ? cpQuery.data : null;
  const items = url.cp ? (viewingCp?.snapshot?.items ?? []) : liveItems;

  if (overview.isLoading) return <IdeaSkeleton />;
  if (overview.error || !ov?.idea) {
    const notFound = overview.error instanceof ApiError && overview.error.status === 404;
    const badBranch = Boolean(url.branch) && overview.error instanceof ApiError && overview.error.status < 500;
    return (
      <div className="mx-auto max-w-3xl px-6 py-16">
        {notFound && !url.branch ? (
          <EmptyState
            icon={Lightbulb}
            title="This idea doesn't exist"
            description="It may have been deleted, or the link is wrong."
            action={
              <Link href="/ideas" className="text-[13px] font-medium text-accent hover:underline">
                Back to ideas
              </Link>
            }
          />
        ) : (
          <div className="space-y-3">
            <ErrorState error={overview.error ?? new Error("Idea not found")} onRetry={() => void overview.refetch()} />
            {badBranch && (
              <Button size="sm" variant="secondary" onClick={() => url.set({ branch: null, cp: null })}>
                Open the default branch instead
              </Button>
            )}
          </div>
        )}
      </div>
    );
  }

  const idea = ov.idea;
  const branch = ov.branch;
  const branches = ov.branches ?? [];
  const checkpoints = ov.checkpoints ?? [];
  const defaultBranchId = idea.default_branch_id ?? branches.find((b) => b.is_default)?.id ?? null;

  const switchBranch = (bid: string) => url.set({ branch: bid === defaultBranchId ? null : bid, cp: null, item: null });
  const viewCheckpoint = (c: Checkpoint | null) => {
    if (!c) url.set({ cp: null });
    else url.set({ cp: c.id, branch: c.branch_id === defaultBranchId ? null : c.branch_id });
  };
  const setTab = (t: IdeaTab) => url.set({ tab: t }, "replace");
  const openItem = (itemId: string) => url.set({ item: itemId }, "replace");
  const toggleChat = () => (wide ? setChatPref(!chatPref) : setChatDrawer((v) => !v));
  const openFork = (sourceId?: string | null) => {
    setForkSourceId(sourceId ?? defaultForkSource(checkpoints, { viewingCheckpointId: url.cp, branchId: branch?.id })?.id ?? null);
    setDialog("fork");
  };
  const chatOpen = wide ? chatPref : chatDrawer;
  const snapshotStatus = url.cp && url.item ? viewingCp?.snapshot?.items?.find((i) => i.id === url.item)?.status : undefined;

  const liveCount = liveItems.filter((i) => isLive(i) && i.review_state === "ACCEPTED").length;
  const knowledgeCount = url.cp ? items.filter(isLive).length : liveCount;

  const chat = (
    <IdeaChatPanel
      key={branch?.id ?? "default"}
      ov={ov}
      items={items}
      viewingCp={viewingCp}
      bare={!wide}
      onClose={() => (wide ? setChatPref(false) : setChatDrawer(false))}
    />
  );

  return (
    <div className="flex h-app min-h-0">
      <div className="min-w-0 flex-1 overflow-y-auto" id="idea-main">
        <div className="mx-auto w-full max-w-5xl px-4 pb-24 pt-6 sm:px-8">
          <IdeaHeader
            idea={idea}
            branch={branch}
            branches={branches}
            checkpoints={checkpoints}
            viewingCpId={url.cp}
            chatOpen={chatOpen}
            actions={{
              onCheckpoint: () => setDialog("checkpoint"),
              onFork: () => openFork(),
              onConclude: () => setDialog("conclude"),
              onDelete: () => setDialog("delete"),
              onToggleChat: toggleChat,
              onSwitchBranch: switchBranch,
              onViewCheckpoint: viewCheckpoint,
              onTab: setTab,
            }}
          />

          <div className="mt-5 space-y-3 empty:hidden">
            {url.cp && (
              <CheckpointBanner
                cp={viewingCp}
                loading={cpQuery.isLoading}
                error={cpQuery.error}
                checkpoints={checkpoints}
                onBack={() => viewCheckpoint(null)}
                onFork={() => openFork(url.cp)}
              />
            )}
            {!url.cp && <ClosedBanner ov={ov} />}
          </div>

          <Tabs value={url.tab} onValueChange={(v) => setTab(v as IdeaTab)} className="mt-6">
            <TabsList className="-mx-4 overflow-x-auto px-4 sm:mx-0 sm:px-0">
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="journey">Journey</TabsTrigger>
              <TabsTrigger value="knowledge">
                Knowledge <Count n={knowledgeCount} />
              </TabsTrigger>
              <TabsTrigger value="branches">
                Branches <Count n={branches.length} />
              </TabsTrigger>
              <TabsTrigger value="conversations">
                Conversations <Count n={ov.conversations?.length ?? 0} />
              </TabsTrigger>
              <TabsTrigger value="artifacts">
                Artifacts <Count n={ov.artifacts?.length ?? 0} />
              </TabsTrigger>
            </TabsList>
            <TabsContent value="overview" className="pt-8">
              {url.cp && cpQuery.isLoading ? <SkeletonLines lines={8} /> : <OverviewTab ov={ov} items={items} viewingCp={viewingCp} onSelectItem={openItem} onTab={setTab} />}
            </TabsContent>
            <TabsContent value="journey" className="pt-8">
              <JourneyTab ideaId={idea.id} branches={branches} defaultBranchId={defaultBranchId} />
            </TabsContent>
            <TabsContent value="knowledge" className="pt-8">
              {url.cp && cpQuery.isLoading ? (
                <SkeletonLines lines={8} />
              ) : (
                <KnowledgeTab
                  ideaId={idea.id}
                  branchId={branch?.id ?? null}
                  branchName={branch?.name}
                  items={items}
                  viewingCp={viewingCp}
                  onSelectItem={openItem}
                  canEdit={idea.status !== "MERGED"}
                />
              )}
            </TabsContent>
            <TabsContent value="branches" className="pt-8">
              <BranchesTab
                ideaId={idea.id}
                currentBranch={branch}
                viewingCpId={url.cp}
                onSwitchBranch={switchBranch}
                onViewCheckpoint={viewCheckpoint}
                onFork={() => openFork()}
                onSelectItem={openItem}
              />
            </TabsContent>
            <TabsContent value="conversations" className="pt-8">
              <ConversationsTab conversations={ov.conversations ?? []} />
            </TabsContent>
            <TabsContent value="artifacts" className="pt-8">
              <ArtifactsTab ideaId={idea.id} artifacts={ov.artifacts ?? []} branch={branch} checkpoints={checkpoints} viewingCpId={url.cp} />
            </TabsContent>
          </Tabs>
        </div>
      </div>

      {wide && chatPref && <aside className="flex h-app w-[380px] shrink-0 flex-col border-l border-border bg-bg 2xl:w-[440px]">{chat}</aside>}
      {!wide && (
        <Dialog open={chatDrawer} onOpenChange={setChatDrawer}>
          {chatDrawer && (
            <SheetContent title={`Chat · ${branch?.name ?? "Main"}${viewingCp ? ` @ ${viewingCp.label}` : ""}`} className="max-w-md">
              <div className="flex h-full flex-col">{chat}</div>
            </SheetContent>
          )}
        </Dialog>
      )}

      <KnowledgeDrawer
        itemId={url.item}
        onOpenChange={(v) => !v && url.set({ item: null }, "replace")}
        onNavigate={openItem}
        readOnly={Boolean(viewingCp)}
        readOnlyReason={
          viewingCp ? `You're viewing ${viewingCp.label}${snapshotStatus ? `, where this item was ${humanize(snapshotStatus).toLowerCase()}` : ""}. Shown below is its full current record; go back to live to change it.` : undefined
        }
      />
      <CheckpointDialog open={dialog === "checkpoint"} onOpenChange={(v) => setDialog(v ? "checkpoint" : null)} ideaId={idea.id} branchId={branch?.id ?? null} branchName={branch?.name} />
      <ForkDialog
        open={dialog === "fork"}
        onOpenChange={(v) => setDialog(v ? "fork" : null)}
        ideaId={idea.id}
        checkpoints={checkpoints}
        defaultSourceId={forkSourceId}
        onForked={(r: ForkResult) => r.branch && url.set({ branch: r.branch.id, cp: null, item: null })}
        onCreateCheckpoint={() => setDialog("checkpoint")}
      />
      <ConcludeDialog
        open={dialog === "conclude"}
        onOpenChange={(v) => setDialog(v ? "conclude" : null)}
        idea={idea}
        branchId={branch?.id ?? null}
        branchName={branch?.name}
        items={liveItems}
      />
      <DeleteIdeaDialog open={dialog === "delete"} onOpenChange={(v) => setDialog(v ? "delete" : null)} idea={idea} />
    </div>
  );
}

function Count({ n }: { n: number }) {
  return <span className="text-[11.5px] font-normal tabular-nums text-faint">{n}</span>;
}

function CheckpointBanner({
  cp,
  loading,
  error,
  checkpoints,
  onBack,
  onFork,
}: {
  cp: Checkpoint | null;
  loading: boolean;
  error: unknown;
  checkpoints: Checkpoint[];
  onBack: () => void;
  onFork: () => void;
}) {
  if (loading) return <Skeleton className="h-14 w-full" />;
  if (error || !cp)
    return (
      <div className="flex flex-wrap items-center gap-3 rounded-[var(--radius-lg)] border border-border bg-surface-2 px-4 py-3 text-[13px]">
        <span className="flex-1 text-muted">That checkpoint couldn&apos;t be loaded — it may belong to another idea.</span>
        <Button size="sm" variant="secondary" onClick={onBack}>
          Back to live
        </Button>
      </div>
    );
  const others = sortCheckpointsDesc(checkpoints).filter((c) => c.id !== cp.id);
  return (
    <div
      className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-3 rounded-[var(--radius-lg)] border px-4 py-3 animate-slide-up"
      style={{ borderColor: "color-mix(in srgb, var(--k-checkpoint) 30%, transparent)", background: "var(--k-checkpoint-soft)" }}
      role="status"
    >
      <Bookmark className="mt-0.5 h-4 w-4 text-k-checkpoint" aria-hidden />
      <div className="min-w-0">
        <p className="flex flex-wrap items-center gap-x-1.5 text-[13.5px] text-fg">
          You&apos;re viewing <CheckpointPill label={cp.label} /> <span className="font-medium [overflow-wrap:anywhere]">{cp.title}</span>
        </p>
        <p className="mt-0.5 text-[12.5px] text-muted">
          An immutable snapshot from {fullDate(cp.created_at)} on {cp.branch_name}. Nothing in it can change.
        </p>
      </div>
      <div className="col-start-2 flex flex-wrap gap-1.5">
        <Button size="sm" variant="primary" onClick={onBack}>
          <Undo2 className="h-3.5 w-3.5" /> Back to live
        </Button>
        <Button size="sm" variant="secondary" onClick={onFork}>
          <GitFork className="h-3.5 w-3.5" /> Fork from here
        </Button>
        <Popover>
          <PopoverTrigger asChild>
            <Button size="sm" variant="secondary" disabled={!others.length}>
              <ArrowLeftRight className="h-3.5 w-3.5" /> Compare with…
            </Button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-80 p-1">
            <p className="px-2.5 py-1.5 text-[11px] font-medium uppercase tracking-wide text-faint">Compare {cp.label} with</p>
            <div className="max-h-72 overflow-y-auto">
              {others.map((o) => {
                const [from, to] = o.number < cp.number ? [o, cp] : [cp, o];
                return (
                  <Link
                    key={o.id}
                    href={`/checkpoints/${to.id}?compare=${from.id}`}
                    className="flex items-start gap-2 rounded-[var(--radius-md)] px-2.5 py-1.5 text-[13px] hover:bg-surface-2"
                  >
                    <CheckpointPill label={o.label} />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-fg">{o.title}</span>
                      <span className="block text-[11.5px] text-faint">{o.branch_name}</span>
                    </span>
                  </Link>
                );
              })}
            </div>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}

function ClosedBanner({ ov }: { ov: IdeaOverview }) {
  const idea = ov.idea!;
  const reopen = useReopen(idea.id);
  if (idea.status === "MERGED") {
    const target = (ov.related_ideas ?? []).find((r) => r.rel_type === "merged_into" && r.id === idea.merged_into_idea_id);
    return (
      <div className="flex flex-wrap items-center gap-3 rounded-[var(--radius-lg)] border border-border bg-surface-2 px-4 py-3 text-[13.5px]">
        <StatusBadge status="MERGED" />
        <p className="min-w-0 flex-1 text-muted">
          This idea was merged into{" "}
          {idea.merged_into_idea_id ? (
            <Link href={`/ideas/${idea.merged_into_idea_id}`} className="font-medium text-fg underline decoration-border-strong underline-offset-2 hover:text-accent">
              {target?.title || "another idea"}
            </Link>
          ) : (
            "another idea"
          )}
          . Its history is kept here for reference; continue the thinking there.
        </p>
      </div>
    );
  }
  if (!["CONCLUDED", "PARKED", "ABANDONED"].includes(idea.status)) return null;
  const info = outcomeInfo(idea.outcome);
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-[var(--radius-lg)] border border-border bg-surface-2 px-4 py-3 text-[13.5px]">
      <StatusBadge status={idea.status} />
      <p className="min-w-0 flex-1 text-muted">
        {info ? (
          <>
            Concluded as <span className="font-medium text-fg">{info.label.toLowerCase()}</span>
          </>
        ) : (
          <>{humanize(idea.status)}</>
        )}
        {idea.concluded_at ? ` on ${fullDate(idea.concluded_at)}` : ""}.{idea.outcome_note ? <span className="text-fg"> “{idea.outcome_note}”</span> : null} Nothing was deleted.
      </p>
      <Button size="sm" variant="secondary" onClick={() => reopen.mutate()} loading={reopen.isPending}>
        <RotateCcw className="h-3.5 w-3.5" /> Reopen
      </Button>
    </div>
  );
}

function IdeaChatPanel({
  ov,
  items,
  viewingCp,
  onClose,
  bare,
}: {
  ov: IdeaOverview;
  items: KnowledgeItem[];
  viewingCp: Checkpoint | null;
  onClose: () => void;
  /** Inside a drawer that already has a title bar. */
  bare?: boolean;
}) {
  const idea = ov.idea!;
  const branch = ov.branch;
  const latestDecision = items.filter((i) => i.kind === "decision" && i.status === "ACTIVE").sort((a, b) => b.ref_number - a.ref_number)[0];
  const suggestions = viewingCp
    ? [`What did we know at ${viewingCp.label}?`, `What has changed since ${viewingCp.label}?`, `Why did we decide what we decided at ${viewingCp.label}?`]
    : [
        "Where does this idea stand right now?",
        ...(latestDecision ? [`Why did we decide ${latestDecision.label}?`] : []),
        "What assumptions haven't I validated?",
        "What should I do next?",
      ];
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className={cn("flex h-12 shrink-0 items-center gap-2 border-b border-border px-4", bare && "hidden")}>
        <MessageSquare className="h-4 w-4 text-muted" aria-hidden />
        <div className="min-w-0 flex-1 text-[13px]">
          <span className="font-medium text-fg">Chat</span>
          <span className="text-faint">
            {" "}
            · {branch?.name ?? "Main"}
            {viewingCp ? ` @ ${viewingCp.label}` : ""}
          </span>
        </div>
        <Button size="icon-sm" variant="ghost" onClick={onClose} aria-label="Hide chat panel" className="hidden xl:inline-flex">
          <PanelRightClose className="h-4 w-4" />
        </Button>
      </div>
      <div className="min-h-0 flex-1">
        <ChatView
          compact
          ideaId={idea.id}
          branchId={branch?.id ?? null}
          checkpointId={viewingCp?.id ?? null}
          emptyTitle={viewingCp ? `Ask about ${viewingCp.label}` : "Think it through"}
          emptySubtitle={
            viewingCp
              ? `Answers are grounded in the ${viewingCp.label} snapshot (${plural(viewingCp.snapshot?.items?.length ?? 0, "item")}).`
              : `Decisions, questions and checkpoints you make here are recorded on ${branch?.name ?? "this branch"}.`
          }
          suggestions={suggestions}
        />
      </div>
    </div>
  );
}

function IdeaSkeleton() {
  return (
    <div className="flex h-app">
      <div className="mx-auto w-full max-w-5xl flex-1 px-4 pt-6 sm:px-8">
        <Skeleton className="mb-4 h-3.5 w-32" />
        <Skeleton className="h-11 w-2/3" />
        <div className="mt-4 flex gap-2">
          <Skeleton className="h-5 w-20 rounded-full" />
          <Skeleton className="h-5 w-16 rounded-full" />
        </div>
        <Skeleton className="mt-6 h-12 w-full" />
        <Skeleton className="mt-8 h-9 w-full" />
        <div className="mt-8 grid gap-6 md:grid-cols-2">
          <SkeletonLines lines={4} />
          <SkeletonLines lines={4} />
        </div>
      </div>
    </div>
  );
}
