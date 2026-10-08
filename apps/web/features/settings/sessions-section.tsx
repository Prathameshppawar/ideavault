"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Laptop, Plus, Terminal } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Session } from "@/lib/types";
import { fullDate, plural, shortDate, timeAgo } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent } from "@/components/ui/overlay";
import { ErrorState, SkeletonLines } from "@/components/ui/primitives";
import { describeUserAgent } from "./policy";
import { CreateTokenDialog } from "./token-dialog";

export function SessionsSection() {
  const { data, isLoading, error, refetch } = useQuery({ queryKey: ["sessions"], queryFn: () => api.get<Session[]>("/v1/auth/sessions") });
  const [createOpen, setCreateOpen] = useState(false);
  const [revoking, setRevoking] = useState<Session | null>(null);
  const [showAll, setShowAll] = useState(false);
  if (isLoading) return <SkeletonLines lines={5} />;
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;
  const tokens = data.filter((s) => s.kind === "api_token");
  const browsers = data.filter((s) => s.kind !== "api_token");
  const shownBrowsers = showAll ? browsers : browsers.slice(0, 6);
  return (
    <div className="space-y-8">
      <div>
        <div className="mb-2 flex flex-wrap items-center justify-between gap-3">
          <h3 className="text-[14px] font-medium text-fg">API tokens</h3>
          <Button size="sm" variant="secondary" onClick={() => setCreateOpen(true)}>
            <Plus className="h-3.5 w-3.5" /> Create token
          </Button>
        </div>
        {tokens.length === 0 ? (
          <p className="text-[13px] text-faint">No API tokens. Create one to use the IdeaVault API from scripts and other tools.</p>
        ) : (
          <ul className="divide-y divide-border rounded-[var(--radius-lg)] border border-border bg-surface">
            {tokens.map((s) => (
              <SessionRow key={s.id} s={s} icon={KeyRound} title={s.label || "API token"} onRevoke={() => setRevoking(s)} />
            ))}
          </ul>
        )}
      </div>
      <div>
        <div className="mb-2 flex flex-wrap items-baseline justify-between gap-3">
          <h3 className="text-[14px] font-medium text-fg">Signed-in sessions</h3>
          <span className="text-[12px] text-faint">{plural(browsers.length, "active session")}</span>
        </div>
        <ul className="divide-y divide-border rounded-[var(--radius-lg)] border border-border bg-surface">
          {shownBrowsers.map((s) => {
            const label = describeUserAgent(s.user_agent);
            return <SessionRow key={s.id} s={s} icon={/curl|Node/.test(label) ? Terminal : Laptop} title={label} onRevoke={() => setRevoking(s)} />;
          })}
        </ul>
        <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
          <p className="text-[12px] text-faint">Revoking the session you are using right now signs you out.</p>
          {browsers.length > 6 && (
            <Button size="xs" variant="ghost" onClick={() => setShowAll((v) => !v)}>
              {showAll ? "Show fewer" : `Show all ${browsers.length}`}
            </Button>
          )}
        </div>
      </div>
      <CreateTokenDialog open={createOpen} onOpenChange={setCreateOpen} />
      {revoking && <RevokeDialog s={revoking} onClose={() => setRevoking(null)} />}
    </div>
  );
}

function SessionRow({ s, icon: Icon, title, onRevoke }: { s: Session; icon: React.ComponentType<{ className?: string }>; title: string; onRevoke: () => void }) {
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <Icon className="h-4 w-4 shrink-0 text-faint" />
      <div className="min-w-0 flex-1">
        <p className="truncate text-[13.5px] text-fg" title={s.user_agent || undefined}>
          {title}
        </p>
        <p className="text-[12px] text-faint">
          <span title={fullDate(s.created_at)}>Created {shortDate(s.created_at)}</span> · <span title={fullDate(s.last_seen_at)}>last used {timeAgo(s.last_seen_at)}</span> ·{" "}
          <span title={fullDate(s.expires_at)}>expires {shortDate(s.expires_at)}</span>
        </p>
      </div>
      <Button size="sm" variant="ghost" onClick={onRevoke} aria-label={`Revoke ${title}`}>
        Revoke
      </Button>
    </li>
  );
}

function RevokeDialog({ s, onClose }: { s: Session; onClose: () => void }) {
  const qc = useQueryClient();
  const isToken = s.kind === "api_token";
  const revoke = useMutation({
    mutationFn: () => api.del(`/v1/auth/sessions/${s.id}`),
    onSuccess: () => {
      toast.success(isToken ? "Token revoked" : "Session revoked");
      void qc.invalidateQueries({ queryKey: ["sessions"] });
      onClose();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent
        title={isToken ? `Revoke “${s.label || "API token"}”?` : "Revoke this session?"}
        description={isToken ? "Anything using this token stops working immediately." : `${describeUserAgent(s.user_agent)} will be signed out. If it's this browser, you'll need to sign in again.`}
      >
        <div className="flex justify-end gap-2">
          <DialogClose asChild>
            <Button variant="ghost">Cancel</Button>
          </DialogClose>
          <Button variant="danger" loading={revoke.isPending} onClick={() => revoke.mutate()}>
            Revoke
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
