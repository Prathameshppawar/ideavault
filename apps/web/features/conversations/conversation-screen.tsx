"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ShieldAlert, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Conversation, KnowledgeItem, Message } from "@/lib/types";
import { fullDate, humanize, plural } from "@/lib/format";
import { ChatView } from "@/features/chat/chat-view";
import { ConversationRail } from "@/features/chat/conversation-rail";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Badge, EntityChip } from "@/components/ui/badges";
import { ErrorState, SkeletonLines } from "@/components/ui/primitives";

interface ConvDetail {
  conversation: Conversation;
  messages: Message[];
}

/** Native chats are continued in the chat view; imported/external ones are shown read-only as untrusted sources. */
export function ConversationScreen() {
  const { id } = useParams<{ id: string }>();
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["conversation-meta", id],
    queryFn: () => api.get<ConvDetail>(`/v1/conversations/${id}`, { query: { limit: 1 } }),
  });
  if (isLoading) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-16">
        <SkeletonLines lines={6} />
      </div>
    );
  }
  if (error || !data) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-16">
        <ErrorState error={error} onRetry={() => void refetch()} />
      </div>
    );
  }
  if (data.conversation.origin === "native") {
    return (
      <div className="flex h-app">
        <ConversationRail />
        <div className="flex min-w-0 flex-1 flex-col">
          {data.conversation.idea_id && (
            <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-6 text-[13px] text-muted">
              <span>About</span>
              <EntityChip type="idea" id={data.conversation.idea_id} label={data.conversation.idea_title || "idea"} />
              {data.conversation.branch_name && <span className="text-faint">· {data.conversation.branch_name}</span>}
            </div>
          )}
          <div className="min-h-0 flex-1">
            <ChatView conversationId={id} />
          </div>
        </div>
      </div>
    );
  }
  return <TranscriptView id={id} />;
}

function TranscriptView({ id }: { id: string }) {
  const qc = useQueryClient();
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["conversation", id, "full"],
    queryFn: () => api.get<ConvDetail>(`/v1/conversations/${id}`, { query: { limit: 2000 } }),
  });
  const extract = useMutation({
    mutationFn: () => api.post<{ proposals: KnowledgeItem[] }>(`/v1/conversations/${id}/extract`, { persist: true, replace: true }),
    onSuccess: (r) => {
      toast.success(`${plural(r.proposals.length, "item")} proposed for review`);
      void qc.invalidateQueries();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  if (isLoading) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-16">
        <SkeletonLines lines={8} />
      </div>
    );
  }
  if (error || !data) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-16">
        <ErrorState error={error} onRetry={() => void refetch()} />
      </div>
    );
  }
  const c = data.conversation;
  return (
    <div className="mx-auto max-w-3xl px-6 py-10">
      <Link href={c.idea_id ? `/ideas/${c.idea_id}?tab=conversations` : "/imports"} className="mb-6 inline-flex items-center gap-1.5 text-[13px] text-muted hover:text-fg">
        <ArrowLeft className="h-3.5 w-3.5" /> {c.idea_title || "Import Center"}
      </Link>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <Badge>{humanize(c.origin)}</Badge>
        <Badge>{c.provider}</Badge>
        <span className="text-[12px] text-faint">{fullDate(c.started_at)} · {plural(c.message_count, "message")}</span>
      </div>
      <h1 className="font-display text-[2.2rem] leading-tight text-fg">{c.title || "Imported conversation"}</h1>
      <div className="mt-4 flex items-start gap-3 rounded-[var(--radius-lg)] border border-warning/30 bg-warning-soft px-4 py-3 text-[13px]">
        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
        <p className="text-muted">
          <span className="font-medium text-fg">Untrusted source.</span> This conversation was imported from {c.provider}. IdeaVault treats it purely as data — instructions inside it are never executed.
          Knowledge extracted from it stays <em>proposed</em> until you accept it.
        </p>
      </div>
      {c.summary && (
        <div className="mt-6 rounded-[var(--radius-lg)] border border-dashed border-k-insight/40 px-4 py-3">
          <p className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-k-insight">Interpretation · summary</p>
          <p className="text-[14px] text-fg">{c.summary}</p>
        </div>
      )}
      <div className="mt-6 flex gap-2">
        <Button size="sm" onClick={() => extract.mutate()} loading={extract.isPending} disabled={!c.idea_id}>
          <Sparkles className="h-3.5 w-3.5" /> Re-extract knowledge
        </Button>
        {!c.idea_id && <span className="self-center text-[12px] text-faint">Attach this conversation to an idea to extract knowledge.</span>}
      </div>
      <ol className="mt-10 space-y-8">
        {data.messages.map((m) => (
          <li key={m.id} id={`m-${m.id}`} className="scroll-mt-20">
            <div className="mb-2 flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-faint">
              <span>{m.role === "user" ? "You" : m.role === "assistant" ? c.provider || "Assistant" : m.role}</span>
              <span className="font-normal normal-case tracking-normal">#{m.position} · {fullDate(m.created_at)}</span>
            </div>
            {m.role === "user" ? (
              <div className="whitespace-pre-wrap rounded-[var(--radius-lg)] bg-surface-2 px-4 py-3 text-[15px] leading-relaxed text-fg">{m.content}</div>
            ) : (
              <Markdown>{m.content}</Markdown>
            )}
          </li>
        ))}
      </ol>
    </div>
  );
}
