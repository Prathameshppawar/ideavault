"use client";

import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { ModelConfig } from "@/lib/types";
import { cn, plural } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Checkbox, Dialog, DialogClose, DialogContent, Switch, Tooltip } from "@/components/ui/overlay";
import { EmptyState, ErrorState, Field, Input, Skeleton } from "@/components/ui/primitives";
import { Select } from "@/features/usage/controls";
import { Meter } from "./meter";
import {
  CAPABILITIES,
  CAPABILITY_HINT,
  PROVIDER_ORDER,
  RELATIVE_COST_LABEL,
  capabilitiesOf,
  formatContext,
  perMTok,
  providerColor,
  providerLabel,
  sortProviders,
  type Capability,
} from "./model-meta";
import { modelKeys, useModels, useProviders } from "./queries";

export function groupByProvider(models: ModelConfig[]): { provider: string; models: ModelConfig[] }[] {
  const map = new Map<string, ModelConfig[]>();
  for (const m of models) map.set(m.provider, [...(map.get(m.provider) ?? []), m]);
  return sortProviders([...map.entries()], ([p]) => p).map(([provider, ms]) => ({
    provider,
    models: [...ms].sort((a, b) => b.quality - a.quality || a.display_name.localeCompare(b.display_name)),
  }));
}

export function RegistryTab() {
  const { data, isLoading, error, refetch } = useModels();
  const { data: providers } = useProviders();
  const [editing, setEditing] = useState<ModelConfig | "new" | null>(null);
  const [deleting, setDeleting] = useState<ModelConfig | null>(null);
  const groups = useMemo(() => groupByProvider(data ?? []), [data]);
  if (isLoading) return <TableSkeleton />;
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;
  const available = data.filter((m) => m.available).length;
  const providerName = (id: string) => providers?.find((p) => p.id === id)?.name ?? providerLabel(id);
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <p className="max-w-2xl text-[14px] leading-relaxed text-muted">
          {plural(data.length, "model")} in the registry, <span className="text-fg">{available} available</span> right now. Ratings and prices drive routing and cost estimates —
          prices change, so edit them when providers do.
        </p>
        <Button size="sm" variant="primary" onClick={() => setEditing("new")}>
          <Plus className="h-3.5 w-3.5" /> Add custom model
        </Button>
      </div>
      {data.length === 0 ? (
        <EmptyState title="The registry is empty" description="Add a model, or add a provider key and import its models." />
      ) : (
        <div className="relative overflow-x-auto rounded-[var(--radius-lg)] border border-border bg-surface">
          <table className="w-full min-w-[920px] border-collapse text-[13px]">
            <thead>
              <tr className="border-b border-border text-left text-[11px] font-medium uppercase tracking-wide text-faint">
                <th className="px-4 py-2.5 font-medium">Model</th>
                <th className="px-3 py-2.5 font-medium">Capabilities</th>
                <th className="px-3 py-2.5 text-right font-medium">Context</th>
                <th className="px-3 py-2.5 font-medium">Speed</th>
                <th className="px-3 py-2.5 font-medium">Quality</th>
                <th className="px-3 py-2.5 font-medium">Cost</th>
                <th className="px-3 py-2.5 text-right font-medium">
                  <Tooltip content="USD per million input / output tokens">
                    <span tabIndex={0}>$ in / out</span>
                  </Tooltip>
                </th>
                <th className="px-3 py-2.5 font-medium">Status</th>
                <th className="px-3 py-2.5 font-medium">
                  <span className="sr-only">Enabled</span>
                </th>
                <th className="px-3 py-2.5">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            {groups.map((g) => (
              <tbody key={g.provider}>
                <tr className="bg-surface-2/60">
                  <th colSpan={10} scope="colgroup" className="border-b border-border px-4 py-1.5 text-left">
                    <span className="inline-flex items-center gap-2 text-[12px] font-semibold text-fg">
                      <span className="h-2 w-2 rounded-full" style={{ background: providerColor(g.provider) }} aria-hidden />
                      {providerName(g.provider)}
                      <span className="font-normal text-faint">· {plural(g.models.length, "model")}</span>
                    </span>
                  </th>
                </tr>
                {g.models.map((m) => (
                  <ModelRow key={m.id} m={m} onEdit={() => setEditing(m)} onDelete={() => setDeleting(m)} />
                ))}
              </tbody>
            ))}
          </table>
        </div>
      )}
      {editing && <ModelDialog model={editing === "new" ? null : editing} onClose={() => setEditing(null)} />}
      {deleting && <DeleteModelDialog model={deleting} onClose={() => setDeleting(null)} />}
    </div>
  );
}

function ModelRow({ m, onEdit, onDelete }: { m: ModelConfig; onEdit: () => void; onDelete: () => void }) {
  const qc = useQueryClient();
  const toggle = useMutation({
    mutationFn: (enabled: boolean) => api.patch(`/v1/models/${m.id}`, { enabled }),
    onMutate: async (enabled) => {
      await qc.cancelQueries({ queryKey: modelKeys.models });
      const prev = qc.getQueryData<ModelConfig[]>(modelKeys.models);
      qc.setQueryData<ModelConfig[]>(modelKeys.models, (old) => old?.map((x) => (x.id === m.id ? { ...x, enabled } : x)));
      return { prev };
    },
    onError: (e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(modelKeys.models, ctx.prev);
      toast.error(errorMessage(e));
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: modelKeys.models });
      void qc.invalidateQueries({ queryKey: modelKeys.routing });
    },
  });
  const caps = capabilitiesOf(m);
  return (
    <tr className={cn("border-b border-border last:border-b-0 hover:bg-surface-2/40", !m.enabled && "text-muted")}>
      <td className="px-4 py-2.5 align-top">
        <div className={cn("font-medium", m.enabled ? "text-fg" : "text-muted")}>{m.display_name || m.model}</div>
        <code className="font-mono text-[11.5px] text-faint">{m.model}</code>
        {!m.is_builtin && <span className="ml-1.5 text-[11px] text-faint">· custom</span>}
      </td>
      <td className="px-3 py-2.5 align-top">
        <div className="flex max-w-[220px] flex-wrap gap-1">
          {caps.length === 0 && <span className="text-faint">—</span>}
          {caps.map((c) => (
            <CapabilityChip key={c} cap={c} />
          ))}
        </div>
      </td>
      <td className="px-3 py-2.5 text-right align-top tabular-nums text-muted">{formatContext(m.context_length)}</td>
      <td className="px-3 py-2.5 align-top">
        <Meter value={m.speed} label="Speed" />
      </td>
      <td className="px-3 py-2.5 align-top">
        <Meter value={m.quality} label="Quality" />
      </td>
      <td className="px-3 py-2.5 align-top">
        <span title={RELATIVE_COST_LABEL[m.relative_cost] ?? ""}>
          <Meter value={m.relative_cost} label="Relative cost" tone="warning" />
        </span>
      </td>
      <td className="whitespace-nowrap px-3 py-2.5 text-right align-top font-mono text-[12px] tabular-nums text-muted">
        {m.input_cost_per_mtok === 0 && m.output_cost_per_mtok === 0 ? "free" : `${perMTok(m.input_cost_per_mtok)} / ${perMTok(m.output_cost_per_mtok)}`}
      </td>
      <td className="whitespace-nowrap px-3 py-2.5 align-top">
        <span className="inline-flex items-center gap-1.5 text-[12px]">
          <span className={cn("h-1.5 w-1.5 rounded-full", m.available ? "bg-success" : "bg-faint/60")} aria-hidden />
          <span className={m.available ? "text-fg" : "text-faint"}>{m.available ? "Available" : m.enabled ? "Provider off" : "Disabled"}</span>
        </span>
      </td>
      <td className="px-3 py-2.5 align-top">
        <Switch checked={m.enabled} onCheckedChange={(v) => toggle.mutate(v)} aria-label={`${m.enabled ? "Disable" : "Enable"} ${m.display_name || m.model}`} />
      </td>
      <td className="whitespace-nowrap px-3 py-2 text-right align-top">
        <Button size="icon-sm" variant="ghost" onClick={onEdit} aria-label={`Edit ${m.display_name || m.model}`}>
          <Pencil className="h-3.5 w-3.5" />
        </Button>
        {m.is_builtin ? (
          <Tooltip content="Built-in models can be disabled, not deleted">
            <span tabIndex={0} className="inline-flex h-7 w-7 items-center justify-center text-faint/50" aria-label="Built-in model cannot be deleted">
              <Trash2 className="h-3.5 w-3.5" />
            </span>
          </Tooltip>
        ) : (
          <Button size="icon-sm" variant="ghost" onClick={onDelete} aria-label={`Delete ${m.display_name || m.model}`}>
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        )}
      </td>
    </tr>
  );
}

export function CapabilityChip({ cap }: { cap: Capability }) {
  return (
    <span title={CAPABILITY_HINT[cap]} className="inline-flex h-[18px] items-center rounded-[3px] border border-border px-1 text-[10.5px] font-medium text-muted">
      {cap}
    </span>
  );
}

function TableSkeleton() {
  return (
    <div className="space-y-2 rounded-[var(--radius-lg)] border border-border p-4">
      {Array.from({ length: 8 }).map((_, i) => (
        <Skeleton key={i} className="h-8 w-full" />
      ))}
    </div>
  );
}

interface Draft {
  provider: string;
  model: string;
  display_name: string;
  context_length: string;
  caps: Set<Capability>;
  speed: number;
  quality: number;
  relative_cost: number;
  input_cost_per_mtok: string;
  output_cost_per_mtok: string;
  enabled: boolean;
  /** Declared capabilities the dialog doesn't edit (e.g. "offline", custom tags) — preserved as-is. */
  extra: string[];
}

function draftFrom(m: ModelConfig | null): Draft {
  return {
    provider: m?.provider ?? "groq",
    model: m?.model ?? "",
    display_name: m?.display_name ?? "",
    context_length: String(m?.context_length ?? 32000),
    caps: new Set(m ? capabilitiesOf(m) : []),
    speed: m?.speed ?? 3,
    quality: m?.quality ?? 3,
    relative_cost: m?.relative_cost ?? 2,
    input_cost_per_mtok: String(m?.input_cost_per_mtok ?? 0),
    output_cost_per_mtok: String(m?.output_cost_per_mtok ?? 0),
    enabled: m?.enabled ?? true,
    extra: (m?.capabilities ?? []).filter((c) => c !== "chat" && !(EDITABLE_CAPS as readonly string[]).includes(c)),
  };
}

const EDITABLE_CAPS = CAPABILITIES.filter((c) => c !== "offline");

/** Request body for POST /v1/models (upsert by provider + model). */
export function modelPayload(d: Draft): Partial<ModelConfig> {
  const caps = [...new Set(["chat", ...d.caps, ...d.extra])];
  return {
    provider: d.provider.trim().toLowerCase(),
    model: d.model.trim(),
    display_name: d.display_name.trim() || d.model.trim(),
    context_length: Math.max(0, parseInt(d.context_length, 10) || 0),
    capabilities: caps,
    tool_calling: d.caps.has("tools"),
    structured_output: d.caps.has("json"),
    vision: d.caps.has("vision"),
    reasoning: d.caps.has("reasoning"),
    speed: d.speed,
    quality: d.quality,
    relative_cost: d.relative_cost,
    input_cost_per_mtok: Math.max(0, parseFloat(d.input_cost_per_mtok) || 0),
    output_cost_per_mtok: Math.max(0, parseFloat(d.output_cost_per_mtok) || 0),
    enabled: d.enabled,
  };
}

function ModelDialog({ model, onClose }: { model: ModelConfig | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [d, setD] = useState<Draft>(() => draftFrom(model));
  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setD((s) => ({ ...s, [k]: v }));
  const save = useMutation({
    mutationFn: () => api.post<ModelConfig>("/v1/models", modelPayload(d)),
    onSuccess: (m) => {
      toast.success(model ? `${m.display_name} updated` : `${m.display_name} added`);
      void qc.invalidateQueries({ queryKey: modelKeys.models });
      void qc.invalidateQueries({ queryKey: modelKeys.routing });
      void qc.invalidateQueries({ queryKey: modelKeys.providers });
      onClose();
    },
  });
  const rating = (k: "speed" | "quality" | "relative_cost", label: string, min: number) => (
    <Field label={label} htmlFor={`md-${k}`}>
      <Select id={`md-${k}`} value={d[k]} onChange={(e) => set(k, Number(e.target.value))}>
        {Array.from({ length: 6 - min }).map((_, i) => {
          const v = i + min;
          return (
            <option key={v} value={v}>
              {k === "relative_cost" ? `${v} · ${RELATIVE_COST_LABEL[v]}` : v}
            </option>
          );
        })}
      </Select>
    </Field>
  );
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent
        wide
        title={model ? `Edit ${model.display_name || model.model}` : "Add custom model"}
        description={model ? "Changes apply to routing immediately." : "Any model your provider serves. It is routed only when its provider is active and its capabilities match the task."}
      >
        <form
          className="space-y-5"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Provider" htmlFor="md-provider">
              <Select id="md-provider" value={d.provider} onChange={(e) => set("provider", e.target.value)} disabled={!!model}>
                {PROVIDER_ORDER.filter((p) => p !== "mock").map((p) => (
                  <option key={p} value={p}>
                    {providerLabel(p)}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Model id" htmlFor="md-model" hint="Exactly as the provider's API expects it.">
              <Input id="md-model" className="font-mono" required value={d.model} onChange={(e) => set("model", e.target.value)} disabled={!!model} placeholder="e.g. llama-3.3-70b-versatile" spellCheck={false} />
            </Field>
            <Field label="Display name" htmlFor="md-name">
              <Input id="md-name" value={d.display_name} onChange={(e) => set("display_name", e.target.value)} placeholder={d.model || "Shown in pickers"} />
            </Field>
            <Field label="Context length (tokens)" htmlFor="md-ctx">
              <Input id="md-ctx" type="number" min={1000} step={1000} value={d.context_length} onChange={(e) => set("context_length", e.target.value)} />
            </Field>
          </div>
          <fieldset>
            <legend className="mb-2 text-[13px] font-medium text-muted">Capabilities</legend>
            <div className="flex flex-wrap gap-x-5 gap-y-2">
              {EDITABLE_CAPS.map((c) => (
                <label key={c} className="inline-flex cursor-pointer items-center gap-2 text-[13px] text-fg" title={CAPABILITY_HINT[c]}>
                  <Checkbox
                    checked={d.caps.has(c)}
                    onCheckedChange={(v) =>
                      setD((s) => {
                        const caps = new Set(s.caps);
                        if (v) caps.add(c);
                        else caps.delete(c);
                        return { ...s, caps };
                      })
                    }
                  />
                  {c}
                </label>
              ))}
            </div>
          </fieldset>
          <div className="grid grid-cols-3 gap-4">
            {rating("speed", "Speed", 1)}
            {rating("quality", "Quality", 1)}
            {rating("relative_cost", "Relative cost", 0)}
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Field label="$ per 1M input tokens" htmlFor="md-in">
              <Input id="md-in" type="number" min={0} step="0.001" value={d.input_cost_per_mtok} onChange={(e) => set("input_cost_per_mtok", e.target.value)} />
            </Field>
            <Field label="$ per 1M output tokens" htmlFor="md-out">
              <Input id="md-out" type="number" min={0} step="0.001" value={d.output_cost_per_mtok} onChange={(e) => set("output_cost_per_mtok", e.target.value)} />
            </Field>
          </div>
          {save.error && (
            <p role="alert" className="rounded-[var(--radius-md)] border border-danger/30 bg-danger-soft px-3 py-2 text-[13px] text-danger">
              {errorMessage(save.error)}
            </p>
          )}
          <div className="flex items-center justify-between gap-3">
            <label className="inline-flex items-center gap-2 text-[13px] text-fg">
              <Switch checked={d.enabled} onCheckedChange={(v) => set("enabled", v)} aria-label="Enabled" /> Enabled
            </label>
            <div className="flex gap-2">
              <DialogClose asChild>
                <Button type="button" variant="ghost">
                  Cancel
                </Button>
              </DialogClose>
              <Button type="submit" variant="primary" loading={save.isPending} disabled={!d.model.trim()}>
                {model ? "Save changes" : "Add model"}
              </Button>
            </div>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DeleteModelDialog({ model, onClose }: { model: ModelConfig; onClose: () => void }) {
  const qc = useQueryClient();
  const del = useMutation({
    mutationFn: () => api.del(`/v1/models/${model.id}`, { headers: { "X-Confirm": "delete" } }),
    onSuccess: () => {
      toast.success(`${model.display_name || model.model} removed from the registry`);
      void qc.invalidateQueries({ queryKey: modelKeys.models });
      void qc.invalidateQueries({ queryKey: modelKeys.routing });
      void qc.invalidateQueries({ queryKey: modelKeys.providers });
      onClose();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent title={`Delete ${model.display_name || model.model}?`} description="It will no longer be routed or offered in the Model Lab. Past usage records are kept.">
        <div className="flex justify-end gap-2">
          <DialogClose asChild>
            <Button variant="ghost">Cancel</Button>
          </DialogClose>
          <Button variant="danger" loading={del.isPending} onClick={() => del.mutate()}>
            Delete model
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
