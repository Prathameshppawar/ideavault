// Turns the server's structured query interpretation into one plain-English sentence.
import type { SearchResponse } from "@/lib/types";
import { entityMeta } from "@/lib/entities";

type Interp = SearchResponse["interpretation"];

/** "a", "a and b", "a, b and c" */
export function listJoin(items: string[]): string {
  if (items.length <= 1) return items[0] ?? "";
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

function statusClause(statuses: string[] | undefined): string {
  if (!statuses?.length) return "";
  const s = new Set(statuses.map((x) => x.toUpperCase()));
  if (s.has("UNVALIDATED") || s.has("VALIDATING")) return " that haven't been validated";
  if (s.has("OPEN")) return " that are still open";
  if (s.has("SUPERSEDED") || s.has("REVERSED")) return " that were reversed or superseded";
  return ` with status ${listJoin([...s].map((x) => x.toLowerCase().replace(/_/g, " ")))}`;
}

const ORDER: Record<string, string> = {
  oldest: "earliest first",
  newest: "newest first",
  relevance: "most relevant first",
};

/**
 * "Showing decisions matching “biker marketplace”, most relevant first."
 * "Showing messages, conversations and ideas matching “ai automation”, earliest first."
 */
export function describeInterpretation(interp: Interp | undefined): string {
  if (!interp) return "";
  const order = ORDER[interp.order] ?? ORDER.relevance;
  if (interp.similar_to) return `Showing ideas similar to “${interp.similar_to}”, ${order}.`;
  const types = (interp.types ?? []).map((t) => entityMeta(t).plural.toLowerCase());
  const what = types.length ? listJoin(types) : "everything in your vault";
  const terms = interp.terms?.trim() ? ` matching “${interp.terms.trim()}”` : "";
  if (!types.length && !terms && !interp.statuses?.length) return interp.explanation ? `${interp.explanation.charAt(0).toUpperCase()}${interp.explanation.slice(1)}.` : "";
  return `Showing ${what}${statusClause(interp.statuses)}${terms}, ${order}.`;
}
