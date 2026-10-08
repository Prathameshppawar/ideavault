"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { FileText, Layers, MessageSquare, PenLine, Search, Sparkles, X } from "lucide-react";
import { ARTIFACT_TYPES, type Artifact } from "@/lib/types";
import { fullDate, plural, timeAgo } from "@/lib/format";
import { EntityChip } from "@/components/ui/badges";
import { EmptyState, ErrorState, Input, PageHeader, Skeleton, Spinner } from "@/components/ui/primitives";
import { ARTIFACT_STATUS_TONE, artifactTypeName, generatorInfo, markdownPreview } from "./artifact-meta";
import { LinkButton, Pill, Segmented, Select } from "./kit";
import { useArtifacts } from "./queries";

const STATUS_OPTIONS = [
  { value: "", label: "All" },
  { value: "DRAFT", label: "Draft" },
  { value: "FINAL", label: "Final" },
  { value: "ARCHIVED", label: "Archived" },
] as const;
type StatusFilter = (typeof STATUS_OPTIONS)[number]["value"];

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

export function ArtifactsScreen() {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const type = params.get("type") ?? "";
  const status = (params.get("status") ?? "") as StatusFilter;
  const ideaId = params.get("idea_id") ?? "";
  const [q, setQ] = useState(params.get("q") ?? "");
  const dq = useDebounced(q.trim(), 250);

  const setParams = useCallback(
    (patch: Record<string, string>) => {
      const next = new URLSearchParams(params.toString());
      for (const [k, v] of Object.entries(patch)) {
        if (v) next.set(k, v);
        else next.delete(k);
      }
      const s = next.toString();
      router.replace(s ? `${pathname}?${s}` : pathname, { scroll: false });
    },
    [params, pathname, router],
  );

  const filters = useMemo(() => ({ type: type || undefined, status: status || undefined, q: dq || undefined, idea_id: ideaId || undefined }), [type, status, dq, ideaId]);
  const { data, isLoading, error, refetch, isFetching } = useArtifacts(filters);
  const filtered = !!(type || status || dq || ideaId);
  const ideaTitle = ideaId ? data?.find((a) => a.idea_id === ideaId)?.idea_title : undefined;

  return (
    <div className="mx-auto max-w-5xl px-5 py-8 sm:px-8 sm:py-10">
      <PageHeader
        title="Artifacts"
        description="Documents generated from your recorded thinking — plans, specs, briefs and prompts. Each one traces back to the decisions it was built from."
      />

      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="relative min-w-[220px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" aria-hidden />
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search titles and content…" className="pl-8" aria-label="Search artifacts" type="search" />
        </div>
        <Select value={type} onChange={(e) => setParams({ type: e.target.value })} aria-label="Filter by type" wrapperClassName="w-full sm:w-52">
          <option value="">All types</option>
          {ARTIFACT_TYPES.map((t) => (
            <option key={t} value={t}>
              {artifactTypeName(t)}
            </option>
          ))}
        </Select>
        <Segmented<StatusFilter> label="Filter by status" value={status} onChange={(v) => setParams({ status: v })} options={[...STATUS_OPTIONS]} />
      </div>

      <div className="mb-2 flex min-h-7 flex-wrap items-center gap-2 text-[12px] text-faint">
        {data && <span>{plural(data.length, "artifact")}</span>}
        {isFetching && !isLoading && <Spinner className="h-3 w-3" />}
        {ideaId && (
          <span className="inline-flex items-center gap-1 rounded-full border border-border bg-surface px-2 py-0.5 text-muted">
            For {ideaTitle ?? "one idea"}
            <button type="button" onClick={() => setParams({ idea_id: "" })} className="rounded-full p-0.5 text-faint hover:text-fg" aria-label="Show artifacts from all ideas">
              <X className="h-3 w-3" />
            </button>
          </span>
        )}
      </div>

      {isLoading ? (
        <ListSkeleton />
      ) : error ? (
        <ErrorState error={error} onRetry={() => void refetch()} />
      ) : !data?.length ? (
        filtered ? (
          <EmptyState
            icon={Search}
            title="Nothing matches these filters"
            description="Try a different search, or clear the filters to see every artifact."
            action={
              <button
                type="button"
                className="text-[13px] font-medium text-accent hover:underline"
                onClick={() => {
                  setQ("");
                  setParams({ type: "", status: "", q: "", idea_id: "" });
                }}
              >
                Clear filters
              </button>
            }
          />
        ) : (
          <EmptyState
            icon={FileText}
            title="No artifacts yet"
            description="Artifacts are generated from an idea's recorded thinking — an action plan, a spec, an implementation prompt. Open an idea to generate one, or ask in chat."
            action={
              <div className="flex flex-wrap justify-center gap-2">
                <LinkButton href="/ideas" size="sm">
                  Browse ideas
                </LinkButton>
                <LinkButton href={`/?q=${encodeURIComponent("Turn my idea into an action plan")}`} variant="primary" size="sm">
                  <MessageSquare className="h-3.5 w-3.5" /> “Turn my idea into an action plan”
                </LinkButton>
              </div>
            }
          />
        )
      ) : (
        <ul className="animate-fade-in border-t border-border">
          {data.map((a) => (
            <ArtifactRow key={a.id} artifact={a} />
          ))}
        </ul>
      )}
    </div>
  );
}

function GeneratorTag({ generator }: { generator: string }) {
  const g = generatorInfo(generator);
  const Icon = g.kind === "ai" ? Sparkles : g.kind === "user" ? PenLine : Layers;
  return (
    <span className="inline-flex min-w-0 items-center gap-1" title={g.detail}>
      <Icon className="h-3 w-3 shrink-0" aria-hidden />
      <span className="truncate">{g.label}</span>
    </span>
  );
}

function ArtifactRow({ artifact: a }: { artifact: Artifact }) {
  const preview = markdownPreview(a.content_markdown, 280);
  return (
    <li className="group relative -mx-3 border-b border-border px-3 py-5 transition-colors hover:bg-surface-2/50 sm:rounded-[var(--radius-md)]">
      <div className="mb-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-1">
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-k-artifact">
          <span className="h-1.5 w-1.5 rounded-full bg-k-artifact" />
          {artifactTypeName(a.type)}
        </span>
        <Pill tone={ARTIFACT_STATUS_TONE[a.status] ?? "muted"}>{a.status.charAt(0) + a.status.slice(1).toLowerCase()}</Pill>
      </div>
      <h3 className="font-display text-[1.4rem] leading-tight text-fg transition-colors group-hover:text-accent">
        <Link href={`/artifacts/${a.id}`} className="after:absolute after:inset-0 after:content-[''] focus-visible:outline-none focus-visible:after:rounded-[var(--radius-md)] focus-visible:after:ring-2 focus-visible:after:ring-accent">
          {a.title}
        </Link>
      </h3>
      {preview && <p className="mt-1.5 line-clamp-2 max-w-3xl text-[14px] leading-relaxed text-muted">{preview}</p>}
      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[12px] text-faint">
        <span className="relative z-10">
          <EntityChip type="idea" id={a.idea_id} label={a.idea_title || "Idea"} />
        </span>
        {a.branch_name && <span>branch {a.branch_name}</span>}
        <span className="font-mono">v{a.current_version}</span>
        <GeneratorTag generator={a.generator} />
        <span title={fullDate(a.updated_at)}>Updated {timeAgo(a.updated_at)}</span>
      </div>
    </li>
  );
}

function ListSkeleton() {
  return (
    <div className="border-t border-border" aria-busy="true" aria-label="Loading artifacts">
      {[0, 1, 2].map((i) => (
        <div key={i} className="space-y-2.5 border-b border-border py-5">
          <Skeleton className="h-3 w-28" />
          <Skeleton className="h-6 w-2/3" />
          <Skeleton className="h-3.5 w-full" />
          <Skeleton className="h-3.5 w-4/5" />
        </div>
      ))}
    </div>
  );
}
