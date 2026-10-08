"use client";

import { cn } from "@/lib/format";
import { DIMENSION_HINT, DIMENSION_LABEL, SCORE_WORD, rubric, scoreBand } from "./insights-data";

/** Overall 0–100 score with a band label. Hero figure: sans, proportional digits. */
export function OverallScore({ overall, className }: { overall: number; className?: string }) {
  const band = scoreBand(overall);
  const v = Math.max(0, Math.min(100, Math.round(overall)));
  return (
    <div className={cn("flex items-end gap-4", className)}>
      <div className="leading-none">
        <span className="text-[56px] font-semibold tracking-tight text-fg" aria-label={`Overall score ${v} out of 100`}>
          {v}
        </span>
        <span className="ml-1 text-[15px] text-faint" aria-hidden>
          /100
        </span>
      </div>
      <div className="pb-2">
        <span className={cn("text-[13px] font-medium", band.tone === "success" ? "text-success" : band.tone === "warning" ? "text-warning" : "text-danger")}>{band.label}</span>
        <div className="mt-1.5 h-1 w-40 overflow-hidden rounded-full bg-surface-3" aria-hidden>
          <div className="h-full rounded-full bg-fg/70 transition-[width] duration-500" style={{ width: `${v}%` }} />
        </div>
      </div>
    </div>
  );
}

/**
 * The 8 rubric dimensions as a compact visual: two pips per dimension
 * (0 = absent, 1 = partial, 2 = strong), labelled in text so it never relies on colour.
 */
export function RubricGrid({ scores, className }: { scores: Record<string, number> | undefined | null; className?: string }) {
  const rows = rubric(scores);
  return (
    <ul className={cn("grid gap-x-8 gap-y-2 sm:grid-cols-2", className)} aria-label="Prompt rubric">
      {rows.map(({ key, score }) => (
        <li key={key} className="flex items-center gap-3 text-[13px]" title={DIMENSION_HINT[key]} aria-label={`${DIMENSION_LABEL[key]}: ${SCORE_WORD[score].toLowerCase()} (${score} of 2)`}>
          <span className="flex shrink-0 gap-1" aria-hidden>
            {[0, 1].map((i) => (
              <span
                key={i}
                className={cn(
                  "h-2.5 w-5 rounded-[2px]",
                  i < score ? (score === 2 ? "bg-success" : "bg-warning") : score === 0 && i === 0 ? "border border-danger/50 bg-danger-soft" : "bg-surface-3",
                )}
              />
            ))}
          </span>
          <span className="min-w-0 flex-1 truncate text-fg">{DIMENSION_LABEL[key]}</span>
          <span className={cn("shrink-0 text-[11.5px]", score === 2 ? "text-muted" : score === 1 ? "text-warning" : "text-danger")}>{SCORE_WORD[score]}</span>
        </li>
      ))}
    </ul>
  );
}
