"use client";

import { useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useInfiniteQuery } from "@tanstack/react-query";
import { ArrowDownWideNarrow, Lightbulb, MessageSquare, Plus, Search, X } from "lucide-react";
import { api } from "@/lib/api";
import type { Idea } from "@/lib/types";
import { IDEA_STATUSES } from "@/lib/types";
import { humanize, plural } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Dropdown, DropdownContent, DropdownItem, DropdownLabel, DropdownTrigger } from "@/components/ui/overlay";
import { EmptyState, ErrorState, Input, PageHeader, Skeleton } from "@/components/ui/primitives";
import { IdeaRow } from "@/components/domain/knowledge";
import { Chip } from "./ui";
import { NewIdeaDialog } from "./new-idea-dialog";
import { useDebounced } from "./hooks";
import { OPEN_STATUSES } from "./lib/lifecycle";

const PAGE = 40;

const SORTS = [
  { value: "recent", label: "Recently active" },
  { value: "created", label: "Newest first" },
  { value: "title", label: "Title A–Z" },
  { value: "momentum", label: "Most momentum" },
] as const;

const STATUS_TONE: Record<string, string> = {
  EXPLORING: "insight",
  ACTIVE: "branch",
  DECIDED: "decision",
  READY_TO_IMPLEMENT: "action",
  CONCLUDED: "checkpoint",
  PARKED: "assumption",
  ABANDONED: "question",
  MERGED: "conversation",
};

export function IdeasIndexScreen() {
  const sp = useSearchParams();
  const pathname = usePathname();
  const router = useRouter();
  const status = sp.get("status") ?? "all";
  const sort = SORTS.some((s) => s.value === sp.get("sort")) ? (sp.get("sort") as string) : "recent";
  const [q, setQ] = useState(sp.get("q") ?? "");
  const dq = useDebounced(q.trim(), 250);

  const setParam = (patch: Record<string, string | null>) => {
    const p = new URLSearchParams(window.location.search);
    for (const [k, v] of Object.entries(patch)) {
      if (!v) p.delete(k);
      else p.set(k, v);
    }
    const s = p.toString();
    window.history.replaceState(null, "", `${pathname}${s ? `?${s}` : ""}`);
  };

  const statusQuery = status === "all" ? undefined : status === "open" ? OPEN_STATUSES.join(",") : status;
  const list = useInfiniteQuery({
    queryKey: ["ideas", { status: statusQuery ?? "all", q: dq, sort }],
    queryFn: ({ pageParam }) => api.get<{ ideas: Idea[]; total: number }>("/v1/ideas", { query: { status: statusQuery, q: dq || undefined, sort, limit: PAGE, offset: pageParam } }),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.ideas.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
    placeholderData: (prev) => prev,
  });

  const ideas = list.data?.pages.flatMap((p) => p.ideas) ?? [];
  const total = list.data?.pages[0]?.total ?? 0;
  const filtered = status !== "all" || dq.length > 0;
  const sortLabel = SORTS.find((s) => s.value === sort)?.label;

  const onQ = (v: string) => {
    setQ(v);
    setParam({ q: v.trim() || null });
  };

  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-8 sm:px-8 sm:py-10">
      <PageHeader
        title="Ideas"
        description="Every idea space you've opened — with its decisions, branches and full history."
        actions={
          <NewIdeaDialog
            trigger={
              <Button variant="primary">
                <Plus className="h-4 w-4" /> New idea
              </Button>
            }
          />
        }
      />

      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="relative min-w-[200px] flex-1 sm:max-w-sm">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" aria-hidden />
          <Input value={q} onChange={(e) => onQ(e.target.value)} placeholder="Filter ideas…" className="pl-8 pr-8" aria-label="Filter ideas by text" />
          {q && (
            <button type="button" onClick={() => onQ("")} className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-faint hover:text-fg" aria-label="Clear filter">
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
        <Dropdown>
          <DropdownTrigger asChild>
            <Button variant="secondary" size="md" aria-label={`Sort: ${sortLabel}`}>
              <ArrowDownWideNarrow className="h-3.5 w-3.5 text-muted" /> {sortLabel}
            </Button>
          </DropdownTrigger>
          <DropdownContent align="end">
            <DropdownLabel>Sort by</DropdownLabel>
            {SORTS.map((s) => (
              <DropdownItem key={s.value} onSelect={() => setParam({ sort: s.value === "recent" ? null : s.value })}>
                {s.label}
                {s.value === sort && <span className="ml-auto text-[11px] text-faint">current</span>}
              </DropdownItem>
            ))}
          </DropdownContent>
        </Dropdown>
      </div>

      <div className="-mx-4 mb-6 flex gap-1.5 overflow-x-auto px-4 pb-1 sm:mx-0 sm:flex-wrap sm:px-0" role="toolbar" aria-label="Filter by status">
        <Chip active={status === "all"} onClick={() => setParam({ status: null })}>
          All
        </Chip>
        <Chip active={status === "open"} onClick={() => setParam({ status: "open" })}>
          Open
        </Chip>
        <span className="mx-1 w-px shrink-0 self-stretch bg-border" aria-hidden />
        {IDEA_STATUSES.map((s) => (
          <Chip key={s} active={status === s} tone={STATUS_TONE[s]} onClick={() => setParam({ status: status === s ? null : s })}>
            {humanize(s)}
          </Chip>
        ))}
      </div>

      {list.isLoading ? (
        <div className="divide-y divide-border">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="space-y-2 py-5">
              <Skeleton className="h-6 w-1/3" />
              <Skeleton className="h-3.5 w-2/3" />
              <Skeleton className="h-3 w-1/4" />
            </div>
          ))}
        </div>
      ) : list.error ? (
        <ErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : ideas.length === 0 ? (
        filtered ? (
          <EmptyState
            icon={Search}
            title="No ideas match"
            description={dq ? `Nothing matches “${dq}” with the current filters.` : "No ideas have this status yet."}
            action={
              <Button
                size="sm"
                variant="secondary"
                onClick={() => {
                  setQ("");
                  setParam({ q: null, status: null });
                }}
              >
                Clear filters
              </Button>
            }
          />
        ) : (
          <EmptyState
            icon={Lightbulb}
            title="No ideas yet"
            description="The easiest way to start is to talk it through in chat — IdeaVault opens an idea space and keeps track of what you decide."
            action={
              <div className="flex flex-wrap justify-center gap-2">
                <Button variant="primary" size="sm" onClick={() => router.push("/")}>
                  <MessageSquare className="h-3.5 w-3.5" /> Start in chat
                </Button>
                <NewIdeaDialog
                  trigger={
                    <Button variant="secondary" size="sm">
                      <Plus className="h-3.5 w-3.5" /> New idea
                    </Button>
                  }
                />
              </div>
            }
          />
        )
      ) : (
        <>
          <p className="mb-1 text-[12px] text-faint" aria-live="polite">
            {plural(total, "idea")}
            {filtered ? " match" : ""}
            {list.isFetching && !list.isFetchingNextPage ? " · updating…" : ""}
          </p>
          <div className="border-t border-border">
            {ideas.map((idea) => (
              <IdeaRow key={idea.id} idea={idea} />
            ))}
          </div>
          {list.hasNextPage && (
            <div className="mt-6 flex justify-center">
              <Button variant="secondary" size="sm" onClick={() => void list.fetchNextPage()} loading={list.isFetchingNextPage}>
                Show more
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
