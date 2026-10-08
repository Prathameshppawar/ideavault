"use client";

import { useState, type ReactNode } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, CornerDownLeft } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { IdeaCandidate, IdeaCreated } from "@/lib/types";
import { cn, truncate } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTrigger } from "@/components/ui/overlay";
import { Field, Input, Textarea } from "@/components/ui/primitives";
import { StatusBadge } from "@/components/ui/badges";

/** "New idea": describe it, check for likely duplicates, then create and open it. */
export function NewIdeaDialog({ trigger }: { trigger: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      {open && <NewIdeaForm onDone={() => setOpen(false)} />}
    </Dialog>
  );
}

function NewIdeaForm({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const qc = useQueryClient();
  const [title, setTitle] = useState("");
  const [text, setText] = useState("");
  const [candidates, setCandidates] = useState<IdeaCandidate[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => api.post<IdeaCreated>("/v1/ideas", { title: title.trim(), origin_text: text.trim(), summary: "", tags: [] }),
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: ["ideas"] });
      toast.success("Idea space created");
      onDone();
      if (r.idea) router.push(`/ideas/${r.idea.id}`);
    },
    onError: (e) => setError(errorMessage(e)),
  });

  const check = useMutation({
    mutationFn: () => api.post<{ candidates: IdeaCandidate[] }>("/v1/ideas/check-duplicates", { text: [title.trim(), text.trim()].filter(Boolean).join(". ") }),
    onSuccess: (r) => {
      const likely = r.candidates.filter((c) => c.verdict !== "weak");
      if (likely.length) setCandidates(likely);
      else create.mutate();
    },
    // Duplicate detection is a courtesy; never block creation on it.
    onError: () => create.mutate(),
  });

  const busy = check.isPending || create.isPending;
  const canSubmit = (title.trim() || text.trim()).length > 0 && !busy;
  const submit = () => {
    setError(null);
    if (!title.trim() && !text.trim()) {
      setError("Give it a title or describe it.");
      return;
    }
    check.mutate();
  };

  if (candidates) {
    return (
      <DialogContent title="This might already exist" description="IdeaVault found ideas that look similar. Continue one of them, or start a separate idea space.">
        <ul className="-mx-1 space-y-1">
          {candidates.map((c) => (
            <li key={c.idea.id}>
              <Link
                href={`/ideas/${c.idea.id}`}
                onClick={onDone}
                className="group flex items-start gap-3 rounded-[var(--radius-md)] px-2.5 py-2.5 transition-colors hover:bg-surface-2"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-display text-[1.2rem] leading-tight text-fg group-hover:text-accent">{c.idea.title}</span>
                    <StatusBadge status={c.idea.status} />
                  </div>
                  {(c.idea.summary || c.idea.origin_text) && <p className="mt-0.5 line-clamp-2 text-[13px] text-muted">{truncate(c.idea.summary || c.idea.origin_text, 220)}</p>}
                  <p className={cn("mt-1 text-[11px] font-medium uppercase tracking-wide", c.verdict === "likely_duplicate" ? "text-warning" : "text-faint")}>
                    {c.verdict === "likely_duplicate" ? "Likely the same idea" : "Related"} · {Math.round(c.score * 100)}% match
                  </p>
                </div>
                <ArrowRight className="mt-1.5 h-4 w-4 shrink-0 text-faint transition-transform group-hover:translate-x-0.5 group-hover:text-fg" aria-hidden />
              </Link>
            </li>
          ))}
        </ul>
        {error && <p className="mt-3 text-[13px] text-danger">{error}</p>}
        <div className="mt-5 flex flex-wrap items-center justify-between gap-2 border-t border-border pt-4">
          <Button variant="ghost" size="sm" onClick={() => setCandidates(null)} disabled={create.isPending}>
            Back
          </Button>
          <Button variant="primary" size="sm" onClick={() => create.mutate()} loading={create.isPending}>
            Create a separate idea
          </Button>
        </div>
      </DialogContent>
    );
  }

  return (
    <DialogContent title="New idea" description="Capture it in your own words. Your description is kept verbatim as the idea's origin.">
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field label="Title" htmlFor="new-idea-title" hint="Optional — IdeaVault can use the first line of your description.">
          <Input id="new-idea-title" value={title} onChange={(e) => setTitle(e.target.value)} maxLength={300} placeholder="e.g. Biker community platform" autoFocus />
        </Field>
        <Field label="Describe it in your own words" htmlFor="new-idea-text">
          <Textarea
            id="new-idea-text"
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={6}
            placeholder="What is it, who is it for, why does it matter to you?"
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                e.preventDefault();
                submit();
              }
            }}
          />
        </Field>
        {error && <p className="text-[13px] text-danger" role="alert">{error}</p>}
        <div className="flex items-center justify-between gap-3 pt-1">
          <span className="hidden items-center gap-1 text-[12px] text-faint sm:inline-flex">
            <CornerDownLeft className="h-3 w-3" /> ⌘ Enter to create
          </span>
          <Button type="submit" variant="primary" loading={busy} disabled={!canSubmit}>
            {check.isPending ? "Checking for duplicates…" : "Create idea"}
          </Button>
        </div>
      </form>
    </DialogContent>
  );
}
