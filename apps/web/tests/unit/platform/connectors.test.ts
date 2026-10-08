import { describe, expect, it } from "vitest";
import { connectPayload, connectorGroup, initialPermissions, statusLabel, statusTone } from "@/features/connectors/connector-meta";
import { providerState } from "@/features/models/providers-tab";
import type { ProviderStatus } from "@/lib/types";

describe("connector helpers", () => {
  it("classifies connectors honestly by how they can be set up", () => {
    expect(connectorGroup({ auth_type: "api_key", implemented: true })).toBe("available");
    expect(connectorGroup({ auth_type: "none", implemented: true })).toBe("available");
    expect(connectorGroup({ auth_type: "export", implemented: false })).toBe("export");
    expect(connectorGroup({ auth_type: "oauth2", implemented: false })).toBe("oauth");
  });

  it("defaults to all permissions, or what was granted when connected", () => {
    expect(initialPermissions({ status: "NOT_CONNECTED", permissions: ["github.read", "github.issues.write"], granted_permissions: [] })).toEqual(["github.read", "github.issues.write"]);
    expect(initialPermissions({ status: "CONNECTED", permissions: ["github.read", "github.issues.write"], granted_permissions: ["github.read"] })).toEqual(["github.read"]);
  });

  it("sends only non-empty, trimmed credentials", () => {
    expect(connectPayload({ api_key: "  ghp_x  ", base_url: "" }, ["github.read"])).toEqual({ credentials: { api_key: "ghp_x" }, permissions: ["github.read"] });
  });

  it("maps statuses to pills", () => {
    expect([statusTone("CONNECTED"), statusTone("ERROR"), statusTone("NOT_CONNECTED")]).toEqual(["success", "danger", "muted"]);
    expect(statusLabel("NOT_CONNECTED")).toBe("Not connected");
  });
});

describe("provider state", () => {
  const p = (over: Partial<ProviderStatus>): ProviderStatus => ({ id: "groq", name: "Groq", configured: false, source: "none", key_required: true, models: 3, notes: "", ...over });
  it("never claims a provider is active unless the server says so", () => {
    expect(providerState(p({ configured: true, source: "vault" })).label).toBe("Active");
    expect(providerState(p({ source: "env" }))).toMatchObject({ tone: "warning", label: "Inactive" });
    expect(providerState(p({})).label).toBe("Not configured");
    expect(providerState(p({ id: "mock", configured: true, source: "builtin" })).label).toBe("Built in");
  });
});
