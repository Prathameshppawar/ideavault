"use client";

// Small UI pieces shared by the artifacts, context-pack, delta and import screens.
// (Candidates for components/ui once the other screens settle.)

import Link from "next/link";
import { forwardRef, useMemo, useState, type KeyboardEvent, type ReactNode, type SelectHTMLAttributes } from "react";
import { useQuery } from "@tanstack/react-query";
import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import { AlertTriangle, ArrowLeft, Check, ChevronDown, CircleCheck, Copy, Info, Minus, ShieldAlert } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { cn } from "@/lib/format";
import type { Idea } from "@/lib/types";
import { Button, type ButtonProps } from "@/components/ui/button";
import type { Tone } from "./artifact-meta";

// ---------- tones ----------

const TONE_VARS: Record<Tone, { fg: string; bg: string }> = {
  muted: { fg: "var(--text-muted)", bg: "var(--surface-2)" },
  faint: { fg: "var(--text-faint)", bg: "var(--surface-2)" },
  success: { fg: "var(--success)", bg: "var(--success-soft)" },
  warning: { fg: "var(--warning)", bg: "var(--warning-soft)" },
  danger: { fg: "var(--danger)", bg: "var(--danger-soft)" },
  accent: { fg: "var(--accent)", bg: "var(--accent-soft)" },
  info: { fg: "var(--k-branch)", bg: "var(--k-branch-soft)" },
};
export const toneVars = (t: Tone) => TONE_VARS[t];

/** Rounded status pill with a dot, coloured by tone. `pulse` animates the dot for live states. */
export function Pill({ tone, children, pulse, className }: { tone: Tone; children: ReactNode; pulse?: boolean; className?: string }) {
  const v = TONE_VARS[tone];
  return (
    <span className={cn("inline-flex h-5 items-center gap-1.5 whitespace-nowrap rounded-full px-2 text-[11px] font-medium", className)} style={{ background: v.bg, color: v.fg }}>
      <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", pulse && "animate-pulse-soft")} style={{ background: v.fg }} />
      {children}
    </span>
  );
}

// ---------- form controls ----------

/** Native select styled like `Input` (accessible, works on touch, respects dark mode). */
export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement> & { wrapperClassName?: string }>(function Select(
  { className, wrapperClassName, children, ...props },
  ref,
) {
  return (
    <div className={cn("relative min-w-0", wrapperClassName)}>
      <select
        ref={ref}
        className={cn(
          "h-9 w-full min-w-0 cursor-pointer appearance-none truncate rounded-[var(--radius-md)] border border-border bg-surface pl-3 pr-8 text-sm text-fg",
          "transition-colors hover:border-border-strong focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/15 disabled:cursor-not-allowed disabled:opacity-50",
          className,
        )}
        {...props}
      >
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" aria-hidden />
    </div>
  );
});

/** Radio-group of small buttons (arrow keys move between options). */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  label,
  className,
  size = "md",
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode }[];
  label: string;
  className?: string;
  size?: "sm" | "md";
}) {
  const onKey = (e: KeyboardEvent<HTMLButtonElement>, i: number) => {
    const d = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
    if (!d) return;
    e.preventDefault();
    const next = options[(i + d + options.length) % options.length];
    onChange(next.value);
    const btns = (e.currentTarget.parentElement?.querySelectorAll("button") ?? []) as NodeListOf<HTMLButtonElement>;
    btns[(i + d + options.length) % options.length]?.focus();
  };
  return (
    <div role="radiogroup" aria-label={label} className={cn("inline-flex max-w-full items-center gap-0.5 overflow-x-auto rounded-[var(--radius-md)] border border-border bg-surface-2 p-0.5", className)}>
      {options.map((o, i) => {
        const active = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={active}
            tabIndex={active ? 0 : -1}
            onKeyDown={(e) => onKey(e, i)}
            onClick={() => onChange(o.value)}
            className={cn(
              "inline-flex shrink-0 items-center gap-1.5 rounded-[5px] font-medium transition-colors",
              size === "sm" ? "h-6 px-2 text-[12px]" : "h-7 px-2.5 text-[13px]",
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

/** Checkbox with an indeterminate ("some selected") state, for select-all rows. */
export function TriCheckbox({ state, onChange, label, disabled }: { state: "all" | "some" | "none"; onChange: (on: boolean) => void; label: string; disabled?: boolean }) {
  return (
    <CheckboxPrimitive.Root
      checked={state === "all" ? true : state === "some" ? "indeterminate" : false}
      onCheckedChange={() => onChange(state !== "all")}
      aria-label={label}
      disabled={disabled}
      className="flex h-4 w-4 shrink-0 items-center justify-center rounded-[4px] border border-border-strong bg-surface data-[state=checked]:border-accent data-[state=checked]:bg-accent data-[state=indeterminate]:border-accent data-[state=indeterminate]:bg-accent disabled:opacity-50"
    >
      <CheckboxPrimitive.Indicator>
        {state === "some" ? <Minus className="h-3 w-3 text-accent-fg" strokeWidth={3} /> : <Check className="h-3 w-3 text-accent-fg" strokeWidth={3} />}
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

// ---------- feedback ----------

const CALLOUT_ICON = { warning: AlertTriangle, danger: AlertTriangle, info: Info, success: CircleCheck, untrusted: ShieldAlert } as const;

/** Inline message block. `untrusted` is used for external/imported content. */
export function Callout({
  tone,
  title,
  children,
  action,
  className,
  icon,
}: {
  tone: "warning" | "danger" | "info" | "success" | "untrusted";
  title?: ReactNode;
  children?: ReactNode;
  action?: ReactNode;
  className?: string;
  icon?: React.ComponentType<{ className?: string }>;
}) {
  const Icon = icon ?? CALLOUT_ICON[tone];
  const styles = {
    warning: "border-warning/30 bg-warning-soft [--c:var(--warning)]",
    untrusted: "border-warning/30 bg-warning-soft [--c:var(--warning)]",
    danger: "border-danger/30 bg-danger-soft [--c:var(--danger)]",
    success: "border-success/30 bg-success-soft [--c:var(--success)]",
    info: "border-border bg-surface-2 [--c:var(--text-muted)]",
  }[tone];
  return (
    <div className={cn("flex items-start gap-3 rounded-[var(--radius-lg)] border px-4 py-3 text-[13px]", styles, className)} role={tone === "danger" ? "alert" : undefined}>
      <Icon className="mt-0.5 h-4 w-4 shrink-0 text-[var(--c)]" />
      <div className="min-w-0 flex-1 leading-relaxed text-muted">
        {title && <p className="font-medium text-fg">{title}</p>}
        {children && <div className={cn(title && "mt-0.5")}>{children}</div>}
      </div>
      {action && <div className="shrink-0 self-center">{action}</div>}
    </div>
  );
}

/** Thin progress bar. `value` 0..1, or undefined for an indeterminate sweep. */
export function ProgressBar({ value, className, tone = "accent", label }: { value?: number | null; className?: string; tone?: Tone; label?: string }) {
  const v = TONE_VARS[tone];
  const indeterminate = value === undefined || value === null;
  return (
    <div
      className={cn("relative h-1 w-full overflow-hidden rounded-full bg-surface-3", className)}
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={indeterminate ? undefined : Math.round((value ?? 0) * 100)}
    >
      {indeterminate ? (
        <div className="iv-indeterminate absolute inset-y-0 w-1/3 rounded-full" style={{ background: v.fg }} />
      ) : (
        <div className="h-full rounded-full transition-[width] duration-500 ease-out" style={{ width: `${Math.max(2, (value ?? 0) * 100)}%`, background: v.fg }} />
      )}
      <style href="iv-progress-sweep" precedence="default">{`@keyframes iv-sweep{0%{left:-35%}100%{left:100%}}.iv-indeterminate{animation:iv-sweep 1.3s cubic-bezier(.4,0,.2,1) infinite}@media (prefers-reduced-motion:reduce){.iv-indeterminate{animation:none;left:0;width:100%;opacity:.5}}`}</style>
    </div>
  );
}

// ---------- actions ----------

/** Copies text to the clipboard with a brief check-mark confirmation. */
export function CopyButton({ text, label = "Copy", copiedLabel = "Copied", toastMessage, ...props }: Omit<ButtonProps, "onClick"> & { text: string | (() => string); label?: ReactNode; copiedLabel?: ReactNode; toastMessage?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      {...props}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(typeof text === "function" ? text() : text);
          setCopied(true);
          if (toastMessage) toast.success(toastMessage);
          setTimeout(() => setCopied(false), 1600);
        } catch {
          toast.error("Couldn't access the clipboard");
        }
      }}
    >
      {copied ? <Check className="h-3.5 w-3.5 text-success" /> : <Copy className="h-3.5 w-3.5" />}
      {label !== null && <span className={props.size === "icon" || props.size === "icon-sm" ? "sr-only" : undefined}>{copied ? copiedLabel : label}</span>}
    </Button>
  );
}

const LINK_VARIANTS = {
  primary: "bg-fg text-bg hover:opacity-90 shadow-sm",
  secondary: "bg-surface text-fg border border-border hover:bg-surface-2 hover:border-border-strong shadow-sm",
  ghost: "text-muted hover:text-fg hover:bg-surface-2",
} as const;
const LINK_SIZES = {
  sm: "h-8 px-2.5 text-[13px] gap-1.5 rounded-[var(--radius-md)]",
  md: "h-9 px-3.5 text-sm gap-2 rounded-[var(--radius-md)]",
} as const;

/** A Next.js link that looks like a `Button` (buttons can't wrap links). */
export function LinkButton({
  href,
  variant = "secondary",
  size = "md",
  className,
  children,
}: {
  href: string;
  variant?: keyof typeof LINK_VARIANTS;
  size?: keyof typeof LINK_SIZES;
  className?: string;
  children: ReactNode;
}) {
  return (
    <Link
      href={href}
      className={cn("inline-flex select-none items-center justify-center whitespace-nowrap font-medium transition-[background,opacity,border-color,color] duration-150", LINK_VARIANTS[variant], LINK_SIZES[size], className)}
    >
      {children}
    </Link>
  );
}

export function BackLink({ href, children, onClick }: { href: string; children: ReactNode; onClick?: (e: React.MouseEvent<HTMLAnchorElement>) => void }) {
  return (
    <Link href={href} onClick={onClick} className="inline-flex items-center gap-1.5 rounded text-[13px] text-muted transition-colors hover:text-fg">
      <ArrowLeft className="h-3.5 w-3.5" /> {children}
    </Link>
  );
}

// ---------- data ----------

interface IdeaList {
  ideas: Idea[];
  total: number;
}

/** All ideas (for pickers and title lookups). */
export function useIdeas() {
  return useQuery({
    queryKey: ["ideas", { limit: 200 }],
    queryFn: () => api.get<IdeaList>("/v1/ideas", { query: { limit: 200 } }),
    staleTime: 30_000,
    select: (d) => d.ideas,
  });
}

/** Idea id → title, from the shared ideas list. */
export function useIdeaTitles(): Map<string, string> {
  const { data } = useIdeas();
  return useMemo(() => new Map((data ?? []).map((i) => [i.id, i.title])), [data]);
}
