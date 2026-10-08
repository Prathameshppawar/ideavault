"use client";

import Link from "next/link";
import { useMutation } from "@tanstack/react-query";
import { Bookmark, Check, ChevronDown, ChevronRight, CircleDot, Flag, GitBranch, GitFork, MessageSquare, MoreHorizontal, RotateCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Branch, Checkpoint, Idea } from "@/lib/types";
import { cn, humanize, shortDate, timeAgo, truncate } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/ui/badges";
import { Dropdown, DropdownContent, DropdownItem, DropdownLabel, DropdownSeparator, DropdownTrigger, Tooltip } from "@/components/ui/overlay";
import { CheckpointPill } from "./ui";
import { useRefreshIdea } from "./hooks";
import { isReopenable, STATUS_HINT, statusOptions } from "./lib/lifecycle";
import { sortCheckpointsDesc } from "./lib/fork";
import { orderBranches } from "./lib/branch-tree";

export interface HeaderActions {
  onCheckpoint: () => void;
  onFork: () => void;
  onConclude: () => void;
  onDelete: () => void;
  onToggleChat: () => void;
  onSwitchBranch: (id: string) => void;
  onViewCheckpoint: (cp: Checkpoint | null) => void;
  onTab: (t: "branches") => void;
}

export function IdeaHeader({
  idea,
  branch,
  branches,
  checkpoints,
  viewingCpId,
  chatOpen,
  actions,
}: {
  idea: Idea;
  branch: Branch | undefined;
  branches: Branch[];
  checkpoints: Checkpoint[];
  viewingCpId: string | null;
  chatOpen: boolean;
  actions: HeaderActions;
}) {
  const merged = idea.status === "MERGED";
  return (
    <header className="animate-fade-in">
      <div className="mb-3 flex items-center justify-between gap-3">
        <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1 text-[13px] text-muted">
          <Link href="/ideas" className="hover:text-fg">
            Ideas
          </Link>
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-faint" aria-hidden />
          <span className="truncate text-faint">{truncate(idea.title, 60)}</span>
        </nav>
        <Tooltip content={chatOpen ? "Hide chat" : "Think about this idea in chat"}>
          <Button size="sm" variant={chatOpen ? "ghost" : "secondary"} onClick={actions.onToggleChat} aria-pressed={chatOpen} aria-label={chatOpen ? "Hide chat panel" : "Show chat panel"}>
            <MessageSquare className="h-3.5 w-3.5" />
            <span className="hidden sm:inline">Chat</span>
          </Button>
        </Tooltip>
      </div>

      <h1 className="font-display text-[2.1rem] leading-[1.06] text-fg [overflow-wrap:anywhere] sm:text-[2.75rem]">{idea.title}</h1>

      <div className="mt-3 flex flex-wrap items-center gap-x-2.5 gap-y-2">
        <StatusMenu idea={idea} onConclude={actions.onConclude} />
        {idea.tags?.map((t) => (
          <span key={t} className="inline-flex h-5 items-center rounded-full border border-border px-2 text-[11.5px] text-muted">
            #{t}
          </span>
        ))}
        <span className="text-[12px] text-faint">
          v{idea.version} · active {timeAgo(idea.last_activity_at)}
        </span>
      </div>

      <div className="mt-5 flex flex-wrap items-center gap-2 border-y border-border py-2.5">
        <BranchSelector branch={branch} branches={branches} checkpoints={checkpoints} onSwitch={actions.onSwitchBranch} onFork={actions.onFork} onCompare={() => actions.onTab("branches")} />
        <CheckpointSelector checkpoints={checkpoints} viewingCpId={viewingCpId} branchId={branch?.id} onView={actions.onViewCheckpoint} onCreate={actions.onCheckpoint} />
        <div className="ml-auto flex items-center gap-1.5">
          <Tooltip content="Save an immutable snapshot of this branch">
            <Button size="sm" variant="ghost" onClick={actions.onCheckpoint} disabled={merged} aria-label="Create checkpoint">
              <Bookmark className="h-3.5 w-3.5" />
              <span className="hidden md:inline">Checkpoint</span>
            </Button>
          </Tooltip>
          <Tooltip content={checkpoints.length ? "Start a new direction from a checkpoint" : "Create a checkpoint first — forks start from one"}>
            <span>
              <Button size="sm" variant="ghost" onClick={actions.onFork} disabled={merged} aria-label="Fork a new branch">
                <GitFork className="h-3.5 w-3.5" />
                <span className="hidden md:inline">Fork</span>
              </Button>
            </span>
          </Tooltip>
          <Tooltip content={merged ? "Already merged into another idea" : "Wrap it up — nothing is deleted"}>
            <span>
              <Button size="sm" variant="secondary" onClick={actions.onConclude} disabled={merged} aria-label="Conclude idea">
                <Flag className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">Conclude</span>
              </Button>
            </span>
          </Tooltip>
          <OverflowMenu idea={idea} onDelete={actions.onDelete} />
        </div>
      </div>
    </header>
  );
}

function StatusMenu({ idea, onConclude }: { idea: Idea; onConclude: () => void }) {
  const refresh = useRefreshIdea();
  const change = useMutation({
    mutationFn: (status: string) => api.patch<Idea>(`/v1/ideas/${idea.id}`, { status }),
    onSuccess: (i) => {
      toast.success(`Status → ${humanize(i.status).toLowerCase()}`);
      void refresh(idea.id);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  if (idea.status === "MERGED") return <StatusBadge status={idea.status} />;
  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-full pr-1.5 transition-opacity hover:opacity-80 disabled:opacity-50"
          aria-label={`Status: ${humanize(idea.status)}. Change status`}
          disabled={change.isPending}
        >
          <StatusBadge status={idea.status} />
          <ChevronDown className="h-3 w-3 text-faint" aria-hidden />
        </button>
      </DropdownTrigger>
      <DropdownContent align="start" className="w-72">
        <DropdownLabel>Lifecycle status</DropdownLabel>
        {statusOptions(idea.status).map((o) => (
          <DropdownItem
            key={o.status}
            disabled={!o.allowed}
            onSelect={() => change.mutate(o.status)}
            className={cn("items-start data-[disabled]:cursor-default data-[disabled]:opacity-45")}
          >
            <span className="min-w-0 flex-1">
              <span className="block">{humanize(o.status)}</span>
              <span className="block text-[11.5px] text-faint">{o.status === idea.status ? "Current status" : !o.allowed ? o.reason : STATUS_HINT[o.status]}</span>
            </span>
            {o.status === idea.status && <Check className="mt-0.5 h-3.5 w-3.5 text-accent" aria-hidden />}
          </DropdownItem>
        ))}
        <DropdownSeparator />
        <DropdownItem onSelect={onConclude}>
          <Flag className="h-3.5 w-3.5 text-muted" /> Conclude with an outcome…
        </DropdownItem>
      </DropdownContent>
    </Dropdown>
  );
}

function BranchSelector({
  branch,
  branches,
  checkpoints,
  onSwitch,
  onFork,
  onCompare,
}: {
  branch: Branch | undefined;
  branches: Branch[];
  checkpoints: Checkpoint[];
  onSwitch: (id: string) => void;
  onFork: () => void;
  onCompare: () => void;
}) {
  const ordered = orderBranches(branches, checkpoints);
  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <Button size="sm" variant="secondary" className="max-w-[min(240px,60vw)]" aria-label={`Branch: ${branch?.name ?? "none"}. Switch branch`}>
          <GitBranch className="h-3.5 w-3.5 shrink-0 text-k-branch" />
          <span className="truncate">{branch?.name ?? "Branch"}</span>
          {branch?.forked_from_checkpoint_number ? <span className="shrink-0 text-[11.5px] font-normal text-faint">from CP{branch.forked_from_checkpoint_number}</span> : null}
          <ChevronDown className="h-3 w-3 shrink-0 text-faint" />
        </Button>
      </DropdownTrigger>
      <DropdownContent align="start" className="w-80 max-w-[calc(100vw-2rem)]">
        <DropdownLabel>Branches</DropdownLabel>
        <div className="max-h-72 overflow-y-auto">
          {ordered.map(({ branch: b, depth }) => (
            <DropdownItem key={b.id} onSelect={() => onSwitch(b.id)} className="items-start">
              <GitBranch className="mt-0.5 h-3.5 w-3.5 shrink-0" style={{ color: b.id === branch?.id ? "var(--k-branch)" : "var(--text-faint)", marginLeft: Math.min(depth, 3) * 10 }} />
              <span className="min-w-0 flex-1">
                <span className="block truncate">{b.name}</span>
                <span className="block text-[11.5px] text-faint">
                  {b.is_default ? "default · " : ""}
                  {b.forked_from_checkpoint_number ? `from CP${b.forked_from_checkpoint_number} · ` : ""}
                  {b.knowledge_count} items · {b.checkpoint_count} checkpoints
                  {b.status !== "ACTIVE" ? ` · ${humanize(b.status).toLowerCase()}` : ""}
                </span>
              </span>
              {b.id === branch?.id && <Check className="mt-0.5 h-3.5 w-3.5 text-accent" aria-hidden />}
            </DropdownItem>
          ))}
        </div>
        <DropdownSeparator />
        <DropdownItem onSelect={onFork}>
          <GitFork className="h-3.5 w-3.5 text-muted" /> Fork a new direction…
        </DropdownItem>
        {branches.length > 1 && (
          <DropdownItem onSelect={onCompare}>
            <GitBranch className="h-3.5 w-3.5 text-muted" /> Compare branches
          </DropdownItem>
        )}
      </DropdownContent>
    </Dropdown>
  );
}

function CheckpointSelector({
  checkpoints,
  viewingCpId,
  branchId,
  onView,
  onCreate,
}: {
  checkpoints: Checkpoint[];
  viewingCpId: string | null;
  branchId?: string;
  onView: (cp: Checkpoint | null) => void;
  onCreate: () => void;
}) {
  const sorted = sortCheckpointsDesc(checkpoints);
  const viewing = sorted.find((c) => c.id === viewingCpId);
  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <Button size="sm" variant={viewing ? "subtle" : "secondary"} aria-label={viewing ? `Viewing ${viewing.label}. Choose checkpoint` : "Viewing live state. Choose checkpoint"}>
          {viewing ? (
            <>
              <CheckpointPill label={viewing.label} />
              <span className="hidden max-w-[160px] truncate sm:inline">{viewing.title}</span>
            </>
          ) : (
            <>
              <CircleDot className="h-3.5 w-3.5 text-success" />
              Live
            </>
          )}
          <ChevronDown className="h-3 w-3 text-faint" />
        </Button>
      </DropdownTrigger>
      <DropdownContent align="start" className="w-[22rem] max-w-[calc(100vw-2rem)]">
        <DropdownItem onSelect={() => onView(null)}>
          <CircleDot className="h-3.5 w-3.5 text-success" />
          <span className="flex-1">Live — current state</span>
          {!viewing && <Check className="h-3.5 w-3.5 text-accent" aria-hidden />}
        </DropdownItem>
        <DropdownSeparator />
        <DropdownLabel>Checkpoints · immutable snapshots</DropdownLabel>
        {sorted.length === 0 ? (
          <p className="px-2.5 py-2 text-[12.5px] text-faint">No checkpoints yet.</p>
        ) : (
          <div className="max-h-80 overflow-y-auto">
            {sorted.map((c) => (
              <DropdownItem key={c.id} onSelect={() => onView(c)} className="items-start">
                <CheckpointPill label={c.label} muted={c.branch_id !== branchId} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{c.title}</span>
                  <span className="block truncate text-[11.5px] text-faint">
                    {c.branch_name} · {shortDate(c.created_at)}
                    {c.kind !== "manual" ? ` · ${humanize(c.kind).toLowerCase()}` : ""}
                  </span>
                </span>
                {c.id === viewingCpId && <Check className="mt-0.5 h-3.5 w-3.5 text-accent" aria-hidden />}
              </DropdownItem>
            ))}
          </div>
        )}
        <DropdownSeparator />
        <DropdownItem onSelect={onCreate}>
          <Bookmark className="h-3.5 w-3.5 text-muted" /> Create checkpoint…
        </DropdownItem>
      </DropdownContent>
    </Dropdown>
  );
}

function OverflowMenu({ idea, onDelete }: { idea: Idea; onDelete: () => void }) {
  const refresh = useRefreshIdea();
  const reopen = useMutation({
    mutationFn: () => api.post<Idea>(`/v1/ideas/${idea.id}/reopen`, {}),
    onSuccess: () => {
      toast.success("Reopened — back to active thinking");
      void refresh(idea.id);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <Button size="icon-sm" variant="ghost" aria-label="More actions">
          <MoreHorizontal className="h-4 w-4" />
        </Button>
      </DropdownTrigger>
      <DropdownContent>
        {isReopenable(idea.status) && (
          <DropdownItem onSelect={() => reopen.mutate()}>
            <RotateCcw className="h-3.5 w-3.5 text-muted" /> Reopen
          </DropdownItem>
        )}
        <DropdownItem
          onSelect={() => {
            void navigator.clipboard?.writeText(window.location.href);
            toast.success("Link copied");
          }}
        >
          Copy link
        </DropdownItem>
        <DropdownSeparator />
        <DropdownItem danger onSelect={onDelete}>
          <Trash2 className="h-3.5 w-3.5" /> Delete idea…
        </DropdownItem>
      </DropdownContent>
    </Dropdown>
  );
}

/** Reopen button for banners. */
export function useReopen(ideaId: string) {
  const refresh = useRefreshIdea();
  return useMutation({
    mutationFn: () => api.post<Idea>(`/v1/ideas/${ideaId}/reopen`, {}),
    onSuccess: () => {
      toast.success("Reopened — back to active thinking");
      void refresh(ideaId);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
}
