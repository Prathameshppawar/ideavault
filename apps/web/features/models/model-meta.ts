// Provider/model presentation helpers shared by the Models, Usage and Settings screens.
import type { ModelConfig } from "@/lib/types";

export const PROVIDER_ORDER = ["groq", "openai", "anthropic", "gemini", "ollama", "mock"] as const;

const PROVIDER_LABEL: Record<string, string> = {
  groq: "Groq",
  openai: "OpenAI",
  anthropic: "Anthropic",
  gemini: "Gemini",
  ollama: "Ollama",
  mock: "Offline planner",
};

/**
 * Fixed provider → colour token mapping (colour follows the entity, never its rank).
 * The offline planner is deliberately neutral: it is not an AI model.
 */
const PROVIDER_TONE: Record<string, string> = {
  groq: "--k-decision",
  openai: "--k-branch",
  anthropic: "--k-assumption",
  gemini: "--k-artifact",
  ollama: "--k-insight",
  mock: "--text-faint",
};

export function providerLabel(id: string): string {
  return PROVIDER_LABEL[id] ?? id;
}

/** CSS custom property name (e.g. "--k-decision") for a provider's series colour. */
export function providerToneVar(id: string): string {
  return PROVIDER_TONE[id] ?? "--k-conversation";
}

export function providerColor(id: string): string {
  return `var(${providerToneVar(id)})`;
}

export function providerRank(id: string): number {
  const i = (PROVIDER_ORDER as readonly string[]).indexOf(id);
  return i === -1 ? PROVIDER_ORDER.length : i;
}

export function sortProviders<T>(items: T[], id: (t: T) => string): T[] {
  return [...items].sort((a, b) => providerRank(id(a)) - providerRank(id(b)) || id(a).localeCompare(id(b)));
}

/** Providers whose API is OpenAI-compatible and can list their own models. */
export const LISTABLE_PROVIDERS = new Set(["groq", "openai", "ollama"]);

export function modelKey(m: Pick<ModelConfig, "provider" | "model">): string {
  return `${m.provider}/${m.model}`;
}

/** Split a usage/routing key "provider/model" (model ids may themselves contain "/"). */
export function splitModelKey(key: string): { provider: string; model: string } {
  const i = key.indexOf("/");
  return i > 0 ? { provider: key.slice(0, i), model: key.slice(i + 1) } : { provider: "", model: key };
}

export const CAPABILITIES = ["tools", "json", "vision", "reasoning", "fast", "local", "offline"] as const;
export type Capability = (typeof CAPABILITIES)[number];

export const CAPABILITY_HINT: Record<Capability, string> = {
  tools: "Native tool / function calling",
  json: "Structured (JSON schema) output",
  vision: "Accepts images",
  reasoning: "Reasoning model",
  fast: "Low-latency",
  local: "Runs locally",
  offline: "Deterministic, no AI",
};

/** Capabilities for display: the declared list merged with the boolean flags. */
export function capabilitiesOf(m: ModelConfig): Capability[] {
  const set = new Set<string>(m.capabilities ?? []);
  if (m.tool_calling) set.add("tools");
  if (m.structured_output) set.add("json");
  if (m.vision) set.add("vision");
  if (m.reasoning) set.add("reasoning");
  return CAPABILITIES.filter((c) => set.has(c));
}

export function formatContext(n: number): string {
  if (!n) return "—";
  // Binary sizes (131072, 1048576) read as 128K / 1M, decimal ones (400000) as 400K.
  if (n >= 1_000_000) return `${+(n % 1_048_576 === 0 ? n / 1_048_576 : n / 1_000_000).toFixed(1)}M`;
  if (n >= 1000) return `${+(n % 1024 === 0 ? n / 1024 : n / 1000).toFixed(0)}K`;
  return String(n);
}

export function perMTok(n: number): string {
  if (!n) return "free";
  return `$${n < 0.1 ? n.toFixed(3).replace(/0+$/, "") : n.toFixed(2)}`;
}

export const RELATIVE_COST_LABEL = ["Free", "Very cheap", "Cheap", "Moderate", "Expensive", "Premium"];

export function humanTask(task: string): string {
  const special: Record<string, string> = { model_lab: "Model Lab", prompt_analysis: "Prompt analysis", thinking_analysis: "Thinking analysis", branch_comparison: "Branch comparison" };
  if (special[task]) return special[task];
  const t = task.replace(/_/g, " ");
  return t.charAt(0).toUpperCase() + t.slice(1);
}
