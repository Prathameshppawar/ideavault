"use client";

import { useCallback, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, errorMessage } from "@/lib/api";
import { postSSE } from "@/lib/sse";
import type { AgentEvent, EntityRef, Message, ToolCallRecord, TraceEvent } from "@/lib/types";

export interface ChatTraceStep {
  id: string;
  label: string;
  tool?: string;
  status: string;
  kind: string;
  refs?: EntityRef[];
}

export interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  createdAt?: string;
  refs?: EntityRef[];
  trace?: ChatTraceStep[];
  model?: string;
  provider?: string;
  stopped?: boolean;
  failed?: boolean;
  pending?: boolean;
}

export interface PendingConfirmation {
  toolCallId: string;
  tool: string;
  category: string;
  description: string;
  arguments: Record<string, unknown>;
}

type Status = "idle" | "streaming";

function fromServer(m: Message): ChatMessage | null {
  if (m.role !== "user" && m.role !== "assistant") return null;
  const meta = (m.metadata ?? {}) as Record<string, unknown>;
  const trace = Array.isArray(meta.trace)
    ? (meta.trace as { label: string; tool?: string; status?: string; kind?: string }[]).map((t, i) => ({
        id: `${m.id}-${i}`,
        label: t.label,
        tool: t.tool,
        status: t.status ?? "done",
        kind: t.kind ?? "tool",
      }))
    : undefined;
  return {
    id: m.id,
    role: m.role,
    content: m.content,
    createdAt: m.created_at,
    refs: (meta.refs as EntityRef[] | undefined) ?? undefined,
    trace,
    model: (meta.model as string) || m.model,
    provider: meta.provider as string | undefined,
    stopped: Boolean(meta.stopped),
    failed: meta.status === "FAILED",
  };
}

export interface UseChatOptions {
  conversationId?: string | null;
  ideaId?: string | null;
  branchId?: string | null;
  checkpointId?: string | null;
  onConversationCreated?: (id: string, title?: string) => void;
}

export function useChat(opts: UseChatOptions) {
  const qc = useQueryClient();
  const [conversationId, setConversationId] = useState<string | null>(opts.conversationId ?? null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [status, setStatus] = useState<Status>("idle");
  const [confirmation, setConfirmation] = useState<PendingConfirmation | null>(null);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  // The conversation whose history is already in `messages` (a streamed-in conversation counts).
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const [prevPropId, setPrevPropId] = useState(opts.conversationId);

  // Follow the conversation chosen by the parent (navigation), unless a turn is streaming.
  if (opts.conversationId !== prevPropId) {
    setPrevPropId(opts.conversationId);
    if (opts.conversationId !== undefined && opts.conversationId !== conversationId && status === "idle") {
      setLoadedFor(null);
      setConversationId(opts.conversationId ?? null);
      if (!opts.conversationId) setMessages([]);
    }
  }

  // Load history for an existing conversation.
  const history = useQuery({
    queryKey: ["conversation", conversationId],
    queryFn: () => api.get<{ messages: Message[] }>(`/v1/conversations/${conversationId}`),
    enabled: Boolean(conversationId) && loadedFor !== conversationId && status === "idle",
  });
  if (history.data && conversationId && loadedFor !== conversationId && status === "idle") {
    setLoadedFor(conversationId);
    setMessages(history.data.messages.map(fromServer).filter(Boolean) as ChatMessage[]);
  }

  // A reopened conversation may have an action still waiting for approval: show its prompt again
  // (once per conversation load, so answering it doesn't bring it back from stale data).
  const [restoredFor, setRestoredFor] = useState<string | null>(null);
  const pendingCalls = useQuery({
    queryKey: ["agent-pending", conversationId],
    queryFn: () => api.get<ToolCallRecord[]>("/v1/agent/pending", { query: { conversation_id: conversationId! } }),
    enabled: Boolean(conversationId) && restoredFor !== conversationId && status === "idle",
    staleTime: 0,
  });
  if (pendingCalls.data && conversationId && restoredFor !== conversationId && status === "idle") {
    setRestoredFor(conversationId);
    const p = pendingCalls.data[pendingCalls.data.length - 1];
    if (p && !confirmation) {
      setConfirmation({ toolCallId: p.id, tool: p.tool_name, category: p.category, description: p.summary, arguments: (p.arguments ?? {}) as Record<string, unknown> });
    }
  }

  const handleEvents = useCallback(
    (draftId: string) => (ev: { event: string; data: unknown }) => {
      const e = { type: ev.event, data: ev.data } as AgentEvent;
      switch (e.type) {
        case "run_started":
          if (e.data.conversation_id && !conversationId) {
            setLoadedFor(e.data.conversation_id);
            setConversationId(e.data.conversation_id);
            opts.onConversationCreated?.(e.data.conversation_id, e.data.conversation_title);
          }
          if (e.data.user_message_id) {
            setMessages((ms) => ms.map((m) => (m.id === `${draftId}-user` ? { ...m, id: e.data.user_message_id! } : m)));
          }
          break;
        case "token":
          setMessages((ms) => ms.map((m) => (m.id === draftId ? { ...m, content: m.content + e.data.text } : m)));
          break;
        case "trace": {
          const t = e.data as TraceEvent;
          setMessages((ms) =>
            ms.map((m) => {
              if (m.id !== draftId) return m;
              const steps = [...(m.trace ?? [])];
              const key = t.tool_call_id ? `${t.tool_call_id}` : `${steps.length}`;
              const idx = steps.findIndex((s) => s.id === key);
              const step: ChatTraceStep = { id: key, label: t.label, tool: t.tool, status: t.status ?? "done", kind: t.kind, refs: t.refs };
              if (idx >= 0) steps[idx] = step;
              else steps.push(step);
              return { ...m, trace: steps };
            }),
          );
          break;
        }
        case "confirmation_required":
          setConfirmation({ toolCallId: e.data.tool_call_id, tool: e.data.tool, category: e.data.category, description: e.data.description, arguments: e.data.arguments });
          break;
        case "message":
          setMessages((ms) =>
            ms.map((m) =>
              m.id === draftId
                ? { ...m, id: e.data.id, content: e.data.content, refs: e.data.refs, model: e.data.model, provider: e.data.provider, createdAt: e.data.created_at, pending: false }
                : m,
            ),
          );
          break;
        case "error":
          setError(e.data.message);
          break;
        case "run_completed":
          void qc.invalidateQueries({ queryKey: ["conversations"] });
          if (e.data.focus?.idea_id) {
            void qc.invalidateQueries({ queryKey: ["idea", e.data.focus.idea_id] });
          }
          void qc.invalidateQueries({ queryKey: ["ideas"] });
          void qc.invalidateQueries({ queryKey: ["today"] });
          break;
      }
    },
    [conversationId, opts, qc],
  );

  const run = useCallback(
    async (path: string, body: unknown, draftId: string) => {
      const ctrl = new AbortController();
      abortRef.current = ctrl;
      setStatus("streaming");
      setError(null);
      try {
        await postSSE(path, body, handleEvents(draftId), ctrl.signal);
      } catch (err) {
        if ((err as Error).name !== "AbortError") setError(errorMessage(err));
      } finally {
        abortRef.current = null;
        setStatus("idle");
        setMessages((ms) =>
          ms.map((m) => (m.id === draftId ? { ...m, pending: false, stopped: ctrl.signal.aborted || m.stopped, content: m.content || (ctrl.signal.aborted ? "_Stopped._" : m.content) } : m)),
        );
        void qc.invalidateQueries({ queryKey: ["conversation", conversationId] });
      }
    },
    [handleEvents, qc, conversationId],
  );

  const send = useCallback(
    async (text: string) => {
      const content = text.trim();
      if (!content || status === "streaming") return;
      const draftId = `draft-${Date.now()}`;
      setConfirmation(null);
      setMessages((ms) => [
        ...ms,
        { id: `${draftId}-user`, role: "user", content, createdAt: new Date().toISOString() },
        { id: draftId, role: "assistant", content: "", pending: true, trace: [] },
      ]);
      await run(
        "/v1/agent/chat",
        {
          message: content,
          conversation_id: conversationId ?? undefined,
          idea_id: conversationId ? undefined : (opts.ideaId ?? undefined),
          branch_id: conversationId ? undefined : (opts.branchId ?? undefined),
          checkpoint_id: opts.checkpointId ?? undefined,
        },
        draftId,
      );
    },
    [status, run, conversationId, opts.ideaId, opts.branchId, opts.checkpointId],
  );

  const retry = useCallback(
    async (assistantId: string) => {
      if (!conversationId || status === "streaming") return;
      const idx = messages.findIndex((m) => m.id === assistantId);
      const userMsg = [...messages.slice(0, idx)].reverse().find((m) => m.role === "user");
      if (!userMsg) return;
      const draftId = `draft-${Date.now()}`;
      setMessages((ms) => [...ms, { id: draftId, role: "assistant", content: "", pending: true, trace: [] }]);
      await run("/v1/agent/chat", { conversation_id: conversationId, retry_message_id: userMsg.id, message: "" }, draftId);
    },
    [conversationId, messages, status, run],
  );

  const respond = useCallback(
    async (approve: boolean) => {
      if (!confirmation) return;
      const c = confirmation;
      setConfirmation(null);
      setRestoredFor(conversationId); // stale pending data must not bring the prompt back
      const draftId = `draft-${Date.now()}`;
      setMessages((ms) => [...ms, { id: draftId, role: "assistant", content: "", pending: true, trace: [] }]);
      await run(`/v1/agent/tool-calls/${c.toolCallId}/confirm`, { approve }, draftId);
    },
    [confirmation, conversationId, run],
  );

  const stop = useCallback(() => abortRef.current?.abort(), []);

  return {
    conversationId,
    messages,
    status,
    error,
    confirmation,
    loadingHistory: history.isLoading && Boolean(conversationId) && messages.length === 0,
    send,
    stop,
    retry,
    respond,
    clearError: () => setError(null),
  };
}
