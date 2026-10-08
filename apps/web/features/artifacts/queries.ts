"use client";

import { useQueries, useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Artifact, ArtifactVersion, Branch, Checkpoint, KnowledgeExplanation, ProvenanceEntry } from "@/lib/types";

export interface ArtifactFilters {
  type?: string;
  status?: string;
  q?: string;
  idea_id?: string;
}

export const artifactKeys = {
  list: (f: ArtifactFilters) => ["artifacts", f] as const,
  one: (id: string) => ["artifact", id] as const,
  versions: (id: string) => ["artifact", id, "versions"] as const,
  provenance: (id: string, version: number) => ["artifact", id, "provenance", version] as const,
};

export function useArtifacts(f: ArtifactFilters) {
  return useQuery({
    queryKey: artifactKeys.list(f),
    queryFn: () => api.get<Artifact[]>("/v1/artifacts", { query: { ...f } }),
    placeholderData: (prev) => prev,
  });
}

export function useArtifact(id: string) {
  return useQuery({ queryKey: artifactKeys.one(id), queryFn: () => api.get<Artifact>(`/v1/artifacts/${id}`) });
}

export function useArtifactVersions(id: string) {
  return useQuery({
    queryKey: artifactKeys.versions(id),
    queryFn: () => api.get<ArtifactVersion[]>(`/v1/artifacts/${id}/versions`),
    select: (vs) => [...vs].sort((a, b) => b.version - a.version),
  });
}

/** Provenance for a version (0 = current). */
export function useProvenance(id: string, version = 0) {
  return useQuery({
    queryKey: artifactKeys.provenance(id, version),
    queryFn: () => api.get<ProvenanceEntry[]>(`/v1/artifacts/${id}/provenance`, { query: { version: version || undefined } }),
  });
}

/**
 * Details (supersession chain) for provenance items that no longer stand, so we can say
 * what replaced them and whether that happened after the version was written.
 */
export function useDeadItemDetails(ids: string[]) {
  return useQueries({
    queries: ids.map((id) => ({
      queryKey: ["knowledge", "detail", id],
      queryFn: () => api.get<KnowledgeExplanation>(`/v1/knowledge/${id}`),
      staleTime: 60_000,
    })),
  });
}

export function useIdeaCheckpoints(ideaId: string | undefined, enabled = true) {
  return useQuery({
    queryKey: ["idea", ideaId, "checkpoints"],
    queryFn: () => api.get<Checkpoint[]>(`/v1/ideas/${ideaId}/checkpoints`),
    enabled: !!ideaId && enabled,
  });
}

export function useIdeaBranches(ideaId: string | undefined, enabled = true) {
  return useQuery({
    queryKey: ["idea", ideaId, "branches"],
    queryFn: () => api.get<Branch[]>(`/v1/ideas/${ideaId}/branches`),
    enabled: !!ideaId && enabled,
  });
}
