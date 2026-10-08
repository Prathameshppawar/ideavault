"use client";

import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from "react";
import { useRouter } from "next/navigation";
import { ArrowUp, FileUp, Import, Link2, Mic, MicOff, Plus, Square } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { cn } from "@/lib/format";
import { Dropdown, DropdownContent, DropdownItem, DropdownTrigger, Tooltip } from "@/components/ui/overlay";
import type { Import as ImportRecord } from "@/lib/types";

// Minimal typing for the Web Speech API (not in TS DOM lib everywhere).
interface SpeechRecognitionLike {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  start(): void;
  stop(): void;
  onresult: ((e: { resultIndex: number; results: ArrayLike<{ 0: { transcript: string }; isFinal: boolean }> }) => void) | null;
  onend: (() => void) | null;
  onerror: ((e: { error: string }) => void) | null;
}

function getRecognition(): SpeechRecognitionLike | null {
  if (typeof window === "undefined") return null;
  const W = window as unknown as { SpeechRecognition?: new () => SpeechRecognitionLike; webkitSpeechRecognition?: new () => SpeechRecognitionLike };
  const Ctor = W.SpeechRecognition ?? W.webkitSpeechRecognition;
  return Ctor ? new Ctor() : null;
}

export function Composer({
  onSend,
  onStop,
  streaming,
  placeholder = "Think out loud…",
  ideaId,
  autoFocus,
  initialValue,
  compact,
}: {
  onSend: (text: string) => void;
  onStop: () => void;
  streaming: boolean;
  placeholder?: string;
  ideaId?: string | null;
  autoFocus?: boolean;
  initialValue?: string;
  compact?: boolean;
}) {
  const router = useRouter();
  const [text, setText] = useState(initialValue ?? "");
  const [listening, setListening] = useState(false);
  const [interim, setInterim] = useState("");
  const [uploading, setUploading] = useState(false);
  const ref = useRef<HTMLTextAreaElement>(null);
  const recRef = useRef<SpeechRecognitionLike | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const speechSupported = typeof window !== "undefined" && Boolean(getRecognition());

  const resize = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 280)}px`;
  }, []);
  useEffect(resize, [text, resize]);
  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, [autoFocus]);

  const submit = () => {
    const t = text.trim();
    if (!t || streaming) return;
    onSend(t);
    setText("");
    setInterim("");
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      submit();
    }
    if (e.key === "Escape" && streaming) onStop();
  };

  // Voice: browser-side recognition only; audio never leaves the browser, only text is sent.
  const toggleMic = () => {
    if (listening) {
      recRef.current?.stop();
      return;
    }
    const rec = getRecognition();
    if (!rec) {
      toast.error("Speech recognition isn't supported in this browser. Try Chrome or Edge.");
      return;
    }
    rec.lang = navigator.language || "en-US";
    rec.continuous = true;
    rec.interimResults = true;
    rec.onresult = (e) => {
      let finalText = "";
      let interimText = "";
      for (let i = e.resultIndex; i < e.results.length; i++) {
        const r = e.results[i];
        if (r.isFinal) finalText += r[0].transcript;
        else interimText += r[0].transcript;
      }
      if (finalText) setText((t) => (t ? `${t.trimEnd()} ${finalText.trim()}` : finalText.trim()));
      setInterim(interimText);
    };
    rec.onerror = (e) => {
      if (e.error !== "aborted" && e.error !== "no-speech") toast.error(`Microphone: ${e.error}`);
    };
    rec.onend = () => {
      setListening(false);
      setInterim("");
    };
    recRef.current = rec;
    rec.start();
    setListening(true);
  };

  const uploadFile = async (file: File) => {
    setUploading(true);
    try {
      const form = new FormData();
      form.append("file", file);
      form.append("sync", "true");
      form.append("extract", "true");
      if (ideaId) form.append("target_idea_id", ideaId);
      else form.append("new_idea", "true");
      const imp = await api.upload<ImportRecord>("/v1/imports", form);
      if (imp.status === "FAILED") throw new Error(imp.error || "Import failed");
      toast.success(`Imported ${file.name}`, { action: { label: "Open", onClick: () => router.push(`/imports/${imp.id}`) } });
      onSend(`I've attached "${file.name}" (imported as ${imp.adapter || "a conversation"}). Please summarise what it adds and list the proposed items for my review.`);
    } catch (err) {
      toast.error(errorMessage(err), {
        action: { label: "Import Center", onClick: () => router.push("/imports") },
      });
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className={cn("rounded-[16px] border border-border bg-surface shadow-sm transition-colors focus-within:border-border-strong", compact ? "p-1.5" : "p-2")}>
      <textarea
        ref={ref}
        value={interim ? `${text}${text ? " " : ""}${interim}` : text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={onKeyDown}
        rows={1}
        placeholder={listening ? "Listening…" : placeholder}
        aria-label="Message IdeaVault"
        className={cn("block max-h-[280px] w-full resize-none bg-transparent px-2.5 py-2 text-fg outline-none placeholder:text-faint", compact ? "text-[14px]" : "text-[15px]")}
      />
      <div className="flex items-center gap-1 px-1 pb-0.5">
        <Dropdown>
          <Tooltip content="Attach, link or import">
            <DropdownTrigger asChild>
              <button className="flex h-8 w-8 items-center justify-center rounded-full text-muted hover:bg-surface-2 hover:text-fg" aria-label="Add attachment">
                <Plus className="h-4 w-4" />
              </button>
            </DropdownTrigger>
          </Tooltip>
          <DropdownContent align="start">
            <DropdownItem onSelect={() => fileRef.current?.click()}>
              <FileUp className="h-3.5 w-3.5 text-muted" /> Attach a file or export…
            </DropdownItem>
            <DropdownItem
              onSelect={() => {
                const url = window.prompt("Paste a public link (e.g. a ChatGPT share link):");
                if (url) setText((t) => `${t}${t ? "\n" : ""}${url.trim()}`);
                ref.current?.focus();
              }}
            >
              <Link2 className="h-3.5 w-3.5 text-muted" /> Add a link
            </DropdownItem>
            <DropdownItem onSelect={() => router.push("/imports")}>
              <Import className="h-3.5 w-3.5 text-muted" /> Open Import Center
            </DropdownItem>
          </DropdownContent>
        </Dropdown>
        <input
          ref={fileRef}
          type="file"
          className="hidden"
          accept=".json,.md,.markdown,.txt,.zip,.html,application/json,text/plain,text/markdown,application/zip"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) void uploadFile(f);
            e.target.value = "";
          }}
        />
        <Tooltip content={speechSupported ? (listening ? "Stop listening" : "Dictate (stays in your browser)") : "Voice input not supported in this browser"}>
          <button
            type="button"
            onClick={toggleMic}
            className={cn(
              "flex h-8 w-8 items-center justify-center rounded-full transition-colors",
              listening ? "bg-danger-soft text-danger" : "text-muted hover:bg-surface-2 hover:text-fg",
            )}
            aria-label={listening ? "Stop voice input" : "Start voice input"}
            aria-pressed={listening}
          >
            {listening ? <MicOff className="h-4 w-4" /> : <Mic className="h-4 w-4" />}
          </button>
        </Tooltip>
        {uploading && <span className="ml-1 text-[12px] text-muted">Importing…</span>}
        <div className="ml-auto flex items-center gap-2">
          {!compact && <span className="hidden text-[11px] text-faint sm:inline">↵ send · ⇧↵ newline</span>}
          {streaming ? (
            <button type="button" onClick={onStop} className="flex h-8 w-8 items-center justify-center rounded-full bg-fg text-bg" aria-label="Stop generating">
              <Square className="h-3 w-3" fill="currentColor" />
            </button>
          ) : (
            <button
              type="button"
              onClick={submit}
              disabled={!text.trim()}
              className="flex h-8 w-8 items-center justify-center rounded-full bg-fg text-bg transition-opacity disabled:opacity-25"
              aria-label="Send message"
            >
              <ArrowUp className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
