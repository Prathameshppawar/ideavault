"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { plural } from "@/lib/format";
import type { Artifact } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent } from "@/components/ui/overlay";
import { Input } from "@/components/ui/primitives";

/** Permanent deletion: the user must type the title; the request carries `X-Confirm: delete`. */
export function DeleteArtifactDialog({ artifact, open, onOpenChange }: { artifact: Artifact; open: boolean; onOpenChange: (o: boolean) => void }) {
  const router = useRouter();
  const qc = useQueryClient();
  const [typed, setTyped] = useState("");
  const matches = typed.trim() === artifact.title.trim();
  const del = useMutation({
    mutationFn: () => api.del(`/v1/artifacts/${artifact.id}`, { headers: { "X-Confirm": "delete" } }),
    onSuccess: () => {
      toast.success("Artifact deleted");
      void qc.invalidateQueries({ queryKey: ["artifacts"] });
      router.push("/artifacts");
      // Drop the cached artifact only after we've left its page, so nothing refetches a 404.
      setTimeout(() => qc.removeQueries({ queryKey: ["artifact", artifact.id] }), 1000);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) setTyped("");
        onOpenChange(o);
      }}
    >
      <DialogContent title="Delete this artifact?" description="This permanently removes the document and its whole version history. The ideas and decisions it was built from are not affected.">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (matches) del.mutate();
          }}
        >
          <div className="mb-4 rounded-[var(--radius-md)] border border-danger/25 bg-danger-soft px-3 py-2 text-[13px] text-muted">
            {plural(artifact.current_version, "version")} will be deleted. This can&apos;t be undone.
          </div>
          <label htmlFor="confirm-title" className="mb-1.5 block text-[13px] text-muted">
            Type <span className="select-all font-medium text-fg">{artifact.title}</span> to confirm
          </label>
          <Input id="confirm-title" value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" spellCheck={false} />
          <div className="mt-5 flex justify-end gap-2">
            <DialogClose asChild>
              <Button type="button" variant="ghost">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" variant="danger" disabled={!matches} loading={del.isPending}>
              <Trash2 className="h-3.5 w-3.5" /> Delete permanently
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
