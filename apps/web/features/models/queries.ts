"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { ModelConfig, ProviderStatus, RoutingPreview } from "@/lib/types";
import type { components } from "@/types/api.generated";

export type BenchmarkTask = components["schemas"]["EvalsBenchmarkTask"];
export type RoutingPolicy = components["schemas"]["ModelsRoutingPolicy"];
export type Candidate = components["schemas"]["ModelsCandidate"];
export type AvailableModels = components["schemas"]["HttpavailableModels"];

export const modelKeys = {
  models: ["models"] as const,
  providers: ["model-providers"] as const,
  routing: ["model-routing"] as const,
  settings: ["settings"] as const,
  benchmarks: ["model-benchmarks"] as const,
  labRuns: ["lab-runs"] as const,
  labRun: (id: string) => ["lab-run", id] as const,
  available: (provider: string) => ["provider-available", provider] as const,
};

export function useModels() {
  return useQuery({ queryKey: modelKeys.models, queryFn: () => api.get<ModelConfig[]>("/v1/models") });
}

export function useProviders() {
  return useQuery({ queryKey: modelKeys.providers, queryFn: () => api.get<ProviderStatus[]>("/v1/models/providers") });
}

export function useRouting() {
  return useQuery({ queryKey: modelKeys.routing, queryFn: () => api.get<RoutingPreview[]>("/v1/models/routing") });
}

/** User settings; the current routing policy lives under `routing`. */
export function useSettings() {
  return useQuery({ queryKey: modelKeys.settings, queryFn: () => api.get<Record<string, unknown>>("/v1/settings") });
}
