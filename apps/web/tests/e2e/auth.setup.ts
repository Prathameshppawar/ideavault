import { expect, test as setup } from "@playwright/test";
import { AUTH_FILE, OWNER } from "./helpers";

// Runs once before the suite: creates the vault owner through the real setup screen
// (the E2E database starts empty) and saves the session for every other spec.
setup("create the vault owner and sign in", async ({ page }) => {
  const status = await (await page.request.get("/api/v1/auth/status")).json();
  if (status.setup_required) {
    await page.goto("/setup");
    await page.getByLabel("Your name").fill("E2E Owner");
  } else {
    await page.goto("/login");
  }
  await page.getByLabel("Email").fill(OWNER.email);
  await page.getByLabel("Password").fill(OWNER.password);
  await page.getByRole("button", { name: /sign in|create vault/i }).click();
  await expect(page.getByLabel("Message IdeaVault")).toBeVisible({ timeout: 60_000 });
  await page.context().storageState({ path: AUTH_FILE });
});
