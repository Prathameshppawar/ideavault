import { expect, type APIRequestContext, type Page } from "@playwright/test";

export const OWNER = { email: "e2e-owner@example.com", password: "e2e-correct-horse-battery" };
/** Session saved by auth.setup.ts and reused by every spec (gitignored). */
export const AUTH_FILE = "tests/e2e/.auth/owner.json";

const JSON_HEADERS = { "Content-Type": "application/json", "X-IdeaVault-CSRF": "1" };

/** JSON API call that shares the browser's session cookie. */
export async function apiCall<T = unknown>(request: APIRequestContext, method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<T> {
  const res = await request.fetch(`/api${path}`, { method, headers: { ...JSON_HEADERS, ...headers }, data: body === undefined ? undefined : JSON.stringify(body) });
  const text = await res.text();
  if (!res.ok()) throw new Error(`${method} ${path} → ${res.status()}: ${text}`);
  return (text ? JSON.parse(text) : undefined) as T;
}

/** Type into the chat composer, send, and wait for the assistant's answer to complete. */
export async function chat(page: Page, text: string): Promise<string> {
  const before = await page.locator('[data-testid="message"][data-role="assistant"]').count();
  const box = page.getByLabel("Message IdeaVault");
  await box.fill(text);
  await page.getByRole("button", { name: "Send message" }).click();
  const answer = page.locator('[data-testid="message"][data-role="assistant"]').nth(before);
  await expect(answer).toHaveAttribute("data-pending", "false", { timeout: 60_000 });
  return (await answer.innerText()).trim();
}

export interface SeededIdea {
  ideaId: string;
  branchId: string;
  checkpoints: { id: string; number: number }[];
  items: Record<string, string>; // label → id
}

/**
 * Builds an idea with N checkpoints through the public API. Checkpoint k adds knowledge so
 * the history is meaningful: CP2 records "no marketplace", CP7 adds research evidence/insight,
 * and a later reversal supersedes the marketplace decision.
 */
export async function seedIdeaWithCheckpoints(request: APIRequestContext, title: string, n = 7): Promise<SeededIdea> {
  const created = await apiCall<{ idea: { id: string }; branch: { id: string } }>(request, "POST", "/v1/ideas", {
    title,
    origin_text: `I have an idea for ${title.toLowerCase()} where riders create trips together.`,
  });
  const ideaId = created.idea.id;
  const branchId = created.branch.id;
  const items: Record<string, string> = {};
  const rec = async (kind: string, statement: string, extra: Record<string, unknown> = {}) => {
    const it = await apiCall<{ id: string; label: string }>(request, "POST", "/v1/knowledge", { idea_id: ideaId, branch_id: branchId, kind, statement, origin: "SOURCE", source_excerpt: statement, ...extra });
    items[it.label] = it.id;
    return it;
  };
  const checkpoints: { id: string; number: number }[] = [];
  for (let k = 1; k <= n; k++) {
    if (k === 1) await rec("question", "Should trips be public by default?");
    if (k === 2) await rec("decision", "We will never build a marketplace", { rationale: "Moderation would delay launch" });
    if (k === 3) await rec("assumption", "Organisers coordinate trips in group chats today", { risk: "HIGH" });
    if (k === 4) await rec("insight", "Safety features are the differentiator");
    if (k === 7) {
      await rec("evidence", "8 of 10 organisers said regrouping is their biggest pain", { stance: "SUPPORTS", strength: "STRONG" });
      await rec("insight", "Organisers, not riders, are the first customers");
    }
    const cp = await apiCall<{ id: string; number: number }>(request, "POST", `/v1/ideas/${ideaId}/checkpoints`, { branch_id: branchId, title: `Step ${k}` });
    checkpoints.push({ id: cp.id, number: cp.number });
  }
  return { ideaId, branchId, checkpoints, items };
}

export function uniqueTitle(base: string) {
  return `${base} ${Math.random().toString(36).slice(2, 7)}`;
}
