"use client";

import { useState } from "react";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, ChevronRight, CircleAlert, Import, Lock, Plug, ShieldCheck, Unplug } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { ConnectorEvent, ConnectorView } from "@/lib/types";
import { cn, fullDate, humanize, plural, timeAgo } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent } from "@/components/ui/overlay";
import { EmptyState, ErrorState, PageHeader, SectionTitle, Skeleton } from "@/components/ui/primitives";
import { StatusPill } from "@/features/usage/controls";
import { ConnectorEventList } from "@/features/usage/connector-events";
import { CategoryTag } from "./category-tag";
import { ConnectDialog, connectorKeys } from "./connect-dialog";
import { AUTH_LABEL, ConnectorIcon, connectorGroup, statusLabel, statusTone } from "./connector-meta";

export function ConnectorsScreen() {
  const list = useQuery({ queryKey: connectorKeys.list, queryFn: () => api.get<ConnectorView[]>("/v1/connectors") });
  const events = useQuery({ queryKey: connectorKeys.events, queryFn: () => api.get<ConnectorEvent[]>("/v1/connectors/events") });
  const all = list.data ?? [];
  const available = all.filter((c) => connectorGroup(c) === "available");
  const exportOnly = all.filter((c) => connectorGroup(c) === "export");
  const oauth = all.filter((c) => connectorGroup(c) === "oauth");
  const connected = all.filter((c) => c.status === "CONNECTED");

  return (
    <div className="mx-auto max-w-5xl px-5 py-8 sm:px-8 sm:py-10">
      <PageHeader
        eyebrow="Platform"
        title="Connectors"
        description="Let the agent read from — and, with your approval, act in — the tools you already use. Status is always verified, never assumed."
      />

      <div className="mb-10 flex items-start gap-3 rounded-[var(--radius-lg)] border border-border bg-surface px-4 py-3.5 text-[13px] leading-relaxed">
        <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-k-evidence" />
        <p className="text-muted">
          <span className="font-medium text-fg">External actions always ask first.</span> Connecting grants a permission; every call the agent makes through a connector (fetching a page,
          reading a repo, creating an issue) still pauses in chat for your confirmation. Fetched content is treated as untrusted data.{" "}
          <Link href="/settings#agent" className="text-fg underline decoration-border-strong underline-offset-2 hover:decoration-fg">
            Agent permissions
          </Link>
        </p>
      </div>

      {list.isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-24 w-full" />
          ))}
        </div>
      ) : list.error ? (
        <ErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <div className="space-y-12">
          <section aria-labelledby="conn-available">
            <SectionTitle action={<span className="text-[12px] text-faint">{connected.length} connected</span>}>
              <span id="conn-available">Available</span>
            </SectionTitle>
            <ul className="divide-y divide-border rounded-[var(--radius-lg)] border border-border bg-surface">
              {available.map((c) => (
                <ConnectorRow key={c.key} c={c} />
              ))}
            </ul>
          </section>

          {exportOnly.length > 0 && (
            <section aria-labelledby="conn-export">
              <SectionTitle>
                <span id="conn-export">AI chat history — via export</span>
              </SectionTitle>
              <p className="-mt-1 mb-3 max-w-2xl text-[13px] leading-relaxed text-muted">
                These platforms offer no API for reading your conversations. Bring them in with their official export in the Import Center — IdeaVault never asks for your password.
              </p>
              <ul className="grid gap-3 md:grid-cols-3">
                {exportOnly.map((c) => (
                  <ExportCard key={c.key} c={c} />
                ))}
              </ul>
            </section>
          )}

          {oauth.length > 0 && (
            <section aria-labelledby="conn-oauth">
              <SectionTitle>
                <span id="conn-oauth">Needs an OAuth app</span>
              </SectionTitle>
              <p className="-mt-1 mb-3 max-w-2xl text-[13px] leading-relaxed text-muted">
                These require an OAuth application configured on the server — <span className="text-fg">not available in this deployment</span>.
              </p>
              <ul className="divide-y divide-border rounded-[var(--radius-lg)] border border-border">
                {oauth.map((c) => (
                  <OAuthRow key={c.key} c={c} />
                ))}
              </ul>
            </section>
          )}

          <section aria-labelledby="conn-events">
            <SectionTitle
              action={
                <Link href="/usage" className="text-[12px] text-muted hover:text-fg">
                  Usage analytics →
                </Link>
              }
            >
              <span id="conn-events">Recent connector events</span>
            </SectionTitle>
            {events.isLoading ? (
              <Skeleton className="h-24 w-full" />
            ) : events.error ? (
              <ErrorState error={events.error} onRetry={() => void events.refetch()} />
            ) : (
              <ConnectorEventList
                events={(events.data ?? []).slice(0, 25)}
                empty={<EmptyState icon={Plug} title="No connector events yet" description="Connection attempts and every tool call made through a connector are logged here." className="py-10" />}
              />
            )}
          </section>
        </div>
      )}
    </div>
  );
}

function ConnectorRow({ c }: { c: ConnectorView }) {
  const [open, setOpen] = useState(false);
  const [disconnectOpen, setDisconnectOpen] = useState(false);
  const [showTools, setShowTools] = useState(false);
  const tools = c.tools ?? [];
  const connected = c.status === "CONNECTED";
  return (
    <li className="px-5 py-4">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[var(--radius-md)] border border-border bg-surface-2 text-muted">
          <ConnectorIcon connectorKey={c.key} className="h-4 w-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-[15px] font-medium text-fg">{c.name}</h3>
            <StatusPill tone={statusTone(c.status)}>{statusLabel(c.status)}</StatusPill>
            <span className="text-[12px] text-faint">{authSummary(c)}</span>
          </div>
          <p className="mt-1 text-[13px] leading-relaxed text-muted">{c.description}</p>
          {c.last_error && c.status === "ERROR" && (
            <p className="mt-2 flex items-start gap-1.5 text-[12.5px] text-danger">
              <CircleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" /> <span className="break-words">{c.last_error}</span>
            </p>
          )}
          <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-faint">
            {(c.capabilities ?? []).length > 0 && <span>{(c.capabilities ?? []).map(humanize).join(" · ")}</span>}
            {connected && c.connected_at && <span title={fullDate(c.connected_at)}>Connected {timeAgo(c.connected_at)}</span>}
            {c.last_checked_at && <span title={fullDate(c.last_checked_at)}>Checked {timeAgo(c.last_checked_at)}</span>}
            {connected && (c.granted_permissions ?? []).length > 0 && (
              <span>
                Granted <code className="font-mono text-muted">{c.granted_permissions.join(", ")}</code>
              </span>
            )}
            {connected &&
              Object.entries(c.credential_hints ?? {}).map(([k, v]) => (
                <span key={k} className="inline-flex items-center gap-1">
                  <Lock className="h-3 w-3" aria-hidden />
                  <span className="font-mono text-muted">{v}</span>
                </span>
              ))}
          </div>
          {tools.length > 0 && (
            <div className="mt-2">
              <button
                type="button"
                onClick={() => setShowTools((v) => !v)}
                aria-expanded={showTools}
                className="inline-flex items-center gap-1 rounded text-[12px] font-medium text-muted hover:text-fg"
              >
                <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", showTools && "rotate-90")} />
                {plural(tools.length, "tool")}
              </button>
              {showTools && (
                <ul className="mt-2 space-y-2 border-l border-border pl-4 animate-fade-in">
                  {tools.map((t) => (
                    <li key={t.name} className="text-[12.5px]">
                      <div className="flex flex-wrap items-center gap-2">
                        <code className="font-mono text-fg">{t.name}</code>
                        <CategoryTag category={t.category} />
                        <span className="font-mono text-[11px] text-faint">{t.permission}</span>
                      </div>
                      <p className="mt-0.5 text-muted">{t.description}</p>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
        <div className="flex shrink-0 gap-2 sm:pl-2">
          {c.status !== "NOT_CONNECTED" && (
            <Button size="sm" variant="ghost" onClick={() => setDisconnectOpen(true)}>
              <Unplug className="h-3.5 w-3.5" /> {connected ? "Disconnect" : "Reset"}
            </Button>
          )}
          <Button size="sm" variant="secondary" onClick={() => setOpen(true)}>
            {connected ? "Reconnect" : c.status === "ERROR" ? "Try again" : "Connect"}
          </Button>
        </div>
      </div>
      {open && <ConnectDialog connector={c} open={open} onOpenChange={setOpen} />}
      <DisconnectDialog c={c} open={disconnectOpen} onOpenChange={setDisconnectOpen} />
    </li>
  );
}

/** "No credentials" / "Personal access token" / "Base URL" — what connecting actually asks for. */
function authSummary(c: ConnectorView): string {
  const fields = c.credential_fields ?? [];
  if (fields.length) return fields.map((f) => f.label).join(" + ");
  return AUTH_LABEL[c.auth_type] ?? c.auth_type;
}

function ExportCard({ c }: { c: ConnectorView }) {
  return (
    <li className="flex flex-col rounded-[var(--radius-lg)] border border-border bg-surface p-4">
      <div className="flex items-center gap-2">
        <ConnectorIcon connectorKey={c.key} className="h-4 w-4 text-muted" />
        <h3 className="text-[14px] font-medium text-fg">{c.name}</h3>
      </div>
      <p className="mt-2 flex-1 text-[12.5px] leading-relaxed text-muted">{c.setup_hint}</p>
      <Link
        href="/imports"
        className="mt-3 inline-flex items-center gap-1.5 self-start rounded text-[13px] font-medium text-fg underline decoration-border-strong underline-offset-4 hover:decoration-fg"
      >
        <Import className="h-3.5 w-3.5" /> Open Import Center <ArrowRight className="h-3 w-3" />
      </Link>
    </li>
  );
}

function OAuthRow({ c }: { c: ConnectorView }) {
  return (
    <li className="flex items-start gap-3 px-5 py-3">
      <ConnectorIcon connectorKey={c.key} className="mt-0.5 h-4 w-4 shrink-0 text-faint" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="text-[14px] text-fg">{c.name}</h3>
          <StatusPill tone="muted">Unavailable</StatusPill>
        </div>
        <p className="mt-0.5 text-[12.5px] text-muted">{c.description}</p>
        <p className="mt-1 text-[12px] text-faint">
          OAuth 2.0
          {(c.permissions ?? []).length > 0 && (
            <>
              {" "}
              · would request <code className="font-mono">{c.permissions.join(", ")}</code>
            </>
          )}
        </p>
      </div>
    </li>
  );
}

function DisconnectDialog({ c, open, onOpenChange }: { c: ConnectorView; open: boolean; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient();
  const disconnect = useMutation({
    mutationFn: () => api.post(`/v1/connectors/${c.key}/disconnect`),
    onSuccess: () => {
      toast.success(c.status === "CONNECTED" ? `${c.name} disconnected` : `${c.name} reset`);
      void qc.invalidateQueries({ queryKey: connectorKeys.list });
      onOpenChange(false);
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={c.status === "CONNECTED" ? `Disconnect ${c.name}?` : `Reset ${c.name}?`}
        description="Stored credentials are deleted and the connector returns to “not connected”; the agent loses access to its tools. Past events stay in the log."
      >
        <div className="flex justify-end gap-2">
          <DialogClose asChild>
            <Button variant="ghost">Cancel</Button>
          </DialogClose>
          <Button variant="danger" loading={disconnect.isPending} onClick={() => disconnect.mutate()}>
            {c.status === "CONNECTED" ? "Disconnect" : "Reset"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
