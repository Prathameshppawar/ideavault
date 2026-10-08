"use client";

import { useRef, useState, type DragEvent } from "react";
import { FileArchive, FileCode, FileJson, FileText, UploadCloud, X } from "lucide-react";
import { cn } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { ACCEPT_ATTR, describeFile, fileExtension, formatBytes, validateImportFile } from "./import-logic";

function FileIcon({ name, className }: { name: string; className?: string }) {
  const ext = fileExtension(name);
  const Icon = ext === ".zip" ? FileArchive : ext === ".json" ? FileJson : ext === ".html" || ext === ".htm" ? FileCode : FileText;
  return <Icon className={className} aria-hidden />;
}

/**
 * Drag-and-drop zone with a keyboard-accessible file picker. Validates type and size
 * client-side; the server still detects the actual format. Dropping onto the selected
 * file card replaces the file.
 */
export function Dropzone({ file, onFile, disabled }: { file: File | null; onFile: (f: File | null) => void; disabled?: boolean }) {
  const inputRef = useRef<HTMLInputElement>(null);
  const depth = useRef(0);
  const [over, setOver] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const error = file ? validateImportFile(file) : null;

  const pick = (files: FileList | null | undefined) => {
    if (!files?.length) return;
    setNote(files.length > 1 ? `${files.length} files dropped — using “${files[0].name}”. Import one file at a time.` : null);
    onFile(files[0]);
  };
  const browse = () => {
    if (!disabled) inputRef.current?.click();
  };
  const dnd = {
    onDragEnter: (e: DragEvent<HTMLDivElement>) => {
      e.preventDefault();
      if (disabled) return;
      depth.current += 1;
      setOver(true);
    },
    onDragLeave: (e: DragEvent<HTMLDivElement>) => {
      e.preventDefault();
      depth.current = Math.max(0, depth.current - 1);
      if (depth.current === 0) setOver(false);
    },
    onDragOver: (e: DragEvent<HTMLDivElement>) => e.preventDefault(),
    onDrop: (e: DragEvent<HTMLDivElement>) => {
      e.preventDefault();
      depth.current = 0;
      setOver(false);
      if (!disabled) pick(e.dataTransfer.files);
    },
  };

  return (
    <div>
      <input
        ref={inputRef}
        type="file"
        accept={ACCEPT_ATTR}
        className="sr-only"
        tabIndex={-1}
        aria-hidden
        data-testid="file-input"
        onChange={(e) => {
          pick(e.target.files);
          e.target.value = "";
        }}
      />
      {file ? (
        <div
          {...dnd}
          className={cn(
            "flex items-center gap-3 rounded-[var(--radius-xl)] border px-4 py-4 transition-colors",
            over ? "border-dashed border-accent bg-accent-soft" : error ? "border-danger/40 bg-danger-soft/40" : "border-border bg-surface",
            disabled && "opacity-60",
          )}
        >
          <span className={cn("flex h-10 w-10 shrink-0 items-center justify-center rounded-[var(--radius-md)]", error ? "bg-danger-soft text-danger" : "bg-surface-2 text-muted")}>
            <FileIcon name={file.name} className="h-5 w-5" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[14px] font-medium text-fg">{file.name}</span>
            <span className="block truncate text-[12px] text-faint">
              {formatBytes(file.size)} · {over ? "Drop to replace" : describeFile(file.name)}
            </span>
          </span>
          <Button type="button" size="sm" variant="ghost" onClick={browse} disabled={disabled}>
            Replace
          </Button>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            aria-label="Remove file"
            disabled={disabled}
            onClick={() => {
              setNote(null);
              onFile(null);
            }}
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
      ) : (
        <div
          {...dnd}
          role="button"
          tabIndex={disabled ? -1 : 0}
          aria-disabled={disabled}
          aria-label="Choose a file to import, or drop it here"
          aria-describedby="dropzone-help"
          onClick={browse}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              browse();
            }
          }}
          data-over={over || undefined}
          className={cn(
            "group flex cursor-pointer flex-col items-center justify-center rounded-[var(--radius-xl)] border border-dashed px-6 py-12 text-center transition-colors",
            over ? "border-accent bg-accent-soft" : "border-border-strong bg-surface hover:border-fg/30 hover:bg-surface-2/60",
            disabled && "pointer-events-none opacity-60",
          )}
        >
          <span className={cn("mb-3 flex h-11 w-11 items-center justify-center rounded-full transition-colors", over ? "bg-accent text-accent-fg" : "bg-surface-2 text-muted group-hover:text-fg")}>
            <UploadCloud className="h-5 w-5" aria-hidden />
          </span>
          <p className="text-[15px] font-medium text-fg">{over ? "Drop to import" : "Drop an export here"}</p>
          <p className="mt-1 text-[13px] text-muted">
            or <span className="font-medium text-accent underline decoration-accent/30 underline-offset-2">browse your files</span>
          </p>
        </div>
      )}
      <p id="dropzone-help" className={cn("mt-2 text-[12px]", error ? "text-danger" : "text-faint")} role={error ? "alert" : undefined}>
        {error ?? note ?? ".json, .zip, .md, .txt or .html — up to 200 MB. One file per import."}
      </p>
    </div>
  );
}
