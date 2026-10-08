import { readFileSync } from "node:fs";
import path from "node:path";
import { expect, test, type APIRequestContext } from "@playwright/test";
import { apiCall, chat, uniqueTitle } from "./helpers";

// Imports, outcomes (artifacts, implementation prompt, context pack) and prompt-injection
// defenses (spec §66). Real API + offline planner; persistence is verified through the API.

const FIXTURES = path.resolve(__dirname, "../../../../tests/fixtures/imports");

type Import = { id: string; status: string; total_items: number };
type ImportDetail = { import: Import; items: { id: string; status: string; title: string; conversation_id?: string; idea_id?: string; injection_flags?: string[] }[] };

async function upload(request: APIRequestContext, rel: string): Promise<Import> {
  const res = await request.post("/api/v1/imports", {
    headers: { "X-IdeaVault-CSRF": "1" },
    multipart: { file: { name: path.basename(rel), mimeType: "application/json", buffer: readFileSync(path.join(FIXTURES, rel)) } },
  });
  expect(res.status(), await res.text()).toBe(202);
  return res.json();
}

async function waitForImport(request: APIRequestContext, id: string, status: string): Promise<ImportDetail> {
  let detail: ImportDetail | undefined;
  await expect
    .poll(async () => {
      detail = await apiCall<ImportDetail>(request, "GET", `/v1/imports/${id}`);
      return detail.import.status;
    }, { timeout: 30_000 })
    .toBe(status);
  return detail!;
}

/** Starts a fresh chat with an idea created through the agent and returns its id. */
async function ideaThroughChat(page: import("@playwright/test").Page, title: string) {
  await page.goto("/");
  const created = await chat(page, `I have an idea for ${title}: a tool that turns trip plans into shared checklists for riding groups.`);
  expect(created).toMatch(/Created a new Idea Space/);
  await expect(page).toHaveURL(/\/conversations\//);
  const convId = page.url().split("/conversations/")[1];
  const conv = await apiCall<{ conversation: { idea_id: string } }>(page.request, "GET", `/v1/conversations/${convId}`);
  return conv.conversation.idea_id;
}

test("import a ChatGPT export: preview → commit as new ideas → provenance, current branch only, dedupe", async ({ page }) => {
  const imp = await upload(page.request, "chatgpt/conversations.json");
  const preview = await waitForImport(page.request, imp.id, "PREVIEW");
  const titles = preview.items.map((i) => i.title);
  expect(titles).toEqual(expect.arrayContaining(["Biker community trip platform", "Clinic management platform"]));

  await apiCall(page.request, "POST", `/v1/imports/${imp.id}/commit`, {
    extract: true,
    items: preview.items.map((i) => ({ item_id: i.id, include: true, new_idea: true })),
  });
  const done = await waitForImport(page.request, imp.id, "COMPLETED");
  const biker = done.items.find((i) => i.title === "Biker community trip platform")!;
  expect(biker.status).toBe("IMPORTED");

  // Only the active branch of the ChatGPT tree is preserved; tool output and edited-away turns are not.
  const conv = await apiCall<{ conversation: { origin: string; provider: string; idea_id: string }; messages: { content: string; role: string }[] }>(
    page.request,
    "GET",
    `/v1/conversations/${biker.conversation_id}`,
  );
  expect(conv.conversation.provider).toBe("chatgpt");
  const all = conv.messages.map((m) => m.content).join("\n");
  expect(all).toContain("community platform for bikers");
  expect(all).not.toContain("OLD BRANCH");
  expect(all).not.toContain("TOOL OUTPUT SHOULD NOT BE IMPORTED");

  // Extracted knowledge is a proposal with source provenance, awaiting review.
  const proposed = await apiCall<{ items: { review_state: string; origin: string; source_message_id?: string }[] }>(
    page.request,
    "GET",
    `/v1/knowledge?idea_id=${conv.conversation.idea_id}&review=PROPOSED`,
  );
  expect(proposed.items.length).toBeGreaterThan(0);
  for (const it of proposed.items) expect(it.source_message_id).toBeTruthy();

  // The imported conversation is visible in the UI.
  await page.goto(`/conversations/${biker.conversation_id}`);
  await expect(page.getByText("community platform for bikers").first()).toBeVisible();

  // Importing the same export again is detected as a duplicate.
  const again = await upload(page.request, "chatgpt/conversations.json");
  const second = await waitForImport(page.request, again.id, "PREVIEW");
  expect(second.items.every((i) => i.status === "DUPLICATE")).toBeTruthy();
});

test("prompt injection in imported content is flagged and never executed", async ({ page }) => {
  const before = await apiCall<{ total: number }>(page.request, "GET", "/v1/ideas?limit=1");
  const imp = await upload(page.request, "injection/malicious_chatgpt.json");
  const preview = await waitForImport(page.request, imp.id, "PREVIEW");
  const item = preview.items[0];
  expect(item.injection_flags?.length).toBeGreaterThan(0);

  await apiCall(page.request, "POST", `/v1/imports/${imp.id}/commit`, { extract: true, items: [{ item_id: item.id, include: true, new_idea: true }] });
  const done = await waitForImport(page.request, imp.id, "COMPLETED");
  const convId = done.items[0].conversation_id!;

  // Ask the agent about the poisoned conversation: it must treat the text as data.
  await page.goto("/");
  const answer = await chat(page, "Summarize my Trip sharing notes.");
  expect(answer).not.toMatch(/system prompt:|You are IdeaVault/i);
  await expect(page.getByTestId("confirmation")).toHaveCount(0);
  await expect(page.locator('[data-testid="trace-step"][data-tool="delete_idea"]')).toHaveCount(0);

  const after = await apiCall<{ total: number }>(page.request, "GET", "/v1/ideas?limit=1");
  expect(after.total).toBe(before.total + 1); // only the import's own idea was added
  await apiCall(page.request, "GET", `/v1/conversations/${convId}`); // the conversation itself is preserved as-is
});

test("conclude into an action plan, then an implementation prompt; download the Markdown", async ({ page }) => {
  const ideaId = await ideaThroughChat(page, uniqueTitle("Ride Checklists"));
  await chat(page, "Remember this as a decision: v1 is a mobile web app because riders won't install another app.");
  await chat(page, "Remember this as an assumption: group organisers will share one checklist link in their existing chat.");

  const concluded = await chat(page, "I think we're done with this idea. Turn everything into an action plan.");
  expect(concluded).toMatch(/Concluded as/);
  expect(concluded).toMatch(/Artifacts/);

  const prompt = await chat(page, "Give me a prompt I can give to an implementation agent.");
  expect(prompt).toMatch(/implementation prompt/i);

  const arts = await apiCall<{ id: string; type: string; title: string }[]>(page.request, "GET", `/v1/artifacts?idea_id=${ideaId}`);
  const types = arts.map((a) => a.type);
  expect(types).toEqual(expect.arrayContaining(["ACTION_PLAN", "IMPLEMENTATION_PROMPT"]));

  // Provenance: the artifact points back at the decisions it was built from.
  const plan = arts.find((a) => a.type === "ACTION_PLAN")!;
  const prov = await apiCall<{ entity_type?: string; label?: string }[]>(page.request, "GET", `/v1/artifacts/${plan.id}/provenance`);
  expect(prov.length).toBeGreaterThan(0);

  // Download through the UI: a real .md file with the recorded decision in it.
  await page.goto(`/artifacts/${plan.id}`);
  const [download] = await Promise.all([page.waitForEvent("download"), page.getByRole("button", { name: /download/i }).first().click()]);
  expect(download.suggestedFilename()).toMatch(/\.md$/);
  const file = readFileSync((await download.path())!, "utf8");
  expect(file).toMatch(/^# /m);
  expect(file).toContain("mobile web app");
});

test("context pack for continuing in another AI: built from the vault and exportable", async ({ page }) => {
  const ideaId = await ideaThroughChat(page, uniqueTitle("Pack Rides"));
  await chat(page, "Remember this as a decision: start with organisers of weekend group rides.");
  const answer = await chat(page, "Build a context pack so I can continue this in ChatGPT.");
  expect(answer).toMatch(/Built a context pack/);

  const packs = await apiCall<{ id: string; idea_id: string; token_estimate: number }[]>(page.request, "GET", `/v1/context-packs?idea_id=${ideaId}`);
  expect(packs.length).toBe(1);
  expect(packs[0].token_estimate).toBeGreaterThan(0);
  const md = await page.request.get(`/api/v1/context-packs/${packs[0].id}/export?format=md`);
  expect(md.ok()).toBeTruthy();
  const text = await md.text();
  expect(text).toContain("weekend group rides");
  // Packs carry instructions for the external AI but no secrets or internal ids.
  expect(text).not.toMatch(/gsk_|sk-[A-Za-z0-9]{10,}/);
});
