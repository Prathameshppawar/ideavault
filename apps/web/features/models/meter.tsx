import { cn } from "@/lib/format";

/** Five-step meter for 1–5 (or 0–5) ratings, e.g. speed, quality, relative cost. */
export function Meter({ value, max = 5, label, tone = "ink", className }: { value: number; max?: number; label: string; tone?: "ink" | "warning"; className?: string }) {
  const v = Math.max(0, Math.min(max, Math.round(value)));
  return (
    <span role="img" aria-label={`${label} ${v} of ${max}`} title={`${label}: ${v} / ${max}`} className={cn("inline-flex items-end gap-[2px]", className)}>
      {Array.from({ length: max }).map((_, i) => (
        <span
          key={i}
          className={cn("w-[5px] rounded-[1.5px]", i < v ? (tone === "warning" ? "bg-warning" : "bg-fg/75") : "bg-surface-3")}
          style={{ height: 6 + i * 1.5 }}
        />
      ))}
    </span>
  );
}
