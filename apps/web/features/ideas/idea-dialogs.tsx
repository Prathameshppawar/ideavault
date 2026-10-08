"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Bookmark } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Checkpoint, Idea } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/overlay";
import { Field, Input, Textarea } from "@/components/ui/primitives";
import { useRefreshIdea } from "./hooks";

/** Create checkpoint → POST /v1/ideas/{id}/checkpoints. */
export function CheckpointDialog({
  open,
  onOpenChange,
  ideaId,
  branchId,
  branchName,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  ideaId: string;
  branchId: string | null;
  branchName?: string;
  onCreated?: (cp: Checkpoint) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && <CheckpointForm ideaId={ideaId} branchId={branchId} branchName={branchName} onDone={(cp) => {
            onOpenChange(false);
            onCreated?.(cp);
          }} />}
    </Dialog>
  );
}

function CheckpointForm({ ideaId, branchId, branchName, onDone }: { ideaId: string; branchId: string | null; branchName?: string; onDone: (cp: Checkpoint) => void }) {
  const refresh = useRefreshIdea();
  const [title, setTitle] = useState("");
  const [note, setNote] = useState("");
  const create = useMutation({
    mutationFn: () => api.post<Checkpoint>(`/v1/ideas/${ideaId}/checkpoints`, { branch_id: branchId ?? undefined, title: title.trim() || undefined, context: note.trim() || undefined }),
    onSuccess: (cp) => {
      toast.success(`${cp.label} saved — ${cp.title}`);
      void refresh(ideaId);
      onDone(cp);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <DialogContent
      title="Create checkpoint"
      description={
        <>
          An immutable snapshot of everything on <span className="font-medium text-fg">{branchName ?? "this branch"}</span> right now. You can always come back to it, compare
          against it, or fork from it.
        </>
      }
    >
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <Field label="Title" htmlFor="cp-title" hint="Leave empty to name it after the latest decision.">
          <Input id="cp-title" value={title} onChange={(e) => setTitle(e.target.value)} maxLength={200} placeholder="e.g. Research: organisers first" autoFocus />
        </Field>
        <Field label="Note" htmlFor="cp-note" hint="Context worth keeping with this moment — kept verbatim in the snapshot.">
          <Textarea id="cp-note" value={note} onChange={(e) => setNote(e.target.value)} rows={3} placeholder="Optional" />
        </Field>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="submit" variant="primary" loading={create.isPending}>
            <Bookmark className="h-4 w-4" /> Save checkpoint
          </Button>
        </div>
      </form>
    </DialogContent>
  );
}

/** Permanently delete an idea: requires typing its title, sends X-Confirm: delete. */
export function DeleteIdeaDialog({ open, onOpenChange, idea }: { open: boolean; onOpenChange: (v: boolean) => void; idea: Idea }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && <DeleteForm idea={idea} onClose={() => onOpenChange(false)} />}
    </Dialog>
  );
}

function DeleteForm({ idea, onClose }: { idea: Idea; onClose: () => void }) {
  const router = useRouter();
  const qc = useQueryClient();
  const [typed, setTyped] = useState("");
  const matches = typed.trim() === idea.title.trim();
  const del = useMutation({
    mutationFn: () => api.del(`/v1/ideas/${idea.id}`, { headers: { "X-Confirm": "delete" } }),
    onSuccess: () => {
      toast.success(`Deleted “${idea.title}”`);
      qc.removeQueries({ queryKey: ["idea", idea.id] });
      void qc.invalidateQueries({ queryKey: ["ideas"] });
      onClose();
      router.push("/ideas");
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <DialogContent title="Delete this idea permanently?">
      <div className="flex gap-3 rounded-[var(--radius-lg)] border border-danger/30 bg-danger-soft px-4 py-3 text-[13px] text-fg">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-danger" />
        <p>
          This erases the idea with <strong>all</strong> its branches, checkpoints, knowledge and artifacts. It cannot be undone. If you only want to stop working on it, conclude it
          as <em>Park</em> or <em>Abandon</em> instead — that keeps the history.
        </p>
      </div>
      <form
        className="mt-4 space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (matches) del.mutate();
        }}
      >
        <Field label={`Type the idea's title to confirm`} htmlFor="delete-confirm">
          <Input id="delete-confirm" value={typed} onChange={(e) => setTyped(e.target.value)} placeholder={idea.title} autoComplete="off" autoFocus />
        </Field>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="danger" disabled={!matches} loading={del.isPending}>
            Delete permanently
          </Button>
        </div>
      </form>
    </DialogContent>
  );
}
