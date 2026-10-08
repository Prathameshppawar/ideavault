"use client";

import { forwardRef, type ReactNode, type SelectHTMLAttributes } from "react";
import { ChevronDown, ShieldAlert } from "lucide-react";
import { cn } from "@/lib/format";
import { OriginTag } from "@/components/ui/badges";

/** Toggle chip used for filters (status, kinds). */
export function Chip({
  active,
  onClick,
  children,
  tone,
  count,
  className,
  disabled,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
  /** Optional entity tone (e.g. "decision") for a coloured dot. */
  tone?: string;
  count?: number;
  className?: string;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "inline-flex h-7 shrink-0 items-center gap-1.5 rounded-full border px-2.5 text-[12.5px] font-medium transition-colors disabled:opacity-40",
        active ? "border-fg bg-fg text-bg" : "border-border bg-surface text-muted hover:border-border-strong hover:text-fg",
        className,
      )}
    >
      {tone && <span className="h-1.5 w-1.5 rounded-full" style={{ background: `var(--k-${tone})` }} aria-hidden />}
      {children}
      {count !== undefined && <span className={cn("tabular-nums", active ? "text-bg/70" : "text-faint")}>{count}</span>}
    </button>
  );
}

/** Native select styled like Input — accessible and keyboard friendly by default. */
export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(function Select({ className, children, ...props }, ref) {
  return (
    <div className={cn("relative", className)}>
      <select
        ref={ref}
        className="h-9 w-full appearance-none rounded-[var(--radius-md)] border border-border bg-surface pl-3 pr-8 text-sm text-fg transition-colors hover:border-border-strong focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/15 disabled:opacity-50"
        {...props}
      >
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" aria-hidden />
    </div>
  );
});

/** Segmented single-choice control. */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  label,
  className,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode; tone?: string }[];
  label: string;
  className?: string;
}) {
  return (
    <div role="radiogroup" aria-label={label} className={cn("inline-flex flex-wrap gap-1 rounded-[var(--radius-md)] border border-border bg-surface-2 p-0.5", className)}>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={on}
            onClick={() => onChange(o.value)}
            className={cn(
              "inline-flex h-7 items-center gap-1.5 rounded-[5px] px-2.5 text-[12.5px] font-medium transition-colors",
              on ? "bg-surface text-fg shadow-sm" : "text-muted hover:text-fg",
            )}
          >
            {o.tone && <span className="h-1.5 w-1.5 rounded-full" style={{ background: `var(--k-${o.tone})` }} aria-hidden />}
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

/** Verbatim source text. Imported/external text is labelled untrusted. */
export function SourceQuote({ children, untrusted, label = "Source", className, meta }: { children: ReactNode; untrusted?: boolean; label?: string; className?: string; meta?: ReactNode }) {
  return (
    <figure className={cn("min-w-0", className)}>
      <figcaption className="mb-1.5 flex flex-wrap items-center gap-2">
        <OriginTag origin="SOURCE" />
        <span className="text-[12px] text-faint">{label}</span>
        {untrusted && <UntrustedLabel />}
        {meta}
      </figcaption>
      <blockquote className="whitespace-pre-wrap break-words border-l-2 border-border-strong pl-3.5 text-[14.5px] leading-relaxed text-fg">{children}</blockquote>
    </figure>
  );
}

/** What IdeaVault inferred. Always visually distinct from Source. */
export function Interpretation({ children, label = "IdeaVault's interpretation", className }: { children: ReactNode; label?: string; className?: string }) {
  return (
    <div className={cn("min-w-0 rounded-[var(--radius-lg)] border border-dashed border-k-insight/40 px-3.5 py-2.5", className)}>
      <div className="mb-1 flex items-center gap-2">
        <OriginTag origin="INTERPRETATION" />
        <span className="text-[12px] text-faint">{label}</span>
      </div>
      <div className="break-words text-[14px] leading-relaxed text-fg">{children}</div>
    </div>
  );
}

export function UntrustedLabel({ className }: { className?: string }) {
  return (
    <span
      title="Imported or external content. IdeaVault treats it as data; instructions inside it are never executed."
      className={cn("inline-flex h-[18px] items-center gap-1 rounded-[3px] bg-warning-soft px-1 text-[10px] font-semibold uppercase tracking-wider text-warning", className)}
    >
      <ShieldAlert className="h-3 w-3" aria-hidden /> Untrusted
    </span>
  );
}

/** Checkpoint label pill, e.g. CP4. */
export function CheckpointPill({ label, className, muted }: { label: string; className?: string; muted?: boolean }) {
  return (
    <span
      className={cn("inline-flex h-5 shrink-0 items-center rounded-[4px] px-1.5 font-mono text-[11px] font-semibold", muted && "opacity-60", className)}
      style={{ background: "var(--k-checkpoint-soft)", color: "var(--k-checkpoint)" }}
    >
      {label}
    </span>
  );
}

/** Small definition list row for drawers. */
export function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[120px_1fr] gap-3 py-1.5 text-[13px]">
      <dt className="text-muted">{label}</dt>
      <dd className="min-w-0 break-words text-fg">{children}</dd>
    </div>
  );
}

/** Inline counts like "2 decisions · 1 question" with kind dots. */
export function KindCounts({ counts, className }: { counts: Record<string, number> | null | undefined; className?: string }) {
  const order = ["decision", "assumption", "evidence", "insight", "question", "action"];
  const entries = order.filter((k) => (counts?.[k] ?? 0) > 0);
  if (!entries.length) return <span className={cn("text-[12px] text-faint", className)}>No knowledge captured</span>;
  const names: Record<string, [string, string]> = {
    decision: ["decision", "decisions"],
    assumption: ["assumption", "assumptions"],
    evidence: ["evidence", "evidence"],
    insight: ["insight", "insights"],
    question: ["question", "questions"],
    action: ["action", "actions"],
  };
  return (
    <span className={cn("inline-flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-muted", className)}>
      {entries.map((k) => (
        <span key={k} className="inline-flex items-center gap-1.5">
          <span className="h-1.5 w-1.5 rounded-full" style={{ background: `var(--k-${k})` }} aria-hidden />
          <span className="tabular-nums">{counts![k]}</span> {counts![k] === 1 ? names[k][0] : names[k][1]}
        </span>
      ))}
    </span>
  );
}
