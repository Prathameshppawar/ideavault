"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Lock, Search, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { cn, humanize } from "@/lib/format";
import { Checkbox, Switch } from "@/components/ui/overlay";
import { ErrorState, Input, SkeletonLines } from "@/components/ui/primitives";
import { CategoryTag } from "@/features/connectors/category-tag";
import { CATEGORY_COPY, DECISION_LABEL, decide, groupTools, toggleDisabled, type AgentPolicy, type ToolInfo } from "./policy";

const policyKey = ["agent-policy"] as const;

export function AgentSection() {
  const qc = useQueryClient();
  const policy = useQuery({ queryKey: policyKey, queryFn: () => api.get<AgentPolicy>("/v1/agent/policy") });
  const tools = useQuery({ queryKey: ["agent-tools"], queryFn: () => api.get<ToolInfo[]>("/v1/agent/tools"), staleTime: 5 * 60_000 });
  const save = useMutation({
    mutationFn: (p: AgentPolicy) => api.put<AgentPolicy>("/v1/agent/policy", p),
    onMutate: async (p) => {
      await qc.cancelQueries({ queryKey: policyKey });
      const prev = qc.getQueryData<AgentPolicy>(policyKey);
      qc.setQueryData(policyKey, p);
      return { prev };
    },
    onError: (e, _p, ctx) => {
      if (ctx?.prev) qc.setQueryData(policyKey, ctx.prev);
      toast.error(errorMessage(e));
    },
    onSuccess: () => toast.success("Agent permissions saved", { id: "agent-policy" }),
    onSettled: () => void qc.invalidateQueries({ queryKey: policyKey }),
  });
  if (policy.isLoading) return <SkeletonLines lines={6} />;
  if (policy.error || !policy.data) return <ErrorState error={policy.error} onRetry={() => void policy.refetch()} />;
  const p = policy.data;
  const update = (patch: Partial<AgentPolicy>) => save.mutate({ ...p, ...patch, disabled_tools: patch.disabled_tools ?? p.disabled_tools ?? [] });
  return (
    <div className="space-y-6">
      <ul className="divide-y divide-border rounded-[var(--radius-lg)] border border-border bg-surface">
        <PolicyRow
          id="confirm-writes"
          title="Ask before every write"
          body="By default the agent records decisions, creates branches and drafts artifacts as soon as you ask. Turn this on to approve each change first."
          checked={p.confirm_writes}
          onChange={(v) => update({ confirm_writes: v })}
        />
        <PolicyRow
          id="auto-external"
          title="Let connectors run external actions without asking"
          body="External actions fetch web pages or act in GitHub and other connected tools."
          checked={p.auto_external}
          onChange={(v) => update({ auto_external: v })}
          warning={p.auto_external ? "On: connector calls run without a confirmation step. Fetched content is still treated as untrusted data." : undefined}
        />
        <li className="flex items-start gap-4 px-4 py-3.5">
          <div className="min-w-0 flex-1">
            <p className="flex items-center gap-1.5 text-[14px] text-fg">
              <Lock className="h-3.5 w-3.5 text-faint" /> Destructive actions always require confirmation
            </p>
            <p className="mt-0.5 text-[12.5px] leading-relaxed text-muted">Deleting an idea or artifact always pauses for your explicit approval. This can&apos;t be turned off.</p>
          </div>
          <Switch checked disabled aria-label="Destructive actions always require confirmation (fixed)" />
        </li>
      </ul>
      <ToolList tools={tools.data} loading={tools.isLoading} error={tools.error} retry={() => void tools.refetch()} policy={p} onToggle={(name, disabled) => update({ disabled_tools: toggleDisabled(p, name, disabled).disabled_tools })} />
    </div>
  );
}

function PolicyRow({ id, title, body, checked, onChange, warning }: { id: string; title: string; body: string; checked: boolean; onChange: (v: boolean) => void; warning?: string }) {
  return (
    <li className="px-4 py-3.5">
      <div className="flex items-start gap-4">
        <label htmlFor={id} className="min-w-0 flex-1 cursor-pointer">
          <span className="block text-[14px] text-fg">{title}</span>
          <span className="mt-0.5 block text-[12.5px] leading-relaxed text-muted">{body}</span>
        </label>
        <Switch id={id} checked={checked} onCheckedChange={onChange} />
      </div>
      {warning && (
        <p className="mt-2 flex items-start gap-1.5 rounded-[var(--radius-md)] bg-warning-soft px-2.5 py-1.5 text-[12.5px] text-warning animate-fade-in">
          <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" /> {warning}
        </p>
      )}
    </li>
  );
}

function ToolList({
  tools,
  loading,
  error,
  retry,
  policy,
  onToggle,
}: {
  tools?: ToolInfo[];
  loading: boolean;
  error: unknown;
  retry: () => void;
  policy: AgentPolicy;
  onToggle: (name: string, disabled: boolean) => void;
}) {
  const [q, setQ] = useState("");
  const groups = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return groupTools((tools ?? []).filter((t) => !needle || t.name.includes(needle) || t.description.toLowerCase().includes(needle)));
  }, [tools, q]);
  const disabledCount = (policy.disabled_tools ?? []).length;
  if (loading) return <SkeletonLines lines={4} />;
  if (error) return <ErrorState error={error} onRetry={retry} />;
  return (
    <div>
      <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h3 className="text-[14px] font-medium text-fg">Tools</h3>
          <p className="text-[12.5px] text-muted">
            Untick a tool to disable it entirely. {disabledCount > 0 ? `${disabledCount} disabled.` : "All tools are enabled."}
          </p>
        </div>
        <div className="relative w-full sm:w-56">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" />
          <Input className="h-8 pl-8 text-[13px]" placeholder="Find a tool" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Find a tool" />
        </div>
      </div>
      <div className="space-y-2">
        {groups.map((g) => (
          <ToolGroup key={g.category} category={g.category} tools={g.tools} policy={policy} onToggle={onToggle} forceOpen={!!q.trim()} />
        ))}
        {groups.length === 0 && <p className="text-[13px] text-faint">No tools match.</p>}
      </div>
    </div>
  );
}

function ToolGroup({ category, tools, policy, onToggle, forceOpen }: { category: string; tools: ToolInfo[]; policy: AgentPolicy; onToggle: (name: string, disabled: boolean) => void; forceOpen: boolean }) {
  const [open, setOpen] = useState(false);
  const expanded = open || forceOpen;
  const disabled = tools.filter((t) => (policy.disabled_tools ?? []).includes(t.name)).length;
  const copy = CATEGORY_COPY[category];
  const sample = decide("", category, { ...policy, disabled_tools: [] });
  return (
    <div className="rounded-[var(--radius-lg)] border border-border bg-surface">
      <button type="button" onClick={() => setOpen((v) => !v)} aria-expanded={expanded} className="flex w-full items-center gap-3 px-4 py-3 text-left">
        <ChevronRight className={cn("h-3.5 w-3.5 shrink-0 text-faint transition-transform", expanded && "rotate-90")} />
        <CategoryTag category={category} />
        <span className="hidden min-w-0 flex-1 truncate text-[13px] text-muted sm:block">{copy?.body ?? humanize(category)}</span>
        <span className="flex-1 sm:hidden" aria-hidden />
        <span className="shrink-0 text-[12px] text-faint">
          {tools.length - disabled}/{tools.length} on · {DECISION_LABEL[sample].toLowerCase()}
        </span>
      </button>
      {expanded && (
        <ul className="divide-y divide-border border-t border-border animate-fade-in">
          {tools.map((t) => {
            const off = (policy.disabled_tools ?? []).includes(t.name);
            const d = decide(t.name, t.category, policy);
            return (
              <li key={t.name} className="flex items-start gap-3 px-4 py-2.5">
                <Checkbox id={`tool-${t.name}`} className="mt-0.5" checked={!off} onCheckedChange={(v) => onToggle(t.name, !v)} aria-label={`${off ? "Enable" : "Disable"} ${t.name}`} />
                <label htmlFor={`tool-${t.name}`} className="min-w-0 flex-1 cursor-pointer">
                  <code className={cn("font-mono text-[12.5px]", off ? "text-faint line-through" : "text-fg")}>{t.name}</code>
                  <span className="block text-[12px] leading-snug text-muted">{t.description}</span>
                </label>
                <span className={cn("shrink-0 pt-0.5 text-[11.5px]", d === "deny" ? "text-faint" : d === "confirm" ? "text-warning" : "text-faint")}>{DECISION_LABEL[d]}</span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
