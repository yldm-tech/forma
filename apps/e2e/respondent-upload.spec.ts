import { expect, test } from "@playwright/test";
import {
  BUCKET_UPLOAD_METHOD,
  DEFAULT_QUESTION,
  PNG,
  finishOnboarding,
  nameSurvey,
  publish,
  signUpAndSignIn,
  startSurveyFromScratch,
  waitForBucketUpload,
} from "./helpers";

// A respondent's answer to a file upload question: the survey runtime (packages/surveys) on the public survey page signs the file with the client API, uploads it straight to the bucket, and submits its private /storage URL, which only the workspace's members can read.

test("a respondent uploads a file on the survey page and the workspace reads it @upload", async ({
  page,
  browser,
  baseURL,
}) => {
  const surveyName = `Upload survey ${Date.now()}`;
  await signUpAndSignIn(page, baseURL!, "respondent-upload");
  const { workspaceId } = await finishOnboarding(page);

  const surveyURL = await test.step("create and publish a survey with a file upload question", async () => {
    await startSurveyFromScratch(page, workspaceId);
    await nameSurvey(page, surveyName);
    await page.getByTestId("add-element-trigger").click();
    await page.getByTestId("add-element-picker").getByRole("button", { name: "File Upload" }).click();
    // The new question opens with an empty headline, which publishing refuses.
    await page.getByRole("textbox", { name: "Question*" }).last().fill("Attach a screenshot");
    await publish(page);
    const share = page.getByRole("dialog", { name: "Share survey" });
    await expect(share.getByRole("textbox", { name: "URL" })).toHaveValue(/\/s\/[^/]+$/);
    return share.getByRole("textbox", { name: "URL" }).inputValue();
  });

  const fileUrl = await test.step("upload a file on the public page and submit", async () => {
    const respondent = await browser.newContext();
    try {
      const survey = await respondent.newPage();
      await survey.goto(surveyURL);
      await survey.getByRole("textbox", { name: DEFAULT_QUESTION }).fill("See the attached screenshot");
      await survey.getByRole("button", { name: "Next" }).click();

      const signed = survey.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).pathname === `/api/v1/client/${workspaceId}/storage`
      );
      const bucketUpload = waitForBucketUpload(survey, baseURL!);
      await survey
        .locator('input[type="file"]')
        .setInputFiles({ name: "screenshot.png", mimeType: "image/png", buffer: PNG });
      const signing = await signed;
      // The runtime sends the decoded file's size and uploads with the method the server signed.
      expect(JSON.parse(signing.request().postData()!)).toMatchObject({
        fileName: "screenshot.png",
        fileSize: PNG.length,
      });
      const { data } = (await signing.json()) as { data: { fileUrl: string } };
      const uploaded = await bucketUpload;
      expect(uploaded.request().method()).toBe(BUCKET_UPLOAD_METHOD);
      expect(uploaded.ok()).toBe(true);
      await expect(survey.getByText("screenshot.png")).toBeVisible();

      await survey.getByRole("button", { name: /^(Finish|Next)$/ }).click();
      await expect(survey.getByRole("heading", { name: "Thank you!" })).toBeVisible();
      // A respondent cannot read the private file back.
      expect((await survey.request.get(data.fileUrl)).status()).toBe(401);
      return data.fileUrl;
    } finally {
      await respondent.close();
    }
  });

  await test.step("the workspace reads the uploaded file through the server", async () => {
    expect(fileUrl).toMatch(
      new RegExp(
        `^/storage/${workspaceId}/private/surveys/[^/]+/elements/[^/]+/screenshot--fid--[0-9a-f-]+\\.png$`
      )
    );
    const response = await page.request.get(fileUrl);
    expect(response.status()).toBe(200);
    expect(response.headers()["content-type"]).toBe("image/png");
    expect(Buffer.from(await response.body()).equals(PNG)).toBe(true);
  });
});
