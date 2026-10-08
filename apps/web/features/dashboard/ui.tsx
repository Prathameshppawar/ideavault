"use client";

// Small presentational pieces shared by the Thinking, Universe and Search screens.
import Link from "next/link";
import { useRef, type KeyboardEvent, type ReactNode } from "react";
import { cn } from "@/lib/format";
import { Skeleton } from "@/components/ui/primitives";

export interface SegmentOption<T extends string> {
  value: T;
  label: ReactNode;
  title?: string;
}

/**
 * Compact segmented control (radio group semantics, arrow-key navigation).
 * Used for range pickers, filters and modes.
 */
export function Segmented<T extends string>({ value, onChange, options, label, size = "sm", className }: {
  value: T;
  onChange: (v: T) => void;
  options: SegmentOption<T>[];
  label: string;
  size?: "xs" | "sm";
  className?: string;
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const onKey = (e: KeyboardEvent<HTMLButtonElement>, i: number) => {
    const dir = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
    if (!dir) return;
    e.preventDefault();
    const next = (i + dir + options.length) % options.length;
    onChange(options[next].value);
    refs.current[next]?.focus();
  };
  return (
    <div role="radiogroup" aria-label={label} className={cn("inline-flex items-center rounded-[var(--radius-md)] border border-border bg-surface-2 p-0.5", className)}>
      {options.map((o, i) => {
        const active = o.value === value;
        return (
          <button
            key={o.value}
            ref={(el) => {
              refs.current[i] = el;
            }}
            type="button"
            role="radio"
            aria-checked={active}
            tabIndex={active ? 0 : -1}
            title={o.title}
            onClick={() => onChange(o.value)}
            onKeyDown={(e) => onKey(e, i)}
            className={cn(
              "inline-flex items-center gap-1.5 whitespace-nowrap rounded-[5px] font-medium transition-colors",
              size === "xs" ? "h-6 px-2 text-[12px]" : "h-7 px-2.5 text-[13px]",
              active ? "bg-surface text-fg shadow-sm" : "text-muted hover:text-fg",
            )}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

/** Toggle chip (aria-pressed) — type filters, graph layers. */
export function ToggleChip({ pressed, onClick, children, tone, className, title }: {
  pressed: boolean;
  onClick: () => void;
  children: ReactNode;
  /** Entity tone suffix for the leading dot, e.g. "decision". */
  tone?: string;
  className?: string;
  title?: string;
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      title={title}
      onClick={onClick}
      className={cn(
        "inline-flex h-7 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 text-[12px] font-medium transition-colors",
        pressed ? "border-border-strong bg-surface text-fg shadow-sm" : "border-border bg-transparent text-faint hover:border-border-strong hover:text-muted",
        className,
      )}
    >
      {tone && <span className={cn("h-2 w-2 rounded-full transition-opacity", !pressed && "opacity-40")} style={{ background: `var(--k-${tone})` }} aria-hidden />}
      {children}
    </button>
  );
}

/** Small idea reference: coloured dot + title. Safe to place next to (not inside) other links. */
export function IdeaLink({ id, title, className }: { id?: string | null; title?: string | null; className?: string }) {
  if (!id && !title) return null;
  const body = (
    <>
      <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-k-idea" aria-hidden />
      <span className="truncate">{title || "Untitled idea"}</span>
    </>
  );
  const cls = cn("relative z-10 inline-flex min-w-0 max-w-full items-center gap-1.5 text-[12px] text-muted", className);
  return id ? (
    <Link href={`/ideas/${id}`} className={cn(cls, "hover:text-fg hover:underline hover:decoration-border-strong hover:underline-offset-2")}>
      {body}
    </Link>
  ) : (
    <span className={cls}>{body}</span>
  );
}

/** Section heading with an optional count, in the app's small-caps style. */
export function Heading({ children, count, action, className, id }: { children: ReactNode; count?: number; action?: ReactNode; className?: string; id?: string }) {
  return (
    <div className={cn("mb-3 flex items-baseline justify-between gap-3 border-b border-border pb-2", className)}>
      <h2 id={id} className="text-[12px] font-semibold uppercase tracking-[0.07em] text-muted">
        {children}
        {count !== undefined && <span className="ml-2 font-normal tabular-nums text-faint">{count}</span>}
      </h2>
      {action}
    </div>
  );
}

/** Loading placeholder for a list of rows. */
export function RowsSkeleton({ rows = 5, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn("space-y-4", className)} aria-busy="true" aria-label="Loading">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex items-start gap-3">
          <Skeleton className="h-5 w-8 shrink-0" />
          <div className="flex-1 space-y-2">
            <Skeleton className={cn("h-3.5", i % 2 ? "w-3/4" : "w-11/12")} />
            <Skeleton className="h-3 w-1/3" />
          </div>
        </div>
      ))}
    </div>
  );
}

/** Quiet, dashed empty block for a section that has nothing yet. */
export function QuietEmpty({ children, className }: { children: ReactNode; className?: string }) {
  return <p className={cn("rounded-[var(--radius-md)] border border-dashed border-border px-3 py-3 text-[13px] text-faint", className)}>{children}</p>;
}

/** A link styled like the app's Button (an <a> must not wrap a <button>). */
export function LinkButton({ href, children, variant = "secondary", size = "lg", className }: {
  href: string;
  children: ReactNode;
  variant?: "primary" | "secondary" | "ghost";
  size?: "sm" | "md" | "lg";
  className?: string;
}) {
  return (
    <Link
      href={href}
      className={cn(
        "inline-flex select-none items-center justify-center gap-2 whitespace-nowrap font-medium transition-[background,opacity,border-color,color] duration-150",
        size === "lg" ? "h-11 rounded-[var(--radius-lg)] px-5 text-[15px]" : size === "md" ? "h-9 rounded-[var(--radius-md)] px-3.5 text-sm" : "h-8 rounded-[var(--radius-md)] px-2.5 text-[13px]",
        variant === "primary" && "bg-fg text-bg shadow-sm hover:opacity-90",
        variant === "secondary" && "border border-border bg-surface text-fg shadow-sm hover:border-border-strong hover:bg-surface-2",
        variant === "ghost" && "text-muted hover:bg-surface-2 hover:text-fg",
        className,
      )}
    >
      {children}
    </Link>
  );
}
