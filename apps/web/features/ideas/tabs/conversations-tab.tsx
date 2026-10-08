"use client";

import Link from "next/link";
import { GitBranch, MessagesSquare } from "lucide-react";
import type { Conversation } from "@/lib/types";
import { cn, fullDate, humanize, plural, shortDate } from "@/lib/format";
import { EmptyState } from "@/components/ui/primitives";
import { UntrustedLabel } from "../ui";

const ORIGIN: Record<string, { label: string; tone: string; hint: string }> = {
  native: { label: "Native", tone: "idea", hint: "A conversation with IdeaVault" },
  imported: { label: "Imported", tone: "conversation", hint: "Imported from another assistant's export" },
  external: { label: "External", tone: "branch", hint: "Pasted from an external AI conversation" },
};

export function ConversationsTab({ conversations }: { conversations: Conversation[] }) {
  if (!conversations.length)
    return (
      <EmptyState
        icon={MessagesSquare}
        title="No conversations yet"
        description="Talk it through in the chat panel — every conversation about this idea is kept here, along with anything you import."
      />
    );
  const sorted = [...conversations].sort((a, b) => new Date(b.updated_at || b.started_at).getTime() - new Date(a.updated_at || a.started_at).getTime());
  return (
    <ul className="divide-y divide-border border-y border-border">
      {sorted.map((c) => {
        const o = ORIGIN[c.origin] ?? { label: humanize(c.origin), tone: "conversation", hint: "" };
        return (
          <li key={c.id}>
            <Link href={`/conversations/${c.id}`} className="group flex items-start gap-3 px-1 py-3.5 transition-colors hover:bg-surface-2/50">
              <MessagesSquare className="mt-0.5 h-4 w-4 shrink-0 text-faint group-hover:text-muted" aria-hidden />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="min-w-0 text-[14.5px] font-medium text-fg group-hover:text-accent [overflow-wrap:anywhere]">{c.title || "Untitled conversation"}</span>
                  <span
                    title={o.hint}
                    className="inline-flex h-5 items-center rounded-full px-2 text-[11px] font-medium"
                    style={{ background: `var(--k-${o.tone}-soft)`, color: `var(--k-${o.tone})` }}
                  >
                    {o.label}
                    {c.origin !== "native" && c.provider ? ` · ${c.provider}` : ""}
                  </span>
                  {c.origin !== "native" && <UntrustedLabel />}
                </div>
                {c.summary && <p className="mt-0.5 line-clamp-2 text-[13px] text-muted">{c.summary}</p>}
                <p className={cn("mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[12px] text-faint")}>
                  <span>{plural(c.message_count, "message")}</span>
                  {c.branch_name && (
                    <span className="inline-flex items-center gap-1">
                      <GitBranch className="h-3 w-3" /> {c.branch_name}
                    </span>
                  )}
                  <span title={fullDate(c.started_at)}>started {shortDate(c.started_at)}</span>
                </p>
              </div>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}
