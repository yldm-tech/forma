import { expect, test } from "@playwright/test";
import { finishOnboarding, signUpAndSignIn } from "./helpers";

// The survey table's columns are fixed tracks beside a flexible name column; this keeps a 1280px screen from squeezing the name to a sliver (it showed all eight columns from xl once, which left the name about 12rem). Measured in the three scripts the catalogue ships, since the header labels differ in length.
test("the survey name keeps at least 16rem at 1280px in English, Japanese and Chinese", async ({
  page,
  baseURL,
}) => {
  await signUpAndSignIn(page, baseURL!, "survey-list");
  const { workspaceId } = await finishOnboarding(page);
  await page.setViewportSize({ width: 1280, height: 800 });

  for (const locale of ["en-US", "ja-JP", "zh-Hans-CN"]) {
    await test.step(locale, async () => {
      const response = await page.request.patch("/api/app/auth/me", {
        data: { locale },
        headers: { Origin: baseURL!, "X-Requested-With": "XMLHttpRequest" },
      });
      expect(response.ok()).toBe(true);
      await page.goto(`/app/workspaces/${workspaceId}/surveys`);
      const table = page.getByRole("main").getByRole("table");
      await expect(table.getByRole("row")).toHaveCount(2);
      const nameHeader = await table.getByRole("columnheader").first().boundingBox();
      expect(nameHeader!.width).toBeGreaterThanOrEqual(16 * 16);
      expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBe(0);
    });
  }
});
