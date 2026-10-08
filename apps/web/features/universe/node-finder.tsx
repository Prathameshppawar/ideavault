"use client";

import { useId, useMemo, useState } from "react";
import { Search } from "lucide-react";
import type { GraphNode } from "@/lib/types";
import { entityMeta, tone } from "@/lib/entities";
import { cn, truncate } from "@/lib/format";
import { searchNodes } from "./graph-model";

/** Combobox to find a node by label/title — the keyboard path into the canvas. */
export function NodeFinder({ nodes, onChoose }: { nodes: GraphNode[]; onChoose: (id: string) => void }) {
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const listId = useId();
  const results = useMemo(() => searchNodes(nodes, q, 8), [nodes, q]);
  const show = open && q.trim().length > 0;

  const choose = (n: GraphNode) => {
    onChoose(n.id);
    setQ("");
    setOpen(false);
  };

  return (
    <div className="relative w-full sm:w-72">
      <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" aria-hidden />
      <input
        type="search"
        role="combobox"
        aria-expanded={show}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={show && results[active] ? `${listId}-${active}` : undefined}
        aria-label="Find a node in the universe"
        placeholder="Find an idea, decision, question…"
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setActive(0);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 120)}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") {
            e.preventDefault();
            setActive((a) => Math.min(a + 1, results.length - 1));
          } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setActive((a) => Math.max(a - 1, 0));
          } else if (e.key === "Enter" && results[active]) {
            e.preventDefault();
            choose(results[active]);
          } else if (e.key === "Escape") {
            if (q) e.stopPropagation();
            setQ("");
            setOpen(false);
          }
        }}
        className="h-8 w-full rounded-[var(--radius-md)] border border-border bg-surface pl-8 pr-3 text-[13px] text-fg placeholder:text-faint hover:border-border-strong focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/15"
      />
      {show && (
        <ul id={listId} role="listbox" className="absolute right-0 top-full z-30 mt-1.5 w-full min-w-[18rem] overflow-hidden rounded-[var(--radius-lg)] border border-border bg-surface p-1 shadow-md">
          {results.length === 0 ? (
            <li className="px-2.5 py-2 text-[13px] text-faint">No matching nodes in this view.</li>
          ) : (
            results.map((n, i) => (
              <li
                key={n.id}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={i === active}
                onMouseDown={(e) => {
                  e.preventDefault();
                  choose(n);
                }}
                onMouseEnter={() => setActive(i)}
                className={cn("flex cursor-pointer items-start gap-2 rounded-[var(--radius-md)] px-2.5 py-1.5", i === active && "bg-surface-2")}
              >
                <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full" style={{ background: tone(n.type) }} aria-hidden />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] text-fg">{n.type === "idea" ? n.label : `${n.label} · ${truncate(n.title || "", 60)}`}</span>
                  <span className="text-[11px] text-faint">{entityMeta(n.type).label}</span>
                </span>
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  );
}
