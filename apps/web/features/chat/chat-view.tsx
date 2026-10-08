"use client";

import { useEffect, useRef } from "react";
import { ShieldAlert, Sparkles, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/primitives";
import { cn, humanize } from "@/lib/format";
import { ChatMessageView } from "./chat-message";
import { Composer } from "./composer";
import { useChat, type UseChatOptions } from "./use-chat";

const SUGGESTIONS = [
  "I have a new idea.",
  "Continue my most recent idea from its latest checkpoint.",
  "Why did we decide that?",
  "Where did I first talk about AI automation?",
  "What assumptions have I never validated?",
  "Give me a prompt I can give to an implementation agent.",
];

function ConfirmationCard({ tool, category, description, onApprove, onDecline }: { tool: string; category: string; description: string; onApprove: () => void; onDecline: () => void }) {
  const destructive = category === "DESTRUCTIVE";
  return (
    <div
      className={cn("rounded-[var(--radius-lg)] border p-4 animate-slide-up", destructive ? "border-danger/40 bg-danger-soft" : "border-warning/40 bg-warning-soft")}
      role="alertdialog"
      aria-label="Confirm action"
      data-testid="confirmation"
    >
      <div className="flex items-start gap-3">
        <ShieldAlert className={cn("mt-0.5 h-4 w-4 shrink-0", destructive ? "text-danger" : "text-warning")} />
        <div className="min-w-0 flex-1">
          <p className="text-[13px] font-semibold text-fg">
            {destructive ? "Confirm destructive action" : "Confirm external action"} · <span className="font-mono text-[12px]">{tool}</span>
          </p>
          <p className="mt-1 text-[13px] text-muted">{description}</p>
          <p className="mt-1 text-[11px] uppercase tracking-wide text-faint">{humanize(category)} tools always require your approval</p>
          <div className="mt-3 flex gap-2">
            <Button size="sm" variant={destructive ? "danger" : "primary"} onClick={onApprove}>
              Approve
            </Button>
            <Button size="sm" variant="secondary" onClick={onDecline}>
              Decline
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

export function ChatView({
  initialPrompt,
  compact,
  emptyTitle,
  emptySubtitle,
  suggestions = SUGGESTIONS,
  ...opts
}: UseChatOptions & { initialPrompt?: string | null; compact?: boolean; emptyTitle?: string; emptySubtitle?: string; suggestions?: string[] }) {
  const EmptyHeading = compact ? "h2" : "h1";
  const chat = useChat(opts);
  const bottomRef = useRef<HTMLDivElement>(null);
  const sentInitial = useRef(false);
  const scrollerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (initialPrompt && !sentInitial.current && !opts.conversationId) {
      sentInitial.current = true;
      void chat.send(initialPrompt);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialPrompt]);

  // Open at the linked message (#m-<id>) or the latest one, then stick to bottom while
  // streaming unless the user scrolled up.
  const initialScroll = useRef(false);
  useEffect(() => {
    const el = scrollerRef.current;
    if (!el || chat.messages.length === 0) return;
    if (!initialScroll.current) {
      initialScroll.current = true;
      const target = window.location.hash.startsWith("#m-") ? document.getElementById(window.location.hash.slice(1)) : null;
      if (target) {
        target.scrollIntoView({ block: "center" });
        target.setAttribute("data-highlight", "true");
      } else {
        bottomRef.current?.scrollIntoView({ block: "end" });
      }
      return;
    }
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 160;
    if (nearBottom || chat.status === "streaming") bottomRef.current?.scrollIntoView({ block: "end" });
  }, [chat.messages, chat.status, chat.confirmation]);

  const empty = chat.messages.length === 0 && !chat.loadingHistory;
  const lastAssistant = [...chat.messages].reverse().find((m) => m.role === "assistant");

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div ref={scrollerRef} className="min-h-0 flex-1 overflow-y-auto">
        <div className={cn("mx-auto w-full", compact ? "max-w-none px-4 py-4" : "max-w-3xl px-6 py-10")}>
          {chat.loadingHistory && (
            <div className="flex justify-center py-16">
              <Spinner />
            </div>
          )}
          {empty && (
            <div className={cn("animate-fade-in", compact ? "py-6" : "pt-[10vh]")}>
              <div className={cn("flex items-center gap-2 text-accent", compact && "hidden")}>
                <Sparkles className="h-5 w-5" />
              </div>
              {/* Embedded (compact) chat sits under its host page's h1. */}
              <EmptyHeading className={cn("font-display text-fg", compact ? "text-2xl" : "mt-4 text-[2.75rem] leading-[1.05]")}>{emptyTitle ?? "What are you thinking about?"}</EmptyHeading>
              <p className={cn("mt-2 max-w-xl text-muted", compact ? "text-[13px]" : "text-[15px]")}>
                {emptySubtitle ??
                  "Start a new idea, continue one from any checkpoint, ask why you decided something, or turn your thinking into a plan. IdeaVault remembers how every idea evolved."}
              </p>
              <div className={cn("mt-6 flex flex-wrap gap-2", compact && "mt-4")}>
                {suggestions.map((s) => (
                  <button
                    key={s}
                    type="button"
                    onClick={() => void chat.send(s)}
                    className="rounded-full border border-border bg-surface px-3.5 py-1.5 text-[13px] text-muted transition-colors hover:border-border-strong hover:text-fg"
                  >
                    {s}
                  </button>
                ))}
              </div>
            </div>
          )}
          <div className="space-y-7">
            {chat.messages.map((m) => (
              <ChatMessageView key={m.id} message={m} canRetry={m.id === lastAssistant?.id && chat.status === "idle" && Boolean(chat.conversationId)} onRetry={() => void chat.retry(m.id)} />
            ))}
            {chat.confirmation && (
              <ConfirmationCard
                tool={chat.confirmation.tool}
                category={chat.confirmation.category}
                description={chat.confirmation.description}
                onApprove={() => void chat.respond(true)}
                onDecline={() => void chat.respond(false)}
              />
            )}
            {chat.error && (
              <div className="flex items-start gap-2 rounded-[var(--radius-md)] border border-danger/30 bg-danger-soft px-3 py-2 text-[13px] text-danger" role="alert">
                <span className="flex-1">{chat.error}</span>
                <button onClick={chat.clearError} aria-label="Dismiss error">
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
            )}
          </div>
          <div ref={bottomRef} className="h-2" />
        </div>
      </div>
      <div className={cn("shrink-0 bg-gradient-to-t from-bg via-bg to-transparent", compact ? "px-3 pb-3 pt-2" : "px-6 pb-6 pt-3")}>
        <div className={cn("mx-auto w-full", compact ? "" : "max-w-3xl")}>
          <Composer
            onSend={(t) => void chat.send(t)}
            onStop={chat.stop}
            streaming={chat.status === "streaming"}
            ideaId={opts.ideaId}
            autoFocus={!compact}
            compact={compact}
            placeholder={compact ? "Ask about this idea…" : undefined}
          />
        </div>
      </div>
    </div>
  );
}
