"use client";

import { useEffect } from "react";
import { useParams, useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Message } from "@/lib/types";
import { ErrorState, Spinner } from "@/components/ui/primitives";

/** Message links (search hits, chat references) open the conversation scrolled to that message. */
export function MessageRedirect() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const { data, error, refetch } = useQuery({ queryKey: ["message", id], queryFn: () => api.get<Message>(`/v1/messages/${id}`) });

  useEffect(() => {
    if (data) router.replace(`/conversations/${data.conversation_id}#m-${data.id}`);
  }, [data, router]);

  if (error) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-16">
        <ErrorState error={error} onRetry={() => void refetch()} />
      </div>
    );
  }
  return (
    <div className="flex h-app items-center justify-center">
      <Spinner />
    </div>
  );
}
