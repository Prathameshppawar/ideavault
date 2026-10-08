import { expect, test } from "@playwright/test";

// The app shell on a phone: the sidebar becomes a drawer and pages get the full width.
test.use({ viewport: { width: 390, height: 844 } });

test("phone: drawer navigation, full-width pages, no horizontal scroll", async ({ page }) => {
  await page.goto("/ideas");
  const nav = page.getByRole("complementary", { name: "Primary" });
  await expect(nav).not.toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();

  await page.getByRole("button", { name: "Open navigation" }).click();
  await expect(nav).toBeInViewport();
  await nav.getByRole("link", { name: "Artifacts" }).click();
  await expect(page).toHaveURL(/\/artifacts$/);
  await expect(nav).not.toBeInViewport(); // closes on navigation

  await page.getByRole("button", { name: "Open navigation" }).click();
  await page.keyboard.press("Escape");
  await expect(nav).not.toBeInViewport();

  // The chat composer is reachable at full width.
  await page.goto("/");
  const box = await page.getByLabel("Message IdeaVault").boundingBox();
  expect(box!.width).toBeGreaterThan(300);
});
