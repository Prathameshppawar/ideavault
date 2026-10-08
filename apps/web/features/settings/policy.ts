// Mirrors apps/api/internal/agent/policy.go so the UI can explain what each tool will do.
import type { components } from "@/types/api.generated";

export type AgentPolicy = components["schemas"]["AgentPolicy"];
export type ToolInfo = components["schemas"]["HttptoolInfo"];

export type Decision = "allow" | "confirm" | "deny";

export const CATEGORY_ORDER = ["READ", "ANALYZE", "WRITE", "EXTERNAL", "DESTRUCTIVE"] as const;

export const CATEGORY_COPY: Record<string, { title: string; body: string }> = {
  READ: { title: "Read", body: "Look things up in your vault." },
  ANALYZE: { title: "Analyze", body: "Compute comparisons, patterns and context packs without changing anything." },
  WRITE: { title: "Write", body: "Record knowledge, create branches, checkpoints and artifacts." },
  EXTERNAL: { title: "External", body: "Reach outside IdeaVault through a connector." },
  DESTRUCTIVE: { title: "Destructive", body: "Permanently delete. Always asks first." },
};

/** Same rules as the server's Policy.Decide. */
export function decide(tool: string, category: string, p: AgentPolicy): Decision {
  if ((p.disabled_tools ?? []).includes(tool)) return "deny";
  switch (category) {
    case "READ":
    case "ANALYZE":
      return "allow";
    case "WRITE":
      return p.confirm_writes ? "confirm" : "allow";
    case "DESTRUCTIVE":
      return "confirm";
    case "EXTERNAL":
      return p.auto_external ? "allow" : "confirm";
  }
  return "deny";
}

export const DECISION_LABEL: Record<Decision, string> = { allow: "Runs automatically", confirm: "Asks first", deny: "Disabled" };

export function toggleDisabled(p: AgentPolicy, tool: string, disabled: boolean): AgentPolicy {
  const set = new Set(p.disabled_tools ?? []);
  if (disabled) set.add(tool);
  else set.delete(tool);
  return { ...p, disabled_tools: [...set].sort() };
}

export function groupTools(tools: ToolInfo[]): { category: string; tools: ToolInfo[] }[] {
  const order = (c: string) => {
    const i = (CATEGORY_ORDER as readonly string[]).indexOf(c);
    return i === -1 ? 99 : i;
  };
  const map = new Map<string, ToolInfo[]>();
  for (const t of tools) map.set(t.category, [...(map.get(t.category) ?? []), t]);
  return [...map.entries()].sort(([a], [b]) => order(a) - order(b)).map(([category, ts]) => ({ category, tools: ts }));
}

/** Friendly device label from a user-agent string. */
export function describeUserAgent(ua: string): string {
  if (!ua) return "Unknown client";
  if (/^curl\//i.test(ua)) return "curl";
  if (/^node$/i.test(ua) || /node-fetch|undici/i.test(ua)) return "Node.js script";
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /HeadlessChrome/.test(ua)
      ? "Headless Chrome"
      : /Chrome\//.test(ua)
        ? "Chrome"
        : /Firefox\//.test(ua)
          ? "Firefox"
          : /Safari\//.test(ua)
            ? "Safari"
            : null;
  const os = /Mac OS X|Macintosh/.test(ua) ? "macOS" : /Windows/.test(ua) ? "Windows" : /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Linux/.test(ua) ? "Linux" : null;
  if (browser) return os ? `${browser} on ${os}` : browser;
  return ua.length > 40 ? `${ua.slice(0, 39)}…` : ua;
}
