"use client";

import { Command } from "cmdk";
import { useQuery } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useState } from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import {
  ArrowRight,
  CornerDownLeft,
  FileText,
  Import,
  Lightbulb,
  MessageSquare,
  Moon,
  Plus,
  Search,
  Sparkles,
} from "lucide-react";
import { api } from "@/lib/api";
import type { Idea } from "@/lib/types";
import { useTheme, useUI } from "@/stores/ui";
import { StatusBadge } from "@/components/ui/badges";
import { Kbd } from "@/components/ui/primitives";

const pages = [
  { href: "/", label: "Chat" },
  { href: "/dashboard", label: "Thinking — today" },
  { href: "/dashboard?view=timeline", label: "Timeline" },
  { href: "/dashboard?view=decisions", label: "Decisions" },
  { href: "/dashboard?view=learning", label: "Learning" },
  { href: "/dashboard?view=momentum", label: "Momentum" },
  { href: "/dashboard?view=archive", label: "Archive / graveyard" },
  { href: "/ideas", label: "All ideas" },
  { href: "/universe", label: "Universe graph" },
  { href: "/search", label: "Search" },
  { href: "/artifacts", label: "Artifacts" },
  { href: "/imports", label: "Import center" },
  { href: "/insights", label: "Thinking & prompt insights" },
  { href: "/models", label: "Models & routing" },
  { href: "/models?tab=lab", label: "Model Lab" },
  { href: "/usage", label: "Usage" },
  { href: "/connectors", label: "Connectors" },
  { href: "/settings", label: "Settings" },
];

const itemClass =
  "flex cursor-pointer items-center gap-2.5 rounded-[var(--radius-md)] px-2.5 py-2 text-[13px] text-fg data-[selected=true]:bg-surface-2 aria-selected:bg-surface-2";

export function CommandPalette() {
  const { paletteOpen: open, setPaletteOpen: setOpen } = useUI();
  return (
    <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/30 backdrop-blur-[2px] data-[state=open]:animate-fade-in" />
        <DialogPrimitive.Content className="fixed left-1/2 top-[14vh] z-50 w-[calc(100vw-2rem)] max-w-xl -translate-x-1/2 overflow-hidden rounded-[var(--radius-xl)] border border-border bg-surface shadow-lg data-[state=open]:animate-slide-up">
          <DialogPrimitive.Title className="sr-only">
            Command palette
          </DialogPrimitive.Title>
          {/* Mounted only while open, so the query starts empty every time. */}
          <PaletteBody close={() => setOpen(false)} />
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

function PaletteBody({ close }: { close: () => void }) {
  const router = useRouter();
  const { mode, setMode } = useTheme();
  const [value, setValue] = useState("");
  const q = value.trim();

  const { data: ideas } = useQuery({
    queryKey: ["palette-ideas", q],
    queryFn: () =>
      api.get<{ ideas: Idea[] }>("/v1/ideas", { query: { q, limit: 6 } }),
    staleTime: 10_000,
  });

  const go = (href: string) => {
    close();
    router.push(href);
  };
  const ask = (text: string) => go(`/?q=${encodeURIComponent(text)}`);

  return (
    <Command label="Command palette" shouldFilter loop>
      <div className="flex items-center gap-2 border-b border-border px-4">
        <Search className="h-4 w-4 text-faint" />
        <Command.Input
          value={value}
          onValueChange={setValue}
          placeholder="Ask IdeaVault, find an idea, or jump to a page…"
          className="h-12 flex-1 bg-transparent text-[15px] text-fg outline-none placeholder:text-faint"
        />
        <Kbd>esc</Kbd>
      </div>
      <Command.List className="max-h-[50vh] overflow-y-auto p-2">
        <Command.Empty className="px-3 py-6 text-center text-[13px] text-muted">
          No matches — press ↵ to ask IdeaVault.
        </Command.Empty>
        {q.length > 0 && (
          <Command.Group
            heading="Ask"
            className="[&_[cmdk-group-heading]]:px-2.5 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-faint"
          >
            <Command.Item
              value={`ask ${q}`}
              onSelect={() => ask(q)}
              className={itemClass}
            >
              <MessageSquare className="h-4 w-4 text-accent" />
              <span className="truncate">
                Ask IdeaVault: <span className="text-muted">{q}</span>
              </span>
              <CornerDownLeft className="ml-auto h-3.5 w-3.5 text-faint" />
            </Command.Item>
          </Command.Group>
        )}
        {ideas && ideas.ideas.length > 0 && (
          <Command.Group
            heading="Ideas"
            className="[&_[cmdk-group-heading]]:px-2.5 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-faint"
          >
            {ideas.ideas.map((i) => (
              <Command.Item
                key={i.id}
                value={`idea ${i.title} ${i.id}`}
                onSelect={() => go(`/ideas/${i.id}`)}
                className={itemClass}
              >
                <Lightbulb className="h-4 w-4 text-k-idea" />
                <span className="truncate">{i.title}</span>
                <span className="ml-auto">
                  <StatusBadge status={i.status} />
                </span>
              </Command.Item>
            ))}
          </Command.Group>
        )}
        <Command.Group
          heading="Actions"
          className="[&_[cmdk-group-heading]]:px-2.5 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-faint"
        >
          <Command.Item
            value="new idea"
            onSelect={() => ask("I have a new idea.")}
            className={itemClass}
          >
            <Plus className="h-4 w-4 text-muted" /> New idea
          </Command.Item>
          <Command.Item
            value="import conversation"
            onSelect={() => go("/imports")}
            className={itemClass}
          >
            <Import className="h-4 w-4 text-muted" /> Import a conversation
          </Command.Item>
          <Command.Item
            value="thinking patterns analysis"
            onSelect={() => go("/insights")}
            className={itemClass}
          >
            <Sparkles className="h-4 w-4 text-muted" /> Analyze my thinking
            patterns
          </Command.Item>
          <Command.Item
            value="artifacts documents"
            onSelect={() => go("/artifacts")}
            className={itemClass}
          >
            <FileText className="h-4 w-4 text-muted" /> Browse artifacts
          </Command.Item>
          <Command.Item
            value="toggle theme dark light"
            onSelect={() => setMode(mode === "dark" ? "light" : "dark")}
            className={itemClass}
          >
            <Moon className="h-4 w-4 text-muted" /> Toggle theme
          </Command.Item>
        </Command.Group>
        <Command.Group
          heading="Go to"
          className="[&_[cmdk-group-heading]]:px-2.5 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-faint"
        >
          {pages.map((p) => (
            <Command.Item
              key={p.href}
              value={`go ${p.label}`}
              onSelect={() => go(p.href)}
              className={itemClass}
            >
              <ArrowRight className="h-4 w-4 text-faint" /> {p.label}
            </Command.Item>
          ))}
        </Command.Group>
      </Command.List>
    </Command>
  );
}
