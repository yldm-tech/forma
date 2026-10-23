import { expect, test } from "@playwright/test";
import { finishOnboarding, signUpAndSignIn } from "./helpers";

// forma2's storage-smoke.spec.ts against real file storage instead of a mocked one: an image added to a question in the survey editor is uploaded to the S3 bucket (RustFS in CI) and served back from the URL the survey stores.

/** A 2x2 PNG, built here so the spec owns its fixture. */
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR42mPgL8sDIgYIBQAZJgPNigndUQAAAABJRU5ErkJggg==",
  "base64"
);

// The browser uploads straight to the bucket. A real deployment's S3 endpoint is https, which the server's Content-Security-Policy allows (connect-src https:); the CI's RustFS is plain http on its own port, which it does not. forma2 widened its policy to loopback origins under E2E_TESTING; the Go server keeps forma2's production policy only, so this spec lets the browser ignore it instead.
test.use({ bypassCSP: true });

test("uploads an image to a question and serves it from storage", async ({ page, baseURL }) => {
  await signUpAndSignIn(page, baseURL!, "upload");
  const { workspaceId, surveyId } = await finishOnboarding(page);

  await page.goto(`/workspaces/${workspaceId}/surveys/${surveyId}/edit`);
  await page.getByRole("button", { name: "Toggle image uploader" }).click();
  await page
    .getByTestId("upload-file-input")
    .setInputFiles({ name: "playwright.png", mimeType: "image/png", buffer: PNG });

  // The editor previews the picked file from memory at once; the URL the survey stores is the one the upload returned, which the live preview beside the editor renders.
  await expect(page.getByRole("main").getByRole("img", { name: "playwright.png" })).toBeVisible();
  const stored = page.getByRole("complementary").locator('img[src*="/storage/"]');
  await expect(stored).toBeVisible();
  // Decoded by the browser, so the bytes really came back from storage.
  await expect
    .poll(() => stored.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth))
    .toBe(2);

  const response = await page.request.get((await stored.getAttribute("src"))!);
  expect(response.status()).toBe(200);
  expect(response.headers()["content-type"]).toContain("image/png");
});
