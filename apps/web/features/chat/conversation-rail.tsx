"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { MessageSquarePlus } from "lucide-react";
import { api } from "@/lib/api";
import type { Conversation } from "@/lib/types";
import { cn, shortDate } from "@/lib/format";
import { Skeleton } from "@/components/ui/primitives";

/** Recent native conversations (chat history). */
export function ConversationRail() {
  const pathname = usePathname();
  const { data, isLoading } = useQuery({
    queryKey: ["conversations", "native"],
    queryFn: () => api.get<Conversation[]>("/v1/conversations", { query: { origin: "native", limit: 40 } }),
  });
  return (
    <aside className="hidden w-[260px] shrink-0 flex-col border-r border-border lg:flex" aria-label="Recent conversations">
      <div className="flex h-14 items-center justify-between px-4">
        <span className="text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">Chats</span>
        <Link href="/" className="rounded p-1.5 text-faint hover:bg-surface-2 hover:text-fg" aria-label="New chat" title="New chat">
          <MessageSquarePlus className="h-4 w-4" />
        </Link>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
        {isLoading && (
          <div className="space-y-2 px-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-9" />
            ))}
          </div>
        )}
        {data?.length === 0 && <p className="px-3 py-4 text-[13px] text-faint">Your conversations will appear here.</p>}
        {data?.map((c) => {
          const active = pathname === `/conversations/${c.id}`;
          return (
            <Link
              key={c.id}
              href={`/conversations/${c.id}`}
              className={cn("block rounded-[var(--radius-md)] px-2.5 py-2 transition-colors", active ? "bg-surface-3" : "hover:bg-surface-2")}
            >
              <div className="truncate text-[13px] text-fg">{c.title || "Untitled conversation"}</div>
              <div className="mt-0.5 flex items-center gap-1.5 truncate text-[11px] text-faint">
                {c.idea_title && <span className="truncate text-muted">{c.idea_title}</span>}
                {c.idea_title && <span>·</span>}
                <span className="shrink-0">{shortDate(c.updated_at)}</span>
              </div>
            </Link>
          );
        })}
      </div>
    </aside>
  );
}
