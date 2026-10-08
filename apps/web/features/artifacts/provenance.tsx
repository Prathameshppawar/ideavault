"use client";

import Link from "next/link";
import { useMemo } from "react";
import { ArrowRight, Bookmark, Sparkles } from "lucide-react";
import { cn, shortDate } from "@/lib/format";
import { entityHref } from "@/lib/entities";
import type { ArtifactVersion, ProvenanceEntry } from "@/lib/types";
import { ItemStatus } from "@/components/ui/badges";
import { RefLabel } from "@/components/domain/knowledge";
import { ErrorState, SectionTitle, SkeletonLines } from "@/components/ui/primitives";
import { deadVerb, groupProvenance, isKnowledgeType, type ProvenanceGroups } from "./artifact-meta";
import { Callout, Select } from "./kit";
import { useDeadItemDetails, useProvenance } from "./queries";

export interface DeadInfo {
  entry: ProvenanceEntry;
  /** The item that replaced it, when it was superseded/reversed. */
  replacedBy?: { id: string; label?: string; kind: string };
  /** The status changed after this version was written (vs. already being history then). */
  changedAfter: boolean;
}

/** For items that no longer stand: what replaced them and whether that happened after the version was written. */
export function useProvenanceHealth(groups: ProvenanceGroups, versionCreatedAt?: string): Map<string, DeadInfo> {
  const ids = groups.dead.map((e) => e.entity_id);
  const results = useDeadItemDetails(ids);
  const written = versionCreatedAt ? Date.parse(versionCreatedAt) : NaN;
  const out = new Map<string, DeadInfo>();
  groups.dead.forEach((entry, i) => {
    const d = results[i]?.data;
    const item = d?.item;
    let replacedBy: DeadInfo["replacedBy"];
    if (item?.superseded_by_id) {
      const r = d?.chain.find((c) => c.id === item.superseded_by_id);
      replacedBy = { id: item.superseded_by_id, label: r?.label, kind: r?.kind ?? entry.entity_type };
    }
    const changedAt = item ? Date.parse(item.updated_at) : NaN;
    out.set(entry.entity_id, { entry, replacedBy, changedAfter: !Number.isNaN(written) && !Number.isNaN(changedAt) && changedAt > written });
  });
  return out;
}

/** "This version relies on D2, which was later reversed (now D3)." */
export function StaleProvenanceCallout({ health, onRegenerate, onOpenSources }: { health: Map<string, DeadInfo>; onRegenerate?: () => void; onOpenSources?: () => void }) {
  const stale = [...health.values()].filter((h) => h.changedAfter);
  if (!stale.length) return null;
  return (
    <Callout
      tone="warning"
      className="mb-8"
      title={stale.length === 1 ? "Some of the thinking behind this has changed" : `${stale.length} items behind this have changed`}
      action={
        onRegenerate && (
          <button type="button" onClick={onRegenerate} className="whitespace-nowrap text-[13px] font-medium text-fg underline decoration-border-strong underline-offset-2 hover:decoration-fg">
            Regenerate
          </button>
        )
      }
    >
      <ul className="space-y-1">
        {stale.map((h) => (
          <li key={h.entry.entity_id}>
            This version relies on{" "}
            <Link href={entityHref(h.entry.entity_type, h.entry.entity_id)} className="font-medium text-fg underline decoration-border-strong underline-offset-2 hover:decoration-fg">
              {h.entry.label}
            </Link>{" "}
            <span className="text-muted">({(h.entry.title ?? "").replace(/[.\s]+$/, "")})</span>, which was later {deadVerb(h.entry.status ?? "")}
            {h.replacedBy && (
              <>
                {" "}
                — now{" "}
                <Link href={entityHref(h.replacedBy.kind, h.replacedBy.id)} className="font-medium text-fg underline decoration-border-strong underline-offset-2 hover:decoration-fg">
                  {h.replacedBy.label ?? "a newer item"}
                </Link>
              </>
            )}
            .
          </li>
        ))}
      </ul>
      {onOpenSources && (
        <button type="button" onClick={onOpenSources} className="mt-1.5 text-[12px] text-muted underline underline-offset-2 hover:text-fg">
          See all sources
        </button>
      )}
    </Callout>
  );
}

/** One provenance entry: label, statement, live status; struck through when it no longer stands. */
export function ProvenanceRow({ entry, dead, compact }: { entry: ProvenanceEntry; dead?: DeadInfo; compact?: boolean }) {
  const isCheckpoint = entry.entity_type === "checkpoint";
  const gone = !!dead;
  return (
    <li>
      <Link
        href={entityHref(entry.entity_type, entry.entity_id)}
        className={cn("group flex items-start gap-2.5 rounded-[var(--radius-md)] px-2 transition-colors hover:bg-surface-2", compact ? "py-1.5" : "py-2")}
      >
        {isCheckpoint ? (
          <span className="inline-flex h-5 shrink-0 items-center gap-1 rounded-[4px] bg-k-checkpoint-soft px-1 font-mono text-[11px] font-semibold text-k-checkpoint">
            <Bookmark className="h-3 w-3" aria-hidden />
            {entry.label}
          </span>
        ) : (
          <RefLabel kind={entry.entity_type} label={entry.label} className={cn(gone && "opacity-60")} />
        )}
        <span className="min-w-0 flex-1">
          <span className={cn("block text-[13px] leading-snug text-fg", compact && "line-clamp-2", gone && "text-muted line-through decoration-faint/70")}>{entry.title || entry.label}</span>
          {!compact && (entry.status || dead) && (
            <span className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5">
              {entry.status && <ItemStatus status={entry.status} />}
              {dead && (
                <span className={cn("text-[11px]", dead.changedAfter ? "font-medium text-warning" : "text-faint")}>
                  {dead.changedAfter ? `${deadVerb(entry.status ?? "")} after this version` : "already history when written"}
                  {dead.replacedBy?.label && (
                    <>
                      {" "}
                      <ArrowRight className="inline h-3 w-3 align-[-2px]" aria-hidden /> {dead.replacedBy.label}
                    </>
                  )}
                </span>
              )}
            </span>
          )}
        </span>
      </Link>
    </li>
  );
}

/** Contents of the Sources sheet: grouped provenance for any version. */
export function ProvenancePanel({
  artifactId,
  versions,
  version,
  onVersionChange,
}: {
  artifactId: string;
  versions: ArtifactVersion[];
  version: number;
  onVersionChange: (v: number) => void;
}) {
  const { data, isLoading, error, refetch } = useProvenance(artifactId, version);
  const groups = useMemo(() => groupProvenance(data), [data]);
  const current = versions[0]?.version ?? 0;
  const v = versions.find((x) => x.version === (version || current));
  const health = useProvenanceHealth(groups, v?.created_at);

  return (
    <div className="px-5 py-5">
      <p className="mb-4 text-[13px] leading-relaxed text-muted">
        The recorded thinking this version was built from. Provenance is captured when a version is created and never rewritten — items that changed since stay visible, struck through.
      </p>
      {versions.length > 1 && (
        <div className="mb-5 flex items-center gap-2">
          <label htmlFor="prov-version" className="text-[13px] text-muted">
            Version
          </label>
          <Select id="prov-version" value={version || current} onChange={(e) => onVersionChange(Number(e.target.value) === current ? 0 : Number(e.target.value))} wrapperClassName="w-56">
            {versions.map((x) => (
              <option key={x.version} value={x.version}>
                v{x.version}
                {x.version === current ? " (current)" : ""} · {shortDate(x.created_at)}
              </option>
            ))}
          </Select>
        </div>
      )}
      {isLoading ? (
        <SkeletonLines lines={6} />
      ) : error ? (
        <ErrorState error={error} onRetry={() => void refetch()} />
      ) : !data?.length ? (
        <p className="rounded-[var(--radius-lg)] border border-dashed border-border px-4 py-6 text-center text-[13px] text-muted">No provenance was recorded for this version.</p>
      ) : (
        <div className="space-y-6">
          <StaleProvenanceCallout health={health} />
          {groups.idea && (
            <p className="flex items-center gap-1.5 text-[13px] text-muted">
              <Sparkles className="h-3.5 w-3.5 text-k-idea" aria-hidden />
              From the idea{" "}
              <Link href={entityHref("idea", groups.idea.entity_id)} className="font-medium text-fg hover:text-accent">
                {groups.idea.title || groups.idea.label}
              </Link>
            </p>
          )}
          <ProvSection title="Base checkpoint" hint="The snapshot of thinking it was generated from." entries={groups.baseCheckpoint} health={health} />
          <ProvSection title="Cited in the text" hint="Referenced explicitly, e.g. [D3]." entries={groups.cited} health={health} />
          <ProvSection title="Also given to the generator" hint="Context it could draw on, without an explicit citation." entries={groups.inputs.filter((e) => isKnowledgeType(e.entity_type) || e.entity_type === "checkpoint")} health={health} />
        </div>
      )}
    </div>
  );
}

function ProvSection({ title, hint, entries, health }: { title: string; hint: string; entries: ProvenanceEntry[]; health: Map<string, DeadInfo> }) {
  if (!entries.length) return null;
  return (
    <section>
      <SectionTitle className="mb-1">
        {title} <span className="ml-1 font-normal normal-case tracking-normal text-faint">{entries.length}</span>
      </SectionTitle>
      <p className="mb-2 text-[12px] text-faint">{hint}</p>
      <ul className="-mx-2">
        {entries.map((e) => (
          <ProvenanceRow key={`${e.role}-${e.entity_id}`} entry={e} dead={health.get(e.entity_id)} />
        ))}
      </ul>
    </section>
  );
}
