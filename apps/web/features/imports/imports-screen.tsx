"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, CircleAlert, CircleCheck, ClipboardPaste, FileUp, Inbox, Link2, Lock, ShieldCheck, Upload } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { cn, compactNumber, fullDate, plural, timeAgo, truncate } from "@/lib/format";
import type { Import } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/overlay";
import { ErrorState, Input, PageHeader, SectionTitle, Skeleton, Textarea } from "@/components/ui/primitives";
import { Pill, ProgressBar, Select, useIdeas } from "@/features/artifacts/kit";
import { Dropzone } from "./dropzone";
import {
  adapterLabel,
  checkConversationLink,
  formatBytes,
  importProgress,
  importSourceLabel,
  importStatusMeta,
  isLiveImport,
  providerName,
  validateImportFile,
  type AdapterInfo,
} from "./import-logic";
import { uploadWithProgress } from "./upload";

type Tab = "upload" | "paste" | "link";
const TABS: Tab[] = ["upload", "paste", "link"];

export function ImportsScreen() {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const raw = params.get("tab") as Tab | null;
  const tab: Tab = raw && TABS.includes(raw) ? raw : "upload";

  return (
    <div className="mx-auto max-w-6xl px-5 py-8 sm:px-8 sm:py-10">
      <PageHeader
        title="Import Center"
        description="Bring conversations from ChatGPT, Claude and Gemini into your vault. You preview everything before it's saved, and choose where each conversation goes."
      />
      <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_19rem]">
        <Tabs value={tab} onValueChange={(v) => router.replace(v === "upload" ? pathname : `${pathname}?tab=${v}`, { scroll: false })} className="min-w-0">
          <TabsList>
            <TabsTrigger value="upload">
              <Upload className="h-3.5 w-3.5" /> Upload
            </TabsTrigger>
            <TabsTrigger value="paste">
              <ClipboardPaste className="h-3.5 w-3.5" /> Paste
            </TabsTrigger>
            <TabsTrigger value="link">
              <Link2 className="h-3.5 w-3.5" /> Link
            </TabsTrigger>
          </TabsList>
          <TabsContent value="upload">
            <UploadPanel />
          </TabsContent>
          <TabsContent value="paste">
            <PastePanel />
          </TabsContent>
          <TabsContent value="link">
            <LinkPanel />
          </TabsContent>
        </Tabs>
        <FormatsAside />
      </div>
      <ImportHistory />
    </div>
  );
}

// ---------- options shared by the three entry modes ----------

function useAdapters() {
  return useQuery({ queryKey: ["import-adapters"], queryFn: () => api.get<AdapterInfo[]>("/v1/imports/adapters"), staleTime: 5 * 60_000 });
}

function ImportOptions({ adapter, setAdapter, target, setTarget, hideAdapter }: { adapter: string; setAdapter: (v: string) => void; target: string; setTarget: (v: string) => void; hideAdapter?: boolean }) {
  const adapters = useAdapters();
  const ideas = useIdeas();
  return (
    <div className={cn("mt-5 grid gap-3", !hideAdapter && "sm:grid-cols-2")}>
      {!hideAdapter && (
        <div>
          <label htmlFor="imp-adapter" className="mb-1.5 block text-[13px] font-medium text-muted">
            Format
          </label>
          <Select id="imp-adapter" value={adapter} onChange={(e) => setAdapter(e.target.value)}>
            <option value="">Detect automatically</option>
            {adapters.data?.map((a) => (
              <option key={a.name} value={a.name}>
                {adapterLabel(a.name)}
              </option>
            ))}
          </Select>
        </div>
      )}
      <div>
        <label htmlFor="imp-target" className="mb-1.5 block text-[13px] font-medium text-muted">
          Add to idea
        </label>
        <Select id="imp-target" value={target} onChange={(e) => setTarget(e.target.value)} disabled={ideas.isLoading}>
          <option value="">Decide per conversation</option>
          {ideas.data?.map((i) => (
            <option key={i.id} value={i.id}>
              {i.title}
            </option>
          ))}
        </Select>
      </div>
    </div>
  );
}

function useStartImport() {
  const router = useRouter();
  const qc = useQueryClient();
  return (im: Import) => {
    void qc.invalidateQueries({ queryKey: ["imports"] });
    qc.setQueryData(["import", im.id], { import: im, items: [] });
    router.push(`/imports/${im.id}`);
  };
}

function UploadPanel() {
  const [file, setFile] = useState<File | null>(null);
  const [adapter, setAdapter] = useState("");
  const [target, setTarget] = useState("");
  const [progress, setProgress] = useState(0);
  const start = useStartImport();
  const invalid = file ? validateImportFile(file) : null;
  const upload = useMutation({
    mutationFn: () => {
      const form = new FormData();
      form.append("file", file as File, (file as File).name);
      if (adapter) form.append("adapter", adapter);
      if (target) form.append("target_idea_id", target);
      setProgress(0);
      return uploadWithProgress<Import>("/v1/imports", form, setProgress);
    },
    onSuccess: start,
    onError: (e) => toast.error("Upload failed", { description: errorMessage(e) }),
  });
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (file && !invalid) upload.mutate();
      }}
    >
      <Dropzone file={file} onFile={setFile} disabled={upload.isPending} />
      <ImportOptions adapter={adapter} setAdapter={setAdapter} target={target} setTarget={setTarget} />
      <div className="mt-6">
        {upload.isPending ? (
          <div aria-live="polite">
            <div className="mb-2 flex items-center justify-between text-[13px]">
              <span className="font-medium text-fg">{progress < 1 ? "Uploading…" : "Reading the file…"}</span>
              <span className="font-mono text-[12px] text-faint">
                {progress < 1 && file ? `${formatBytes(Math.round(file.size * progress))} of ${formatBytes(file.size)}` : ""}
              </span>
            </div>
            <ProgressBar value={progress < 1 ? progress : undefined} label="Upload progress" />
          </div>
        ) : (
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" variant="primary" disabled={!file || !!invalid}>
              Upload &amp; preview <ArrowRight className="h-4 w-4" />
            </Button>
            <span className="text-[12px] text-faint">Nothing is saved until you confirm the preview.</span>
          </div>
        )}
      </div>
    </form>
  );
}

function PastePanel() {
  const [text, setText] = useState("");
  const [adapter, setAdapter] = useState("");
  const [target, setTarget] = useState("");
  const start = useStartImport();
  const paste = useMutation({
    mutationFn: () => api.post<Import>("/v1/imports", { source_kind: "paste", text, adapter: adapter || undefined, target_idea_id: target || undefined }),
    onSuccess: start,
    onError: (e) => toast.error("Couldn't start the import", { description: errorMessage(e) }),
  });
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (text.trim()) paste.mutate();
      }}
    >
      <label htmlFor="imp-paste" className="mb-1.5 block text-[13px] font-medium text-muted">
        Conversation
      </label>
      <Textarea
        id="imp-paste"
        rows={13}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={"User: How should a small clinic handle double bookings?\n\nAssistant: Prevent them at booking time…"}
        className="min-h-[260px] font-mono text-[13px]"
      />
      <p className="mt-1.5 flex justify-between gap-3 text-[12px] text-faint">
        <span>Paste a chat copied from ChatGPT, Claude or Gemini, a Markdown transcript, or exported JSON.</span>
        <span className="shrink-0 tabular-nums">{text.length ? `${compactNumber(text.length)} chars` : ""}</span>
      </p>
      <ImportOptions adapter={adapter} setAdapter={setAdapter} target={target} setTarget={setTarget} />
      <div className="mt-6 flex flex-wrap items-center gap-3">
        <Button type="submit" variant="primary" disabled={!text.trim()} loading={paste.isPending}>
          Preview import <ArrowRight className="h-4 w-4" />
        </Button>
        <span className="text-[12px] text-faint">Nothing is saved until you confirm the preview.</span>
      </div>
    </form>
  );
}

function LinkPanel() {
  const [url, setUrl] = useState("");
  const [target, setTarget] = useState("");
  const start = useStartImport();
  const check = checkConversationLink(url);
  const blocked = check.state === "private" || check.state === "invalid" || check.state === "empty";
  const fetchUrl = useMutation({
    mutationFn: () => api.post<Import>("/v1/imports", { source_kind: "url", url: url.trim(), target_idea_id: target || undefined }),
    onSuccess: start,
    onError: (e) => toast.error("Couldn't import that link", { description: errorMessage(e) }),
  });
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (!blocked) fetchUrl.mutate();
      }}
    >
      <label htmlFor="imp-url" className="mb-1.5 block text-[13px] font-medium text-muted">
        Public share link
      </label>
      <Input id="imp-url" type="url" inputMode="url" autoComplete="off" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://chatgpt.com/share/…" />
      {check.state !== "empty" && (
        <p
          className={cn(
            "mt-2 flex items-start gap-1.5 text-[12.5px] leading-relaxed",
            check.state === "public" ? "text-success" : check.state === "unknown" ? "text-muted" : "text-danger",
          )}
          role={check.state === "private" || check.state === "invalid" ? "alert" : undefined}
        >
          {check.state === "public" ? <CircleCheck className="mt-0.5 h-3.5 w-3.5 shrink-0" /> : <CircleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />}
          {check.message}
        </p>
      )}
      <ul className="mt-4 space-y-2 rounded-[var(--radius-lg)] border border-border bg-surface px-4 py-3 text-[13px]">
        <li className="flex items-start gap-2">
          <CircleCheck className="mt-0.5 h-3.5 w-3.5 shrink-0 text-success" aria-hidden />
          <span className="text-muted">
            Public share links work — <span className="font-mono text-[12px] text-fg">chatgpt.com/share/…</span>, <span className="font-mono text-[12px] text-fg">claude.ai/share/…</span>,{" "}
            <span className="font-mono text-[12px] text-fg">g.co/gemini/share/…</span>
          </span>
        </li>
        <li className="flex items-start gap-2">
          <CircleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-danger" aria-hidden />
          <span className="text-muted">
            Private links like <span className="font-mono text-[12px] text-fg">chatgpt.com/c/…</span> only open while you&apos;re signed in, so they can&apos;t be read. Use <span className="text-fg">Share → Copy link</span>, or upload the official export.
          </span>
        </li>
        <li className="flex items-start gap-2">
          <Lock className="mt-0.5 h-3.5 w-3.5 shrink-0 text-faint" aria-hidden />
          <span className="text-muted">IdeaVault never asks for your ChatGPT, Claude or Google password.</span>
        </li>
      </ul>
      <ImportOptions adapter="" setAdapter={() => {}} target={target} setTarget={setTarget} hideAdapter />
      <div className="mt-6 flex flex-wrap items-center gap-3">
        <Button type="submit" variant="primary" disabled={blocked} loading={fetchUrl.isPending}>
          Fetch &amp; preview <ArrowRight className="h-4 w-4" />
        </Button>
      </div>
    </form>
  );
}

const FORMATS = [
  { name: "ChatGPT", how: "Settings → Data controls → Export data. Upload the .zip or conversations.json." },
  { name: "Claude", how: "Settings → Privacy → Export data. Upload conversations.json." },
  { name: "Gemini", how: "Google Takeout → My Activity → Gemini Apps (JSON). Upload MyActivity.json." },
  { name: "Markdown & text", how: "Transcripts with “User:” / “Assistant:” turns, or a copied chat." },
  { name: "Generic JSON", how: "A list of messages with role and content." },
];

function FormatsAside() {
  return (
    <aside className="space-y-6 lg:pt-12" aria-label="Supported formats">
      <div>
        <SectionTitle>Supported formats</SectionTitle>
        <dl className="space-y-3">
          {FORMATS.map((f) => (
            <div key={f.name}>
              <dt className="text-[13px] font-medium text-fg">{f.name}</dt>
              <dd className="mt-0.5 text-[12.5px] leading-relaxed text-muted">{f.how}</dd>
            </div>
          ))}
        </dl>
      </div>
      <div className="flex items-start gap-2.5 border-t border-border pt-5 text-[12.5px] leading-relaxed text-muted">
        <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-faint" aria-hidden />
        <p>
          Imported conversations are stored as <span className="text-fg">untrusted sources</span>: instructions inside them are never followed, and anything extracted stays a proposal until you accept it.
        </p>
      </div>
    </aside>
  );
}

// ---------- history ----------

function SourceIcon({ kind }: { kind: string }) {
  const Icon = kind === "url" ? Link2 : kind === "paste" ? ClipboardPaste : FileUp;
  return (
    <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[var(--radius-md)] bg-surface-2 text-muted">
      <Icon className="h-4 w-4" aria-hidden />
    </span>
  );
}

export function importCounts(im: Import): string {
  switch (im.status) {
    case "QUEUED":
      return "Waiting to start";
    case "PROCESSING":
      return im.stage === "commit" ? `Importing ${im.processed_items + im.failed_items} of ${im.total_items}` : "Reading the input…";
    case "PREVIEW":
      return `${plural(im.total_items - im.failed_items, "conversation")} found`;
    case "FAILED":
      return im.error ? truncate(im.error, 90) : "Failed";
    default:
      return `${plural(im.processed_items, "conversation")} imported${im.failed_items ? ` · ${im.failed_items} failed` : ""}`;
  }
}

function ImportHistory() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["imports"],
    queryFn: () => api.get<Import[]>("/v1/imports"),
    refetchInterval: (q) => (q.state.data?.some((i) => isLiveImport(i.status)) ? 1500 : false),
  });
  return (
    <section className="mt-14" aria-labelledby="imp-history">
      <SectionTitle>
        <span id="imp-history">Recent imports</span>
      </SectionTitle>
      {isLoading ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      ) : error ? (
        <ErrorState error={error} onRetry={() => void refetch()} />
      ) : !data?.length ? (
        <div className="flex items-center gap-3 rounded-[var(--radius-lg)] border border-dashed border-border px-4 py-6 text-[13px] text-muted">
          <Inbox className="h-4 w-4 text-faint" aria-hidden /> No imports yet. Your first one will show up here with its progress.
        </div>
      ) : (
        <ul className="border-t border-border">
          {data.map((im) => {
            const s = importStatusMeta(im.status);
            const live = isLiveImport(im.status);
            const meta = [providerName(im.provider) || adapterLabel(im.adapter), importCounts(im), im.byte_size ? formatBytes(im.byte_size) : ""].filter(Boolean);
            return (
              <li key={im.id}>
                <Link href={`/imports/${im.id}`} className="group -mx-3 flex items-center gap-3.5 border-b border-border px-3 py-3.5 transition-colors hover:bg-surface-2/50 sm:rounded-[var(--radius-md)]">
                  <SourceIcon kind={im.source_kind} />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[14px] font-medium text-fg group-hover:text-accent">{importSourceLabel(im)}</p>
                    <p className={cn("truncate text-[12px]", im.status === "FAILED" ? "text-danger" : "text-faint")}>{meta.join(" · ")}</p>
                    {live && <ProgressBar value={importProgress(im)} className="mt-2 max-w-xs" tone="info" label="Import progress" />}
                  </div>
                  <div className="flex shrink-0 flex-col items-end gap-1 sm:flex-row sm:items-center sm:gap-4">
                    <Pill tone={s.tone} pulse={live}>
                      {s.label}
                    </Pill>
                    <span className="text-right text-[12px] text-faint sm:w-24" title={fullDate(im.created_at)}>
                      {timeAgo(im.created_at)}
                    </span>
                  </div>
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
