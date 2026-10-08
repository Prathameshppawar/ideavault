"use client";

import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { components } from "@/types/api.generated";
import { cn, humanize } from "@/lib/format";
import { tone } from "@/lib/entities";
import { Button } from "@/components/ui/button";
import { ErrorState, SkeletonLines } from "@/components/ui/primitives";
import { KV, StatusPill } from "@/features/usage/controls";
import { providerColor } from "@/features/models/model-meta";

type SystemStatus = components["schemas"]["HttpsystemStatus"];

const HEALTHY = /^(up|ok|ready|healthy)$/i;

export function healthTone(v: unknown): "success" | "warning" | "danger" {
  if (v === true) return "success";
  if (v === false) return "danger";
  const s = String(v ?? "");
  if (HEALTHY.test(s)) return "success";
  if (/down|error|fail|unreachable/i.test(s)) return "danger";
  return "warning";
}

export function SystemSection() {
  const qc = useQueryClient();
  // Own key (not the shell's ["system-status"]): data the shell already fetched must not be
  // present while this Suspense boundary hydrates, or server and client markup diverge.
  const { data, isLoading, error, refetch } = useQuery({ queryKey: ["system-status", "detail"], queryFn: () => api.get<SystemStatus>("/v1/system/status"), staleTime: 15_000 });
  const backfill = useMutation({
    mutationFn: () => api.post<{ queued: boolean }>("/v1/embeddings/backfill"),
    onSuccess: (r) => {
      toast.success(r.queued ? "Embedding rebuild queued" : "A rebuild is already queued", { description: "Coverage updates as the background job runs." });
      setTimeout(() => void qc.invalidateQueries({ queryKey: ["system-status"] }), 4000);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  if (isLoading) return <SkeletonLines lines={8} />;
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;

  const health = Object.entries(data.health ?? {}).filter(([k]) => k !== "ok");
  const coverage = Object.entries(data.embedding_coverage ?? {}).sort((a, b) => b[1] - a[1]);
  const maxCov = Math.max(1, ...coverage.map(([, n]) => n));
  const jobs = Object.entries(data.jobs ?? {}).sort((a, b) => b[1] - a[1]);
  const embedder = data.embedder ?? {};
  const providers = (data.providers ?? []).filter((p) => p.id !== "mock");
  const active = providers.filter((p) => p.configured);

  return (
    <div className="space-y-8">
      <p className="text-[14px] leading-relaxed text-muted">
        IdeaVault <span className="font-mono text-[13px] text-fg">{data.version}</span> running in <span className="text-fg">{data.env || "unknown"}</span>.{" "}
        {data.offline_mode ? (
          <span className="text-warning">
            Offline mode — no AI provider can serve the agent, so the rule-based planner answers.{" "}
            <Link href="/models" className="underline underline-offset-2">
              Add a provider
            </Link>
          </span>
        ) : (
          <>
            AI is served by {active.map((p) => p.name).join(", ") || "configured providers"}.
          </>
        )}
      </p>

      <div className="grid gap-8 md:grid-cols-2">
        <div>
          <h3 className="mb-1 text-[13px] font-medium text-fg">Health</h3>
          <dl>
            {health.map(([k, v]) => (
              <KV key={k} label={k === "ai" ? "AI" : humanize(k)}>
                <StatusPill tone={healthTone(v)}>{String(v)}</StatusPill>
              </KV>
            ))}
          </dl>
        </div>
        <div>
          <h3 className="mb-1 text-[13px] font-medium text-fg">AI providers</h3>
          <dl>
            {providers.map((p) => (
              <KV key={p.id} label={p.name}>
                <span className="inline-flex items-center gap-1.5">
                  <span className="h-1.5 w-1.5 rounded-full" style={{ background: p.configured ? providerColor(p.id) : "var(--border-strong)" }} aria-hidden />
                  <span className={p.configured ? "text-fg" : "text-faint"}>{p.configured ? "Active" : p.source === "env" ? "Key set, inactive" : "Not configured"}</span>
                </span>
              </KV>
            ))}
          </dl>
        </div>
      </div>

      <div className="grid gap-8 md:grid-cols-2">
        <div>
          <div className="mb-1 flex items-center justify-between gap-3">
            <h3 className="text-[13px] font-medium text-fg">Embeddings</h3>
            <Button size="xs" variant="ghost" loading={backfill.isPending} onClick={() => backfill.mutate()}>
              {!backfill.isPending && <RefreshCw className="h-3 w-3" />} Rebuild embeddings
            </Button>
          </div>
          <dl>
            <KV label="Provider">{String(embedder.provider ?? "—")}</KV>
            <KV label="Model" mono>
              {String(embedder.model ?? "—")}
            </KV>
            {embedder.dims != null && <KV label="Dimensions">{String(embedder.dims)}</KV>}
          </dl>
          {embedder.provider === "local" && (
            <p className="mt-2 text-[12px] leading-relaxed text-faint">The local embedder is a deterministic hash model — search works offline, but semantic matching is approximate.</p>
          )}
        </div>
        <div>
          <h3 className="mb-2 text-[13px] font-medium text-fg">Embedded items by type</h3>
          {coverage.length === 0 ? (
            <p className="text-[13px] text-faint">Nothing embedded yet.</p>
          ) : (
            <ul className="space-y-1.5">
              {coverage.map(([type, n]) => (
                <li key={type} className="grid grid-cols-[92px_minmax(0,1fr)_40px] items-center gap-3 text-[12.5px]">
                  <span className="truncate text-muted">{humanize(type)}</span>
                  <span className="h-1.5 overflow-hidden rounded-full bg-surface-3">
                    <span className="block h-full rounded-full" style={{ width: `${(n / maxCov) * 100}%`, background: tone(type) }} />
                  </span>
                  <span className="text-right tabular-nums text-fg">{n}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      <div>
        <h3 className="mb-2 text-[13px] font-medium text-fg">Background jobs</h3>
        {jobs.length === 0 ? (
          <p className="text-[13px] text-faint">The job queue is empty.</p>
        ) : (
          <p className="flex flex-wrap gap-x-5 gap-y-1 text-[13px]">
            {jobs.map(([status, n]) => (
              <span key={status} className="inline-flex items-baseline gap-1.5">
                <span className={cn("tabular-nums", /FAIL|DEAD/.test(status) ? "text-danger" : "text-fg")}>{n.toLocaleString("en")}</span>
                <span className="text-muted">{humanize(status).toLowerCase()}</span>
              </span>
            ))}
          </p>
        )}
      </div>
    </div>
  );
}
