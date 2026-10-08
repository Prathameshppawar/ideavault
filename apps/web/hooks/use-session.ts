"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { User } from "@/lib/types";

export interface AuthStatus {
  setup_required: boolean;
  authenticated: boolean;
  user?: User;
  version: string;
}

export function useSession() {
  return useQuery({
    queryKey: ["auth-status"],
    queryFn: () => api.get<AuthStatus>("/v1/auth/status"),
    staleTime: 60_000,
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return async () => {
    await api.post("/v1/auth/logout");
    qc.clear();
    // Full reload on purpose: drops every piece of in-memory state from the signed-out session.
    // eslint-disable-next-line @next/next/no-location-assign-relative-destination
    window.location.href = "/login";
  };
}

export interface SystemStatus {
  version: string;
  env: string;
  offline_mode: boolean;
  providers: { id: string; name: string; configured: boolean; source: string }[];
  embedder: { provider?: string; model?: string };
  jobs: Record<string, number>;
  health: Record<string, unknown>;
}

export function useSystemStatus() {
  return useQuery({
    queryKey: ["system-status"],
    queryFn: () => api.get<SystemStatus>("/v1/system/status"),
    staleTime: 60_000,
  });
}
