import { expect, test } from "@playwright/test";
import { finishOnboarding, signUpAndSignIn } from "./helpers";

// forma2's storage-smoke.spec.ts against real file storage instead of a mocked one: an image added to a question in the survey editor is uploaded to the S3 bucket (RustFS in CI) and served back from the URL the survey stores.

/** A 2x2 PNG, built here so the spec owns its fixture. */
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR42mPgL8sDIgYIBQAZJgPNigndUQAAAABJRU5ErkJggg==",
  "base64"
);

// The browser uploads straight to the bucket, under the server's Content-Security-Policy. The CI's RustFS is plain http on a loopback port, which the policy allows (connect-src) only under E2E_TESTING or outside production, as forma2's did; a real deployment's S3 endpoint is https. A policy that blocks the upload fails this spec.
test("uploads an image to a question and serves it from storage", async ({ page, baseURL }) => {
  const cspViolations: string[] = [];
  page.on("console", (message) => {
    if (message.text().includes("Content Security Policy")) cspViolations.push(message.text());
  });
  await signUpAndSignIn(page, baseURL!, "upload");
  const { workspaceId, surveyId } = await finishOnboarding(page);

  await page.goto(`/app/workspaces/${workspaceId}/surveys/${surveyId}/edit`);
  await page.getByRole("button", { name: "Add photo or video" }).click();
  const appOrigin = new URL(baseURL!).origin;
  const bucketUpload = page.waitForResponse(
    (response) => response.request().method() === "POST" && new URL(response.url()).origin !== appOrigin,
    { timeout: 15_000 }
  );
  await page
    .getByTestId("upload-file-input")
    .setInputFiles({ name: "playwright.png", mimeType: "image/png", buffer: PNG });
  // The presigned POST went from the page to the bucket's own origin and was accepted.
  expect((await bucketUpload).ok()).toBe(true);

  // Once the upload is done, the editor shows the file under the name the bucket stores it as (the picked name with a file id), and the live preview beside the editor renders the URL the upload returned.
  await expect(
    page.getByRole("main").getByRole("img", { name: /^playwright--fid--[0-9a-f-]+\.png$/ })
  ).toBeVisible();
  const stored = page.getByRole("complementary").locator('img[src*="/storage/"]');
  await expect(stored).toBeVisible();
  // Decoded by the browser, so the bytes really came back from storage.
  await expect
    .poll(() => stored.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth))
    .toBe(2);

  const response = await page.request.get((await stored.getAttribute("src"))!);
  expect(response.status()).toBe(200);
  expect(response.headers()["content-type"]).toContain("image/png");
  expect(cspViolations).toEqual([]);
});
