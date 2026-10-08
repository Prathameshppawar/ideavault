"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, ListPlus, Lock, Plus, Search, ShieldCheck, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { ModelConfig, ProviderStatus } from "@/lib/types";
import { cn, plural } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, Tooltip } from "@/components/ui/overlay";
import { EmptyState, ErrorState, Field, Input, Skeleton, Spinner } from "@/components/ui/primitives";
import { StatusPill } from "@/features/usage/controls";
import { LISTABLE_PROVIDERS, modelKey, providerColor } from "./model-meta";
import { modelKeys, useModels, useProviders, type AvailableModels } from "./queries";

const SOURCE_LABEL: Record<string, string> = { env: "Server environment", vault: "Encrypted vault", builtin: "Built in", none: "—" };

function useInvalidateModelData() {
  const qc = useQueryClient();
  return () => {
    for (const k of [modelKeys.providers, modelKeys.models, modelKeys.routing, ["system-status"]]) void qc.invalidateQueries({ queryKey: k });
  };
}

export function providerState(p: ProviderStatus): { tone: "success" | "warning" | "danger" | "muted"; label: string; detail?: string } {
  if (p.id === "mock") return { tone: "muted", label: "Built in", detail: "Always available as the last-resort fallback." };
  if (p.configured) return { tone: "success", label: "Active" };
  if (p.source === "env")
    return {
      tone: "warning",
      label: "Inactive",
      detail: "A key is set in the server environment, but this provider is not active (the server is running in offline mode, or the key could not be used).",
    };
  if (p.source === "vault") return { tone: "danger", label: "Inactive", detail: "A key is stored, but the provider could not be activated. Replace the key to re-verify it." };
  return { tone: "muted", label: "Not configured" };
}

export function ProvidersTab() {
  const { data, isLoading, error, refetch } = useProviders();
  if (isLoading) {
    return (
      <div className="divide-y divide-border rounded-[var(--radius-lg)] border border-border">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="space-y-2 p-5">
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-3 w-2/3" />
          </div>
        ))}
      </div>
    );
  }
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;
  const active = data.filter((p) => p.configured && p.id !== "mock");
  return (
    <div className="space-y-5">
      <p className="max-w-3xl text-[14px] leading-relaxed text-muted">
        {active.length === 0 ? (
          <>
            <span className="font-medium text-fg">No AI provider is active.</span> IdeaVault is using the offline rule-based planner, which never generates text with AI. Add a key below
            to turn on chat, extraction and artifacts.
          </>
        ) : (
          <>
            <span className="font-medium text-fg">{plural(active.length, "provider")} active</span> ({active.map((p) => p.name).join(", ")}). Tasks are routed across them by capability.
          </>
        )}{" "}
        Keys are verified with a real request, encrypted at rest, and never shown again — only a masked hint.
      </p>
      <ul className="divide-y divide-border overflow-hidden rounded-[var(--radius-lg)] border border-border bg-surface">
        {data.map((p) => (
          <ProviderRow key={p.id} p={p} />
        ))}
      </ul>
    </div>
  );
}

function ProviderRow({ p }: { p: ProviderStatus }) {
  const st = providerState(p);
  const [keyOpen, setKeyOpen] = useState(false);
  const [removeOpen, setRemoveOpen] = useState(false);
  const [availOpen, setAvailOpen] = useState(false);
  const isLocal = p.id === "ollama";
  const isMock = p.id === "mock";
  const hasStored = p.source !== "none" && !isMock;
  return (
    <li className="flex flex-col gap-4 px-5 py-4 sm:flex-row sm:items-start sm:justify-between">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: providerColor(p.id) }} aria-hidden />
          <h3 className="text-[15px] font-medium text-fg">{p.name}</h3>
          {st.detail ? (
            <Tooltip content={st.detail}>
              <span tabIndex={0} className="rounded-full">
                <StatusPill tone={st.tone}>{st.label}</StatusPill>
              </span>
            </Tooltip>
          ) : (
            <StatusPill tone={st.tone}>{st.label}</StatusPill>
          )}
        </div>
        <p className="mt-1 max-w-2xl text-[13px] leading-relaxed text-muted">{p.notes}</p>
        <dl className="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-[12px] text-faint">
          <div className="flex gap-1.5">
            <dt>Source</dt>
            <dd className="text-muted">{SOURCE_LABEL[p.source] ?? p.source}</dd>
          </div>
          {p.hint && (
            <div className="flex gap-1.5">
              <dt>Key</dt>
              <dd className={cn("text-muted", p.source === "vault" && "font-mono")}>{p.hint}</dd>
            </div>
          )}
          {p.base_url && (
            <div className="flex min-w-0 gap-1.5">
              <dt>Base URL</dt>
              <dd className="truncate font-mono text-muted">{p.base_url}</dd>
            </div>
          )}
          <div className="flex gap-1.5">
            <dt>Registry</dt>
            <dd className="text-muted">{plural(p.models, "model")}</dd>
          </div>
        </dl>
      </div>
      {!isMock && (
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {p.configured && LISTABLE_PROVIDERS.has(p.id) && (
            <Button size="sm" variant="ghost" onClick={() => setAvailOpen(true)}>
              <ListPlus className="h-3.5 w-3.5" /> Available models
            </Button>
          )}
          {p.source === "vault" && (
            <Button size="sm" variant="ghost" onClick={() => setRemoveOpen(true)} aria-label={`Remove stored ${isLocal ? "settings" : "key"} for ${p.name}`}>
              <Trash2 className="h-3.5 w-3.5" /> Remove stored {isLocal ? "URL" : "key"}
            </Button>
          )}
          <Button size="sm" variant="secondary" onClick={() => setKeyOpen(true)}>
            <KeyRound className="h-3.5 w-3.5" />
            {isLocal ? (hasStored ? "Change URL" : "Set base URL") : p.source === "vault" ? "Replace key" : p.source === "env" ? "Override key" : "Add key"}
          </Button>
        </div>
      )}
      <ProviderKeyDialog provider={p} open={keyOpen} onOpenChange={setKeyOpen} />
      <RemoveKeyDialog provider={p} open={removeOpen} onOpenChange={setRemoveOpen} />
      {availOpen && <AvailableModelsDialog provider={p} open={availOpen} onOpenChange={setAvailOpen} />}
    </li>
  );
}

/** Write-only key form: the key is sent once for verification and never displayed again. */
export function ProviderKeyDialog({ provider, open, onOpenChange }: { provider: ProviderStatus; open: boolean; onOpenChange: (v: boolean) => void }) {
  const isLocal = provider.id === "ollama";
  const allowsBaseUrl = isLocal || provider.id === "openai";
  const [apiKey, setApiKey] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const invalidate = useInvalidateModelData();
  const save = useMutation({
    mutationFn: () => api.put<ProviderStatus>(`/v1/models/providers/${provider.id}`, { api_key: apiKey.trim() || undefined, base_url: baseUrl.trim() || undefined }),
    onSuccess: () => {
      toast.success(`${provider.name} verified and saved`);
      invalidate();
      close(false);
    },
  });
  const close = (v: boolean) => {
    if (!v) {
      setApiKey("");
      setBaseUrl("");
      save.reset();
    }
    onOpenChange(v);
  };
  const canSubmit = isLocal ? baseUrl.trim() !== "" : apiKey.trim() !== "";
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent
        title={isLocal ? `Connect ${provider.name}` : `${provider.source === "vault" ? "Replace" : provider.source === "env" ? "Override" : "Add"} ${provider.name} key`}
        description={
          isLocal
            ? "Point IdeaVault at an OpenAI-compatible server (Ollama, LM Studio, vLLM…). IdeaVault checks that it responds before saving."
            : "IdeaVault makes a small verification request with this key before storing it encrypted. It is never shown again."
        }
      >
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (canSubmit) save.mutate();
          }}
        >
          {allowsBaseUrl && (
            <Field label={isLocal ? "Base URL" : "Base URL (optional)"} htmlFor="pk-base" hint={isLocal ? "e.g. http://localhost:11434/v1" : "Only for OpenAI-compatible proxies."}>
              <Input id="pk-base" type="url" inputMode="url" autoComplete="off" spellCheck={false} placeholder={isLocal ? "http://localhost:11434/v1" : "https://api.openai.com/v1"} value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />
            </Field>
          )}
          {(!isLocal || provider.key_required) && (
            <Field label="API key" htmlFor="pk-key" hint={
                provider.source === "vault" && provider.hint
                  ? `Replaces the stored key (${provider.hint}).`
                  : provider.source === "env"
                    ? "Stored in the vault, it takes precedence over the key set in the server environment."
                    : undefined
              }>
              <Input
                id="pk-key"
                type="password"
                autoComplete="off"
                spellCheck={false}
                data-1p-ignore
                className="font-mono"
                placeholder="Paste your key"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                autoFocus
              />
            </Field>
          )}
          {isLocal && (
            <Field label="API key (optional)" htmlFor="pk-key-local">
              <Input id="pk-key-local" type="password" autoComplete="off" spellCheck={false} className="font-mono" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
            </Field>
          )}
          {save.error && (
            <p role="alert" className="rounded-[var(--radius-md)] border border-danger/30 bg-danger-soft px-3 py-2 text-[13px] text-danger">
              {errorMessage(save.error)}
            </p>
          )}
          <div className="flex items-center justify-between gap-3 pt-1">
            <span className="inline-flex items-center gap-1.5 text-[12px] text-faint">
              <Lock className="h-3 w-3" /> Encrypted at rest · write-only
            </span>
            <div className="flex gap-2">
              <DialogClose asChild>
                <Button type="button" variant="ghost">
                  Cancel
                </Button>
              </DialogClose>
              <Button type="submit" variant="primary" loading={save.isPending} disabled={!canSubmit}>
                <ShieldCheck className="h-3.5 w-3.5" /> Verify & save
              </Button>
            </div>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RemoveKeyDialog({ provider, open, onOpenChange }: { provider: ProviderStatus; open: boolean; onOpenChange: (v: boolean) => void }) {
  const invalidate = useInvalidateModelData();
  const remove = useMutation({
    mutationFn: () => api.del(`/v1/models/providers/${provider.id}`),
    onSuccess: () => {
      toast.success(`Stored credentials for ${provider.name} removed`);
      invalidate();
      onOpenChange(false);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title={`Remove stored ${provider.name} credentials?`} description="The encrypted key and base URL are deleted from the vault. A key set in the server environment (if any) stays active.">
        <div className="flex justify-end gap-2">
          <DialogClose asChild>
            <Button variant="ghost">Cancel</Button>
          </DialogClose>
          <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
            Remove
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function AvailableModelsDialog({ provider, open, onOpenChange }: { provider: ProviderStatus; open: boolean; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient();
  const [filter, setFilter] = useState("");
  const { data: registry } = useModels();
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: modelKeys.available(provider.id),
    queryFn: () => api.get<AvailableModels>(`/v1/models/providers/${provider.id}/available`),
    enabled: open,
    staleTime: 60_000,
  });
  const inRegistry = useMemo(() => new Set((registry ?? []).map(modelKey)), [registry]);
  const add = useMutation({
    mutationFn: (model: string) =>
      api.post<ModelConfig>("/v1/models", {
        provider: provider.id,
        model,
        display_name: model,
        capabilities: ["chat"],
        context_length: 32000,
        speed: 3,
        quality: 3,
        relative_cost: 2,
        enabled: true,
      } satisfies Partial<ModelConfig>),
    onSuccess: (m) => {
      toast.success(`${m.model} added to the registry`, { description: "Set its capabilities and prices in the Registry tab." });
      void qc.invalidateQueries({ queryKey: modelKeys.models });
      void qc.invalidateQueries({ queryKey: modelKeys.routing });
      void qc.invalidateQueries({ queryKey: modelKeys.providers });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const ids = (data?.models ?? []).filter((m) => m.toLowerCase().includes(filter.trim().toLowerCase()));
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title={`Models offered by ${provider.name}`} description="Listed live from the provider's API. Added models start with neutral ratings — edit them in the Registry." wide>
        <div className="relative mb-3">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" />
          <Input className="pl-8" placeholder="Filter model ids" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter model ids" />
        </div>
        {isLoading ? (
          <div className="flex items-center gap-2 py-8 text-[13px] text-muted">
            <Spinner /> Asking {provider.name} for its model list…
          </div>
        ) : error ? (
          <ErrorState error={error} onRetry={() => void refetch()} />
        ) : ids.length === 0 ? (
          <EmptyState title={filter ? "No models match" : "The provider returned no models"} />
        ) : (
          <ul className="max-h-[50vh] divide-y divide-border overflow-y-auto rounded-[var(--radius-md)] border border-border">
            {ids.map((id) => {
              const have = inRegistry.has(`${provider.id}/${id}`);
              return (
                <li key={id} className="flex items-center justify-between gap-3 px-3 py-2">
                  <code className="min-w-0 truncate font-mono text-[12.5px] text-fg">{id}</code>
                  {have ? (
                    <span className="text-[12px] text-faint">In registry</span>
                  ) : (
                    <Button size="xs" variant="secondary" onClick={() => add.mutate(id)} loading={add.isPending && add.variables === id} aria-label={`Add ${id} to registry`}>
                      <Plus className="h-3 w-3" /> Add to registry
                    </Button>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}
