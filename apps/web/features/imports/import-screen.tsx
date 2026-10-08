"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Circle, CircleCheck, CircleSlash, CircleX, Copy, Lightbulb, Loader2, MessagesSquare, Search, ShieldAlert } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api, errorMessage } from "@/lib/api";
import { cn, fullDate, plural, shortDate, timeAgo } from "@/lib/format";
import type { Idea, Import, ImportDetail, ImportItem } from "@/lib/types";
import { EntityChip } from "@/components/ui/badges";
import { Button } from "@/components/ui/button";
import { Checkbox, Switch, Tooltip } from "@/components/ui/overlay";
import { EmptyState, ErrorState, Input, Skeleton, SkeletonLines } from "@/components/ui/primitives";
import { BackLink, Callout, LinkButton, Pill, ProgressBar, Select, TriCheckbox, useIdeaTitles, useIdeas } from "@/features/artifacts/kit";
import {
  adapterLabel,
  buildCommitPayload,
  defaultChoices,
  formatBytes,
  importProgress,
  importSourceLabel,
  importStatusMeta,
  isLiveImport,
  isSelectable,
  providerName,
  selectedCount,
  type Destination,
  type ItemChoice,
} from "./import-logic";

const SOURCE_KIND: Record<string, string> = { file: "File upload", paste: "Pasted text", url: "Share link" };

export function ImportScreen() {
  const { id } = useParams<{ id: string }>();
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["import", id],
    queryFn: () => api.get<ImportDetail>(`/v1/imports/${id}`),
    refetchInterval: (q) => (isLiveImport(q.state.data?.import?.status) ? 1000 : false),
  });

  if (isLoading) {
    return (
      <div className="mx-auto max-w-4xl px-5 pt-8 sm:px-8 sm:pt-10" aria-busy="true">
        <Skeleton className="h-3.5 w-28" />
        <Skeleton className="mt-6 h-10 w-1/2" />
        <Skeleton className="mt-4 h-4 w-1/3" />
        <div className="mt-10">
          <SkeletonLines lines={6} />
        </div>
      </div>
    );
  }
  if (error || !data?.import) {
    return (
      <div className="mx-auto max-w-3xl px-5 py-16 sm:px-8">
        {error instanceof ApiError && error.status === 404 ? (
          <EmptyState title="This import doesn't exist" action={<LinkButton href="/imports">Import Center</LinkButton>} />
        ) : (
          <ErrorState error={error} onRetry={() => void refetch()} />
        )}
      </div>
    );
  }

  const im = data.import;
  const items = [...(data.items ?? [])].sort((a, b) => a.position - b.position);
  const s = importStatusMeta(im.status);
  const live = isLiveImport(im.status);
  const sub = [SOURCE_KIND[im.source_kind] ?? im.source_kind, providerName(im.provider) || adapterLabel(im.adapter), im.byte_size ? formatBytes(im.byte_size) : ""].filter(Boolean);

  return (
    <div className="mx-auto max-w-4xl px-5 pb-10 pt-8 sm:px-8 sm:pt-10">
      <BackLink href="/imports">Import Center</BackLink>
      <header className="mt-5">
        <div className="mb-2 flex flex-wrap items-center gap-2.5">
          <span className="text-[12px] text-faint">
            {sub.join(" · ")}
            {im.adapter && (
              <span className="ml-1.5 rounded-[3px] border border-border px-1 font-mono text-[10.5px]" title="Import adapter">
                {im.adapter}
              </span>
            )}
          </span>
          <Pill tone={s.tone} pulse={live}>
            {s.label}
          </Pill>
        </div>
        <h1 className="break-words font-display text-[2rem] leading-[1.1] text-fg sm:text-[2.4rem]">{importSourceLabel(im)}</h1>
        <p className="mt-2 text-[13px] text-muted">
          Started <span title={fullDate(im.created_at)}>{timeAgo(im.created_at)}</span>
          {im.completed_at && <> · finished {timeAgo(im.completed_at)}</>}
          {im.source_kind === "url" && im.uri && (
            <>
              {" "}
              ·{" "}
              <a href={im.uri} target="_blank" rel="noopener noreferrer nofollow" className="underline decoration-border-strong underline-offset-2 hover:text-fg">
                open original
              </a>
            </>
          )}
        </p>
      </header>

      {im.status === "FAILED" && <FailedNotice im={im} />}
      {im.warnings?.length > 0 && (
        <Callout tone="warning" className="mt-6" title={plural(im.warnings.length, "warning")}>
          <ul className="list-disc space-y-0.5 pl-4">
            {im.warnings.map((w, i) => (
              <li key={i}>{w}</li>
            ))}
          </ul>
        </Callout>
      )}

      <div className="mt-8">
        {im.status === "QUEUED" || (im.status === "PROCESSING" && im.stage !== "commit") ? (
          <ParsingProgress im={im} />
        ) : im.status === "PREVIEW" ? (
          <PreviewSelection key={im.id} im={im} items={items} />
        ) : im.status === "PROCESSING" || im.status === "COMPLETED" || im.status === "PARTIAL" || (im.status === "FAILED" && items.some((i) => i.status !== "PENDING")) ? (
          <Results im={im} items={items} />
        ) : null}
      </div>
    </div>
  );
}

function FailedNotice({ im }: { im: Import }) {
  return (
    <div className="mt-6 rounded-[var(--radius-lg)] border border-danger/30 bg-danger-soft px-5 py-4" role="alert">
      <p className="flex items-center gap-2 text-[13px] font-semibold text-danger">
        <CircleX className="h-4 w-4" aria-hidden /> This import didn&apos;t go through
      </p>
      <p className="mt-2 whitespace-pre-line text-[15px] leading-relaxed text-fg">{im.error || "Something went wrong while reading this input."}</p>
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <LinkButton href="/imports" size="sm" variant="primary">
          Try another file or link
        </LinkButton>
        {im.source_kind === "url" && (
          <LinkButton href="/imports?tab=upload" size="sm">
            Upload the official export instead
          </LinkButton>
        )}
      </div>
    </div>
  );
}

function ParsingProgress({ im }: { im: Import }) {
  return (
    <div className="rounded-[var(--radius-lg)] border border-border bg-surface px-5 py-5" aria-live="polite">
      <div className="flex items-center gap-3">
        <Loader2 className="h-4 w-4 animate-spin text-muted" aria-hidden />
        <p className="text-[14px] font-medium text-fg">{im.status === "QUEUED" ? "Waiting for a worker…" : im.source_kind === "url" ? "Fetching the shared conversation…" : "Reading the export…"}</p>
      </div>
      <ProgressBar className="mt-4" tone="info" label="Reading the import" />
      <p className="mt-3 text-[12.5px] text-muted">Detecting the format, finding conversations, checking for duplicates and instruction-like text. Large exports can take a minute — this page updates on its own.</p>
    </div>
  );
}

// ---------- preview ----------

const destValue = (d: Destination) => (d.mode === "idea" ? `idea:${d.ideaId}` : d.mode);

function PreviewSelection({ im, items }: { im: Import; items: ImportItem[] }) {
  const qc = useQueryClient();
  const ideas = useIdeas();
  const [choices, setChoices] = useState<Record<string, ItemChoice>>(() => defaultChoices(items, im.target_idea_id));
  const [extract, setExtract] = useState(true);
  const [filter, setFilter] = useState("");

  const selectable = items.filter(isSelectable);
  const failed = items.filter((i) => !isSelectable(i));
  const nSelected = selectedCount(items, choices);
  const visible = filter.trim() ? selectable.filter((i) => i.title.toLowerCase().includes(filter.trim().toLowerCase())) : selectable;
  const allState = nSelected === 0 ? "none" : nSelected === selectable.length ? "all" : "some";
  const anyIdea = selectable.some((i) => choices[i.id]?.include && choices[i.id]?.dest.mode !== "store");

  const update = (id: string, patch: Partial<ItemChoice>) => setChoices((c) => ({ ...c, [id]: { ...c[id], ...patch } }));
  const parseDest = (v: string, it: ImportItem): Destination => (v === "new" ? { mode: "new", title: it.title } : v === "store" ? { mode: "store" } : { mode: "idea", ideaId: v.slice(5) });

  const commit = useMutation({
    mutationFn: () => api.post<Import>(`/v1/imports/${im.id}/commit`, buildCommitPayload(items, choices, extract)),
    onSuccess: (r) => {
      qc.setQueryData<ImportDetail>(["import", im.id], (d) => ({ import: r, items: (d?.items ?? items).map((i) => (choices[i.id]?.include && isSelectable(i) ? { ...i, status: "SELECTED" } : i)) }));
      void qc.invalidateQueries({ queryKey: ["import", im.id] });
      void qc.invalidateQueries({ queryKey: ["imports"] });
      void qc.invalidateQueries({ queryKey: ["ideas"] });
      window.scrollTo({ top: 0, behavior: "smooth" });
    },
    onError: (e) => toast.error("Couldn't start the import", { description: errorMessage(e) }),
  });

  return (
    <div>
      <div className="mb-5">
        <h2 className="text-[17px] font-semibold text-fg">
          Found {plural(selectable.length, "conversation")}
          {failed.length > 0 && <span className="font-normal text-danger"> · {failed.length} couldn&apos;t be read</span>}
        </h2>
        <p className="mt-1 text-[13.5px] text-muted">Choose what to bring in and where each conversation goes. Nothing is saved until you import.</p>
      </div>

      <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-3 rounded-[var(--radius-lg)] border border-border bg-surface px-4 py-3">
        <label className="flex items-center gap-2.5 text-[13px] text-fg">
          <TriCheckbox
            state={allState}
            onChange={(on) => setChoices((c) => Object.fromEntries(Object.entries(c).map(([k, v]) => [k, { ...v, include: on && isSelectable(items.find((i) => i.id === k)!) }])))}
            label="Select all conversations"
          />
          <span>
            <span className="font-medium">{nSelected}</span> of {selectable.length} selected
          </span>
        </label>
        {selectable.length > 1 && (
          <div className="flex min-w-0 items-center gap-2">
            <label htmlFor="bulk-dest" className="shrink-0 text-[13px] text-muted">
              Send selected to
            </label>
            <Select
              id="bulk-dest"
              value=""
              wrapperClassName="w-48"
              className="h-8 text-[13px]"
              onChange={(e) => {
                const v = e.target.value;
                if (!v) return;
                setChoices((c) => {
                  const next = { ...c };
                  for (const it of selectable) if (next[it.id]?.include) next[it.id] = { ...next[it.id], dest: parseDest(v, it) };
                  return next;
                });
              }}
            >
              <option value="">Choose…</option>
              <option value="new">A new idea each</option>
              <option value="store">Just store them</option>
              {ideas.data && ideas.data.length > 0 && (
                <optgroup label="An existing idea">
                  {ideas.data.map((i) => (
                    <option key={i.id} value={`idea:${i.id}`}>
                      {i.title}
                    </option>
                  ))}
                </optgroup>
              )}
            </Select>
          </div>
        )}
        {selectable.length > 6 && (
          <div className="relative ml-auto w-full sm:w-56">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" aria-hidden />
            <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter by title" className="h-8 pl-8 text-[13px]" aria-label="Filter conversations" />
          </div>
        )}
      </div>

      <ul>
        {visible.map((it) => (
          <PreviewRow key={it.id} item={it} choice={choices[it.id]} ideas={ideas.data ?? []} onChange={(p) => update(it.id, p)} parseDest={parseDest} />
        ))}
        {failed.map((it) => (
          <li key={it.id} className="flex gap-3 border-b border-border py-4 opacity-80">
            <CircleX className="mt-0.5 h-4 w-4 shrink-0 text-danger" aria-hidden />
            <div className="min-w-0">
              <p className="text-[14px] font-medium text-fg">{it.title || "Unreadable conversation"}</p>
              <p className="mt-0.5 text-[12.5px] text-danger">{it.error || "This conversation couldn't be read."}</p>
            </div>
          </li>
        ))}
        {visible.length === 0 && filter && <li className="py-6 text-center text-[13px] text-muted">No conversation titles match “{filter}”.</li>}
      </ul>

      <div className="sticky bottom-0 z-20 -mx-5 mt-6 border-t border-border bg-bg px-5 py-4 sm:-mx-8 sm:px-8">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
          <label className="flex min-w-[15rem] flex-1 items-start gap-3">
            <Switch checked={extract} onCheckedChange={setExtract} className="mt-0.5" aria-describedby="extract-help" />
            <span className="min-w-0">
              <span className="block text-[13.5px] font-medium text-fg">Extract knowledge as proposals</span>
              <span id="extract-help" className="block text-[12px] leading-snug text-muted">
                {anyIdea ? "Decisions, assumptions and questions are proposed for review on the idea page — nothing counts until you accept it." : "Only applies to conversations added to an idea."}
              </span>
            </span>
          </label>
          <Button variant="primary" size="lg" onClick={() => commit.mutate()} disabled={nSelected === 0} loading={commit.isPending}>
            Import {plural(nSelected, "conversation")} <ArrowRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}

function PreviewRow({
  item: it,
  choice,
  ideas,
  onChange,
  parseDest,
}: {
  item: ImportItem;
  choice: ItemChoice | undefined;
  ideas: Idea[];
  onChange: (p: Partial<ItemChoice>) => void;
  parseDest: (v: string, it: ImportItem) => Destination;
}) {
  const [expanded, setExpanded] = useState(false);
  const include = !!choice?.include;
  const dup = it.status === "DUPLICATE";
  const flagged = (it.injection_flags ?? []).length > 0;
  const preview = it.preview ?? [];
  const shown = expanded ? preview : preview.slice(0, 2);
  const cb = `inc-${it.id}`;
  return (
    <li className={cn("flex gap-3 border-b border-border py-4 transition-opacity", !include && "opacity-70")}>
      <Checkbox id={cb} checked={include} onCheckedChange={(v) => onChange({ include: v === true })} className="mt-1" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <label htmlFor={cb} className="min-w-0 cursor-pointer text-[15px] font-medium leading-snug text-fg">
            {it.title || "Untitled conversation"}
          </label>
          {dup && (
            <span className="inline-flex h-5 items-center gap-1 rounded-[4px] bg-surface-3 px-1.5 text-[11px] font-medium text-muted">
              <Copy className="h-3 w-3" aria-hidden /> Duplicate
            </span>
          )}
          {flagged && (
            <Tooltip content={`Detected: ${it.injection_flags.join(", ")}`}>
              <span className="inline-flex h-5 items-center gap-1 rounded-[4px] bg-warning-soft px-1.5 text-[11px] font-medium text-warning" tabIndex={0}>
                <ShieldAlert className="h-3 w-3" aria-hidden /> Instruction-like text
              </span>
            </Tooltip>
          )}
        </div>
        <p className="mt-0.5 text-[12px] text-faint">
          {plural(it.message_count, "message")}
          {it.started_at && <> · {shortDate(it.started_at)}</>}
        </p>
        {dup && (
          <p className="mt-1.5 text-[12.5px] text-muted">
            Already in your vault
            {it.duplicate_of && (
              <>
                {" — "}
                <Link href={`/conversations/${it.duplicate_of}`} className="text-fg underline decoration-border-strong underline-offset-2 hover:decoration-fg">
                  open the existing conversation
                </Link>
              </>
            )}
            . Tick it to import a second copy.
          </p>
        )}
        {flagged && (
          <p className="mt-1.5 text-[12.5px] text-warning">This conversation contains instruction-like text; it will be treated purely as data and never followed.</p>
        )}

        {preview.length > 0 && (
          <div className="mt-2.5 space-y-1 rounded-[var(--radius-md)] bg-surface-2 px-3 py-2">
            {shown.map((m, i) => (
              <p key={i} className="line-clamp-2 text-[12.5px] leading-relaxed text-muted">
                <span className="mr-1.5 font-semibold uppercase tracking-wide text-faint">{m.role === "user" ? "You" : m.role === "assistant" ? "AI" : m.role}</span>
                {m.excerpt}
              </p>
            ))}
            {preview.length > 2 && (
              <button type="button" onClick={() => setExpanded((v) => !v)} className="text-[12px] font-medium text-muted hover:text-fg">
                {expanded ? "Show less" : `Show ${preview.length - 2} more`}
              </button>
            )}
          </div>
        )}

        {include && choice && (
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <label htmlFor={`dest-${it.id}`} className="text-[12.5px] text-muted">
              Goes to
            </label>
            <Select id={`dest-${it.id}`} value={destValue(choice.dest)} onChange={(e) => onChange({ dest: parseDest(e.target.value, it) })} wrapperClassName="w-full sm:w-56" className="h-8 text-[13px]">
              <option value="new">A new idea</option>
              <option value="store">Just store the conversation</option>
              {ideas.length > 0 && (
                <optgroup label="An existing idea">
                  {ideas.map((i) => (
                    <option key={i.id} value={`idea:${i.id}`}>
                      {i.title}
                    </option>
                  ))}
                </optgroup>
              )}
            </Select>
            {choice.dest.mode === "new" && (
              <Input
                aria-label="New idea title"
                value={choice.dest.title}
                onChange={(e) => onChange({ dest: { mode: "new", title: e.target.value } })}
                placeholder="Idea title"
                className="h-8 min-w-0 flex-1 text-[13px] sm:min-w-[200px]"
              />
            )}
          </div>
        )}
      </div>
    </li>
  );
}

// ---------- results ----------

function Results({ im, items }: { im: Import; items: ImportItem[] }) {
  const qc = useQueryClient();
  const titles = useIdeaTitles();
  const committing = im.status === "PROCESSING";
  // Ideas created by the background commit aren't in the cached ideas list yet: refresh once it finishes.
  const missingIdea = !committing && items.some((i) => i.idea_id && !titles.has(i.idea_id));
  useEffect(() => {
    if (missingIdea) void qc.invalidateQueries({ queryKey: ["ideas"] });
  }, [missingIdea, qc]);
  const progress = importProgress(im);
  const imported = items.filter((i) => i.status === "IMPORTED");
  const failed = items.filter((i) => i.status === "FAILED");
  const proposals = imported.reduce((n, i) => n + (i.extracted_count || 0), 0);
  const ideaIds = [...new Set(imported.map((i) => i.idea_id).filter((x): x is string => !!x))];

  return (
    <div>
      {committing ? (
        <div className="mb-6 rounded-[var(--radius-lg)] border border-border bg-surface px-5 py-4" aria-live="polite">
          <div className="flex items-center justify-between gap-3 text-[14px]">
            <span className="flex items-center gap-2.5 font-medium text-fg">
              <Loader2 className="h-4 w-4 animate-spin text-muted" aria-hidden /> Importing conversations…
            </span>
            <span className="font-mono text-[12px] text-faint">
              {im.processed_items + im.failed_items} / {im.total_items}
            </span>
          </div>
          <ProgressBar className="mt-3" value={progress ?? undefined} tone="info" label="Import progress" />
        </div>
      ) : im.status === "COMPLETED" ? (
        <Callout tone="success" className="mb-6" title={`Imported ${plural(imported.length, "conversation")}`}>
          {proposals > 0
            ? `${plural(proposals, "knowledge item")} proposed for review — open the idea to accept or reject them.`
            : ideaIds.length
              ? "Open the idea to keep thinking."
              : "Stored as untrusted sources you can search and attach to ideas later."}
        </Callout>
      ) : im.status === "PARTIAL" ? (
        <Callout tone="warning" className="mb-6" title={`Imported ${imported.length} of ${imported.length + failed.length} — ${plural(failed.length, "conversation")} failed`}>
          The rest were saved. Failed conversations are listed below with the reason.
        </Callout>
      ) : null}

      {!committing && ideaIds.length > 0 && (
        <div className="mb-6 flex flex-wrap items-center gap-2 text-[13px] text-muted">
          <Lightbulb className="h-3.5 w-3.5 text-k-idea" aria-hidden /> Ideas:
          {ideaIds.map((iid) => (
            <EntityChip key={iid} type="idea" id={iid} label={titles.get(iid) ?? "Idea"} />
          ))}
        </div>
      )}

      <ul className="border-t border-border">
        {items.map((it) => (
          <ResultRow key={it.id} item={it} ideaTitle={it.idea_id ? titles.get(it.idea_id) : undefined} committing={committing} />
        ))}
      </ul>
    </div>
  );
}

function ResultRow({ item: it, ideaTitle, committing }: { item: ImportItem; ideaTitle?: string; committing: boolean }) {
  const st = it.status;
  const icon =
    st === "IMPORTED" ? (
      <CircleCheck className="h-4 w-4 text-success" aria-label="Imported" />
    ) : st === "FAILED" ? (
      <CircleX className="h-4 w-4 text-danger" aria-label="Failed" />
    ) : st === "SELECTED" && committing ? (
      <Loader2 className="h-4 w-4 animate-spin text-muted" aria-label="Importing" />
    ) : st === "SKIPPED" || st === "DUPLICATE" ? (
      <CircleSlash className="h-4 w-4 text-faint" aria-label="Skipped" />
    ) : (
      <Circle className="h-4 w-4 text-faint" aria-label="Not imported" />
    );
  return (
    <li className="flex gap-3 border-b border-border py-3.5">
      <span className="mt-0.5 shrink-0">{icon}</span>
      <div className="min-w-0 flex-1">
        <p className={cn("text-[14px] font-medium leading-snug", st === "IMPORTED" || st === "FAILED" || st === "SELECTED" ? "text-fg" : "text-muted")}>{it.title || "Untitled conversation"}</p>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[12.5px] text-muted">
          {st === "IMPORTED" && (
            <>
              {it.conversation_id && (
                <Link href={`/conversations/${it.conversation_id}`} className="inline-flex items-center gap-1 text-fg hover:text-accent">
                  <MessagesSquare className="h-3.5 w-3.5" aria-hidden /> Open conversation
                </Link>
              )}
              {it.idea_id ? <EntityChip type="idea" id={it.idea_id} label={ideaTitle ?? "Idea"} /> : <span className="text-faint">Stored without an idea</span>}
              {it.idea_id &&
                (it.extracted_count > 0 ? (
                  <Link href={`/ideas/${it.idea_id}`} className="inline-flex items-center gap-1 font-medium text-warning hover:underline">
                    {plural(it.extracted_count, "proposal")} to review on the idea page <ArrowRight className="h-3 w-3" aria-hidden />
                  </Link>
                ) : (
                  <span className="text-faint">No knowledge extracted</span>
                ))}
            </>
          )}
          {st === "FAILED" && <span className="text-danger">{it.error || "Failed to import."}</span>}
          {st === "SELECTED" && <span>{committing ? "Importing…" : "Selected"}</span>}
          {(st === "SKIPPED" || st === "PENDING") && <span className="text-faint">{st === "SKIPPED" ? "Skipped" : "Not imported"}</span>}
          {st === "DUPLICATE" && (
            <span className="text-faint">
              Skipped — already in your vault
              {it.duplicate_of && (
                <>
                  {" "}
                  ·{" "}
                  <Link href={`/conversations/${it.duplicate_of}`} className="underline underline-offset-2 hover:text-fg">
                    open existing
                  </Link>
                </>
              )}
            </span>
          )}
          <span className="text-faint">{plural(it.message_count, "message")}</span>
        </div>
      </div>
    </li>
  );
}
