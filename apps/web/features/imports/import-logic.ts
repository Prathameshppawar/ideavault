// Pure logic for the Import Center: file validation, link checks, statuses and the commit payload.
import type { components } from "@/types/api.generated";
import type { Import, ImportItem } from "@/lib/types";
import type { Tone } from "@/features/artifacts/artifact-meta";

export type CommitImportInput = components["schemas"]["ServiceCommitImportInput"];
export type CommitItem = components["schemas"]["ServiceCommitItem"];
export type AdapterInfo = components["schemas"]["HttpadapterInfo"];

export const ACCEPTED_EXTENSIONS = [".json", ".zip", ".md", ".markdown", ".txt", ".html", ".htm"] as const;
export const ACCEPT_ATTR = ACCEPTED_EXTENSIONS.join(",");
/** Mirrors the API's default upload limit (MaxUploadBytes, 200 MB). */
export const MAX_UPLOAD_BYTES = 200 * 1024 * 1024;

export function fileExtension(name: string): string {
  const i = name.lastIndexOf(".");
  return i >= 0 ? name.slice(i).toLowerCase() : "";
}

/** Returns a user-facing problem with the chosen file, or null when it can be uploaded. */
export function validateImportFile(file: { name: string; size: number }): string | null {
  const ext = fileExtension(file.name);
  if (!(ACCEPTED_EXTENSIONS as readonly string[]).includes(ext)) {
    return `${ext ? `“${ext}” files aren't supported` : "This file has no extension"}. Use a .json, .zip, .md, .txt or .html export.`;
  }
  if (file.size === 0) return "This file is empty.";
  if (file.size > MAX_UPLOAD_BYTES) return `This file is ${formatBytes(file.size)}; the limit is ${formatBytes(MAX_UPLOAD_BYTES)}.`;
  return null;
}

/** A friendly guess of what the user dropped, shown before upload (the server detects for real). */
export function describeFile(name: string): string {
  const lower = name.toLowerCase();
  const ext = fileExtension(lower);
  if (lower.includes("myactivity")) return "Looks like a Gemini Takeout export";
  if (lower === "conversations.json") return "Looks like a ChatGPT or Claude export";
  if (ext === ".zip") return "ZIP archive — e.g. the full ChatGPT export";
  if (ext === ".md" || ext === ".markdown") return "Markdown transcript";
  if (ext === ".txt") return "Plain-text transcript";
  if (ext === ".html" || ext === ".htm") return "Saved web page (e.g. a share page)";
  if (ext === ".json") return "JSON — the format is detected automatically";
  return "File";
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(n < 10 * 1024 * 1024 ? 1 : 0)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

export type LinkCheck =
  | { state: "empty" }
  | { state: "invalid"; message: string }
  | { state: "private"; provider: string; message: string }
  | { state: "public"; provider: string; message: string }
  | { state: "unknown"; message: string };

/** Client-side hint about whether a conversation link can be fetched without signing in. */
export function checkConversationLink(raw: string): LinkCheck {
  const s = raw.trim();
  if (!s) return { state: "empty" };
  let u: URL;
  try {
    u = new URL(/^[a-z]+:\/\//i.test(s) ? s : `https://${s}`);
  } catch {
    return { state: "invalid", message: "That doesn't look like a link." };
  }
  if (u.protocol !== "https:" && u.protocol !== "http:") return { state: "invalid", message: "Links must start with https://" };
  const host = u.hostname.replace(/^www\./, "");
  const path = u.pathname;
  if (host === "chatgpt.com" || host === "chat.openai.com") {
    if (path.startsWith("/share/")) return { state: "public", provider: "ChatGPT", message: "Public ChatGPT share link — this works." };
    if (path.startsWith("/c/") || path.startsWith("/g/"))
      return { state: "private", provider: "ChatGPT", message: "This is a private ChatGPT chat link; it only opens while you're signed in. In ChatGPT use Share → Copy link, or upload the official export." };
  }
  if (host === "claude.ai") {
    if (path.startsWith("/share/")) return { state: "public", provider: "Claude", message: "Public Claude share link — this works." };
    if (path.startsWith("/chat/"))
      return { state: "private", provider: "Claude", message: "This is a private Claude chat link. In Claude use Share → Copy link, or upload your data export." };
  }
  if (host === "gemini.google.com" || host === "g.co") {
    if (path.startsWith("/share/") || path.startsWith("/gemini/share/")) return { state: "public", provider: "Gemini", message: "Public Gemini share link — this works." };
    if (path.startsWith("/app"))
      return { state: "private", provider: "Gemini", message: "This is a private Gemini chat link. Use Share → Create public link, or upload a Google Takeout export." };
  }
  return { state: "unknown", message: "IdeaVault will try to read this public page. Pages behind a login can't be imported." };
}

// ---------- statuses ----------

export const IMPORT_STATUS_META: Record<string, { label: string; tone: Tone; live?: boolean }> = {
  QUEUED: { label: "Queued", tone: "muted", live: true },
  PROCESSING: { label: "Processing", tone: "info", live: true },
  PREVIEW: { label: "Ready to review", tone: "warning" },
  COMPLETED: { label: "Completed", tone: "success" },
  PARTIAL: { label: "Partially imported", tone: "warning" },
  FAILED: { label: "Failed", tone: "danger" },
};

export function importStatusMeta(status: string) {
  return IMPORT_STATUS_META[status] ?? { label: status.toLowerCase(), tone: "muted" as Tone };
}

export const isLiveImport = (status: string | undefined) => status === "QUEUED" || status === "PROCESSING";

/** 0..1 while an import commits; null when progress isn't measurable (queued / parsing). */
export function importProgress(im: Pick<Import, "status" | "stage" | "processed_items" | "failed_items" | "total_items">): number | null {
  if (im.status === "COMPLETED" || im.status === "PARTIAL") return 1;
  if (im.status !== "PROCESSING" || im.stage !== "commit" || !im.total_items) return null;
  return Math.min(1, (im.processed_items + im.failed_items) / im.total_items);
}

export function importSourceLabel(im: Pick<Import, "source_kind" | "filename" | "uri">): string {
  if (im.source_kind === "url" && im.uri) {
    try {
      const u = new URL(im.uri);
      return `${u.hostname.replace(/^www\./, "")}${u.pathname.length > 1 ? u.pathname : ""}`;
    } catch {
      return im.uri;
    }
  }
  if (im.source_kind === "paste") return "Pasted conversation";
  return im.filename || "Uploaded file";
}

const PROVIDER_NAMES: Record<string, string> = { chatgpt: "ChatGPT", claude: "Claude", gemini: "Gemini", markdown: "Markdown", text: "Text", json: "JSON", web: "Web page" };
export const providerName = (p: string | undefined | null) => (p ? (PROVIDER_NAMES[p.toLowerCase()] ?? p) : "");

/** "chatgpt/v2" → "ChatGPT · v2". */
export function adapterLabel(adapter: string | undefined | null): string {
  if (!adapter) return "";
  const [p, v] = adapter.split("/");
  return v ? `${providerName(p)} · ${v}` : providerName(p);
}

// ---------- preview selection & commit payload ----------

export type Destination = { mode: "new"; title: string } | { mode: "idea"; ideaId: string } | { mode: "store" };

export interface ItemChoice {
  include: boolean;
  dest: Destination;
}

export const isSelectable = (it: Pick<ImportItem, "status">) => it.status !== "FAILED";

/**
 * Defaults for the preview: duplicates and failures are left out; with a target idea everything
 * goes there, otherwise the first conversation starts a new idea and the rest are just stored.
 */
export function defaultChoices(items: ImportItem[], targetIdeaId?: string | null): Record<string, ItemChoice> {
  const out: Record<string, ItemChoice> = {};
  let first = true;
  for (const it of [...items].sort((a, b) => a.position - b.position)) {
    const include = it.status !== "FAILED" && it.status !== "DUPLICATE";
    let dest: Destination;
    if (targetIdeaId) dest = { mode: "idea", ideaId: targetIdeaId };
    else if (include && first) dest = { mode: "new", title: it.title };
    else dest = { mode: "store" };
    if (include) first = false;
    out[it.id] = { include, dest };
  }
  return out;
}

export function buildCommitPayload(items: ImportItem[], choices: Record<string, ItemChoice>, extract: boolean): CommitImportInput {
  const out: CommitItem[] = [];
  let duplicates = false;
  for (const it of [...items].sort((a, b) => a.position - b.position)) {
    if (!isSelectable(it)) continue;
    const c = choices[it.id];
    if (!c) continue;
    const ci: CommitItem = { item_id: it.id, include: c.include };
    if (c.include) {
      if (it.status === "DUPLICATE") duplicates = true;
      if (c.dest.mode === "idea" && c.dest.ideaId) ci.idea_id = c.dest.ideaId;
      if (c.dest.mode === "new") {
        ci.new_idea = true;
        const t = c.dest.title.trim();
        if (t && t !== it.title) ci.new_idea_title = t;
      }
    }
    out.push(ci);
  }
  const payload: CommitImportInput = { extract, items: out };
  if (duplicates) payload.include_duplicates = true;
  return payload;
}

export function selectedCount(items: ImportItem[], choices: Record<string, ItemChoice>): number {
  return items.filter((it) => isSelectable(it) && choices[it.id]?.include).length;
}
