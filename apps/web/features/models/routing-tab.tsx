"use client";

import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Info, Pin, RotateCcw, Route as RouteIcon } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { ModelConfig, RoutingPreview } from "@/lib/types";
import { cn } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badges";
import { Checkbox } from "@/components/ui/overlay";
import { ErrorState, Panel, SectionTitle, Skeleton } from "@/components/ui/primitives";
import { Select } from "@/features/usage/controls";
import { RELATIVE_COST_LABEL, formatContext, humanTask, modelKey, providerColor, providerLabel, sortProviders, PROVIDER_ORDER } from "./model-meta";
import { modelKeys, useModels, useRouting, useSettings, type Candidate, type RoutingPolicy } from "./queries";
import { buildRoutingPolicy, draftFromSettings, move, samePolicy, type RoutingDraft } from "./routing";

const TASK_ORDER = [
  "supervisor",
  "extraction",
  "summary",
  "synthesis",
  "artifact",
  "contradiction",
  "branch_comparison",
  "prompt_analysis",
  "thinking_analysis",
  "delta",
  "title",
  "classification",
  "model_lab",
];

export function RoutingTab({ onGoToProviders }: { onGoToProviders: () => void }) {
  const routing = useRouting();
  const settings = useSettings();
  const models = useModels();
  if (routing.isLoading || settings.isLoading) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-24 w-full" />
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-16 w-full" />
        ))}
      </div>
    );
  }
  if (routing.error || !routing.data) return <ErrorState error={routing.error} onRetry={() => void routing.refetch()} />;
  if (settings.error) return <ErrorState error={settings.error} onRetry={() => void settings.refetch()} />;
  return <RoutingEditor key={JSON.stringify(settings.data?.routing ?? null)} routing={routing.data} saved={draftFromSettings(settings.data)} models={models.data ?? []} onGoToProviders={onGoToProviders} />;
}

function RoutingEditor({ routing, saved, models, onGoToProviders }: { routing: RoutingPreview[]; saved: RoutingDraft; models: ModelConfig[]; onGoToProviders: () => void }) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState<RoutingDraft>(saved);
  const dirty = !samePolicy(draft, saved);
  const save = useMutation({
    mutationFn: (d: RoutingDraft) => api.put<RoutingPolicy>("/v1/models/routing", buildRoutingPolicy(d)),
    onSuccess: () => {
      toast.success("Routing preferences saved");
      void qc.invalidateQueries({ queryKey: modelKeys.settings });
      void qc.invalidateQueries({ queryKey: modelKeys.routing });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const tasks = useMemo(
    () => [...routing].sort((a, b) => (TASK_ORDER.indexOf(a.task) + 1 || 99) - (TASK_ORDER.indexOf(b.task) + 1 || 99)),
    [routing],
  );
  const offlineOnly = tasks.every((t) => t.candidates.every((c) => c.config.provider === "mock"));
  const byKey = useMemo(() => new Map(models.map((m) => [modelKey(m), m])), [models]);
  const pinnable = useMemo(() => sortProviders(models.filter((m) => m.enabled), (m) => m.provider), [models]);

  return (
    <div className="space-y-8 pb-16">
      <div className="flex max-w-3xl items-start gap-3 text-[14px] leading-relaxed text-muted">
        <RouteIcon className="mt-0.5 h-4 w-4 shrink-0 text-faint" />
        <p>
          Tasks are routed by capability; if a model fails or is rate-limited IdeaVault falls back to the next. Each task needs certain capabilities (tool calling, JSON output, context
          size); among the models that qualify, <em>fast</em> tasks favour speed and cost, <em>strong</em> tasks favour quality.
        </p>
      </div>

      {offlineOnly && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-[var(--radius-lg)] border border-warning/30 bg-warning-soft px-4 py-3 text-[13px]">
          <p className="text-muted">
            <span className="font-medium text-fg">Every task currently falls back to the offline planner</span> because no AI provider is active.
          </p>
          <Button size="sm" variant="secondary" onClick={onGoToProviders}>
            Add a provider
          </Button>
        </div>
      )}

      <section aria-labelledby="routing-prefs">
        <SectionTitle>
          <span id="routing-prefs">Preferences</span>
        </SectionTitle>
        <Panel className="grid gap-6 p-5 md:grid-cols-[1fr_260px]">
          <ProviderPreference draft={draft} setDraft={setDraft} />
          <div>
            <label htmlFor="max-cost" className="mb-1.5 block text-[13px] font-medium text-fg">
              Maximum relative cost
            </label>
            <Select id="max-cost" value={draft.maxCost} onChange={(e) => setDraft((d) => ({ ...d, maxCost: Number(e.target.value) }))}>
              <option value={0}>No limit</option>
              {[1, 2, 3, 4].map((v) => (
                <option key={v} value={v}>
                  Up to {v} · {RELATIVE_COST_LABEL[v]}
                </option>
              ))}
            </Select>
            <p className="mt-1.5 text-[12px] leading-snug text-faint">Models above this are skipped unless you pin them.</p>
          </div>
        </Panel>
      </section>

      <section aria-labelledby="routing-tasks">
        <SectionTitle>
          <span id="routing-tasks">Tasks</span>
        </SectionTitle>
        <ol className="divide-y divide-border rounded-[var(--radius-lg)] border border-border bg-surface">
          {tasks.map((t) => (
            <TaskRow
              key={t.task}
              t={t}
              pin={draft.pins[t.task] ?? ""}
              savedPin={saved.pins[t.task] ?? ""}
              pinnable={pinnable}
              pinned={byKey.get(draft.pins[t.task] ?? "")}
              onPin={(key) => setDraft((d) => ({ ...d, pins: { ...d.pins, [t.task]: key } }))}
            />
          ))}
        </ol>
      </section>

      <div
        className={cn(
          "sticky bottom-4 z-10 flex items-center justify-between gap-3 rounded-[var(--radius-lg)] border border-border bg-surface px-4 py-3 shadow-md transition-all",
          dirty ? "translate-y-0 opacity-100" : "pointer-events-none translate-y-2 opacity-0",
        )}
        aria-hidden={!dirty}
      >
        <span className="text-[13px] text-muted">Unsaved routing changes</span>
        <div className="flex gap-2">
          <Button size="sm" variant="ghost" onClick={() => setDraft(saved)} tabIndex={dirty ? 0 : -1}>
            <RotateCcw className="h-3.5 w-3.5" /> Reset
          </Button>
          <Button size="sm" variant="primary" loading={save.isPending} onClick={() => save.mutate(draft)} tabIndex={dirty ? 0 : -1}>
            Save routing
          </Button>
        </div>
      </div>
    </div>
  );
}

function ProviderPreference({ draft, setDraft }: { draft: RoutingDraft; setDraft: React.Dispatch<React.SetStateAction<RoutingDraft>> }) {
  const all = PROVIDER_ORDER.filter((p) => p !== "mock") as string[];
  const rest = all.filter((p) => !draft.prefer.includes(p));
  const ordered = [...draft.prefer, ...rest];
  return (
    <div>
      <p className="mb-1 text-[13px] font-medium text-fg">Preferred providers</p>
      <p className="mb-3 text-[12px] leading-snug text-faint">Ticked providers get a boost, strongest first. Others are still used when they fit better.</p>
      <ul className="space-y-1">
        {ordered.map((p) => {
          const idx = draft.prefer.indexOf(p);
          const on = idx !== -1;
          return (
            <li key={p} className={cn("flex items-center gap-3 rounded-[var(--radius-md)] px-2 py-1.5", on && "bg-surface-2")}>
              <Checkbox
                id={`pref-${p}`}
                checked={on}
                onCheckedChange={(v) => setDraft((d) => ({ ...d, prefer: v ? [...d.prefer, p] : d.prefer.filter((x) => x !== p) }))}
              />
              <label htmlFor={`pref-${p}`} className="flex flex-1 cursor-pointer items-center gap-2 text-[13px] text-fg">
                <span className="h-2 w-2 rounded-full" style={{ background: providerColor(p) }} aria-hidden />
                {providerLabel(p)}
                {on && <span className="text-[11px] tabular-nums text-faint">#{idx + 1}</span>}
              </label>
              {on && (
                <span className="flex">
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    disabled={idx === 0}
                    onClick={() => setDraft((d) => ({ ...d, prefer: move(d.prefer, idx, -1) }))}
                    aria-label={`Move ${providerLabel(p)} up`}
                  >
                    <ArrowUp className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    disabled={idx === draft.prefer.length - 1}
                    onClick={() => setDraft((d) => ({ ...d, prefer: move(d.prefer, idx, 1) }))}
                    aria-label={`Move ${providerLabel(p)} down`}
                  >
                    <ArrowDown className="h-3.5 w-3.5" />
                  </Button>
                </span>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function TaskRow({
  t,
  pin,
  savedPin,
  pinnable,
  pinned,
  onPin,
}: {
  t: RoutingPreview;
  pin: string;
  savedPin: string;
  pinnable: ModelConfig[];
  pinned?: ModelConfig;
  onPin: (key: string) => void;
}) {
  const spec = t.spec;
  const reqs = [spec.needs_tools && "tools", spec.needs_json && "json", spec.min_context > 0 && `≥ ${formatContext(spec.min_context)} context`].filter(Boolean) as string[];
  const changed = pin !== savedPin;
  const groups = useMemo(() => {
    const m = new Map<string, ModelConfig[]>();
    for (const x of pinnable) m.set(x.provider, [...(m.get(x.provider) ?? []), x]);
    return [...m.entries()];
  }, [pinnable]);
  return (
    <li className="grid gap-4 px-5 py-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)]">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="text-[14px] font-medium text-fg">{humanTask(t.task)}</h3>
          <Badge className={spec.tier === "fast" ? "border-k-branch/30 bg-k-branch-soft text-k-branch" : "border-k-decision/30 bg-k-decision-soft text-k-decision"}>{spec.tier}</Badge>
        </div>
        <p className="mt-0.5 text-[13px] leading-snug text-muted">{spec.description}</p>
        <div className="mt-2 flex flex-wrap items-center gap-1.5 text-[11px] text-faint">
          <code className="font-mono">{t.task}</code>
          {reqs.length > 0 && <span aria-hidden>·</span>}
          {reqs.map((r) => (
            <span key={r} className="rounded-[3px] border border-border px-1 py-px text-muted">
              needs {r}
            </span>
          ))}
        </div>
        <div className="mt-3 flex items-center gap-2">
          <Pin className={cn("h-3.5 w-3.5 shrink-0", pin ? "text-accent" : "text-faint")} aria-hidden />
          <Select
            aria-label={`Pin a model for ${humanTask(t.task)}`}
            value={pin}
            onChange={(e) => onPin(e.target.value)}
            className={cn("h-8 text-[13px]", changed && "border-accent")}
            wrapperClassName="max-w-[300px]"
          >
            <option value="">Automatic — best match</option>
            {groups.map(([provider, ms]) => (
              <optgroup key={provider} label={providerLabel(provider)}>
                {ms.map((m) => (
                  <option key={m.id} value={modelKey(m)}>
                    {m.display_name || m.model}
                    {m.available ? "" : " (provider off)"}
                  </option>
                ))}
              </optgroup>
            ))}
          </Select>
        </div>
        {pin && pinned && !pinned.available && (
          <p className="mt-1.5 flex items-start gap-1.5 text-[12px] text-warning">
            <Info className="mt-0.5 h-3 w-3 shrink-0" /> {providerLabel(pinned.provider)} isn&apos;t active, so this pin is skipped until it is.
          </p>
        )}
        {changed && <p className="mt-1.5 text-[12px] text-faint">Takes effect after you save.</p>}
      </div>
      <CandidateList candidates={t.candidates} />
    </li>
  );
}

function CandidateList({ candidates }: { candidates: Candidate[] }) {
  if (candidates.length === 0) {
    return <p className="self-center text-[13px] text-faint">No enabled model meets this task&apos;s requirements.</p>;
  }
  return (
    <ol className="space-y-1.5" aria-label="Ranked candidates">
      {candidates.map((c, i) => (
        <li
          key={`${c.config.provider}/${c.config.model}`}
          className={cn("grid grid-cols-[18px_minmax(0,1fr)_auto] items-start gap-2 rounded-[var(--radius-md)] px-2.5 py-1.5", i === 0 ? "bg-surface-2" : "")}
        >
          <span className={cn("pt-px text-[12px] tabular-nums", i === 0 ? "font-semibold text-fg" : "text-faint")}>{i + 1}</span>
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-1.5">
              <span className="h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: providerColor(c.config.provider) }} aria-hidden />
              <span className={cn("truncate text-[13px]", i === 0 ? "font-medium text-fg" : "text-muted")}>{c.config.display_name || c.config.model}</span>
              {i === 0 && <span className="shrink-0 rounded-[3px] bg-fg px-1 text-[10px] font-semibold uppercase tracking-wide text-bg">Selected</span>}
            </div>
            <p className="truncate text-[11.5px] text-faint" title={c.reason}>
              {c.reason}
            </p>
          </div>
          <span className="pt-px font-mono text-[11px] tabular-nums text-faint" title="Routing score">
            {c.score >= 1000 ? "pinned" : c.score.toFixed(0)}
          </span>
        </li>
      ))}
    </ol>
  );
}
