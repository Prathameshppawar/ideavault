import { createElement } from "react";
import {
  CalendarDays,
  Code,
  Globe,
  Hash,
  HardDrive,
  Mail,
  MessageSquareText,
  NotebookText,
  Plug,
  Webhook,
  type LucideIcon,
} from "lucide-react";
import type { ConnectorView } from "@/lib/types";

const ICONS: Record<string, LucideIcon> = {
  web: Globe,
  github: Code,
  custom: Webhook,
  gdrive: HardDrive,
  gmail: Mail,
  slack: Hash,
  calendar: CalendarDays,
  notion: NotebookText,
  chatgpt: MessageSquareText,
  claude: MessageSquareText,
  gemini: MessageSquareText,
};

/** Icon for a connector (generic glyphs; no brand marks). */
export function ConnectorIcon({ connectorKey, className }: { connectorKey: string; className?: string }) {
  return createElement(ICONS[connectorKey] ?? Plug, { className, "aria-hidden": true });
}

const NAMES: Record<string, string> = {
  web: "Web",
  github: "GitHub",
  custom: "Custom HTTP",
  gdrive: "Google Drive",
  gmail: "Gmail",
  slack: "Slack",
  calendar: "Google Calendar",
  notion: "Notion",
  chatgpt: "ChatGPT",
  claude: "Claude",
  gemini: "Gemini",
};

export function connectorName(key: string): string {
  return NAMES[key] ?? key;
}

export const AUTH_LABEL: Record<string, string> = {
  none: "No credentials",
  api_key: "API key / URL",
  oauth2: "OAuth 2.0",
  export: "Export file",
};

export type ConnectorGroup = "available" | "export" | "oauth";

/** How a connector can be set up in this deployment (drives the honest CTA). */
export function connectorGroup(c: Pick<ConnectorView, "auth_type" | "implemented">): ConnectorGroup {
  if (c.auth_type === "export") return "export";
  if (!c.implemented || c.auth_type === "oauth2") return "oauth";
  return "available";
}

export function statusTone(status: string): "success" | "danger" | "muted" {
  if (status === "CONNECTED") return "success";
  if (status === "ERROR") return "danger";
  return "muted";
}

export function statusLabel(status: string): string {
  if (status === "CONNECTED") return "Connected";
  if (status === "ERROR") return "Error";
  return "Not connected";
}

/** Initial permission selection: what's granted when connected, otherwise everything. */
export function initialPermissions(c: Pick<ConnectorView, "status" | "permissions" | "granted_permissions">): string[] {
  const all = c.permissions ?? [];
  const granted = (c.granted_permissions ?? []).filter((p) => all.includes(p));
  return c.status === "CONNECTED" && granted.length > 0 ? granted : all;
}

/** Body for POST /v1/connectors/{key}/connect: trimmed, non-empty credentials only. */
export function connectPayload(values: Record<string, string>, permissions: string[]) {
  const credentials: Record<string, string> = {};
  for (const [k, v] of Object.entries(values)) if (v.trim()) credentials[k] = v.trim();
  return { credentials, permissions };
}
