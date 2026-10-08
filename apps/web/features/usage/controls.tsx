"use client";

// Small form controls used by the platform screens (models, usage, connectors,
// settings, insights). Candidates for promotion to components/ui.
import { forwardRef, type ReactNode, type SelectHTMLAttributes } from "react";
import { ChevronDown } from "lucide-react";
import { cn } from "@/lib/format";

/** Native <select> styled like `Input` — accessible and keyboard-friendly by default. */
export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement> & { wrapperClassName?: string }>(function Select(
  { className, wrapperClassName, children, ...props },
  ref,
) {
  return (
    <span className={cn("relative inline-flex w-full", wrapperClassName)}>
      <select
        ref={ref}
        className={cn(
          "h-9 w-full appearance-none rounded-[var(--radius-md)] border border-border bg-surface pl-3 pr-8 text-sm text-fg",
          "transition-colors hover:border-border-strong focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/15 disabled:opacity-50",
          className,
        )}
        {...props}
      >
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" aria-hidden />
    </span>
  );
});

/** Segmented control (radio group semantics). */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  label,
  size = "md",
  className,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode; icon?: React.ComponentType<{ className?: string }> }[];
  label: string;
  size?: "sm" | "md";
  className?: string;
}) {
  return (
    <div role="radiogroup" aria-label={label} className={cn("inline-flex items-center rounded-[var(--radius-md)] border border-border bg-surface-2 p-0.5", className)}>
      {options.map((o) => {
        const active = o.value === value;
        const Icon = o.icon;
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(o.value)}
            onKeyDown={(e) => {
              if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
              e.preventDefault();
              const i = options.findIndex((x) => x.value === value);
              const next = options[(i + (e.key === "ArrowRight" ? 1 : options.length - 1)) % options.length];
              onChange(next.value);
              const el = (e.currentTarget.parentElement?.querySelectorAll("button") ?? [])[options.indexOf(next)] as HTMLButtonElement | undefined;
              el?.focus();
            }}
            tabIndex={active ? 0 : -1}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-[5px] font-medium transition-colors",
              size === "sm" ? "h-6 px-2 text-[12px]" : "h-7 px-2.5 text-[13px]",
              active ? "bg-surface text-fg shadow-sm" : "text-muted hover:text-fg",
            )}
          >
            {Icon && <Icon className="h-3.5 w-3.5" />}
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

/** Status dot + label, coloured by meaning (success / warning / danger / muted). */
export function StatusPill({ tone, children, className }: { tone: "success" | "warning" | "danger" | "muted" | "accent"; children: ReactNode; className?: string }) {
  const styles: Record<string, string> = {
    success: "bg-success-soft text-success",
    warning: "bg-warning-soft text-warning",
    danger: "bg-danger-soft text-danger",
    accent: "bg-accent-soft text-accent",
    muted: "bg-surface-2 text-muted",
  };
  const dot: Record<string, string> = { success: "bg-success", warning: "bg-warning", danger: "bg-danger", accent: "bg-accent", muted: "bg-faint" };
  return (
    <span className={cn("inline-flex h-5 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full px-2 text-[11px] font-medium", styles[tone], className)}>
      <span className={cn("h-1.5 w-1.5 rounded-full", dot[tone])} aria-hidden />
      {children}
    </span>
  );
}

/** Hairline-separated definition list row used across settings/system panels. */
export function KV({ label, children, mono }: { label: string; children: ReactNode; mono?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-6 border-b border-border py-2.5 text-[13px] last:border-b-0">
      <dt className="shrink-0 text-muted">{label}</dt>
      <dd className={cn("min-w-0 truncate text-right text-fg", mono && "font-mono text-[12px]")}>{children}</dd>
    </div>
  );
}
