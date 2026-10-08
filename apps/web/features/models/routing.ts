// Pure helpers for the routing preferences editor (unit-tested).
import type { RoutingPolicy } from "./queries";

export interface RoutingDraft {
  /** task → "provider/model"; "" means automatic. */
  pins: Record<string, string>;
  /** Preferred providers, most preferred first. */
  prefer: string[];
  /** Exclude models with relative cost above this (0 = no limit). */
  maxCost: number;
}

export const EMPTY_DRAFT: RoutingDraft = { pins: {}, prefer: [], maxCost: 0 };

/** Read the stored policy from GET /v1/settings (`settings.routing`), tolerating missing/garbage values. */
export function draftFromSettings(settings: Record<string, unknown> | undefined | null): RoutingDraft {
  const raw = (settings?.routing ?? null) as Partial<RoutingPolicy> | null;
  if (!raw || typeof raw !== "object") return { ...EMPTY_DRAFT, pins: {} };
  const pins: Record<string, string> = {};
  if (raw.pins && typeof raw.pins === "object") {
    for (const [task, key] of Object.entries(raw.pins)) if (typeof key === "string" && key) pins[task] = key;
  }
  const prefer = Array.isArray(raw.prefer_providers) ? raw.prefer_providers.filter((p): p is string => typeof p === "string" && p !== "") : [];
  const maxCost = typeof raw.max_relative_cost === "number" && raw.max_relative_cost > 0 ? Math.round(raw.max_relative_cost) : 0;
  return { pins, prefer, maxCost };
}

/** Body for PUT /v1/models/routing — always the full policy (the server replaces it). */
export function buildRoutingPolicy(d: RoutingDraft): Required<RoutingPolicy> {
  const pins: Record<string, string> = {};
  for (const [task, key] of Object.entries(d.pins)) {
    const k = key.trim();
    if (k) pins[task] = k;
  }
  const seen = new Set<string>();
  const prefer = d.prefer.map((p) => p.trim().toLowerCase()).filter((p) => p && !seen.has(p) && (seen.add(p), true));
  return { pins, prefer_providers: prefer, max_relative_cost: Math.max(0, Math.min(5, Math.round(d.maxCost || 0))) };
}

export function samePolicy(a: RoutingDraft, b: RoutingDraft): boolean {
  return JSON.stringify(canonical(buildRoutingPolicy(a))) === JSON.stringify(canonical(buildRoutingPolicy(b)));
}

function canonical(p: Required<RoutingPolicy>) {
  return { ...p, pins: Object.fromEntries(Object.entries(p.pins).sort(([a], [b]) => a.localeCompare(b))) };
}

/** Move an item one step up/down within a list (no-op at the edges). */
export function move<T>(list: T[], index: number, delta: -1 | 1): T[] {
  const j = index + delta;
  if (index < 0 || index >= list.length || j < 0 || j >= list.length) return list;
  const out = [...list];
  [out[index], out[j]] = [out[j], out[index]];
  return out;
}
