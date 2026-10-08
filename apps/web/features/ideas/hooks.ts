"use client";

import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { BranchTree, Checkpoint, IdeaOverview, InheritanceRecord, JourneyNode, KnowledgeExplanation } from "@/lib/types";

// ---------- Query keys ----------
// Everything about one idea lives under ["idea", id] so one invalidation (also done by the chat
// when a run touches the idea) refreshes overview, journey, tree and checkpoint lists together.
export const ideaKeys = {
  idea: (id: string) => ["idea", id] as const,
  overview: (id: string, branchId: string | null) => ["idea", id, "overview", branchId ?? "default"] as const,
  journey: (id: string) => ["idea", id, "journey"] as const,
  tree: (id: string) => ["idea", id, "tree"] as const,
  checkpoints: (id: string) => ["idea", id, "checkpoints"] as const,
};

export function useIdeaOverview(id: string | undefined, branchId: string | null) {
  return useQuery({
    queryKey: ideaKeys.overview(id ?? "", branchId),
    queryFn: () => api.get<IdeaOverview>(`/v1/ideas/${id}`, { query: { branch_id: branchId ?? undefined } }),
    enabled: Boolean(id),
    placeholderData: (prev) => (prev && prev.idea?.id === id ? prev : undefined),
  });
}

export function useJourney(id: string, enabled = true) {
  return useQuery({ queryKey: ideaKeys.journey(id), queryFn: () => api.get<JourneyNode[]>(`/v1/ideas/${id}/journey`), enabled });
}

export function useBranchTree(id: string, enabled = true) {
  return useQuery({ queryKey: ideaKeys.tree(id), queryFn: () => api.get<BranchTree>(`/v1/ideas/${id}/tree`), enabled });
}

export function useIdeaCheckpoints(id: string | undefined) {
  return useQuery({
    queryKey: ideaKeys.checkpoints(id ?? ""),
    queryFn: () => api.get<Checkpoint[]>(`/v1/ideas/${id}/checkpoints`),
    enabled: Boolean(id),
  });
}

/** A checkpoint with its full snapshot. Snapshots are immutable, so they never go stale. */
export function useCheckpoint(id: string | null | undefined) {
  return useQuery({
    queryKey: ["checkpoint", id],
    queryFn: () => api.get<Checkpoint>(`/v1/checkpoints/${id}`),
    enabled: Boolean(id),
    staleTime: Infinity,
  });
}

export function useKnowledgeExplanation(id: string | null | undefined) {
  return useQuery({
    queryKey: ["knowledge", id],
    queryFn: () => api.get<KnowledgeExplanation>(`/v1/knowledge/${id}`),
    enabled: Boolean(id),
  });
}

export function useInheritance(branchId: string | null | undefined) {
  return useQuery({
    queryKey: ["branch", branchId, "inheritance"],
    queryFn: () => api.get<InheritanceRecord[]>(`/v1/branches/${branchId}/inheritance`),
    enabled: Boolean(branchId),
  });
}

/** Refresh everything that might show this idea after a mutation. */
export function useRefreshIdea() {
  const qc = useQueryClient();
  return useCallback(
    (ideaId: string) =>
      Promise.all([
        qc.invalidateQueries({ queryKey: ["idea", ideaId] }),
        qc.invalidateQueries({ queryKey: ["ideas"] }),
        qc.invalidateQueries({ queryKey: ["knowledge"] }),
        qc.invalidateQueries({ queryKey: ["branch"] }),
      ]),
    [qc],
  );
}

// ---------- URL state ----------

export type IdeaTab = "overview" | "journey" | "knowledge" | "branches" | "conversations" | "artifacts";
export const IDEA_TABS: IdeaTab[] = ["overview", "journey", "knowledge", "branches", "conversations", "artifacts"];

/**
 * Idea page state that belongs in the URL: ?branch=&tab=&cp=&item=.
 * Uses the native History API, which Next.js syncs with useSearchParams (no server round trip).
 */
export function useIdeaUrlState() {
  const sp = useSearchParams();
  const pathname = usePathname();
  const tabParam = sp.get("tab");
  const tab: IdeaTab = (IDEA_TABS as string[]).includes(tabParam ?? "") ? (tabParam as IdeaTab) : "overview";
  const set = useCallback(
    (patch: Record<string, string | null | undefined>, mode: "push" | "replace" = "push") => {
      const p = new URLSearchParams(window.location.search);
      for (const [k, v] of Object.entries(patch)) {
        if (v === null || v === undefined || v === "") p.delete(k);
        else p.set(k, v);
      }
      if (p.get("tab") === "overview") p.delete("tab");
      const s = p.toString();
      const url = `${pathname}${s ? `?${s}` : ""}`;
      if (mode === "push") window.history.pushState(null, "", url);
      else window.history.replaceState(null, "", url);
    },
    [pathname],
  );
  return {
    branch: sp.get("branch"),
    tab,
    cp: sp.get("cp"),
    item: sp.get("item"),
    set,
  };
}

// ---------- Small utilities ----------

export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (cb) => {
      const m = window.matchMedia(query);
      m.addEventListener("change", cb);
      return () => m.removeEventListener("change", cb);
    },
    () => window.matchMedia(query).matches,
    () => false,
  );
}

export function useDebounced<T>(value: T, ms = 250): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/** Seconds elapsed while `running` is true (for long operations like artifact generation). */
export function useElapsed(running: boolean): number {
  const [state, setState] = useState({ start: 0, now: 0 });
  useEffect(() => {
    if (!running) return;
    const start = Date.now();
    const t = setInterval(() => setState({ start, now: Date.now() }), 1000);
    return () => {
      clearInterval(t);
      setState({ start: 0, now: 0 });
    };
  }, [running]);
  return running && state.start ? Math.floor((state.now - state.start) / 1000) : 0;
}
