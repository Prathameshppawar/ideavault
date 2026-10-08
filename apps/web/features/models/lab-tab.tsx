"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Braces, CircleCheck, CircleX, Clock, Coins, FlaskConical, History, Play, Target, Trophy, Upload, Zap } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { LabResult, LabRun, ModelConfig } from "@/lib/types";
import { cn, compactNumber, fullDate, plural, timeAgo } from "@/lib/format";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badges";
import { Checkbox, Switch } from "@/components/ui/overlay";
import { EmptyState, ErrorState, Field, Input, Panel, SectionTitle, Skeleton, SkeletonLines, Spinner, Textarea } from "@/components/ui/primitives";
import { Select } from "@/features/usage/controls";
import { formatCost, formatLatency, prettyJson, rankLabResults, schemaErrors, schemaInputError, type LabRanking } from "./lab";
import { humanTask, modelKey, providerColor, providerLabel, sortProviders } from "./model-meta";
import { modelKeys, useModels, type BenchmarkTask } from "./queries";

const MAX_MODELS = 8;
const TASKS = ["custom", "extraction", "classification", "summary", "title", "synthesis", "artifact", "contradiction", "prompt_analysis"];

interface LabForm {
  models: string[];
  task: string;
  title: string;
  system: string;
  prompt: string;
  expectJson: boolean;
  schema: string;
  reference: string;
}

const EMPTY_FORM: LabForm = { models: [], task: "custom", title: "", system: "", prompt: "", expectJson: false, schema: "", reference: "" };

export function labPayload(f: LabForm) {
  return {
    models: f.models,
    task: f.task.trim() || "custom",
    title: f.title.trim(),
    system: f.system,
    prompt: f.prompt,
    expect_json: f.expectJson,
    json_schema: f.expectJson && f.schema.trim() ? (JSON.parse(f.schema) as unknown) : undefined,
    reference: f.reference.trim() || undefined,
  };
}

export function LabTab() {
  const params = useSearchParams();
  const router = useRouter();
  const qc = useQueryClient();
  const runId = params.get("run");
  const [form, setForm] = useState<LabForm>(EMPTY_FORM);
  const { data: models } = useModels();
  const runs = useQuery({ queryKey: modelKeys.labRuns, queryFn: () => api.get<LabRun[]>("/v1/models/lab") });

  const openRun = (id: string | null) => {
    const p = new URLSearchParams(params.toString());
    p.set("tab", "lab");
    if (id) p.set("run", id);
    else p.delete("run");
    router.replace(`?${p.toString()}`, { scroll: false });
  };

  const run = useMutation({
    mutationFn: () => api.post<LabRun>("/v1/models/lab", labPayload(form)),
    onSuccess: (r) => {
      qc.setQueryData(modelKeys.labRun(r.id), r);
      void qc.invalidateQueries({ queryKey: modelKeys.labRuns });
      void qc.invalidateQueries({ queryKey: ["usage"] });
      openRun(r.id);
      toast.success("Lab run complete");
    },
  });

  const loadIntoForm = (r: LabRun) => {
    setForm((f) => ({
      ...f,
      task: r.task || "custom",
      title: r.title,
      system: r.system,
      prompt: r.prompt,
      expectJson: r.expect_json,
      schema: r.json_schema ? JSON.stringify(r.json_schema, null, 2) : "",
      reference: r.reference,
      models: r.results?.map((x) => `${x.provider}/${x.model}`).filter((k) => (models ?? []).some((m) => m.available && modelKey(m) === k)) ?? f.models,
    }));
    openRun(null);
    window.scrollTo({ top: 0, behavior: "smooth" });
  };

  return (
    <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_260px]">
      <div className="min-w-0 space-y-8">
        <LabFormPanel form={form} setForm={setForm} models={models ?? []} running={run.isPending} onRun={() => run.mutate()} error={run.error} />
        {run.isPending ? (
          <RunningPanel form={form} models={models ?? []} />
        ) : runId ? (
          <RunView id={runId} models={models ?? []} onClose={() => openRun(null)} onLoad={loadIntoForm} autoScroll={run.data?.id === runId} />
        ) : null}
      </div>
      <aside aria-labelledby="lab-history" className="min-w-0">
        <SectionTitle>
          <span id="lab-history" className="inline-flex items-center gap-1.5">
            <History className="h-3.5 w-3.5" /> Past runs
          </span>
        </SectionTitle>
        {runs.isLoading ? (
          <SkeletonLines lines={5} />
        ) : runs.error ? (
          <ErrorState error={runs.error} onRetry={() => void runs.refetch()} />
        ) : (runs.data ?? []).length === 0 ? (
          <p className="text-[13px] leading-relaxed text-faint">No runs yet. Results are saved here so you can compare models over time.</p>
        ) : (
          <ul className="-mx-2 space-y-0.5">
            {runs.data!.map((r) => (
              <li key={r.id}>
                <button
                  type="button"
                  onClick={() => openRun(r.id)}
                  aria-current={r.id === runId ? "true" : undefined}
                  className={cn(
                    "w-full rounded-[var(--radius-md)] px-2 py-2 text-left transition-colors hover:bg-surface-2",
                    r.id === runId && "bg-surface-3 hover:bg-surface-3",
                  )}
                >
                  <span className="line-clamp-2 text-[13px] leading-snug text-fg">{r.title || "Untitled run"}</span>
                  <span className="mt-0.5 block text-[11.5px] text-faint">
                    {humanTask(r.task)} · {timeAgo(r.created_at)}
                    {r.status !== "COMPLETED" && ` · ${r.status.toLowerCase()}`}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </aside>
    </div>
  );
}

function LabFormPanel({
  form,
  setForm,
  models,
  running,
  onRun,
  error,
}: {
  form: LabForm;
  setForm: React.Dispatch<React.SetStateAction<LabForm>>;
  models: ModelConfig[];
  running: boolean;
  onRun: () => void;
  error: unknown;
}) {
  const benchmarks = useQuery({ queryKey: modelKeys.benchmarks, queryFn: () => api.get<BenchmarkTask[]>("/v1/models/benchmarks"), staleTime: Infinity });
  const available = useMemo(() => sortProviders(models.filter((m) => m.available), (m) => m.provider), [models]);
  const unavailable = models.filter((m) => m.enabled && !m.available).length;
  const set = <K extends keyof LabForm>(k: K, v: LabForm[K]) => setForm((f) => ({ ...f, [k]: v }));
  const schemaErr = form.expectJson ? schemaInputError(form.schema) : null;
  const canRun = form.models.length > 0 && form.prompt.trim() !== "" && !schemaErr && !running;

  const loadBenchmark = (id: string) => {
    const b = benchmarks.data?.find((x) => x.id === id);
    if (!b) return;
    setForm((f) => ({
      ...f,
      task: b.task,
      title: b.title,
      system: b.system,
      prompt: b.prompt,
      expectJson: b.expect_json,
      schema: b.json_schema ? JSON.stringify(b.json_schema, null, 2) : "",
      reference: b.reference ?? "",
    }));
    toast.message(`Loaded “${b.title}”`);
  };

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (canRun) onRun();
      }}
      className="space-y-6"
    >
      <div className="flex flex-wrap items-end justify-between gap-3">
        <p className="max-w-xl text-[14px] leading-relaxed text-muted">
          Run one prompt on several models at once and compare output, JSON validity, accuracy against a reference, latency, tokens and cost.
        </p>
        <Select
          aria-label="Load a built-in benchmark"
          value=""
          onChange={(e) => loadBenchmark(e.target.value)}
          wrapperClassName="w-auto min-w-[220px]"
          className="h-8 text-[13px]"
          disabled={!benchmarks.data?.length}
        >
          <option value="">Load a benchmark…</option>
          {(benchmarks.data ?? []).map((b) => (
            <option key={b.id} value={b.id}>
              {b.title}
            </option>
          ))}
        </Select>
      </div>

      <fieldset>
        <legend className="mb-2 flex w-full items-baseline justify-between gap-3 text-[13px] font-medium text-muted">
          <span>Models</span>
          <span className="text-[12px] font-normal text-faint">
            {form.models.length} / {MAX_MODELS} selected
          </span>
        </legend>
        {available.length === 0 ? (
          <p className="text-[13px] text-faint">No models are available. Enable a model or add a provider key.</p>
        ) : (
          <div className="grid gap-1.5 sm:grid-cols-2 xl:grid-cols-3">
            {available.map((m) => {
              const key = modelKey(m);
              const checked = form.models.includes(key);
              const disabled = !checked && form.models.length >= MAX_MODELS;
              return (
                <label
                  key={m.id}
                  className={cn(
                    "flex cursor-pointer items-center gap-2.5 rounded-[var(--radius-md)] border px-3 py-2 transition-colors",
                    checked ? "border-fg/40 bg-surface-2" : "border-border hover:border-border-strong",
                    disabled && "cursor-not-allowed opacity-50",
                  )}
                >
                  <Checkbox
                    checked={checked}
                    disabled={disabled}
                    onCheckedChange={(v) => set("models", v ? [...form.models, key] : form.models.filter((x) => x !== key))}
                    aria-label={m.display_name || m.model}
                  />
                  <span className="h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: providerColor(m.provider) }} aria-hidden />
                  <span className="min-w-0">
                    <span className="block truncate text-[13px] text-fg">{m.display_name || m.model}</span>
                    <span className="block truncate font-mono text-[11px] text-faint">{key}</span>
                  </span>
                </label>
              );
            })}
          </div>
        )}
        {unavailable > 0 && <p className="mt-2 text-[12px] text-faint">{plural(unavailable, "more model")} will appear here once their provider is active.</p>}
      </fieldset>

      <div className="grid gap-4 sm:grid-cols-[200px_minmax(0,1fr)]">
        <Field label="Task" htmlFor="lab-task">
          <Select id="lab-task" value={TASKS.includes(form.task) ? form.task : "custom"} onChange={(e) => set("task", e.target.value)}>
            {TASKS.map((t) => (
              <option key={t} value={t}>
                {humanTask(t)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Title (optional)" htmlFor="lab-title">
          <Input id="lab-title" value={form.title} onChange={(e) => set("title", e.target.value)} placeholder="Defaults to the start of the prompt" />
        </Field>
      </div>
      <Field label="System prompt (optional)" htmlFor="lab-system">
        <Textarea id="lab-system" rows={2} value={form.system} onChange={(e) => set("system", e.target.value)} placeholder="Instructions every model receives" />
      </Field>
      <Field label="Prompt" htmlFor="lab-prompt">
        <Textarea id="lab-prompt" rows={6} required value={form.prompt} onChange={(e) => set("prompt", e.target.value)} placeholder="What should the models do?" className="font-[450]" />
      </Field>

      <div className="space-y-3 rounded-[var(--radius-lg)] border border-border p-4">
        <label className="flex items-center justify-between gap-3">
          <span>
            <span className="flex items-center gap-1.5 text-[13px] font-medium text-fg">
              <Braces className="h-3.5 w-3.5 text-muted" /> Expect JSON
            </span>
            <span className="block text-[12px] text-faint">Check each output parses as JSON (and matches the schema, if given).</span>
          </span>
          <Switch checked={form.expectJson} onCheckedChange={(v) => set("expectJson", v)} aria-label="Expect JSON" />
        </label>
        {form.expectJson && (
          <Field label="JSON schema (optional)" htmlFor="lab-schema" error={schemaErr ?? undefined}>
            <Textarea
              id="lab-schema"
              rows={6}
              spellCheck={false}
              className="font-mono text-[12.5px]"
              value={form.schema}
              onChange={(e) => set("schema", e.target.value)}
              placeholder={'{\n  "type": "object",\n  "properties": { … }\n}'}
            />
          </Field>
        )}
      </div>
      <Field label="Reference answer (optional)" htmlFor="lab-ref" hint="When given, each output gets a lexical-overlap correctness score (0–100%).">
        <Textarea id="lab-ref" rows={3} value={form.reference} onChange={(e) => set("reference", e.target.value)} />
      </Field>

      {error ? (
        <p role="alert" className="rounded-[var(--radius-md)] border border-danger/30 bg-danger-soft px-3 py-2 text-[13px] text-danger">
          {errorMessage(error)}
        </p>
      ) : null}
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" variant="primary" loading={running} disabled={!canRun}>
          {!running && <Play className="h-3.5 w-3.5" />} Run on {plural(form.models.length, "model")}
        </Button>
        {(form.prompt || form.models.length > 0) && !running && (
          <Button type="button" variant="ghost" size="sm" onClick={() => setForm(EMPTY_FORM)}>
            Clear
          </Button>
        )}
        <span className="text-[12px] text-faint">Runs in parallel; may take up to a few minutes. Usage is recorded under the Model Lab task.</span>
      </div>
    </form>
  );
}

function RunningPanel({ form, models }: { form: LabForm; models: ModelConfig[] }) {
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    const start = Date.now();
    const t = setInterval(() => setElapsed(Math.floor((Date.now() - start) / 1000)), 500);
    return () => clearInterval(t);
  }, []);
  return (
    <Panel className="animate-fade-in p-5" aria-live="polite">
      <div className="mb-4 flex items-center gap-2 text-[14px] text-fg">
        <Spinner />
        Running on {plural(form.models.length, "model")}… <span className="tabular-nums text-faint">{elapsed}s</span>
      </div>
      <ul className="space-y-2">
        {form.models.map((k) => {
          const m = models.find((x) => modelKey(x) === k);
          return (
            <li key={k} className="flex items-center gap-3 text-[13px]">
              <span className="h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: providerColor(m?.provider ?? "") }} aria-hidden />
              <span className="w-48 shrink-0 truncate text-muted">{m?.display_name ?? k}</span>
              <span className="relative h-1 flex-1 overflow-hidden rounded-full bg-surface-3">
                <span className="absolute inset-y-0 left-0 w-1/3 animate-pulse-soft rounded-full bg-fg/30" />
              </span>
            </li>
          );
        })}
      </ul>
    </Panel>
  );
}

function RunView({ id, models, onClose, onLoad, autoScroll }: { id: string; models: ModelConfig[]; onClose: () => void; onLoad: (r: LabRun) => void; autoScroll?: boolean }) {
  const { data, isLoading, error, refetch } = useQuery({ queryKey: modelKeys.labRun(id), queryFn: () => api.get<LabRun>(`/v1/models/lab/${id}`) });
  if (isLoading) {
    return (
      <div className="grid gap-4 md:grid-cols-2">
        <Skeleton className="h-64" />
        <Skeleton className="h-64" />
      </div>
    );
  }
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;
  return <LabRunResults run={data} models={models} onClose={onClose} onLoad={() => onLoad(data)} autoScroll={autoScroll} />;
}

export function LabRunResults({ run, models, onClose, onLoad, autoScroll }: { run: LabRun; models: ModelConfig[]; onClose?: () => void; onLoad?: () => void; autoScroll?: boolean }) {
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    // Bring fresh results (or a run opened from history) into view below the form.
    ref.current?.scrollIntoView({ behavior: autoScroll ? "smooth" : "auto", block: "start" });
  }, [run.id, autoScroll]);
  const results = useMemo(() => sortProviders(run.results ?? [], (r) => r.provider), [run.results]);
  const ranking = useMemo(() => rankLabResults(results, { expectJson: run.expect_json, hasReference: !!run.reference?.trim() }), [results, run]);
  const name = (r: LabResult) => models.find((m) => m.provider === r.provider && m.model === r.model)?.display_name || r.model;
  const failed = results.filter((r) => r.error).length;
  return (
    <section ref={ref} aria-labelledby="lab-results" className="animate-slide-up scroll-mt-6 space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3 border-t border-border pt-6">
        <div className="min-w-0">
          <h2 id="lab-results" className="text-[17px] font-semibold text-fg">
            {run.title || "Lab run"}
          </h2>
          <p className="mt-0.5 text-[12.5px] text-faint">
            {humanTask(run.task)} · {fullDate(run.created_at)} · {plural(results.length, "model")}
            {failed > 0 && <span className="text-danger"> · {failed} failed</span>}
            {run.expect_json && " · JSON expected"}
            {run.reference && " · scored against reference"}
          </p>
        </div>
        <div className="flex gap-2">
          {onLoad && (
            <Button size="sm" variant="ghost" onClick={onLoad}>
              <Upload className="h-3.5 w-3.5" /> Load into form
            </Button>
          )}
          {onClose && (
            <Button size="sm" variant="ghost" onClick={onClose}>
              Close
            </Button>
          )}
        </div>
      </div>
      <details className="group rounded-[var(--radius-md)] border border-border bg-surface-2/50 px-4 py-2.5 text-[13px]">
        <summary className="cursor-pointer select-none text-muted marker:text-faint hover:text-fg">Prompt & settings</summary>
        <div className="mt-3 space-y-3">
          {run.system && <PromptBlock label="System">{run.system}</PromptBlock>}
          <PromptBlock label="Prompt">{run.prompt}</PromptBlock>
          {run.reference && <PromptBlock label="Reference answer">{run.reference}</PromptBlock>}
          {run.json_schema != null && <PromptBlock label="JSON schema">{JSON.stringify(run.json_schema, null, 2)}</PromptBlock>}
        </div>
      </details>
      <Highlights ranking={ranking} results={results} name={name} />
      {results.length === 0 ? (
        <EmptyState icon={FlaskConical} title="No results recorded" description="The run finished without storing any model output." />
      ) : (
        <div className="relative -mx-1 overflow-x-auto px-1 pb-2">
          <div className="grid auto-cols-[minmax(280px,1fr)] grid-flow-col gap-4" style={{ minWidth: results.length > 1 ? undefined : "auto" }}>
            {results.map((r) => (
              <ResultColumn key={r.id} r={r} name={name(r)} ranking={ranking} expectJson={run.expect_json} hasReference={!!run.reference?.trim()} />
            ))}
          </div>
        </div>
      )}
    </section>
  );
}

function PromptBlock({ label, children }: { label: string; children: string }) {
  return (
    <div>
      <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-faint">{label}</div>
      <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-[var(--radius-md)] border border-border bg-surface px-3 py-2 font-mono text-[12px] leading-relaxed text-fg">
        {children}
      </pre>
    </div>
  );
}

function Highlights({ ranking, results, name }: { ranking: LabRanking; results: LabResult[]; name: (r: LabResult) => string }) {
  const by = (id?: string) => results.find((r) => r.id === id);
  const items = [
    { k: "fastest", icon: Zap, label: "Fastest", r: by(ranking.fastest), v: (r: LabResult) => formatLatency(r.latency_ms) },
    { k: "cheapest", icon: Coins, label: "Cheapest", r: by(ranking.cheapest), v: (r: LabResult) => formatCost(r.estimated_cost_usd) },
    { k: "best", icon: Trophy, label: "Best answer", r: by(ranking.best), v: (r: LabResult) => (r.correctness != null ? `${Math.round(r.correctness * 100)}% match` : "only valid JSON") },
  ].filter((x) => x.r);
  if (items.length === 0) return null;
  return (
    <p className="flex flex-wrap gap-x-5 gap-y-1 text-[13px] text-muted">
      {items.map(({ k, icon: Icon, label, r, v }) => (
        <span key={k} className="inline-flex items-center gap-1.5">
          <Icon className="h-3.5 w-3.5 text-accent" aria-hidden />
          {label}: <span className="font-medium text-fg">{name(r!)}</span> <span className="text-faint">({v(r!)})</span>
        </span>
      ))}
    </p>
  );
}

function ResultColumn({ r, name, ranking, expectJson, hasReference }: { r: LabResult; name: string; ranking: LabRanking; expectJson: boolean; hasReference: boolean }) {
  const tags = [ranking.fastest === r.id && "Fastest", ranking.cheapest === r.id && "Cheapest", ranking.best === r.id && "Best"].filter(Boolean) as string[];
  const errs = schemaErrors(r.schema_errors);
  const pretty = expectJson ? prettyJson(r.output) : null;
  return (
    <article className={cn("flex min-w-0 flex-col rounded-[var(--radius-lg)] border bg-surface", tags.length ? "border-fg/30" : "border-border")} aria-label={`${name} result`}>
      <header className="border-b border-border px-4 py-3">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <div className="flex items-center gap-1.5">
              <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: providerColor(r.provider) }} aria-hidden />
              <h3 className="truncate text-[14px] font-medium text-fg">{name}</h3>
            </div>
            <code className="block truncate font-mono text-[11px] text-faint">
              {providerLabel(r.provider)} · {r.model}
            </code>
          </div>
          <div className="flex shrink-0 flex-wrap justify-end gap-1">
            {tags.map((t) => (
              <Badge key={t} className="border-accent/30 bg-accent-soft text-accent">
                {t}
              </Badge>
            ))}
          </div>
        </div>
        <dl className="mt-3 grid grid-cols-3 gap-x-3 gap-y-2 text-[12px]">
          <Metric icon={Clock} label="Latency" value={formatLatency(r.latency_ms)} />
          <Metric icon={Coins} label="Est. cost" value={r.error ? "—" : formatCost(r.estimated_cost_usd)} />
          {hasReference ? (
            <Metric icon={Target} label="Correctness" value={r.correctness != null ? `${Math.round(r.correctness * 100)}%` : "—"} />
          ) : (
            <Metric icon={Target} label="Total tokens" value={r.error ? "—" : compactNumber(r.total_tokens)} />
          )}
          <Metric label="Input" value={r.error ? "—" : r.input_tokens.toLocaleString()} />
          <Metric label="Output" value={r.error ? "—" : r.output_tokens.toLocaleString()} />
          {hasReference && <Metric label="Total" value={r.error ? "—" : r.total_tokens.toLocaleString()} />}
        </dl>
        {expectJson && !r.error && (
          <div className={cn("mt-3 flex items-start gap-1.5 text-[12px]", r.valid_json ? "text-success" : "text-danger")}>
            {r.valid_json ? <CircleCheck className="mt-px h-3.5 w-3.5 shrink-0" /> : <CircleX className="mt-px h-3.5 w-3.5 shrink-0" />}
            <div className="min-w-0">
              <span className="font-medium">{r.valid_json ? "Valid JSON" : "Invalid JSON"}</span>
              {errs.length > 0 && (
                <ul className="mt-1 space-y-0.5 text-muted">
                  {errs.slice(0, 5).map((e, i) => (
                    <li key={i} className="break-words font-mono text-[11px]">
                      {e}
                    </li>
                  ))}
                  {errs.length > 5 && <li className="text-faint">+{errs.length - 5} more</li>}
                </ul>
              )}
            </div>
          </div>
        )}
      </header>
      <div className="max-h-[420px] min-h-[120px] flex-1 overflow-auto px-4 py-3">
        {r.error ? (
          <p className="break-words text-[13px] text-danger">{r.error}</p>
        ) : pretty ? (
          <pre className="whitespace-pre-wrap break-words font-mono text-[12px] leading-relaxed text-fg">{pretty}</pre>
        ) : r.output.trim() ? (
          <Markdown>{r.output}</Markdown>
        ) : (
          <p className="text-[13px] text-faint">Empty output.</p>
        )}
      </div>
    </article>
  );
}

function Metric({ label, value, icon: Icon }: { label: string; value: string; icon?: React.ComponentType<{ className?: string }> }) {
  return (
    <div className="min-w-0">
      <dt className="flex items-center gap-1 text-[10.5px] uppercase tracking-wide text-faint">
        {Icon && <Icon className="h-3 w-3" aria-hidden />}
        {label}
      </dt>
      <dd className="truncate tabular-nums text-fg">{value}</dd>
    </div>
  );
}
