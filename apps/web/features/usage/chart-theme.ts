"use client";

import { useEffect, useState } from "react";
import { useTheme } from "@/stores/ui";

/**
 * Resolve CSS custom properties (e.g. "--k-decision") to concrete colours for SVG charts.
 * Re-reads whenever the resolved theme changes so charts follow dark mode.
 */
export function useCssVars<T extends string>(names: readonly T[]): Record<T, string> {
  const resolved = useTheme((s) => s.resolved);
  const key = names.join("|");
  const [vals, setVals] = useState<Record<T, string>>(() => read(names));
  useEffect(() => {
    // The theme class is toggled synchronously before the store updates; read on the next frame
    // so computed styles reflect it.
    const id = requestAnimationFrame(() => setVals(read(key.split("|") as T[])));
    return () => cancelAnimationFrame(id);
  }, [resolved, key]);
  return vals;
}

function read<T extends string>(names: readonly T[]): Record<T, string> {
  const out = {} as Record<T, string>;
  const style = typeof window !== "undefined" ? getComputedStyle(document.documentElement) : null;
  for (const n of names) out[n] = style?.getPropertyValue(n).trim() || "currentColor";
  return out;
}

export const CHART_BASE_VARS = ["--surface", "--border", "--text", "--text-muted", "--text-faint", "--danger", "--accent", "--surface-2", "--k-checkpoint"] as const;
