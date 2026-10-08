"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ArrowLeftRight, GitBranch, GitFork, MoreHorizontal, Star } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Branch, BranchComparison, Checkpoint, InheritanceRecord, KnowledgeItem } from "@/lib/types";
import { entityMeta, tone } from "@/lib/entities";
import { cn, fullDate, humanize, plural, shortDate } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Badge, ItemStatus } from "@/components/ui/badges";
import { Dropdown, DropdownContent, DropdownItem, DropdownLabel, DropdownSeparator, DropdownTrigger, Tooltip } from "@/components/ui/overlay";
import { ErrorState, SectionTitle, Skeleton, SkeletonLines } from "@/components/ui/primitives";
import { RefLabel } from "@/components/domain/knowledge";
import { Markdown } from "@/components/markdown";
import { CheckpointPill, Select } from "../ui";
import { useBranchTree, useInheritance, useRefreshIdea } from "../hooks";
import { forkPath, layoutBranchTree, orderBranches } from "../lib/branch-tree";

export function BranchesTab({
  ideaId,
  currentBranch,
  viewingCpId,
  onSwitchBranch,
  onViewCheckpoint,
  onFork,
  onSelectItem,
}: {
  ideaId: string;
  currentBranch: Branch | undefined;
  viewingCpId: string | null;
  onSwitchBranch: (id: string) => void;
  onViewCheckpoint: (cp: Checkpoint) => void;
  onFork: () => void;
  onSelectItem: (id: string) => void;
}) {
  const tree = useBranchTree(ideaId);
  if (tree.isLoading)
    return (
      <div className="space-y-4">
        <Skeleton className="h-40 w-full" />
        <SkeletonLines lines={4} />
      </div>
    );
  if (tree.error || !tree.data) return <ErrorState error={tree.error} onRetry={() => void tree.refetch()} />;
  const { branches, checkpoints } = tree.data;
  return (
    <div className="space-y-12">
      <section>
        <SectionTitle
          action={
            <Button size="sm" variant="secondary" onClick={onFork}>
              <GitFork className="h-3.5 w-3.5" /> Fork
            </Button>
          }
        >
          Branch tree
        </SectionTitle>
        <BranchTreeDiagram
          branches={branches ?? []}
          checkpoints={checkpoints ?? []}
          currentBranchId={currentBranch?.id}
          viewingCpId={viewingCpId}
          onSwitchBranch={onSwitchBranch}
          onViewCheckpoint={onViewCheckpoint}
        />
      </section>
      <BranchList ideaId={ideaId} branches={branches ?? []} checkpoints={checkpoints ?? []} currentBranchId={currentBranch?.id} onSwitchBranch={onSwitchBranch} />
      {(branches?.length ?? 0) > 1 && <CompareBranches branches={branches ?? []} currentBranchId={currentBranch?.id} onSelectItem={onSelectItem} />}
      {currentBranch && <InheritanceList branch={currentBranch} checkpoints={checkpoints ?? []} onSelectItem={onSelectItem} />}
    </div>
  );
}

// ---------- Tree ----------

const LANE_H = 56;

export function BranchTreeDiagram({
  branches,
  checkpoints,
  currentBranchId,
  viewingCpId,
  onSwitchBranch,
  onViewCheckpoint,
}: {
  branches: Branch[];
  checkpoints: Checkpoint[];
  currentBranchId?: string;
  viewingCpId: string | null;
  onSwitchBranch: (id: string) => void;
  onViewCheckpoint: (cp: Checkpoint) => void;
}) {
  const layout = useMemo(() => layoutBranchTree(branches, checkpoints, { laneHeight: LANE_H }), [branches, checkpoints]);
  if (!branches.length) return <p className="text-[13px] text-faint">No branches.</p>;
  return (
    <div className="flex overflow-hidden rounded-[var(--radius-lg)] border border-border bg-surface">
      {/* Lane labels */}
      <ul className="w-[clamp(120px,32%,220px)] shrink-0 border-r border-border" style={{ paddingTop: layout.lanes.length ? layout.lanes[0].y - LANE_H / 2 : 0 }}>
        {layout.lanes.map((l) => {
          const cur = l.branch.id === currentBranchId;
          return (
            <li key={l.branch.id} style={{ height: LANE_H }}>
              <button
                type="button"
                onClick={() => onSwitchBranch(l.branch.id)}
                aria-current={cur ? "true" : undefined}
                className={cn("flex h-full w-full items-center gap-2 px-3 text-left transition-colors hover:bg-surface-2", cur && "bg-k-branch-soft/60")}
                style={{ paddingLeft: 12 + Math.min(l.depth, 3) * 10 }}
              >
                <GitBranch className="h-3.5 w-3.5 shrink-0" style={{ color: cur ? "var(--k-branch)" : "var(--text-faint)" }} aria-hidden />
                <span className="min-w-0 flex-1">
                  <span className={cn("block truncate text-[13px]", cur ? "font-semibold text-fg" : "text-fg")}>{l.branch.name}</span>
                  <span className="block truncate text-[11px] text-faint">
                    {l.branch.is_default ? "default" : l.branch.forked_from_checkpoint_number ? `from CP${l.branch.forked_from_checkpoint_number}` : "branch"}
                    {l.branch.status !== "ACTIVE" ? ` · ${humanize(l.branch.status).toLowerCase()}` : ""}
                  </span>
                </span>
              </button>
            </li>
          );
        })}
      </ul>
      {/* Graph */}
      <div className="min-w-0 flex-1 overflow-x-auto">
        <div className="relative min-w-full" style={{ width: layout.width, height: layout.height }}>
          <svg className="absolute inset-0 h-full w-full" aria-hidden>
            {layout.lanes.map((l) => {
              const cur = l.branch.id === currentBranchId;
              const stroke = cur ? "var(--k-branch)" : "var(--border-strong)";
              const dim = l.branch.status !== "ACTIVE";
              return (
                <g key={l.branch.id} opacity={dim ? 0.55 : 1}>
                  {cur && <rect x={0} y={l.y - LANE_H / 2} width="100%" height={LANE_H} fill="var(--k-branch-soft)" opacity={0.6} />}
                  {l.fork && <path d={forkPath(l.fork.fromX, l.fork.fromY, l.startX, l.y)} fill="none" stroke={stroke} strokeWidth={2} />}
                  <line x1={l.startX} y1={l.y} x2={l.endX} y2={l.y} stroke={stroke} strokeWidth={2} />
                  {l.headX !== null && <line x1={l.endX} y1={l.y} x2={l.headX} y2={l.y} stroke={stroke} strokeWidth={2} strokeDasharray="2 4" />}
                  {l.headX !== null && <circle cx={l.headX} cy={l.y} r={4} fill="var(--surface)" stroke={stroke} strokeWidth={2} />}
                </g>
              );
            })}
          </svg>
          {layout.dots.map((d) => {
            const on = d.checkpoint.id === viewingCpId;
            const special = d.checkpoint.kind === "conclusion" ? "action" : d.checkpoint.kind === "fork_base" ? "branch" : "checkpoint";
            return (
              <Tooltip
                key={d.checkpoint.id}
                content={
                  <span>
                    {d.checkpoint.label} · {d.checkpoint.title}
                    <br />
                    <span className="opacity-70">
                      {d.checkpoint.branch_name} · {shortDate(d.checkpoint.created_at)}
                    </span>
                  </span>
                }
              >
                <button
                  type="button"
                  onClick={() => onViewCheckpoint(d.checkpoint)}
                  className="group absolute flex -translate-x-1/2 flex-col items-center"
                  style={{ left: d.x, top: d.y - 7 }}
                  aria-label={`View ${d.checkpoint.label}: ${d.checkpoint.title}`}
                >
                  <span
                    className={cn("block h-3.5 w-3.5 rounded-full border-2 transition-transform group-hover:scale-125", on && "scale-125 ring-4 ring-accent/25")}
                    style={{ borderColor: `var(--k-${special})`, background: on ? `var(--k-${special})` : "var(--surface)" }}
                  />
                  <span className={cn("mt-1 font-mono text-[10.5px] font-semibold", on ? "text-accent" : "text-muted group-hover:text-fg")}>{d.checkpoint.label}</span>
                </button>
              </Tooltip>
            );
          })}
          {layout.lanes.map((l) =>
            l.headX !== null ? (
              <span key={`h-${l.branch.id}`} className="pointer-events-none absolute -translate-x-1/2 text-[10px] font-medium uppercase tracking-wide text-faint" style={{ left: l.headX, top: l.y + 8 }}>
                live
              </span>
            ) : null,
          )}
        </div>
      </div>
    </div>
  );
}

// ---------- Branch list + actions ----------

function BranchList({
  ideaId,
  branches,
  checkpoints,
  currentBranchId,
  onSwitchBranch,
}: {
  ideaId: string;
  branches: Branch[];
  checkpoints: Checkpoint[];
  currentBranchId?: string;
  onSwitchBranch: (id: string) => void;
}) {
  const refresh = useRefreshIdea();
  const ordered = useMemo(() => orderBranches(branches, checkpoints), [branches, checkpoints]);
  const update = useMutation({
    mutationFn: (v: { id: string; body: { status?: string; make_default?: boolean }; msg: string }) => api.patch<Branch>(`/v1/branches/${v.id}`, v.body),
    onSuccess: (_, v) => {
      toast.success(v.msg);
      void refresh(ideaId);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <section>
      <SectionTitle>{plural(branches.length, "branch", "branches")}</SectionTitle>
      <ul className="divide-y divide-border border-y border-border">
        {ordered.map(({ branch: b, depth }) => {
          const cur = b.id === currentBranchId;
          return (
            <li key={b.id} className="flex items-start gap-3 py-3" style={{ paddingLeft: Math.min(depth, 3) * 16 }}>
              <GitBranch className="mt-1 h-4 w-4 shrink-0" style={{ color: cur ? "var(--k-branch)" : "var(--text-faint)" }} aria-hidden />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <button type="button" onClick={() => onSwitchBranch(b.id)} className={cn("text-left text-[14.5px] font-medium hover:text-accent", cur ? "text-fg" : "text-fg")}>
                    {b.name}
                  </button>
                  {b.is_default && (
                    <Badge>
                      <Star className="h-2.5 w-2.5" /> default
                    </Badge>
                  )}
                  {cur && <Badge className="border-transparent bg-k-branch-soft text-k-branch">viewing</Badge>}
                  {b.status !== "ACTIVE" && <Badge>{humanize(b.status)}</Badge>}
                  {b.fork_mode === "selective" && <Badge>selective fork</Badge>}
                </div>
                {b.description && <p className="mt-0.5 text-[13px] text-muted">{b.description}</p>}
                <p className="mt-1 text-[12px] text-faint">
                  {b.forked_from_checkpoint_number ? `Forked from CP${b.forked_from_checkpoint_number} · ` : ""}
                  {plural(b.checkpoint_count, "checkpoint")} · {plural(b.knowledge_count, "item")} · created {shortDate(b.created_at)}
                </p>
              </div>
              <Dropdown>
                <DropdownTrigger asChild>
                  <Button size="icon-sm" variant="ghost" aria-label={`Actions for ${b.name}`}>
                    <MoreHorizontal className="h-4 w-4" />
                  </Button>
                </DropdownTrigger>
                <DropdownContent>
                  <DropdownLabel>{b.name}</DropdownLabel>
                  {!cur && <DropdownItem onSelect={() => onSwitchBranch(b.id)}>Switch to this branch</DropdownItem>}
                  {!b.is_default && (
                    <DropdownItem onSelect={() => update.mutate({ id: b.id, body: { make_default: true }, msg: `${b.name} is now the default branch` })}>
                      <Star className="h-3.5 w-3.5 text-muted" /> Make default
                    </DropdownItem>
                  )}
                  <DropdownSeparator />
                  {b.status === "ACTIVE" ? (
                    <>
                      <DropdownItem onSelect={() => update.mutate({ id: b.id, body: { status: "CONCLUDED" }, msg: `${b.name} concluded` })}>Mark concluded</DropdownItem>
                      <DropdownItem onSelect={() => update.mutate({ id: b.id, body: { status: "ARCHIVED" }, msg: `${b.name} archived` })}>Archive</DropdownItem>
                    </>
                  ) : (
                    <DropdownItem onSelect={() => update.mutate({ id: b.id, body: { status: "ACTIVE" }, msg: `${b.name} is active again` })}>Reactivate</DropdownItem>
                  )}
                </DropdownContent>
              </Dropdown>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

// ---------- Compare ----------

function CompareBranches({ branches, currentBranchId, onSelectItem }: { branches: Branch[]; currentBranchId?: string; onSelectItem: (id: string) => void }) {
  const def = branches.find((b) => b.is_default) ?? branches[0];
  const other = branches.find((b) => b.id !== (currentBranchId ?? def.id)) ?? branches[1];
  const [a, setA] = useState(currentBranchId && currentBranchId !== other?.id ? currentBranchId : def.id);
  const [b, setB] = useState(other?.id ?? "");
  const [run, setRun] = useState<{ a: string; b: string } | null>(null);
  const cmp = useQuery({
    queryKey: ["branch-compare", run?.a, run?.b],
    queryFn: () => api.get<BranchComparison>("/v1/branches/compare", { query: { a: run!.a, b: run!.b, explain: true } }),
    enabled: Boolean(run),
    staleTime: 60_000,
  });
  const name = (id: string) => branches.find((x) => x.id === id)?.name ?? "branch";
  return (
    <section>
      <SectionTitle>Compare branches</SectionTitle>
      <form
        className="flex flex-wrap items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (a && b && a !== b) setRun({ a, b });
        }}
      >
        <Select value={a} onChange={(e) => setA(e.target.value)} className="min-w-[160px] flex-1" aria-label="Branch A">
          {branches.map((x) => (
            <option key={x.id} value={x.id}>
              {x.name}
            </option>
          ))}
        </Select>
        <Button
          type="button"
          size="icon"
          variant="ghost"
          onClick={() => {
            setA(b);
            setB(a);
          }}
          aria-label="Swap branches"
        >
          <ArrowLeftRight className="h-4 w-4" />
        </Button>
        <Select value={b} onChange={(e) => setB(e.target.value)} className="min-w-[160px] flex-1" aria-label="Branch B">
          {branches.map((x) => (
            <option key={x.id} value={x.id}>
              {x.name}
            </option>
          ))}
        </Select>
        <Button type="submit" variant="primary" disabled={!a || !b || a === b} loading={cmp.isFetching}>
          Compare
        </Button>
      </form>
      {a === b && <p className="mt-2 text-[12.5px] text-faint">Pick two different branches.</p>}
      {run && (
        <div className="mt-6">
          {cmp.isLoading ? (
            <SkeletonLines lines={5} />
          ) : cmp.error ? (
            <ErrorState error={cmp.error} onRetry={() => void cmp.refetch()} />
          ) : cmp.data ? (
            <ComparisonView c={cmp.data} aName={name(run.a)} bName={name(run.b)} onSelectItem={onSelectItem} />
          ) : null}
        </div>
      )}
    </section>
  );
}

function ComparisonView({ c, aName, bName, onSelectItem }: { c: BranchComparison; aName: string; bName: string; onSelectItem: (id: string) => void }) {
  return (
    <div className="space-y-6 animate-fade-in">
      <div className="rounded-[var(--radius-lg)] border border-dashed border-k-insight/40 px-4 py-3">
        <p className="mb-1 flex flex-wrap items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-k-insight">
          Interpretation · {c.analyzer === "deterministic" ? "summary" : `explained by ${c.analyzer.replace(/^llm:/, "")}`}
        </p>
        <div className="text-[14px]">
          <Markdown>{c.narrative}</Markdown>
        </div>
        <p className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-muted">
          <span>{plural(c.shared, "shared item")}</span>
          {c.common_ancestor && (
            <span className="inline-flex items-center gap-1.5">
              diverged at <CheckpointPill label={c.common_ancestor.label ?? "CP"} />
            </span>
          )}
        </p>
      </div>
      <div className="grid gap-6 md:grid-cols-2">
        <DiffColumn title={`Only in ${aName}`} items={c.only_in_a} onSelectItem={onSelectItem} />
        <DiffColumn title={`Only in ${bName}`} items={c.only_in_b} onSelectItem={onSelectItem} />
      </div>
      {c.diverged.length > 0 && (
        <div>
          <h4 className="mb-2 text-[12px] font-semibold uppercase tracking-wider text-muted">Same item, different status</h4>
          <ul className="divide-y divide-border rounded-[var(--radius-lg)] border border-border">
            {c.diverged.map((d) => (
              <li key={d.before.id} className="flex flex-wrap items-center gap-3 px-3 py-2.5">
                <button type="button" onClick={() => onSelectItem(d.before.id)} className="flex min-w-0 flex-1 items-start gap-2.5 text-left hover:opacity-80">
                  <RefLabel kind={d.before.kind} label={d.before.label} />
                  <span className="min-w-0 text-[13.5px] text-fg">{d.before.statement}</span>
                </button>
                <span className="flex shrink-0 items-center gap-2 text-[12px]">
                  <span className="text-faint">{aName}:</span> <ItemStatus status={d.before.status} />
                  <span className="text-faint">{bName}:</span> <ItemStatus status={d.after.status} />
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function DiffColumn({ title, items, onSelectItem }: { title: string; items: KnowledgeItem[]; onSelectItem: (id: string) => void }) {
  return (
    <div className="min-w-0">
      <h4 className="mb-2 flex items-baseline gap-2 text-[12px] font-semibold uppercase tracking-wider text-muted">
        <span className="truncate">{title}</span> <span className="font-normal tabular-nums text-faint">{items.length}</span>
      </h4>
      {items.length === 0 ? (
        <p className="text-[13px] text-faint">Nothing unique.</p>
      ) : (
        <ul className="space-y-0.5">
          {items.map((it) => (
            <li key={it.id}>
              <button type="button" onClick={() => onSelectItem(it.id)} className="flex w-full items-start gap-2.5 rounded-[var(--radius-md)] px-1.5 py-1.5 text-left hover:bg-surface-2">
                <RefLabel kind={it.kind} label={it.label} />
                <span className="min-w-0 text-[13.5px] leading-snug text-fg">{it.statement}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// ---------- Inheritance ----------

const VIA_LABEL: Record<string, { title: string; hint: string }> = {
  checkpoint: { title: "From the fork checkpoint", hint: "Copied exactly as it was in the snapshot the branch started from." },
  selection: { title: "Selected from later thinking", hint: "Hand-picked from a later checkpoint when forking." },
  merge: { title: "Merged in", hint: "Brought in afterwards from another branch or external thinking." },
};

function InheritanceList({ branch, checkpoints, onSelectItem }: { branch: Branch; checkpoints: Checkpoint[]; onSelectItem: (id: string) => void }) {
  const inh = useInheritance(branch.id);
  const cpLabel = useMemo(() => new Map(checkpoints.map((c) => [c.id, c.label])), [checkpoints]);
  const groups = useMemo(() => {
    const m = new Map<string, InheritanceRecord[]>();
    for (const r of inh.data ?? []) {
      const l = m.get(r.via) ?? [];
      l.push(r);
      m.set(r.via, l);
    }
    return [...m.entries()].sort(([a], [b]) => ["checkpoint", "selection", "merge"].indexOf(a) - ["checkpoint", "selection", "merge"].indexOf(b));
  }, [inh.data]);
  return (
    <section>
      <SectionTitle>What {branch.name} inherited</SectionTitle>
      {inh.isLoading ? (
        <SkeletonLines lines={3} />
      ) : inh.error ? (
        <ErrorState error={inh.error} onRetry={() => void inh.refetch()} />
      ) : groups.length === 0 ? (
        <p className="text-[13.5px] text-muted">
          {branch.is_default && !branch.parent_branch_id ? `${branch.name} is the original line of thinking — it didn't inherit anything.` : "This branch didn't inherit anything."}
        </p>
      ) : (
        <div className="space-y-6">
          {groups.map(([via, recs]) => (
            <div key={via}>
              <div className="mb-1.5 flex flex-wrap items-baseline gap-x-2">
                <h4 className="text-[13.5px] font-medium text-fg">{VIA_LABEL[via]?.title ?? humanize(via)}</h4>
                <span className="text-[12px] tabular-nums text-faint">{recs.length}</span>
                <span className="text-[12px] text-faint">· {VIA_LABEL[via]?.hint}</span>
              </div>
              <ul className="-mx-1">
                {recs.map((r) => {
                  const knowledge = ["decision", "assumption", "evidence", "insight", "question", "action"].includes(r.entity_type);
                  const target = r.new_entity_id ?? r.source_entity_id;
                  const body = (
                    <>
                      {r.label ? (
                        <RefLabel kind={r.entity_type} label={r.label} />
                      ) : (
                        <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: tone(r.entity_type) }} aria-hidden />
                      )}
                      <span className="min-w-0 flex-1 text-[13.5px] leading-snug text-fg">{r.title || entityMeta(r.entity_type).label}</span>
                      {r.source_checkpoint_id && cpLabel.get(r.source_checkpoint_id) && <CheckpointPill label={cpLabel.get(r.source_checkpoint_id)!} muted />}
                      <span className="hidden shrink-0 text-[11px] text-faint sm:inline" title={fullDate(r.created_at)}>
                        {shortDate(r.created_at)}
                      </span>
                    </>
                  );
                  return (
                    <li key={r.id}>
                      {knowledge ? (
                        <button type="button" onClick={() => onSelectItem(target)} className="flex w-full items-start gap-2.5 rounded-[var(--radius-md)] px-1.5 py-1.5 text-left hover:bg-surface-2">
                          {body}
                        </button>
                      ) : (
                        <div className="flex items-start gap-2.5 px-1.5 py-1.5">{body}</div>
                      )}
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
