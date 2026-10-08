"use client";

import type { ReactNode } from "react";
import { ArrowRight, Equal, Minus, Plus, Replace } from "lucide-react";
import type { CheckpointDiff, KnowledgeItem } from "@/lib/types";
import { cn, humanize, plural } from "@/lib/format";
import { ItemStatus } from "@/components/ui/badges";
import { RefLabel } from "@/components/domain/knowledge";
import { CheckpointPill } from "@/features/ideas/ui";
import { isLive } from "@/features/knowledge/lib";

/** Added / removed / changed / superseded between two checkpoints (GET /v1/checkpoints/compare). */
export function CheckpointDiffView({ diff, onSelectItem }: { diff: CheckpointDiff; onSelectItem: (id: string) => void }) {
  const total = diff.added.length + diff.removed.length + diff.changed.length + diff.superseded.length;
  return (
    <div className="space-y-6 animate-fade-in">
      <p className="flex flex-wrap items-center gap-2 text-[13px] text-muted">
        <CheckpointPill label={diff.from.label ?? "CP"} />
        <ArrowRight className="h-3.5 w-3.5 text-faint" aria-hidden />
        <CheckpointPill label={diff.to.label ?? "CP"} />
        <span>
          {total === 0 ? "No differences" : plural(total, "change")} · {plural(diff.unchanged, "item")} unchanged
        </span>
      </p>
      {total === 0 ? null : (
        <div className="grid gap-x-8 gap-y-6 md:grid-cols-2">
          <Group icon={<Plus className="h-3.5 w-3.5" />} title="Added" tone="action" count={diff.added.length}>
            {diff.added.map((it) => (
              <ItemLine key={it.id} item={it} onSelect={onSelectItem} />
            ))}
          </Group>
          <Group icon={<Minus className="h-3.5 w-3.5" />} title="Removed" tone="question" count={diff.removed.length}>
            {diff.removed.map((it) => (
              <ItemLine key={it.id} item={it} onSelect={onSelectItem} struck />
            ))}
          </Group>
          <Group icon={<Equal className="h-3.5 w-3.5" />} title="Changed" tone="assumption" count={diff.changed.length}>
            {diff.changed.map((c) => (
              <li key={c.after.id} className="px-1.5 py-1.5">
                <button type="button" onClick={() => onSelectItem(c.after.id)} className="flex w-full items-start gap-2.5 text-left hover:opacity-80">
                  <RefLabel kind={c.after.kind} label={c.after.label} />
                  <span className="min-w-0 flex-1 text-[13.5px] leading-snug text-fg">{c.after.statement}</span>
                </button>
                <p className="ml-[38px] mt-1 flex flex-wrap items-center gap-1.5 text-[12px] text-muted">
                  {c.fields.includes("status") ? (
                    <>
                      <ItemStatus status={c.before.status} /> <ArrowRight className="h-3 w-3 text-faint" /> <ItemStatus status={c.after.status} />
                    </>
                  ) : (
                    <span>{c.fields.map(humanize).join(", ")} changed</span>
                  )}
                </p>
              </li>
            ))}
          </Group>
          <Group icon={<Replace className="h-3.5 w-3.5" />} title="Superseded" tone="decision" count={diff.superseded.length}>
            {diff.superseded.map((s) => (
              <li key={s.old.id} className="px-1.5 py-1.5">
                <button type="button" onClick={() => onSelectItem(s.old.id)} className="flex w-full items-start gap-2.5 text-left hover:opacity-80">
                  <RefLabel kind={s.old.kind} label={s.old.label} className="opacity-60" />
                  <span className="min-w-0 flex-1 text-[13.5px] leading-snug text-faint line-through decoration-faint/60">{s.old.statement}</span>
                </button>
                <button type="button" onClick={() => onSelectItem(s.new.id)} className="ml-[38px] mt-1 flex items-start gap-2 text-left hover:opacity-80">
                  <ArrowRight className="mt-0.5 h-3 w-3 shrink-0 text-faint" />
                  <RefLabel kind={s.new.kind} label={s.new.label} />
                  <span className="min-w-0 text-[13.5px] leading-snug text-fg">{s.new.statement}</span>
                </button>
              </li>
            ))}
          </Group>
        </div>
      )}
    </div>
  );
}

function Group({ icon, title, tone, count, children }: { icon: ReactNode; title: string; tone: string; count: number; children: ReactNode }) {
  return (
    <section className="min-w-0">
      <h4 className="mb-1.5 flex items-center gap-2 border-b border-border pb-1.5 text-[13px] font-medium text-fg">
        <span className="flex h-5 w-5 items-center justify-center rounded-[4px]" style={{ background: `var(--k-${tone}-soft)`, color: `var(--k-${tone})` }} aria-hidden>
          {icon}
        </span>
        {title}
        <span className="tabular-nums text-faint">{count}</span>
      </h4>
      {count === 0 ? <p className="px-1.5 py-1 text-[13px] text-faint">None.</p> : <ul className="-mx-1.5">{children}</ul>}
    </section>
  );
}

function ItemLine({ item, onSelect, struck }: { item: KnowledgeItem; onSelect: (id: string) => void; struck?: boolean }) {
  return (
    <li>
      <button type="button" onClick={() => onSelect(item.id)} className="flex w-full items-start gap-2.5 rounded-[var(--radius-md)] px-1.5 py-1.5 text-left hover:bg-surface-2">
        <RefLabel kind={item.kind} label={item.label} className={cn((struck || !isLive(item)) && "opacity-60")} />
        <span className={cn("min-w-0 flex-1 text-[13.5px] leading-snug", struck || !isLive(item) ? "text-faint line-through decoration-faint/60" : "text-fg")}>{item.statement}</span>
      </button>
    </li>
  );
}
