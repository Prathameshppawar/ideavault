"use client";

import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronsUpDown, Download, GitCompareArrows, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { api, downloadFromApi, errorMessage } from "@/lib/api";
import { cn, fullDate, humanize, timeAgo } from "@/lib/format";
import type { Artifact, ArtifactResult, ArtifactVersion } from "@/lib/types";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Checkbox, Tooltip } from "@/components/ui/overlay";
import { generatorInfo, stripLeadingTitle } from "./artifact-meta";
import { CopyButton, Pill, Segmented } from "./kit";
import { collapseUnchanged, diffLines, diffStats, type DiffLine } from "./line-diff";

const actorName = (a: string) => (a === "user" ? "You" : humanize(a));

/**
 * Version history: pick one version to read (and download / restore it), or two to see a line diff.
 */
export function ArtifactHistory({ artifact, versions }: { artifact: Artifact; versions: ArtifactVersion[] }) {
  // Click order matters: picking a third version drops the oldest pick.
  const [picked, setPicked] = useState<number[]>(() => (versions.length >= 2 ? [versions[1].version, versions[0].version] : versions[0] ? [versions[0].version] : []));
  const toggle = (v: number) =>
    setPicked((p) => (p.includes(v) ? p.filter((x) => x !== v) : p.length >= 2 ? [p[1], v] : [...p, v]));
  const byVersion = useMemo(() => new Map(versions.map((v) => [v.version, v])), [versions]);
  const sel = [...picked].sort((a, b) => a - b).map((v) => byVersion.get(v)).filter((v): v is ArtifactVersion => !!v);

  return (
    <div className="grid animate-fade-in gap-8 lg:grid-cols-[300px_minmax(0,1fr)]">
      <aside>
        <p className="mb-3 text-[13px] text-muted">Select one version to read it, or two to compare.</p>
        <ol className="space-y-1.5" aria-label="Versions">
          {versions.map((v) => {
            const on = picked.includes(v.version);
            const g = generatorInfo(v.generator);
            return (
              <li
                key={v.id}
                className={cn(
                  "flex items-start gap-3 rounded-[var(--radius-lg)] border px-3 py-2.5 transition-colors",
                  on ? "border-border-strong bg-surface" : "border-transparent hover:bg-surface-2",
                )}
              >
                <Checkbox id={`ver-${v.version}`} checked={on} onCheckedChange={() => toggle(v.version)} className="mt-0.5" aria-label={`Select version ${v.version}`} />
                <label htmlFor={`ver-${v.version}`} className="min-w-0 flex-1 cursor-pointer">
                  <span className="flex items-center gap-2">
                    <span className="font-mono text-[13px] font-semibold text-fg">v{v.version}</span>
                    {v.version === artifact.current_version && <Pill tone="success">Current</Pill>}
                    <span className="ml-auto text-[11px] text-faint" title={fullDate(v.created_at)}>
                      {timeAgo(v.created_at)}
                    </span>
                  </span>
                  <span className="mt-0.5 block text-[13px] leading-snug text-fg">{v.change_note || "Edited"}</span>
                  <span className="mt-0.5 block truncate text-[11px] text-faint" title={g.detail}>
                    {actorName(v.created_by)} · {g.label}
                  </span>
                </label>
                <Tooltip content={`Download v${v.version} (.md)`}>
                  <Button size="icon-sm" variant="ghost" aria-label={`Download version ${v.version}`} onClick={() => downloadFromApi(`/v1/artifacts/${artifact.id}/download?version=${v.version}`)}>
                    <Download className="h-3.5 w-3.5" />
                  </Button>
                </Tooltip>
              </li>
            );
          })}
        </ol>
      </aside>

      <section className="min-w-0">
        {sel.length === 2 ? (
          <VersionDiff key={`${sel[0].version}-${sel[1].version}`} from={sel[0]} to={sel[1]} />
        ) : sel.length === 1 ? (
          <VersionView key={sel[0].version} artifact={artifact} version={sel[0]} />
        ) : (
          <div className="flex h-64 flex-col items-center justify-center rounded-[var(--radius-lg)] border border-dashed border-border text-center">
            <GitCompareArrows className="mb-2 h-5 w-5 text-faint" />
            <p className="text-sm text-muted">Pick a version on the left.</p>
          </div>
        )}
      </section>
    </div>
  );
}

function VersionView({ artifact, version: v }: { artifact: Artifact; version: ArtifactVersion }) {
  const qc = useQueryClient();
  const isCurrent = v.version === artifact.current_version;
  const restore = useMutation({
    mutationFn: () => api.patch<ArtifactResult>(`/v1/artifacts/${artifact.id}`, { title: v.title, content_markdown: v.content_markdown, change_note: `Restored v${v.version}` }),
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: ["artifact", artifact.id] });
      void qc.invalidateQueries({ queryKey: ["artifacts"] });
      toast.success(`Restored v${v.version} as v${r.version?.version ?? artifact.current_version + 1}`);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <div>
      <div className="mb-5 flex flex-wrap items-start justify-between gap-3 border-b border-border pb-4">
        <div className="min-w-0">
          <p className="text-[13px] text-muted">
            <span className="font-mono font-semibold text-fg">v{v.version}</span> · {v.change_note || "Edited"} · {fullDate(v.created_at)}
          </p>
          {v.title !== artifact.title && <p className="mt-1 text-[13px] text-faint">Titled “{v.title}”</p>}
        </div>
        <div className="flex flex-wrap gap-1.5">
          <CopyButton size="sm" variant="ghost" text={v.content_markdown} label="Copy" toastMessage={`v${v.version} copied as Markdown`} />
          <Button size="sm" variant="ghost" onClick={() => downloadFromApi(`/v1/artifacts/${artifact.id}/download?version=${v.version}`)}>
            <Download className="h-3.5 w-3.5" /> Download
          </Button>
          {!isCurrent && (
            <Button size="sm" variant="secondary" onClick={() => restore.mutate()} loading={restore.isPending}>
              <RotateCcw className="h-3.5 w-3.5" /> Restore as v{artifact.current_version + 1}
            </Button>
          )}
        </div>
      </div>
      <article className="max-w-[70ch]">
        <Markdown>{stripLeadingTitle(v.content_markdown, v.title)}</Markdown>
      </article>
    </div>
  );
}

function VersionDiff({ from, to }: { from: ArtifactVersion; to: ArtifactVersion }) {
  const [mode, setMode] = useState<"changes" | "full">("changes");
  const lines = useMemo(() => diffLines(from.content_markdown, to.content_markdown), [from.content_markdown, to.content_markdown]);
  const stats = diffStats(lines);
  const titleChanged = from.title !== to.title;
  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3 border-b border-border pb-4">
        <div className="min-w-0">
          <p className="text-[13px] text-muted">
            Comparing <span className="font-mono font-semibold text-fg">v{from.version}</span> → <span className="font-mono font-semibold text-fg">v{to.version}</span>
            <span className="ml-3 font-mono text-[12px] text-success">+{stats.added}</span>
            <span className="ml-1.5 font-mono text-[12px] text-danger">−{stats.removed}</span>
          </p>
          <p className="mt-0.5 text-[12px] text-faint">{to.change_note || "Edited"}</p>
        </div>
        <Segmented<"changes" | "full">
          label="Diff view"
          size="sm"
          value={mode}
          onChange={setMode}
          options={[
            { value: "changes", label: "Changes" },
            { value: "full", label: "Full document" },
          ]}
        />
      </div>
      {titleChanged && (
        <p className="mb-3 text-[13px] text-muted">
          Title: <span className="text-danger line-through decoration-danger/50">{from.title}</span> → <span className="text-fg">{to.title}</span>
        </p>
      )}
      {stats.added === 0 && stats.removed === 0 ? (
        <p className="rounded-[var(--radius-lg)] border border-dashed border-border px-4 py-8 text-center text-[13px] text-muted">
          The content of these versions is identical{titleChanged ? " — only the title changed" : ""}.
        </p>
      ) : (
        <DiffView lines={lines} collapse={mode === "changes"} />
      )}
    </div>
  );
}

export function DiffView({ lines, collapse }: { lines: DiffLine[]; collapse: boolean }) {
  const chunks = useMemo(() => (collapse ? collapseUnchanged(lines, 3) : [{ kind: "lines" as const, lines }]), [lines, collapse]);
  const [open, setOpen] = useState<Set<number>>(new Set());
  return (
    <div className="overflow-hidden rounded-[var(--radius-lg)] border border-border bg-surface font-mono text-[12.5px] leading-[1.65]" role="table" aria-label="Line diff">
      {chunks.map((c, i) =>
        c.kind === "skip" && !open.has(i) ? (
          <button
            key={i}
            type="button"
            onClick={() => setOpen((s) => new Set(s).add(i))}
            className="flex w-full items-center gap-2 border-y border-border bg-surface-2 px-3 py-1 text-left font-sans text-[12px] text-faint transition-colors first:border-t-0 last:border-b-0 hover:text-fg"
          >
            <ChevronsUpDown className="h-3.5 w-3.5" aria-hidden /> {c.lines.length} unchanged lines
          </button>
        ) : (
          c.lines.map((l, j) => <DiffRow key={`${i}-${j}`} line={l} />)
        ),
      )}
    </div>
  );
}

function DiffRow({ line: l }: { line: DiffLine }) {
  return (
    <div role="row" className={cn("grid grid-cols-[2.25rem_2.25rem_1.25rem_minmax(0,1fr)]", l.op === "add" && "bg-success-soft", l.op === "del" && "bg-danger-soft")}>
      <span role="cell" className="select-none border-r border-border/60 pr-1.5 text-right text-faint">
        {l.a ?? ""}
      </span>
      <span role="cell" className="select-none border-r border-border/60 pr-1.5 text-right text-faint">
        {l.b ?? ""}
      </span>
      <span role="cell" aria-label={l.op === "add" ? "added" : l.op === "del" ? "removed" : undefined} className={cn("select-none text-center", l.op === "add" ? "text-success" : l.op === "del" ? "text-danger" : "text-faint")}>
        {l.op === "add" ? "+" : l.op === "del" ? "−" : ""}
      </span>
      <span role="cell" className={cn("whitespace-pre-wrap break-words pr-3", l.op === "equal" ? "text-muted" : "text-fg")}>
        {l.text || " "}
      </span>
    </div>
  );
}
