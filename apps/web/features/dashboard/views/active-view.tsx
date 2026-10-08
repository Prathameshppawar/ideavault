"use client";

import { useQuery } from "@tanstack/react-query";
import { Lightbulb } from "lucide-react";
import { api } from "@/lib/api";
import type { Idea } from "@/lib/types";
import { plural } from "@/lib/format";
import { IdeaRow } from "@/components/domain/knowledge";
import { EmptyState, ErrorState } from "@/components/ui/primitives";
import { Heading, LinkButton, RowsSkeleton } from "../ui";

/** Most-mature first: what's closest to action leads. */
const ORDER = ["READY_TO_IMPLEMENT", "DECIDED", "ACTIVE", "EXPLORING"] as const;
const LABEL: Record<(typeof ORDER)[number], { title: string; note: string }> = {
  READY_TO_IMPLEMENT: { title: "Ready to implement", note: "Decided enough to build." },
  DECIDED: { title: "Decided", note: "The direction is set; details remain." },
  ACTIVE: { title: "Active", note: "Being worked through right now." },
  EXPLORING: { title: "Exploring", note: "Still open — questions outnumber answers." },
};

export function ActiveView() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["ideas", { status: ORDER.join(","), sort: "recent" }],
    queryFn: () => api.get<{ ideas: Idea[]; total: number }>("/v1/ideas", { query: { status: ORDER.join(","), sort: "recent", limit: 200 } }),
  });
  if (isLoading) return <RowsSkeleton rows={6} />;
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;
  const ideas = data.ideas ?? [];
  if (!ideas.length) {
    return (
      <EmptyState
        icon={Lightbulb}
        title="Nothing in motion"
        description="Ideas you're exploring, deciding or ready to build will gather here."
        action={
          <LinkButton href="/" size="sm" variant="primary">
            Start a new idea
          </LinkButton>
        }
      />
    );
  }
  return (
    <div className="space-y-10">
      <p className="max-w-2xl text-[15px] text-muted">
        <span className="text-fg">{plural(data.total, "idea")}</span> still taking shape
        {ideas.length < data.total ? `, showing the ${ideas.length} most recent` : ""}. Grouped by how close each is to action.
      </p>
      {ORDER.map((status) => {
        const group = ideas.filter((i) => i.status === status);
        if (!group.length) return null;
        return (
          <section key={status} aria-labelledby={`status-${status}`}>
            <Heading id={`status-${status}`} count={group.length} action={<span className="hidden text-[12px] text-faint sm:inline">{LABEL[status].note}</span>}>
              {LABEL[status].title}
            </Heading>
            <div>
              {group.map((idea) => (
                <IdeaRow key={idea.id} idea={idea} />
              ))}
            </div>
          </section>
        );
      })}
    </div>
  );
}
