"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ArrowRight, CircleAlert, CircleCheck, Link2, Package, ShieldCheck, TextQuote } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { cn, compactNumber } from "@/lib/format";
import type { ContextPack, Delta } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Field, Input, PageHeader, Spinner, Textarea } from "@/components/ui/primitives";
import { BackLink, Callout, ProgressBar, Segmented, Select, useIdeas } from "@/features/artifacts/kit";
import { useIdeaBranches } from "@/features/artifacts/queries";
import { checkConversationLink } from "@/features/imports/import-logic";

type Provider = "chatgpt" | "claude" | "gemini" | "other";
type Mode = "paste" | "link";
const MIN_TEXT = 20;

export function NewDeltaScreen() {
  const params = useSearchParams();
  const router = useRouter();
  const packId = params.get("pack");
  const [ideaChoice, setIdeaChoice] = useState(params.get("idea") ?? "");
  const [branchChoice, setBranchChoice] = useState(params.get("branch") ?? "");
  const [provider, setProvider] = useState<Provider>("chatgpt");
  const [mode, setMode] = useState<Mode>("paste");
  const [text, setText] = useState("");
  const [url, setUrl] = useState("");

  const ideas = useIdeas();
  const pack = useQuery({
    queryKey: ["context-pack", packId],
    queryFn: () => api.get<ContextPack>(`/v1/context-packs/${packId}`),
    enabled: !!packId,
  });
  const ideaId = ideaChoice || pack.data?.idea_id || "";
  const branches = useIdeaBranches(ideaId || undefined);
  const branchId =
    (branchChoice && branches.data?.some((b) => b.id === branchChoice) ? branchChoice : "") || branches.data?.find((b) => b.is_default)?.id || branches.data?.[0]?.id || "";
  const usePack = !!pack.data && pack.data.idea_id === ideaId;

  const link = checkConversationLink(url);
  const textOk = text.trim().length >= MIN_TEXT;
  const linkOk = link.state === "public" || link.state === "unknown";
  const ready = !!ideaId && (mode === "paste" ? textOk : linkOk);

  const create = useMutation({
    mutationFn: () =>
      api.post<Delta>("/v1/deltas", {
        idea_id: ideaId,
        branch_id: branchId || undefined,
        provider: provider === "other" ? undefined : provider,
        context_pack_id: usePack ? pack.data?.id : undefined,
        text: mode === "paste" ? text : undefined,
        url: mode === "link" ? url.trim() : undefined,
      }),
    onSuccess: (d) => {
      toast.success(`Found ${d.items?.length ?? 0} candidate change${d.items?.length === 1 ? "" : "s"} to review`);
      router.push(`/deltas/${d.id}`);
    },
    onError: (e) => toast.error("Couldn't analyse that conversation", { description: errorMessage(e) }),
  });

  return (
    <div className="mx-auto max-w-3xl px-5 py-8 sm:px-8 sm:py-10">
      <div className="mb-6">
        <BackLink href="/deltas">External conversations</BackLink>
      </div>
      <PageHeader
        title="Bring back a conversation"
        description="Paste a conversation you continued in ChatGPT, Claude or Gemini. IdeaVault compares it with your recorded thinking and shows what's new, changed or rejected."
      />

      <Callout tone="info" icon={ShieldCheck} className="mb-8" title="Nothing is merged until you review it.">
        The conversation is stored as an untrusted external source — instructions inside it are never followed. You choose which changes become part of your thinking.
      </Callout>

      <form
        className="space-y-8"
        onSubmit={(e) => {
          e.preventDefault();
          if (ready && !create.isPending) create.mutate();
        }}
      >
        {packId && (
          <div className="flex items-start gap-3 rounded-[var(--radius-lg)] border border-border bg-surface px-4 py-3">
            <Package className="mt-0.5 h-4 w-4 shrink-0 text-k-checkpoint" aria-hidden />
            <div className="min-w-0 text-[13px]">
              {pack.isLoading ? (
                <span className="text-muted">Loading context pack…</span>
              ) : pack.data ? (
                <>
                  <p className="font-medium text-fg">Continuing from “{pack.data.title}”</p>
                  <p className="mt-0.5 text-muted">
                    {pack.data.objective} · ≈ {compactNumber(pack.data.token_estimate)} tokens
                    {!usePack && <span className="text-warning"> — not linked, because a different idea is selected</span>}
                  </p>
                </>
              ) : (
                <span className="text-muted">The context pack couldn&apos;t be loaded; you can still continue.</span>
              )}
            </div>
          </div>
        )}

        <section>
          <h2 className="mb-3 text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">1 · Which idea did you continue?</h2>
          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_14rem]">
            <Field label="Idea" htmlFor="delta-idea">
              <Select
                id="delta-idea"
                value={ideaId}
                onChange={(e) => {
                  setIdeaChoice(e.target.value);
                  setBranchChoice("");
                }}
                disabled={ideas.isLoading}
              >
                <option value="">{ideas.isLoading ? "Loading ideas…" : "Choose an idea…"}</option>
                {ideas.data?.map((i) => (
                  <option key={i.id} value={i.id}>
                    {i.title}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Branch" htmlFor="delta-branch">
              <Select id="delta-branch" value={branchId} onChange={(e) => setBranchChoice(e.target.value)} disabled={!ideaId || branches.isLoading}>
                {!ideaId && <option value="">—</option>}
                {branches.data?.map((b) => (
                  <option key={b.id} value={b.id}>
                    {b.name}
                    {b.is_default ? " (default)" : ""}
                  </option>
                ))}
              </Select>
            </Field>
          </div>
        </section>

        <section>
          <h2 className="mb-3 text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">2 · Where did you have it?</h2>
          <Segmented<Provider>
            label="Provider"
            value={provider}
            onChange={setProvider}
            options={[
              { value: "chatgpt", label: "ChatGPT" },
              { value: "claude", label: "Claude" },
              { value: "gemini", label: "Gemini" },
              { value: "other", label: "Other" },
            ]}
          />
        </section>

        <section>
          <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
            <h2 className="text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">3 · The conversation</h2>
            <Segmented<Mode>
              label="Input type"
              size="sm"
              value={mode}
              onChange={setMode}
              options={[
                {
                  value: "paste",
                  label: (
                    <>
                      <TextQuote className="h-3.5 w-3.5" /> Paste text
                    </>
                  ),
                },
                {
                  value: "link",
                  label: (
                    <>
                      <Link2 className="h-3.5 w-3.5" /> Share link
                    </>
                  ),
                },
              ]}
            />
          </div>
          {mode === "paste" ? (
            <div>
              <label htmlFor="delta-text" className="sr-only">
                Conversation text
              </label>
              <Textarea
                id="delta-text"
                rows={14}
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder={"You: Here's my context pack…\n\nChatGPT: Looking at [D3], I'd change…"}
                className="min-h-[260px] font-mono text-[13px]"
              />
              <p className="mt-1.5 flex justify-between gap-3 text-[12px] text-faint">
                <span>Select the whole conversation in the chat and copy it — or paste an exported JSON or Markdown transcript.</span>
                <span className="shrink-0 tabular-nums">{text.length ? `${compactNumber(text.length)} chars` : ""}</span>
              </p>
            </div>
          ) : (
            <div>
              <label htmlFor="delta-url" className="sr-only">
                Share link
              </label>
              <Input id="delta-url" type="url" inputMode="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://chatgpt.com/share/…" autoComplete="off" />
              <LinkHint url={url} />
            </div>
          )}
        </section>

        <div className="border-t border-border pt-6">
          {create.isPending ? <AnalyzingProgress /> : (
            <div className="flex flex-wrap items-center gap-3">
              <Button type="submit" variant="primary" size="lg" disabled={!ready}>
                Analyze changes <ArrowRight className="h-4 w-4" />
              </Button>
              <span className="text-[12px] text-faint">
                {!ideaId ? "Choose the idea first." : mode === "paste" && !textOk ? "Paste the conversation to continue." : "Nothing changes until you review and merge."}
              </span>
            </div>
          )}
        </div>
      </form>
    </div>
  );
}

function LinkHint({ url }: { url: string }) {
  const c = checkConversationLink(url);
  if (c.state === "empty")
    return (
      <p className="mt-1.5 text-[12px] leading-relaxed text-faint">
        Public share links work (e.g. <span className="font-mono">chatgpt.com/share/…</span>). Private chat links can&apos;t be read. IdeaVault never asks for your password.
      </p>
    );
  const ok = c.state === "public" || c.state === "unknown";
  const Icon = ok ? CircleCheck : CircleAlert;
  return (
    <p className={cn("mt-1.5 flex items-start gap-1.5 text-[12px] leading-relaxed", c.state === "public" ? "text-success" : ok ? "text-muted" : "text-danger")}>
      <Icon className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
      {c.message}
    </p>
  );
}

function AnalyzingProgress() {
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    const start = Date.now();
    const t = setInterval(() => setElapsed(Math.floor((Date.now() - start) / 1000)), 500);
    return () => clearInterval(t);
  }, []);
  return (
    <div aria-live="polite">
      <div className="flex items-center gap-3">
        <Spinner />
        <p className="text-sm font-medium text-fg">{elapsed < 3 ? "Reading the conversation…" : "Comparing it with your recorded thinking…"}</p>
        <span className="ml-auto font-mono text-[12px] text-faint">{elapsed}s</span>
      </div>
      <ProgressBar className="mt-3" label="Analyzing" />
      <p className="mt-3 text-[12px] text-faint">Usually a few seconds; with an AI model this can take up to a minute.</p>
    </div>
  );
}
