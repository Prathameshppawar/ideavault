"use client";

import { useEffect } from "react";
import { useParams, useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Branch } from "@/lib/types";
import { ErrorState, Spinner } from "@/components/ui/primitives";

/** /branches/[id] has no page of its own: it opens the idea on that branch. */
export function BranchRedirect() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const { data, error, refetch } = useQuery({ queryKey: ["branch", id], queryFn: () => api.get<Branch>(`/v1/branches/${id}`) });
  useEffect(() => {
    if (data) router.replace(`/ideas/${data.idea_id}?branch=${data.id}`);
  }, [data, router]);
  if (error) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-16">
        <ErrorState error={error} onRetry={() => void refetch()} />
      </div>
    );
  }
  return (
    <div className="flex h-[60vh] flex-col items-center justify-center gap-3 text-[13px] text-muted">
      <Spinner />
      <span>Opening branch…</span>
    </div>
  );
}
