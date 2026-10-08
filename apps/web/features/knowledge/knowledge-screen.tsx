"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { ChevronRight } from "lucide-react";
import { entityMeta } from "@/lib/entities";
import { Skeleton } from "@/components/ui/primitives";
import { useIdeaOverview, useKnowledgeExplanation } from "@/features/ideas/hooks";
import { KnowledgeExplanationView } from "./knowledge-explanation";

/** Full-page explanation of a knowledge item, with a breadcrumb back to its idea. */
export function KnowledgeScreen() {
  const { id } = useParams<{ id: string }>();
  const ex = useKnowledgeExplanation(id);
  const item = ex.data?.item;
  const idea = useIdeaOverview(item?.idea_id, item?.branch_id ?? null);
  const branch = idea.data?.branch;
  const branchQuery = branch && !branch.is_default ? `branch=${branch.id}&` : "";

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-8 sm:px-8 sm:py-10">
      <nav aria-label="Breadcrumb" className="mb-6 flex min-w-0 flex-wrap items-center gap-1 text-[13px] text-muted">
        <Link href="/ideas" className="hover:text-fg">
          Ideas
        </Link>
        <ChevronRight className="h-3.5 w-3.5 text-faint" aria-hidden />
        {item ? (
          idea.data?.idea ? (
            <Link href={`/ideas/${item.idea_id}?${branchQuery}tab=knowledge`} className="max-w-[20rem] truncate hover:text-fg">
              {idea.data.idea.title}
            </Link>
          ) : (
            <Skeleton className="h-3.5 w-32" />
          )
        ) : (
          <Skeleton className="h-3.5 w-32" />
        )}
        {branch && !branch.is_default && <span className="text-faint">· {branch.name}</span>}
        <ChevronRight className="h-3.5 w-3.5 text-faint" aria-hidden />
        <span className="text-fg">{item ? `${item.label} · ${entityMeta(item.kind).label}` : "…"}</span>
      </nav>
      <div className="-mx-5 overflow-hidden rounded-[var(--radius-xl)] border border-border bg-surface sm:mx-0">
        <KnowledgeExplanationView id={id} />
      </div>
    </div>
  );
}
