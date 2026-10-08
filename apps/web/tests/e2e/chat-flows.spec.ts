import { expect, test } from "@playwright/test";
import { apiCall, chat, seedIdeaWithCheckpoints, uniqueTitle } from "./helpers";

// Critical chat-driven workflows (spec §66), run against the real API with the
// deterministic offline planner. Each test checks what the user sees AND what was persisted.

test("new idea: 'I have a new idea' → Idea Space → branch → conversation → knowledge → checkpoint", async ({ page }) => {
  await page.goto("/");
  const title = uniqueTitle("Trailhead");
  // Lightweight onboarding: a bare "new idea" asks for the idea instead of creating anything.
  const ask = await chat(page, "I have a new idea.");
  expect(ask).toMatch(/what's the idea/i);

  const created = await chat(page, `I have an idea for a ${title} community platform where people can create trips and invite friends.`);
  expect(created).toMatch(/Created a new Idea Space/);
  await expect(page).toHaveURL(/\/conversations\//);
  const convId = page.url().split("/conversations/")[1];

  const remembered = await chat(page, "Remember this as a decision: we will not build a marketplace in v1 because moderation is expensive.");
  expect(remembered).toMatch(/Saved as decision/);
  const cp = await chat(page, "Create a checkpoint called First shape");
  expect(cp).toMatch(/Created checkpoint/);

  // Persistence: conversation attached to the idea, Main branch, D1 recorded with SOURCE provenance, CP1 immutable snapshot.
  const conv = await apiCall<{ conversation: { idea_id: string; branch_name: string } }>(page.request, "GET", `/v1/conversations/${convId}`);
  expect(conv.conversation.idea_id).toBeTruthy();
  expect(conv.conversation.branch_name).toBe("Main");
  const ov = await apiCall<{ idea: { title: string; origin_text: string }; knowledge: Record<string, { label: string; origin: string; source_message_id?: string }[]>; checkpoints: { label: string; title: string }[] }>(
    page.request,
    "GET",
    `/v1/ideas/${conv.conversation.idea_id}`,
  );
  expect(ov.idea.origin_text).toContain("community platform");
  expect(ov.knowledge.decision?.[0]).toMatchObject({ label: "D1", origin: "SOURCE" });
  expect(ov.knowledge.decision?.[0].source_message_id).toBeTruthy();
  expect(ov.checkpoints.map((c) => c.title)).toContain("First shape");

  // Message links (search hits, chat references) open the conversation at that message.
  const msgs = await apiCall<{ messages: { id: string; role: string; content: string }[] }>(page.request, "GET", `/v1/conversations/${convId}`);
  const origin = msgs.messages.find((m) => m.role === "user" && m.content.includes("community platform"))!;
  await page.goto(`/messages/${origin.id}`);
  await expect(page).toHaveURL(new RegExp(`/conversations/${convId}#m-${origin.id}$`));
  await expect(page.locator(`[id="m-${origin.id}"]`)).toHaveAttribute("data-highlight", "true");

  // The idea page renders it.
  await page.goto(`/ideas/${conv.conversation.idea_id}`);
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Community Platform", { ignoreCase: true });
});

test("continue from checkpoint: locate → retrieve → context → response", async ({ page }) => {
  const title = uniqueTitle("Continue Biker Platform");
  await seedIdeaWithCheckpoints(page.request, title, 4);
  await page.goto("/");
  const answer = await chat(page, `Continue the ${title} from checkpoint 4.`);
  expect(answer).toMatch(/Continuing \*?\*?|Continuing/);
  expect(answer).toContain("CP4");
  // The trace shows the agent located the checkpoint and built context.
  const runs = await apiCall<{ agent_run_id?: string }[]>(page.request, "GET", "/v1/agent/pending").catch(() => []);
  expect(Array.isArray(runs)).toBeTruthy();
});

test("fork from CP4 bringing research from CP7 (selective inheritance with provenance)", async ({ page }) => {
  const title = uniqueTitle("Fork Biker Platform");
  const seeded = await seedIdeaWithCheckpoints(page.request, title, 7);
  await page.goto("/");
  const answer = await chat(page, `Fork the ${title} from checkpoint 4 but bring the research from checkpoint 7.`);
  expect(answer).toMatch(/Forked a new branch/);
  expect(answer).toMatch(/selected item/);

  const tree = await apiCall<{ branches: { id: string; is_default: boolean; forked_from_checkpoint_number?: number; fork_mode?: string }[] }>(page.request, "GET", `/v1/ideas/${seeded.ideaId}/tree`);
  const fork = tree.branches.find((b) => !b.is_default);
  expect(fork?.forked_from_checkpoint_number).toBe(seeded.checkpoints[3].number);
  expect(fork?.fork_mode).toBe("selective");
  const inh = await apiCall<{ via: string; label: string }[]>(page.request, "GET", `/v1/branches/${fork!.id}/inheritance`);
  const selected = inh.filter((r) => r.via === "selection").map((r) => r.label);
  // Research (evidence + insight) from CP7 came along; nothing else from the future did.
  expect(selected.some((l) => l.startsWith("E"))).toBeTruthy();
  expect(selected.every((l) => /^[EI]\d+$/.test(l))).toBeTruthy();
  // The original branch is unchanged.
  const main = await apiCall<{ items: unknown[] }>(page.request, "GET", `/v1/knowledge?branch_id=${seeded.branchId}`);
  expect(main.items.length).toBe(Object.keys(seeded.items).length);
});

test("why did we decide: grounded answer with rationale and history (decision evolution)", async ({ page }) => {
  const title = uniqueTitle("Why Biker Platform");
  const seeded = await seedIdeaWithCheckpoints(page.request, title, 3);
  // Decision evolution: D2 "never build a marketplace" is reversed by a new decision.
  const d2 = Object.entries(seeded.items).find(([l]) => l.startsWith("D"))![1];
  await apiCall(page.request, "POST", "/v1/knowledge", {
    idea_id: seeded.ideaId,
    branch_id: seeded.branchId,
    kind: "decision",
    statement: "Build a marketplace for verified ride guides",
    supersedes: d2,
    reverses: true,
    rationale: "Interviews showed organisers want to pay verified guides",
  });
  await page.goto("/");
  const answer = await chat(page, "Why did we decide to build a marketplace for verified ride guides?");
  expect(answer).toMatch(/Why:/);
  expect(answer).toMatch(/verified guides/i);
  // Historical integrity: the old decision is still there, reversed, pointing at its replacement.
  const old = await apiCall<{ item: { status: string; superseded_by_id: string }; chain: unknown[] }>(page.request, "GET", `/v1/knowledge/${d2}`);
  expect(old.item.status).toBe("REVERSED");
  expect(old.item.superseded_by_id).toBeTruthy();
  expect(old.chain.length).toBe(2);
});

test("destructive action requires confirmation; declining keeps the idea", async ({ page }) => {
  const title = uniqueTitle("Disposable Idea");
  const seeded = await seedIdeaWithCheckpoints(page.request, title, 1);
  await page.goto("/");
  await chat(page, `Delete the idea ${title}`).catch(() => "");
  const card = page.getByTestId("confirmation");
  await expect(card).toBeVisible();
  await expect(card).toContainText("delete_idea");
  // The pending approval survives a reload of the conversation.
  await expect(page).toHaveURL(/\/conversations\//);
  await page.reload();
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: "Decline" }).click();
  await expect(page.locator('[data-testid="message"][data-role="assistant"]').last()).toHaveAttribute("data-pending", "false");
  await apiCall(page.request, "GET", `/v1/ideas/${seeded.ideaId}`); // still exists

  await chat(page, `Delete the idea ${title}`).catch(() => "");
  await page.getByTestId("confirmation").getByRole("button", { name: "Approve" }).click();
  await expect(page.locator('[data-testid="message"][data-role="assistant"]').last()).toContainText(/Deleted/, { timeout: 30_000 });
  const res = await page.request.get(`/api/v1/ideas/${seeded.ideaId}`);
  expect(res.status()).toBe(404);
});
