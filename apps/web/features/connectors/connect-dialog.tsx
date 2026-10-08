"use client";

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Lock, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { ConnectorView } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Checkbox, Dialog, DialogClose, DialogContent } from "@/components/ui/overlay";
import { Field, Input } from "@/components/ui/primitives";
import { connectPayload, initialPermissions } from "./connector-meta";

export const connectorKeys = { list: ["connectors"] as const, events: ["connector-events"] as const };

/** Credentials are write-only: inputs start empty and stored values are only shown as masked hints. */
export function ConnectDialog({ connector: c, open, onOpenChange }: { connector: ConnectorView; open: boolean; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient();
  const fields = c.credential_fields ?? [];
  const [values, setValues] = useState<Record<string, string>>({});
  const [perms, setPerms] = useState<string[]>(() => initialPermissions(c));
  const connect = useMutation({
    mutationFn: () => api.post<ConnectorView>(`/v1/connectors/${c.key}/connect`, connectPayload(values, perms)),
    onSuccess: () => {
      toast.success(`${c.name} connected`);
      close(false);
    },
    onSettled: () => {
      // A failed verification is recorded too (status ERROR + event), so refresh either way.
      void qc.invalidateQueries({ queryKey: connectorKeys.list });
      void qc.invalidateQueries({ queryKey: connectorKeys.events });
      void qc.invalidateQueries({ queryKey: ["usage"] });
    },
  });
  const close = (v: boolean) => {
    if (!v) {
      setValues({});
      setPerms(initialPermissions(c));
      connect.reset();
    }
    onOpenChange(v);
  };
  const missing = fields.some((f) => f.required && !(values[f.name] ?? "").trim());
  const reconnect = c.status === "CONNECTED";
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent
        title={`${reconnect ? "Reconnect" : "Connect"} ${c.name}`}
        description={
          fields.length
            ? "IdeaVault verifies these with a real request, then stores them encrypted. They are never shown again."
            : "No credentials needed — connecting grants the agent the permissions below."
        }
      >
        <form
          className="space-y-5"
          onSubmit={(e) => {
            e.preventDefault();
            if (!missing) connect.mutate();
          }}
        >
          {c.setup_hint && <p className="rounded-[var(--radius-md)] bg-surface-2 px-3 py-2 text-[12.5px] leading-relaxed text-muted">{c.setup_hint}</p>}
          {fields.map((f, i) => {
            const hint = c.credential_hints?.[f.name];
            return (
              <Field
                key={f.name}
                label={f.required ? f.label : `${f.label} (optional)`}
                htmlFor={`cred-${f.name}`}
                hint={hint ? `Currently ${hint}. Enter it again to reconnect.` : f.hint}
              >
                <Input
                  id={`cred-${f.name}`}
                  type={f.secret ? "password" : "url"}
                  autoComplete="off"
                  spellCheck={false}
                  data-1p-ignore={f.secret ? true : undefined}
                  className={f.secret ? "font-mono" : undefined}
                  placeholder={f.secret ? "Paste token" : f.hint}
                  value={values[f.name] ?? ""}
                  onChange={(e) => setValues((v) => ({ ...v, [f.name]: e.target.value }))}
                  autoFocus={i === 0}
                />
              </Field>
            );
          })}
          {(c.permissions ?? []).length > 0 && (
            <fieldset>
              <legend className="mb-2 text-[13px] font-medium text-muted">Permissions granted to the agent</legend>
              <div className="space-y-2">
                {(c.permissions ?? []).map((p) => {
                  const tools = (c.tools ?? []).filter((t) => t.permission === p);
                  return (
                    <label key={p} className="flex cursor-pointer items-start gap-2.5 text-[13px]">
                      <Checkbox
                        className="mt-0.5"
                        checked={perms.includes(p)}
                        onCheckedChange={(v) => setPerms((s) => (v ? [...new Set([...s, p])] : s.filter((x) => x !== p)))}
                      />
                      <span>
                        <code className="font-mono text-[12px] text-fg">{p}</code>
                        {tools.length > 0 && <span className="block text-[12px] text-faint">{tools.map((t) => t.name).join(", ")}</span>}
                      </span>
                    </label>
                  );
                })}
              </div>
              <p className="mt-2 text-[12px] text-faint">Each external action still asks for your confirmation in chat.</p>
            </fieldset>
          )}
          {connect.error && (
            <p role="alert" className="rounded-[var(--radius-md)] border border-danger/30 bg-danger-soft px-3 py-2 text-[13px] text-danger">
              {errorMessage(connect.error)}
            </p>
          )}
          <div className="flex items-center justify-between gap-3">
            <span className="inline-flex items-center gap-1.5 text-[12px] text-faint">{fields.some((f) => f.secret) && (<><Lock className="h-3 w-3" /> Encrypted at rest</>)}</span>
            <div className="flex gap-2">
              <DialogClose asChild>
                <Button type="button" variant="ghost">
                  Cancel
                </Button>
              </DialogClose>
              <Button type="submit" variant="primary" loading={connect.isPending} disabled={missing || ((c.permissions ?? []).length > 0 && perms.length === 0)}>
                <ShieldCheck className="h-3.5 w-3.5" /> {fields.length ? "Verify & connect" : "Connect"}
              </Button>
            </div>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
