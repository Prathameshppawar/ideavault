import path from "node:path";
import { expect, test } from "@playwright/test";
import { apiCall, uniqueTitle } from "./helpers";

const FIXTURES = path.resolve(__dirname, "../../../../tests/fixtures/imports");

// Review screens (spec §66): an external AI conversation is analysed against the vault,
// reviewed in the UI and merged selectively — history is superseded, never rewritten.

const TRANSCRIPT = `User: We decided we will not build a marketplace for riders in version one.

Assistant: That makes sense. I assume riders will pay for premium route packs.

User: I assume riders will pay for premium route packs and group expense splitting features.

User: We need to prototype the shared route planner before the beta launch.`;

type Item = { id: string; label: string; status: string; statement: string; superseded_by_id?: string };

test("external delta: paste → classified review → merge selected → merge checkpoint, old items preserved", async ({ page }) => {
  const title = uniqueTitle("Delta Riders");
  const created = await apiCall<{ idea: { id: string }; branch: { id: string } }>(page.request, "POST", "/v1/ideas", { title, origin_text: "Riders and trips." });
  const ideaId = created.idea.id;
  const d1 = await apiCall<Item>(page.request, "POST", "/v1/knowledge", { idea_id: ideaId, kind: "decision", statement: "We will build a marketplace for riders in version one" });
  const a1 = await apiCall<Item>(page.request, "POST", "/v1/knowledge", { idea_id: ideaId, kind: "assumption", statement: "Riders will pay for premium route packs" });

  // Paste the external conversation.
  await page.goto("/deltas/new");
  await page.getByLabel("Idea", { exact: true }).selectOption({ label: title });
  await page.getByLabel("Conversation text").fill(TRANSCRIPT);
  await page.getByRole("button", { name: /Analyze changes/ }).click();
  await expect(page).toHaveURL(/\/deltas\/[0-9a-f-]{36}$/, { timeout: 30_000 });
  await expect(page.getByRole("heading", { level: 1, name: "Review what changed" })).toBeVisible();

  // Each class gets its own group; restated items are not pre-selected.
  for (const group of ["New", "Changed", "Rejected", "Unchanged"]) {
    await expect(page.getByRole("heading", { level: 2, name: new RegExp(`^${group}`) })).toBeVisible();
  }
  const deltaId = page.url().split("/deltas/")[1];
  const delta = await apiCall<{ status: string; items: { id: string; classification: string; selected: boolean; statement: string }[] }>(page.request, "GET", `/v1/deltas/${deltaId}`);
  expect(delta.status).toBe("PENDING_REVIEW");
  const unchanged = delta.items.find((i) => i.classification === "UNCHANGED")!;
  const showUnchanged = page.locator("section", { has: page.getByRole("heading", { name: /^Unchanged/ }) }).getByRole("button", { name: /^Show/ });
  if (await showUnchanged.isVisible()) await showUnchanged.click();
  await expect(page.getByRole("checkbox", { name: `Include: ${unchanged.statement}` })).not.toBeChecked();

  // Nothing changes before the merge.
  expect((await apiCall<{ item: Item }>(page.request, "GET", `/v1/knowledge/${d1.id}`)).item.status).toBe("ACTIVE");

  const selected = delta.items.filter((i) => i.selected).length;
  await page.getByRole("button", { name: `Merge ${selected} selected` }).click();
  await expect(page.getByText(/Merged \d+ changes? into your thinking/)).toBeVisible({ timeout: 30_000 });

  const after = await apiCall<{ status: string; merge_checkpoint_id?: string }>(page.request, "GET", `/v1/deltas/${deltaId}`);
  expect(after.status).toBe("PARTIALLY_MERGED"); // the UNCHANGED item stayed out
  expect(after.merge_checkpoint_id).toBeTruthy();

  // The rejected decision is reversed and the changed assumption superseded — both still exist, unedited.
  const oldD = (await apiCall<{ item: Item }>(page.request, "GET", `/v1/knowledge/${d1.id}`)).item;
  const oldA = (await apiCall<{ item: Item }>(page.request, "GET", `/v1/knowledge/${a1.id}`)).item;
  expect(oldD).toMatchObject({ status: "REVERSED", statement: d1.statement });
  expect(oldA).toMatchObject({ status: "SUPERSEDED", statement: a1.statement });
  expect(oldD.superseded_by_id).toBeTruthy();

  const cps = await apiCall<{ id: string; kind: string }[]>(page.request, "GET", `/v1/ideas/${ideaId}/checkpoints`);
  expect(cps.find((c) => c.id === after.merge_checkpoint_id)?.kind).toBe("merge");
});

test("Import Center: upload a Claude export → preview → import → ideas with proposals", async ({ page }) => {
  await page.goto("/imports");
  await page.getByTestId("file-input").setInputFiles(path.join(FIXTURES, "claude/conversations.json"));
  await page.getByRole("button", { name: /Upload & preview/ }).click();
  await expect(page).toHaveURL(/\/imports\/[0-9a-f-]{36}$/, { timeout: 30_000 });
  await expect(page.getByRole("heading", { level: 2, name: /^Found \d+ conversations?/ })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("Clinic scheduling assistant")).toBeVisible();

  await page.getByRole("button", { name: /^Import \d+ conversations?/ }).click();
  await expect(page.getByText(/^Imported \d+ conversations?$/)).toBeVisible({ timeout: 30_000 });

  const importId = page.url().split("/imports/")[1];
  const detail = await apiCall<{ import: { status: string; adapter: string }; items: { status: string; title: string; idea_id?: string }[] }>(page.request, "GET", `/v1/imports/${importId}`);
  expect(detail.import.status).toBe("COMPLETED");
  expect(detail.import.adapter).toMatch(/^claude/);
  const clinic = detail.items.find((i) => i.title === "Clinic scheduling assistant")!;
  expect(clinic.status).toBe("IMPORTED");
});

test("Import Center flags instruction-like text in the preview", async ({ page }) => {
  await page.goto("/imports");
  await page.getByTestId("file-input").setInputFiles(path.join(FIXTURES, "injection/malicious_chatgpt.json"));
  await page.getByRole("button", { name: /Upload & preview/ }).click();
  await expect(page.getByText("Trip sharing notes")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("Instruction-like text").first()).toBeVisible();
});
