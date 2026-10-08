"use client";

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, KeyRound, TriangleAlert } from "lucide-react";
import { api, errorMessage } from "@/lib/api";
import type { components } from "@/types/api.generated";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent } from "@/components/ui/overlay";
import { Field, Input } from "@/components/ui/primitives";

type TokenResp = components["schemas"]["HttptokenResp"];

/**
 * Create a personal API token. The secret is shown exactly once: it lives only in this
 * dialog's state and is discarded when the dialog closes.
 */
export function CreateTokenDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient();
  const [label, setLabel] = useState("");
  const [created, setCreated] = useState<TokenResp | null>(null);
  const [copied, setCopied] = useState(false);
  const create = useMutation({
    mutationFn: () => api.post<TokenResp>("/v1/auth/tokens", { label: label.trim() || "API token" }),
    onSuccess: (r) => {
      setCreated(r);
      void qc.invalidateQueries({ queryKey: ["sessions"] });
    },
  });
  const close = (v: boolean) => {
    if (!v) {
      setLabel("");
      setCreated(null);
      setCopied(false);
      create.reset();
    }
    onOpenChange(v);
  };
  const copy = async () => {
    if (!created) return;
    try {
      await navigator.clipboard.writeText(created.token);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent
        title={created ? "Copy your new token" : "Create an API token"}
        description={created ? undefined : "For scripts and integrations. It acts as you, with full access to your vault, until you revoke it."}
      >
        {created ? (
          <div className="space-y-4">
            <div className="flex items-start gap-2.5 rounded-[var(--radius-md)] border border-warning/30 bg-warning-soft px-3 py-2.5 text-[13px]" role="alert">
              <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
              <p className="text-fg">This is the only time the token is shown. Store it somewhere safe now — IdeaVault keeps only a hash.</p>
            </div>
            <div className="flex gap-2">
              <Input readOnly value={created.token} aria-label="New API token" className="font-mono text-[12.5px]" onFocus={(e) => e.currentTarget.select()} />
              <Button type="button" variant="secondary" onClick={() => void copy()} aria-label="Copy token">
                {copied ? <Check className="h-4 w-4 text-success" /> : <Copy className="h-4 w-4" />}
                {copied ? "Copied" : "Copy"}
              </Button>
            </div>
            <p className="text-[12.5px] leading-relaxed text-muted">
              Send it as <code className="rounded bg-surface-2 px-1 font-mono text-[12px] text-fg">Authorization: Bearer &lt;token&gt;</code>. Label:{" "}
              <span className="text-fg">{created.session?.label || label || "API token"}</span>
              {created.session?.expires_at && <> · expires {new Date(created.session.expires_at).toLocaleDateString()}</>}.
            </p>
            <div className="flex justify-end">
              <DialogClose asChild>
                <Button variant="primary">I&apos;ve stored it</Button>
              </DialogClose>
            </div>
          </div>
        ) : (
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate();
            }}
          >
            <Field label="Label" htmlFor="token-label" hint="So you can recognise it later, e.g. “Raycast” or “backup script”.">
              <Input id="token-label" value={label} onChange={(e) => setLabel(e.target.value)} maxLength={80} placeholder="API token" autoFocus />
            </Field>
            {create.error && (
              <p role="alert" className="rounded-[var(--radius-md)] border border-danger/30 bg-danger-soft px-3 py-2 text-[13px] text-danger">
                {errorMessage(create.error)}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <DialogClose asChild>
                <Button type="button" variant="ghost">
                  Cancel
                </Button>
              </DialogClose>
              <Button type="submit" variant="primary" loading={create.isPending}>
                <KeyRound className="h-3.5 w-3.5" /> Create token
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
