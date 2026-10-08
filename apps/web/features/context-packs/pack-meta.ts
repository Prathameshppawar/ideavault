import type { ContextPack } from "@/lib/types";

interface PackJson {
  idea?: { title?: string };
  branch?: { name?: string };
  checkpoint?: { label?: string; title?: string };
}

/** Idea/branch/checkpoint names live in the pack's own JSON snapshot. */
export function packMeta(pack: Pick<ContextPack, "content_json">): { ideaTitle?: string; branchName?: string; checkpointLabel?: string } {
  const j = (pack.content_json ?? {}) as PackJson;
  return { ideaTitle: j.idea?.title, branchName: j.branch?.name, checkpointLabel: j.checkpoint?.label };
}

/** How comfortably a pack fits into typical chat context windows. */
export function tokenFit(tokens: number): { label: string; detail: string } {
  if (tokens <= 8_000) return { label: "Fits easily in any AI chat", detail: `About ${tokens} tokens — small enough for any model.` };
  if (tokens <= 32_000) return { label: "Fits in most AI chats", detail: `About ${tokens} tokens — fine for current ChatGPT, Claude and Gemini models.` };
  return { label: "Large — some chats may truncate it", detail: `About ${tokens} tokens — consider a narrower objective or a checkpoint.` };
}
