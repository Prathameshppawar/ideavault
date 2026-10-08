import { format, formatDistanceToNowStrict, isToday, isYesterday, parseISO } from "date-fns";
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

function toDate(d: string | Date | undefined | null): Date | null {
  if (!d) return null;
  const date = typeof d === "string" ? parseISO(d) : d;
  return Number.isNaN(date.getTime()) ? null : date;
}

export function timeAgo(d: string | Date | undefined | null): string {
  const date = toDate(d);
  if (!date) return "";
  const diff = Date.now() - date.getTime();
  if (diff < 45_000) return "just now";
  return `${formatDistanceToNowStrict(date)} ago`;
}

export function shortDate(d: string | Date | undefined | null): string {
  const date = toDate(d);
  if (!date) return "";
  if (isToday(date)) return format(date, "HH:mm");
  if (isYesterday(date)) return "Yesterday";
  return format(date, date.getFullYear() === new Date().getFullYear() ? "d MMM" : "d MMM yyyy");
}

export function fullDate(d: string | Date | undefined | null): string {
  const date = toDate(d);
  return date ? format(date, "d MMM yyyy, HH:mm") : "";
}

export function dayKey(d: string | Date): string {
  const date = toDate(d);
  return date ? format(date, "yyyy-MM-dd") : "";
}

/** "READY_TO_IMPLEMENT" → "Ready to implement" */
export function humanize(s: string | undefined | null): string {
  if (!s) return "";
  const t = s.replace(/[_-]+/g, " ").toLowerCase().trim();
  return t.charAt(0).toUpperCase() + t.slice(1);
}

export function plural(n: number, word: string, pluralWord?: string): string {
  return `${n} ${n === 1 ? word : (pluralWord ?? `${word}s`)}`;
}

export function compactNumber(n: number): string {
  return Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 }).format(n);
}

export function usd(n: number): string {
  if (n === 0) return "$0";
  if (n < 0.01) return `$${n.toFixed(4)}`;
  return `$${n.toFixed(2)}`;
}

export function truncate(s: string, n: number): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s;
}
