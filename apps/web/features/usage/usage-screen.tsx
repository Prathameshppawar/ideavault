"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useQueries, useQuery } from "@tanstack/react-query";
import { BarChart3, CircleCheck, CircleX, Plug, X } from "lucide-react";
import { api } from "@/lib/api";
import type { ConnectorEvent, UsageEvent, UsageGroup, UsageSummary } from "@/lib/types";
import type { components } from "@/types/api.generated";
import { cn, compactNumber, fullDate, humanize, shortDate, timeAgo } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Tooltip } from "@/components/ui/overlay";
import { EmptyState, ErrorState, PageHeader, SectionTitle, Skeleton } from "@/components/ui/primitives";
import { humanTask, providerColor, providerLabel, sortProviders, splitModelKey } from "@/features/models/model-meta";
import { Segmented, Select } from "./controls";
import { ConnectorEventList } from "./connector-events";
import { connectorName } from "@/features/connectors/connector-meta";
import { ChartFrame, CostPerDayChart, LatencyChart, Legend, TaskCallsChart, TokensPerDayChart } from "./usage-charts";
import { buildDailySeries, dayKeys, estimatedShare, formatMs, formatUsd, headlineParts, isEmptySeries, latencyRows, taskRows } from "./usage-data";

type ConnectorUsageRow = components["schemas"]["PostgresConnectorUsageRow"];

const RANGES = ["7", "30", "90"] as const;

interface Filters {
  days: string;
  provider: string;
  model: string; // "provider/model"
  task: string;
}

function usageQuery(f: Filters, providerOverride?: string) {
  const m = f.model ? splitModelKey(f.model) : null;
  return {
    days: f.days,
    provider: providerOverride ?? (m?.provider || f.provider || undefined),
    model: m?.model || undefined,
    task: f.task || undefined,
  };
}

export function UsageScreen() {
  const params = useSearchParams();
  const router = useRouter();
  const filters: Filters = {
    days: (RANGES as readonly string[]).includes(params.get("days") ?? "") ? params.get("days")! : "30",
    provider: params.get("provider") ?? "",
    model: params.get("model") ?? "",
    task: params.get("task") ?? "",
  };
  const setFilters = (patch: Partial<Filters>) => {
    const next = { ...filters, ...patch };
    const p = new URLSearchParams();
    for (const [k, v] of Object.entries(next)) if (v && !(k === "days" && v === "30")) p.set(k, v);
    const s = p.toString();
    router.replace(s ? `?${s}` : "?", { scroll: false });
  };

  const q = useQuery({
    queryKey: ["usage", filters],
    queryFn: () => api.get<UsageSummary>("/v1/usage", { query: usageQuery(filters) }),
    placeholderData: (prev) => prev,
  });
  const data = q.data;
  const filtered = !!(filters.provider || filters.model || filters.task);

  return (
    <div className="mx-auto max-w-6xl px-5 py-8 sm:px-8 sm:py-10">
      <PageHeader eyebrow="Platform" title="Usage" description="What IdeaVault spent on your behalf: model calls, tokens, estimated cost and latency — plus connector activity." />

      <div className="mb-6 flex flex-wrap items-center gap-2">
        <Segmented
          label="Time range"
          value={filters.days as (typeof RANGES)[number]}
          onChange={(v) => setFilters({ days: v })}
          options={RANGES.map((r) => ({ value: r, label: `${r} days` }))}
        />
        <FilterSelects data={data} filters={filters} setFilters={setFilters} />
        {filtered && (
          <Button size="sm" variant="ghost" onClick={() => setFilters({ provider: "", model: "", task: "" })}>
            <X className="h-3.5 w-3.5" /> Clear filters
          </Button>
        )}
        {q.isFetching && !q.isLoading && <span className="text-[12px] text-faint">Updating…</span>}
      </div>

      {q.isLoading ? (
        <UsageSkeleton />
      ) : q.error || !data ? (
        <ErrorState error={q.error} onRetry={() => void q.refetch()} />
      ) : (
        <UsageBody data={data} filters={filters} setFilters={setFilters} />
      )}
    </div>
  );
}

function FilterSelects({ data, filters, setFilters }: { data?: UsageSummary; filters: Filters; setFilters: (p: Partial<Filters>) => void }) {
  // Options come from the data in range; keep the active value selectable even when it has no rows.
  const providers = useMemo(() => {
    const s = new Set((data?.by_provider ?? []).map((g) => g.key));
    if (filters.provider) s.add(filters.provider);
    return sortProviders([...s], (x) => x);
  }, [data, filters.provider]);
  const models = useMemo(() => {
    const s = new Set((data?.by_model ?? []).map((g) => g.key));
    if (filters.model) s.add(filters.model);
    return [...s].sort();
  }, [data, filters.model]);
  const tasks = useMemo(() => {
    const s = new Set((data?.by_task ?? []).map((g) => g.key));
    if (filters.task) s.add(filters.task);
    return [...s].sort();
  }, [data, filters.task]);
  return (
    <>
      <Select
        aria-label="Filter by provider"
        value={filters.model ? splitModelKey(filters.model).provider : filters.provider}
        onChange={(e) => setFilters({ provider: e.target.value, model: "" })}
        wrapperClassName="w-auto"
        className="h-8 min-w-[140px] text-[13px]"
      >
        <option value="">All providers</option>
        {providers.map((p) => (
          <option key={p} value={p}>
            {providerLabel(p)}
          </option>
        ))}
      </Select>
      <Select aria-label="Filter by model" value={filters.model} onChange={(e) => setFilters({ model: e.target.value, provider: "" })} wrapperClassName="w-auto" className="h-8 min-w-[160px] max-w-[260px] text-[13px]">
        <option value="">All models</option>
        {models.map((m) => (
          <option key={m} value={m}>
            {m}
          </option>
        ))}
      </Select>
      <Select aria-label="Filter by task" value={filters.task} onChange={(e) => setFilters({ task: e.target.value })} wrapperClassName="w-auto" className="h-8 min-w-[130px] text-[13px]">
        <option value="">All tasks</option>
        {tasks.map((t) => (
          <option key={t} value={t}>
            {humanTask(t)}
          </option>
        ))}
      </Select>
    </>
  );
}

function UsageBody({ data, filters, setFilters }: { data: UsageSummary; filters: Filters; setFilters: (p: Partial<Filters>) => void }) {
  const t = data.totals;
  const keys = useMemo(() => dayKeys(data.since), [data.since]);
  const providers = useMemo(() => sortProviders((data.by_provider ?? []).filter((g) => g.calls > 0).map((g) => g.key), (x) => x), [data.by_provider]);

  // by_day isn't split by provider, so fetch each provider's daily series (same filters) for the stacked chart.
  const singleProvider = providers.length <= 1;
  const perProvider = useQueries({
    queries: (singleProvider ? [] : providers).map((p) => ({
      queryKey: ["usage", { ...filters, provider: p, model: filters.model }],
      queryFn: () => api.get<UsageSummary>("/v1/usage", { query: usageQuery(filters, p) }),
      staleTime: 30_000,
    })),
  });
  const tokenRows = useMemo(() => {
    if (singleProvider) return buildDailySeries(keys, { [providers[0] ?? "total"]: data.by_day ?? [] }, (g) => g.total_tokens);
    const by: Record<string, UsageGroup[] | undefined> = {};
    providers.forEach((p, i) => (by[p] = perProvider[i]?.data?.by_day));
    return buildDailySeries(keys, by, (g) => g.total_tokens);
  }, [singleProvider, keys, providers, data.by_day, perProvider]);
  const seriesKeys = singleProvider ? [providers[0] ?? "total"] : providers;
  const perProviderLoading = perProvider.some((x) => x.isLoading);
  const costRows = useMemo(() => buildDailySeries(keys, { cost: data.by_day ?? [] }, (g) => g.cost_usd), [keys, data.by_day]);
  const latency = useMemo(() => latencyRows(data.by_model ?? []), [data.by_model]);
  const tasks = useMemo(() => taskRows(data.by_task ?? []), [data.by_task]);

  const estShare = estimatedShare(t);
  if (t.calls === 0 && (data.connectors ?? []).length === 0) {
    return (
      <EmptyState
        icon={BarChart3}
        title={`No model calls in the last ${filters.days} days`}
        description={
          filters.provider || filters.model || filters.task
            ? "Nothing matches these filters. Clear them to see all usage."
            : "Usage appears here as soon as IdeaVault calls a model — chatting, extracting knowledge, generating artifacts or running the Model Lab."
        }
      />
    );
  }

  return (
    <div className="space-y-12 animate-fade-in">
      <section aria-label="Summary">
        <p className="text-[clamp(1.25rem,2.4vw,1.6rem)] leading-snug text-fg">
          {headlineParts(t).map((part, i) => (
            <span key={i}>
              {i > 0 && <span className="px-2 text-faint">·</span>}
              <span className={cn(i === 3 && t.failures > 0 && "text-danger")}>{part}</span>
            </span>
          ))}
        </p>
        <p className="mt-2 text-[14px] text-muted">
          In the last {filters.days} days{filters.provider && ` on ${providerLabel(filters.provider)}`}
          {filters.model && ` with ${filters.model}`}
          {filters.task && ` for ${humanTask(filters.task).toLowerCase()}`}. Average latency {formatMs(t.avg_latency_ms)}
          {t.tool_calls > 0 && `, ${t.tool_calls.toLocaleString("en")} tool calls`}.{" "}
          {t.estimated > 0 ? (
            <span className="text-warning">
              {Math.round(estShare * 100)}% of calls report estimated token counts (the provider didn&apos;t return usage), so totals are approximate.
            </span>
          ) : (
            "Token counts are as reported by providers; costs are estimates from registry prices."
          )}
        </p>
      </section>

      {t.calls > 0 && (
        <section aria-labelledby="usage-charts" className="space-y-10">
          <h2 id="usage-charts" className="sr-only">
            Charts
          </h2>
          <div className="grid gap-10 lg:grid-cols-2">
            <ChartFrame
              title="Tokens per day"
              subtitle={singleProvider ? providerLabel(providers[0] ?? "") : perProviderLoading ? "Loading provider breakdown…" : "Stacked by provider"}
              legend={!singleProvider && <Legend items={providers.map((p) => ({ key: p, label: providerLabel(p), color: providerColor(p) }))} />}
              empty={isEmptySeries(tokenRows, seriesKeys) && !perProviderLoading}
            >
              <TokensPerDayChart rows={tokenRows} providers={seriesKeys} />
            </ChartFrame>
            <ChartFrame title="Estimated cost per day" subtitle="USD, from registry prices" empty={isEmptySeries(costRows, ["cost"])}>
              <CostPerDayChart rows={costRows} />
            </ChartFrame>
          </div>
          <div className="grid gap-10 lg:grid-cols-2">
            <ChartFrame
              title="Latency by model"
              subtitle="Slowest 95th percentile first"
              legend={
                <Legend
                  items={[
                    { key: "avg", label: "Average", color: "var(--k-checkpoint)" },
                    { key: "p95", label: "p95", color: "var(--text-faint)" },
                  ]}
                />
              }
              empty={latency.length === 0}
            >
              <LatencyChart rows={latency} />
            </ChartFrame>
            <ChartFrame
              title="Calls by task"
              legend={
                <Legend
                  items={[
                    { key: "ok", label: "Succeeded", color: "var(--k-checkpoint)" },
                    { key: "failed", label: "Failed", color: "var(--danger)" },
                  ]}
                />
              }
              empty={tasks.length === 0}
            >
              <TaskCallsChart rows={tasks} />
            </ChartFrame>
          </div>
        </section>
      )}

      {t.calls > 0 && <Breakdowns data={data} setFilters={setFilters} />}
      {t.calls > 0 && <RecentCalls events={data.recent ?? []} />}
      <ConnectorUsage rows={data.connectors ?? []} events={data.connector_events ?? []} days={filters.days} />
    </div>
  );
}

const DIMENSIONS = [
  { key: "provider", label: "Provider" },
  { key: "model", label: "Model" },
  { key: "task", label: "Task" },
  { key: "idea", label: "Idea" },
] as const;
type Dim = (typeof DIMENSIONS)[number]["key"];

function Breakdowns({ data, setFilters }: { data: UsageSummary; setFilters: (p: Partial<Filters>) => void }) {
  const [dim, setDim] = useState<Dim>("provider");
  const rows = useMemo(() => {
    const src = { provider: data.by_provider, model: data.by_model, task: data.by_task, idea: data.by_idea }[dim] ?? [];
    return [...src].sort((a, b) => b.total_tokens - a.total_tokens || b.calls - a.calls);
  }, [data, dim]);
  const label = (g: UsageGroup) => (dim === "provider" ? providerLabel(g.key) : dim === "task" ? humanTask(g.key) : g.key);
  const filterable = dim !== "idea";
  const apply = (g: UsageGroup) => {
    if (dim === "provider") setFilters({ provider: g.key, model: "" });
    if (dim === "model") setFilters({ model: g.key, provider: "" });
    if (dim === "task") setFilters({ task: g.key });
  };
  return (
    <section aria-labelledby="usage-breakdown">
      <SectionTitle action={<Segmented size="sm" label="Group by" value={dim} onChange={setDim} options={DIMENSIONS.map((d) => ({ value: d.key, label: d.label }))} />}>
        <span id="usage-breakdown">Breakdown</span>
      </SectionTitle>
      <div className="relative overflow-x-auto rounded-[var(--radius-lg)] border border-border bg-surface">
        <table className="w-full min-w-[880px] border-collapse text-[13px]">
          <thead>
            <tr className="border-b border-border text-left text-[11px] uppercase tracking-wide text-faint">
              <th className="px-4 py-2.5 font-medium">{DIMENSIONS.find((d) => d.key === dim)?.label}</th>
              <Th>Calls</Th>
              <Th>Failures</Th>
              <Th>Input</Th>
              <Th>Output</Th>
              <Th>Total tokens</Th>
              <Th>Cost</Th>
              <Th>Avg</Th>
              <Th>p95</Th>
              <Th>Tools</Th>
              <Th title="Calls whose token counts were estimated because the provider didn't report usage">Est.</Th>
            </tr>
          </thead>
          <tbody className="tabular-nums">
            {rows.map((g) => (
              <tr key={g.key} className="border-b border-border last:border-b-0 hover:bg-surface-2/40">
                <td className="max-w-[280px] px-4 py-2">
                  {filterable ? (
                    <button type="button" onClick={() => apply(g)} className="inline-flex max-w-full items-center gap-2 text-left text-fg hover:underline" title={`Filter by ${g.key}`}>
                      {dim === "provider" && <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: providerColor(g.key) }} aria-hidden />}
                      <span className={cn("truncate", dim === "model" && "font-mono text-[12px]")}>{label(g)}</span>
                    </button>
                  ) : (
                    <span className={cn("block truncate", g.key === "(no idea)" ? "text-faint" : "text-fg")}>{g.key === "(no idea)" ? "Not tied to an idea" : g.key}</span>
                  )}
                </td>
                <Td>{g.calls.toLocaleString("en")}</Td>
                <Td className={g.failures ? "text-danger" : "text-faint"}>{g.failures.toLocaleString("en")}</Td>
                <Td>{compactNumber(g.input_tokens)}</Td>
                <Td>{compactNumber(g.output_tokens)}</Td>
                <Td className="text-fg">{compactNumber(g.total_tokens)}</Td>
                <Td>{formatUsd(g.cost_usd)}</Td>
                <Td>{formatMs(g.avg_latency_ms)}</Td>
                <Td>{formatMs(g.p95_latency_ms)}</Td>
                <Td className={g.tool_calls ? "" : "text-faint"}>{g.tool_calls}</Td>
                <Td className={g.estimated ? "text-warning" : "text-faint"}>{Math.round(estimatedShare(g) * 100)}%</Td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function Th({ children, title }: { children: React.ReactNode; title?: string }) {
  return (
    <th className="px-3 py-2.5 text-right font-medium" title={title}>
      {children}
    </th>
  );
}
function Td({ children, className }: { children: React.ReactNode; className?: string }) {
  return <td className={cn("whitespace-nowrap px-3 py-2 text-right text-muted", className)}>{children}</td>;
}

function RecentCalls({ events }: { events: UsageEvent[] }) {
  const [all, setAll] = useState(false);
  const shown = all ? events : events.slice(0, 15);
  return (
    <section aria-labelledby="usage-recent">
      <SectionTitle action={<span className="text-[12px] text-faint">{events.length >= 200 ? "Latest 200" : `${events.length} in range`}</span>}>
        <span id="usage-recent">Recent model calls</span>
      </SectionTitle>
      <div className="relative overflow-x-auto rounded-[var(--radius-lg)] border border-border bg-surface">
        <table className="w-full min-w-[860px] border-collapse text-[13px]">
          <thead>
            <tr className="border-b border-border text-left text-[11px] uppercase tracking-wide text-faint">
              <th className="px-4 py-2.5 font-medium">Time</th>
              <th className="px-3 py-2.5 font-medium">Model</th>
              <th className="px-3 py-2.5 font-medium">Operation</th>
              <th className="px-3 py-2.5 font-medium">Task</th>
              <Th>Tokens</Th>
              <Th>Cost</Th>
              <Th>Latency</Th>
              <th className="px-4 py-2.5 font-medium">Result</th>
            </tr>
          </thead>
          <tbody className="tabular-nums">
            {shown.map((e) => (
              <tr key={e.id} className="border-b border-border last:border-b-0">
                <td className="whitespace-nowrap px-4 py-2 text-muted" title={fullDate(e.created_at)}>
                  {shortDate(e.created_at)}
                </td>
                <td className="max-w-[260px] px-3 py-2">
                  <span className="flex min-w-0 items-center gap-1.5">
                    <span className="h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: providerColor(e.provider) }} aria-hidden />
                    <span className="truncate font-mono text-[12px] text-fg">{e.model}</span>
                  </span>
                </td>
                <td className="px-3 py-2 text-muted">{humanize(e.operation)}</td>
                <td className="px-3 py-2 text-muted">{humanTask(e.task)}</td>
                <Td>
                  {e.tokens_estimated ? (
                    <Tooltip content="Estimated — the provider didn't report token usage">
                      <span tabIndex={0} className="text-warning">
                        ~{e.total_tokens.toLocaleString("en")}
                      </span>
                    </Tooltip>
                  ) : (
                    <span title={`${e.input_tokens.toLocaleString("en")} in · ${e.output_tokens.toLocaleString("en")} out`}>{e.total_tokens.toLocaleString("en")}</span>
                  )}
                </Td>
                <Td>{formatUsd(e.estimated_cost_usd)}</Td>
                <Td>{formatMs(e.latency_ms)}</Td>
                <td className="px-4 py-2">
                  {e.success ? (
                    <span className="inline-flex items-center gap-1 text-[12px] text-success">
                      <CircleCheck className="h-3.5 w-3.5" /> OK
                    </span>
                  ) : (
                    <Tooltip content={e.error || "Failed (no error message recorded)"}>
                      <span tabIndex={0} className="inline-flex max-w-[180px] items-center gap-1 text-[12px] text-danger">
                        <CircleX className="h-3.5 w-3.5 shrink-0" /> <span className="truncate">{e.error ? e.error : "Failed"}</span>
                      </span>
                    </Tooltip>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {events.length > 15 && (
        <Button size="sm" variant="ghost" className="mt-2" onClick={() => setAll((v) => !v)}>
          {all ? "Show fewer" : `Show all ${events.length}`}
        </Button>
      )}
    </section>
  );
}

function ConnectorUsage({ rows, events, days }: { rows: ConnectorUsageRow[]; events: ConnectorEvent[]; days: string }) {
  return (
    <section aria-labelledby="usage-connectors">
      <SectionTitle
        action={
          <Link href="/connectors" className="text-[12px] text-muted hover:text-fg">
            Manage connectors →
          </Link>
        }
      >
        <span id="usage-connectors">Connector usage</span>
      </SectionTitle>
      {rows.length === 0 ? (
        <EmptyState
          icon={Plug}
          title={`No connector activity in the last ${days} days`}
          description="When the agent reads the web, GitHub or your custom endpoint (always after you confirm), each call is logged here with its latency and outcome."
          className="py-10"
        />
      ) : (
        <div className="grid gap-8 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
          <div className="relative overflow-x-auto rounded-[var(--radius-lg)] border border-border bg-surface">
            <table className="w-full min-w-[520px] border-collapse text-[13px]">
              <thead>
                <tr className="border-b border-border text-left text-[11px] uppercase tracking-wide text-faint">
                  <th className="px-4 py-2.5 font-medium">Connector / tool</th>
                  <Th>Calls</Th>
                  <Th>Succeeded</Th>
                  <Th>Failed</Th>
                  <Th>Avg latency</Th>
                  <Th>Last</Th>
                </tr>
              </thead>
              <tbody className="tabular-nums">
                {rows.map((r) => (
                  <tr key={`${r.connector_key}/${r.tool}`} className="border-b border-border last:border-b-0">
                    <td className="px-4 py-2">
                      <span className="text-fg">{connectorName(r.connector_key)}</span>
                      <span className="ml-1.5 font-mono text-[11.5px] text-faint">{r.tool || "connect"}</span>
                    </td>
                    <Td>{r.calls}</Td>
                    <Td className="text-success">{r.calls - r.failures}</Td>
                    <Td className={r.failures ? "text-danger" : "text-faint"}>{r.failures}</Td>
                    <Td>{r.avg_latency_ms < 1 ? "<1 ms" : formatMs(r.avg_latency_ms)}</Td>
                    <Td className="text-faint">{timeAgo(r.last_at)}</Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <ConnectorEventList events={events.slice(0, 12)} />
        </div>
      )}
    </section>
  );
}

function UsageSkeleton() {
  return (
    <div className="space-y-10">
      <div className="space-y-2">
        <Skeleton className="h-7 w-3/4" />
        <Skeleton className="h-4 w-1/2" />
      </div>
      <div className="grid gap-10 lg:grid-cols-2">
        <Skeleton className="h-[260px]" />
        <Skeleton className="h-[260px]" />
      </div>
      <Skeleton className="h-48" />
    </div>
  );
}
