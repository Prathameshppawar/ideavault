"use client";

import { useState } from "react";
import { Check, ChevronRight, CircleAlert, CircleCheck, CircleDashed, Copy, Hourglass, RotateCw, ShieldAlert } from "lucide-react";
import { Markdown } from "@/components/markdown";
import { EntityChip } from "@/components/ui/badges";
import { Tooltip } from "@/components/ui/overlay";
import { cn, shortDate } from "@/lib/format";
import type { ChatMessage, ChatTraceStep } from "./use-chat";

function StepIcon({ status }: { status: string }) {
  switch (status) {
    case "running":
      return <CircleDashed className="h-3.5 w-3.5 animate-spin text-accent" />;
    case "failed":
    case "denied":
      return <CircleAlert className="h-3.5 w-3.5 text-danger" />;
    case "awaiting_confirmation":
      return <ShieldAlert className="h-3.5 w-3.5 text-warning" />;
    case "waiting":
      return <Hourglass className="h-3.5 w-3.5 text-warning" />;
    default:
      return <CircleCheck className="h-3.5 w-3.5 text-success" />;
  }
}

/** Safe execution trace: what the agent did, never its private reasoning. */
export function Trace({ steps, live }: { steps: ChatTraceStep[]; live?: boolean }) {
  const [open, setOpen] = useState(false);
  if (!steps.length) return null;
  const running = steps.some((s) => s.status === "running");
  const expanded = open || live;
  const last = steps[steps.length - 1];
  return (
    <div className="mb-2.5">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="group flex items-center gap-1.5 text-[12px] text-muted hover:text-fg"
        aria-expanded={expanded}
      >
        <ChevronRight className={cn("h-3 w-3 transition-transform", expanded && "rotate-90")} />
        {running ? <span className="animate-pulse-soft">{last.label}…</span> : <span>{steps.length === 1 ? last.label : `${steps.length} steps · ${last.label}`}</span>}
      </button>
      {expanded && (
        <ol className="ml-[5px] mt-1.5 space-y-1 border-l border-border pl-3.5">
          {steps.map((s) => (
            <li key={s.id} data-testid="trace-step" data-tool={s.tool} data-status={s.status} className="flex items-start gap-2 text-[12px] leading-5 text-muted animate-fade-in">
              <span className="mt-[3px]">
                <StepIcon status={s.status} />
              </span>
              <span className="min-w-0">
                {s.label}
                {s.refs && s.refs.length > 0 && (
                  <span className="ml-1.5 inline-flex flex-wrap gap-1 align-middle">
                    {s.refs.slice(0, 4).map((r) => (
                      <EntityChip key={r.id} type={r.type} id={r.id} label={r.label || r.title} title={r.title} />
                    ))}
                  </span>
                )}
              </span>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

/** Deep links (#m-<id>) land on a message; the chat view marks it with data-highlight. */
const ANCHOR = "scroll-mt-24 rounded-[var(--radius-lg)] transition-shadow data-[highlight=true]:ring-2 data-[highlight=true]:ring-accent/40 data-[highlight=true]:ring-offset-4 data-[highlight=true]:ring-offset-bg";

export function ChatMessageView({ message, onRetry, canRetry }: { message: ChatMessage; onRetry?: () => void; canRetry?: boolean }) {
  const [copied, setCopied] = useState(false);
  if (message.role === "user") {
    return (
      <div id={`m-${message.id}`} className={cn("flex justify-end animate-fade-in", ANCHOR)} data-testid="message" data-role="user">
        <div className="max-w-[85%] whitespace-pre-wrap break-words rounded-[14px] rounded-br-[4px] bg-surface-3 px-4 py-2.5 text-[15px] leading-relaxed text-fg">
          {message.content}
        </div>
      </div>
    );
  }
  const refs = (message.refs ?? []).filter((r, i, a) => a.findIndex((x) => x.id === r.id) === i);
  return (
    <div id={`m-${message.id}`} className={cn("group animate-fade-in", ANCHOR)} data-testid="message" data-role="assistant" data-pending={message.pending ? "true" : "false"}>
      {message.trace && message.trace.length > 0 && <Trace steps={message.trace} live={message.pending} />}
      {message.pending && !message.content ? (
        (!message.trace || message.trace.length === 0) && (
          <div className="flex items-center gap-1.5 py-2" aria-label="Thinking">
            {[0, 1, 2].map((i) => (
              <span key={i} className="h-1.5 w-1.5 animate-pulse-soft rounded-full bg-faint" style={{ animationDelay: `${i * 150}ms` }} />
            ))}
          </div>
        )
      ) : (
        <Markdown className={cn(message.failed && "text-danger")}>{message.content}</Markdown>
      )}
      {!message.pending && refs.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1.5">
          {refs.slice(0, 10).map((r) => (
            <EntityChip key={r.id} type={r.type} id={r.id} label={r.label || r.title} title={r.title} />
          ))}
        </div>
      )}
      {!message.pending && (
        <div className="mt-2 flex items-center gap-1 text-faint opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <Tooltip content={copied ? "Copied" : "Copy"}>
            <button
              type="button"
              onClick={() => {
                void navigator.clipboard.writeText(message.content);
                setCopied(true);
                setTimeout(() => setCopied(false), 1500);
              }}
              className="rounded p-1 hover:bg-surface-2 hover:text-fg"
              aria-label="Copy message"
            >
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            </button>
          </Tooltip>
          {canRetry && onRetry && (
            <Tooltip content="Retry">
              <button type="button" onClick={onRetry} className="rounded p-1 hover:bg-surface-2 hover:text-fg" aria-label="Retry">
                <RotateCw className="h-3.5 w-3.5" />
              </button>
            </Tooltip>
          )}
          {(message.model || message.createdAt) && (
            <span className="ml-1 text-[11px]">
              {message.model && <span>{message.model === "offline-planner" ? "offline planner (no AI)" : message.model}</span>}
              {message.model && message.createdAt && " · "}
              {message.createdAt && shortDate(message.createdAt)}
            </span>
          )}
        </div>
      )}
    </div>
  );
}
