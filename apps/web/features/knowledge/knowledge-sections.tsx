"use client";

import { useMemo } from "react";
import { KNOWLEDGE_KINDS, type KnowledgeItem, type KnowledgeKind } from "@/lib/types";
import { entityMeta, tone } from "@/lib/entities";
import { cn } from "@/lib/format";
import { KnowledgeRow } from "@/components/domain/knowledge";
import { groupByKind, isLive, KIND_BLURB } from "./lib";

/**
 * The six knowledge kinds as sections with counts. Superseded/reversed/rejected items are hidden
 * unless `showHistory`, and then shown struck-through with a pointer to their replacement.
 */
export function KnowledgeSections({
  items,
  showHistory,
  onSelect,
  kinds = KNOWLEDGE_KINDS,
  hideEmpty,
  className,
}: {
  items: KnowledgeItem[];
  showHistory: boolean;
  onSelect: (item: KnowledgeItem) => void;
  kinds?: KnowledgeKind[];
  hideEmpty?: boolean;
  className?: string;
}) {
  const grouped = useMemo(() => groupByKind(items), [items]);
  const byId = useMemo(() => new Map(items.map((i) => [i.id, i])), [items]);
  return (
    <div className={cn("space-y-9", className)}>
      {kinds.map((k) => {
        const all = grouped[k];
        const live = all.filter(isLive);
        const shown = showHistory ? all : live;
        const hidden = all.length - live.length;
        if (hideEmpty && shown.length === 0) return null;
        return (
          <section key={k} aria-labelledby={`ks-${k}`}>
            <div className="mb-2 flex items-baseline justify-between gap-3 border-b border-border pb-2">
              <h3 id={`ks-${k}`} className="flex items-baseline gap-2">
                <span className="text-[13px] font-semibold uppercase tracking-[0.06em]" style={{ color: tone(k) }}>
                  {entityMeta(k).plural}
                </span>
                <span className="text-[13px] tabular-nums text-faint">{live.length}</span>
                <span className="hidden text-[12px] text-faint sm:inline">· {KIND_BLURB[k]}</span>
              </h3>
              {hidden > 0 && <span className="text-[11.5px] text-faint">{showHistory ? `incl. ${hidden} from history` : `${hidden} in history`}</span>}
            </div>
            {shown.length === 0 ? (
              <p className="px-2.5 py-2 text-[13px] text-faint">None {all.length ? "live" : "yet"}.</p>
            ) : (
              <div className="-mx-1">
                {shown.map((it) => {
                  const repl = it.superseded_by_id ? byId.get(it.superseded_by_id) : undefined;
                  return (
                    <KnowledgeRow
                      key={it.id}
                      item={it}
                      onClick={() => onSelect(it)}
                      trailing={
                        repl ? (
                          <span className="mt-0.5 shrink-0 text-[11px] text-faint">
                            → <span className="font-mono font-semibold" style={{ color: tone(repl.kind) }}>{repl.label}</span>
                          </span>
                        ) : undefined
                      }
                    />
                  );
                })}
              </div>
            )}
          </section>
        );
      })}
    </div>
  );
}
