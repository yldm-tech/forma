import { expect, test } from "@playwright/test";
import { BUCKET_UPLOAD_METHOD, PNG, finishOnboarding, signUpAndSignIn, waitForBucketUpload } from "./helpers";

// The workspace logo (Workspace settings > Look) is a public file: the admin app signs it with POST /api/v1/management/storage, uploads it straight to the bucket, saves its relative /storage URL, and anyone can then load it through the server, which reads the private bucket for them.

test("uploads a workspace logo and serves it to anyone @upload", async ({ page, browser, baseURL }) => {
  await signUpAndSignIn(page, baseURL!, "logo");
  const { workspaceId } = await finishOnboarding(page);

  await page.goto(`/app/workspaces/${workspaceId}/settings/workspace/look`);
  const card = page.locator("#edit-logo");
  const signed = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/v1/management/storage"
  );
  const bucketUpload = waitForBucketUpload(page, baseURL!);
  await card
    .getByTestId("upload-file-input")
    .setInputFiles({ name: "brand.png", mimeType: "image/png", buffer: PNG });
  const signing = (await (await signed).json()) as { data: { fileUrl: string; uploadMethod?: string } };
  // The admin app sends the file's size, and uploads with the method the server signed.
  expect(JSON.parse((await signed).request().postData()!)).toMatchObject({ fileSize: PNG.length });
  expect(signing.data.uploadMethod?.toUpperCase() ?? "POST").toBe(BUCKET_UPLOAD_METHOD);
  const uploaded = await bucketUpload;
  expect(uploaded.request().method()).toBe(BUCKET_UPLOAD_METHOD);
  expect(uploaded.ok()).toBe(true);

  const logo = card.getByRole("img", { name: "Logo" });
  await expect(logo).toHaveAttribute("src", signing.data.fileUrl);
  await expect.poll(() => logo.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth)).toBe(2);
  await card.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Logo updated successfully")).toBeVisible();

  // Someone who is not signed in loads the saved logo from the server, never from the bucket.
  const visitor = await browser.newContext();
  try {
    const response = await visitor.request.get(signing.data.fileUrl);
    expect(response.status()).toBe(200);
    expect(response.headers()["content-type"]).toBe("image/png");
    expect(Buffer.from(await response.body()).equals(PNG)).toBe(true);
  } finally {
    await visitor.close();
  }
});

// The presigned PUT binds the size the client declared: the store refuses a body of any other length, so the 5 MB limit checked at signing cannot be stepped around with a bigger file.
test("a presigned PUT stores only a body of the declared size @upload", async ({ page, baseURL }) => {
  test.skip(BUCKET_UPLOAD_METHOD !== "PUT", "presigned PUT uploads are off (STORAGE_UPLOAD_METHOD=post)");
  await signUpAndSignIn(page, baseURL!, "put-size");
  const { workspaceId } = await finishOnboarding(page);

  const sign = await page.request.post("/api/v1/management/storage", {
    data: { workspaceId, fileName: "probe.png", fileType: "image/png", fileSize: PNG.length },
    headers: { Origin: baseURL!, "X-Requested-With": "XMLHttpRequest" },
  });
  expect(sign.ok()).toBe(true);
  const { data } = (await sign.json()) as {
    data: { fileUrl: string; uploadMethod: string; uploadUrl: string; uploadHeaders: Record<string, string> };
  };
  expect(data.uploadMethod).toBe("put");

  const larger = await page.request.put(data.uploadUrl, {
    headers: data.uploadHeaders,
    data: Buffer.concat([PNG, Buffer.from("x")]),
  });
  expect(larger.status()).toBe(403);
  expect((await page.request.get(data.fileUrl)).status()).toBe(404);

  const exact = await page.request.put(data.uploadUrl, { headers: data.uploadHeaders, data: PNG });
  expect(exact.status()).toBe(200);
  const stored = await page.request.get(data.fileUrl);
  expect(stored.status()).toBe(200);
  expect(Buffer.from(await stored.body()).equals(PNG)).toBe(true);
});
