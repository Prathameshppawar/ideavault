"use client";

import { useCallback, useMemo, useSyncExternalStore } from "react";

// ---------- Theme-aware colours ----------
// Charts (SVG via recharts) and the universe canvas need concrete colour values. We resolve
// CSS custom properties from <html> and re-resolve whenever the theme class changes.

function subscribeTheme(cb: () => void) {
  const obs = new MutationObserver(cb);
  obs.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "style", "data-theme"] });
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  mq.addEventListener?.("change", cb);
  return () => {
    obs.disconnect();
    mq.removeEventListener?.("change", cb);
  };
}
const themeSnapshot = () => `${document.documentElement.className}|${document.documentElement.getAttribute("style") ?? ""}`;
const serverThemeSnapshot = () => "";

/** Stable token that changes whenever the theme changes ("" during SSR). */
export function useThemeKey(): string {
  return useSyncExternalStore(subscribeTheme, themeSnapshot, serverThemeSnapshot);
}

/**
 * Resolve CSS variables (e.g. "--k-decision", "--border") to computed colour strings.
 * Returns empty strings until mounted on the client.
 */
export function useCssVars<T extends string>(names: readonly T[]): Record<T, string> {
  const key = useThemeKey();
  const namesKey = names.join(",");
  return useMemo(() => {
    const out = {} as Record<T, string>;
    const style = key && typeof window !== "undefined" ? getComputedStyle(document.documentElement) : null;
    for (const n of namesKey.split(",") as T[]) out[n] = style ? style.getPropertyValue(n).trim() : "";
    return out;
  }, [key, namesKey]);
}

// ---------- Reduced motion ----------
function subscribeMotion(cb: () => void) {
  const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
  mq.addEventListener?.("change", cb);
  return () => mq.removeEventListener?.("change", cb);
}
export function useReducedMotion(): boolean {
  return useSyncExternalStore(
    subscribeMotion,
    () => window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    () => false,
  );
}

// ---------- Current time ----------
// A minute-resolution clock. Null during SSR/hydration so time-relative text never mismatches.
let nowValue = 0;
const nowListeners = new Set<() => void>();
let nowTimer: ReturnType<typeof setInterval> | null = null;
function subscribeNow(cb: () => void) {
  nowListeners.add(cb);
  if (!nowTimer) {
    nowTimer = setInterval(() => {
      nowValue = Date.now();
      nowListeners.forEach((l) => l());
    }, 60_000);
  }
  return () => {
    nowListeners.delete(cb);
    if (nowListeners.size === 0 && nowTimer) {
      clearInterval(nowTimer);
      nowTimer = null;
    }
  };
}
function nowSnapshot() {
  // Refresh lazily if the cached value is stale (e.g. first read, or after the tab slept).
  const t = Date.now();
  if (t - nowValue > 60_000) nowValue = t;
  return nowValue;
}
export function useNow(): Date | null {
  const t = useSyncExternalStore(subscribeNow, nowSnapshot, () => 0);
  return useMemo(() => (t ? new Date(t) : null), [t]);
}

// ---------- Media query ----------
export function useMediaQuery(query: string, serverValue = false): boolean {
  const subscribe = useCallback(
    (cb: () => void) => {
      const mq = window.matchMedia(query);
      mq.addEventListener?.("change", cb);
      return () => mq.removeEventListener?.("change", cb);
    },
    [query],
  );
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia(query).matches,
    () => serverValue,
  );
}
