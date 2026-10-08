"use client";

import { useRouter } from "next/navigation";
import { useDeferredValue, useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { cn, plural } from "@/lib/format";
import type { Artifact, ArtifactResult } from "@/lib/types";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent } from "@/components/ui/overlay";
import { Input, Kbd } from "@/components/ui/primitives";
import { Segmented } from "./kit";
import { artifactKeys } from "./queries";
import { useUnsavedChangesGuard } from "./use-unsaved-guard";

/**
 * Plain Markdown editor with live preview. Saving appends a new immutable version
 * (PATCH title/content/change_note); nothing earlier is overwritten.
 */
export function ArtifactEditor({ artifact, onDone }: { artifact: Artifact; onDone: () => void }) {
  const qc = useQueryClient();
  const router = useRouter();
  const [title, setTitle] = useState(artifact.title);
  const [content, setContent] = useState(artifact.content_markdown);
  const [note, setNote] = useState("");
  const [pane, setPane] = useState<"write" | "preview">("write");
  // `leave` is where to go after confirming: a path, or "" to just exit edit mode.
  const [leave, setLeave] = useState<string | null>(null);
  const preview = useDeferredValue(content);
  const textRef = useRef<HTMLTextAreaElement>(null);

  const titleChanged = title.trim() !== artifact.title && title.trim() !== "";
  const contentChanged = content !== artifact.content_markdown;
  const dirty = titleChanged || contentChanged || (title.trim() === "" && artifact.title !== "");
  const nextVersion = artifact.current_version + 1;

  const save = useMutation({
    mutationFn: () =>
      api.patch<ArtifactResult>(`/v1/artifacts/${artifact.id}`, {
        title: titleChanged ? title.trim() : undefined,
        content_markdown: contentChanged ? content : undefined,
        change_note: note.trim() || undefined,
      }),
    onSuccess: (r) => {
      if (r.artifact) qc.setQueryData(artifactKeys.one(artifact.id), r.artifact);
      void qc.invalidateQueries({ queryKey: ["artifact", artifact.id] });
      void qc.invalidateQueries({ queryKey: ["artifacts"] });
      toast.success(`Saved as v${r.version?.version ?? nextVersion}`);
      onDone();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  const canSave = (titleChanged || contentChanged) && title.trim() !== "" && !save.isPending;
  useUnsavedChangesGuard(dirty && !save.isPending && !save.isSuccess, setLeave);

  // ⌘/Ctrl+S saves (the latest closure is kept in a ref so the listener is registered once).
  const saveRef = useRef(() => {});
  useEffect(() => {
    saveRef.current = () => {
      if (canSave) save.mutate();
    };
  });
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        saveRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    textRef.current?.focus({ preventScroll: true });
  }, []);

  const cancel = () => (dirty ? setLeave("") : onDone());
  const words = content.trim() ? content.trim().split(/\s+/).length : 0;
  const lines = content ? content.split("\n").length : 0;

  return (
    <div className="animate-fade-in">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <p className="text-[13px] text-muted">
          Saving creates <span className="font-mono text-fg">v{nextVersion}</span>. Earlier versions stay in history.
        </p>
        <Segmented<"write" | "preview">
          label="Editor pane"
          size="sm"
          className="lg:hidden"
          value={pane}
          onChange={setPane}
          options={[
            { value: "write", label: "Write" },
            { value: "preview", label: "Preview" },
          ]}
        />
      </div>

      <label htmlFor="artifact-title" className="sr-only">
        Title
      </label>
      <Input id="artifact-title" value={title} onChange={(e) => setTitle(e.target.value)} className="mb-3 h-11 font-display text-[1.45rem]" placeholder="Title" maxLength={300} />

      <div className="grid overflow-hidden rounded-[var(--radius-lg)] border border-border bg-surface transition-colors focus-within:border-border-strong lg:grid-cols-2">
        <div className={cn("flex min-w-0 flex-col", pane !== "write" && "hidden lg:flex")}>
          <div className="flex h-9 items-center justify-between border-b border-border px-4 text-[11px] font-semibold uppercase tracking-wider text-faint">
            <label htmlFor="artifact-md">Markdown</label>
            <span className="font-normal normal-case tracking-normal">
              {plural(lines, "line")} · {plural(words, "word")}
            </span>
          </div>
          <textarea
            id="artifact-md"
            ref={textRef}
            value={content}
            onChange={(e) => setContent(e.target.value)}
            spellCheck
            className="block h-[62vh] min-h-[320px] w-full resize-none bg-surface px-4 py-3 font-mono text-[13px] leading-6 text-fg placeholder:text-faint focus:outline-none"
          />
        </div>
        <div className={cn("flex min-w-0 flex-col border-border lg:border-l", pane !== "preview" && "hidden lg:flex")}>
          <div className="flex h-9 items-center border-b border-border px-4 text-[11px] font-semibold uppercase tracking-wider text-faint">Preview</div>
          <div className="h-[62vh] min-h-[320px] overflow-y-auto bg-bg px-6 py-5">
            {preview.trim() ? <Markdown>{preview}</Markdown> : <p className="text-[13px] text-faint">Nothing to preview yet.</p>}
          </div>
        </div>
      </div>

      <div className="sticky bottom-0 z-10 -mx-1 mt-3 flex flex-wrap items-center gap-2 border-t border-border bg-bg px-1 py-3">
        <label htmlFor="change-note" className="sr-only">
          Change note
        </label>
        <Input
          id="change-note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="What changed? (optional, shown in history)"
          className="min-w-[200px] flex-1"
          maxLength={200}
          onKeyDown={(e) => {
            if (e.key === "Enter" && canSave) save.mutate();
          }}
        />
        <span className={cn("text-[12px]", dirty ? "text-warning" : "text-faint")} aria-live="polite">
          {dirty ? "Unsaved changes" : "No changes"}
        </span>
        <Button variant="ghost" onClick={cancel} disabled={save.isPending}>
          Cancel
        </Button>
        <Button variant="primary" onClick={() => save.mutate()} disabled={!canSave} loading={save.isPending}>
          Save as v{nextVersion}
          <Kbd className="ml-1 hidden border-transparent bg-bg/15 text-bg/80 sm:inline-flex">⌘S</Kbd>
        </Button>
      </div>

      <Dialog open={leave !== null} onOpenChange={(o) => !o && setLeave(null)}>
        <DialogContent title="Discard unsaved changes?" description="Your edits haven't been saved as a new version yet.">
          <div className="flex justify-end gap-2">
            <DialogClose asChild>
              <Button variant="ghost">Keep editing</Button>
            </DialogClose>
            <Button
              variant="danger"
              onClick={() => {
                const to = leave;
                setLeave(null);
                onDone();
                if (to) router.push(to);
              }}
            >
              Discard changes
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
