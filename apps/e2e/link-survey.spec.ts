import { readFile } from "node:fs/promises";
import { expect, test } from "@playwright/test";
import {
  DEFAULT_QUESTION,
  finishOnboarding,
  nameSurvey,
  publish,
  signUpAndSignIn,
  startSurveyFromScratch,
} from "./helpers";

// forma2's survey.spec.ts, response-filter.spec.ts and response-export.spec.ts, end to end on one survey: create and publish a link survey, answer it on its public page, then find the response, filter by completion and export it.
test("publishes a link survey, collects a response, filters and exports it", async ({
  page,
  browser,
  baseURL,
}) => {
  const surveyName = `Link survey ${Date.now()}`;
  const answer = `Answered by Playwright ${Math.random().toString(36).slice(2, 8)}`;

  await signUpAndSignIn(page, baseURL!, "link");
  const { workspaceId } = await finishOnboarding(page);

  const surveyURL = await test.step("create and publish", async () => {
    await startSurveyFromScratch(page, workspaceId);
    await nameSurvey(page, surveyName);
    await publish(page);
    const share = page.getByRole("dialog", { name: "Share survey" });
    await expect(share.getByRole("textbox", { name: "URL" })).toHaveValue(/\/s\/[^/]+$/);
    return share.getByRole("textbox", { name: "URL" }).inputValue();
  });
  expect(surveyURL).toMatch(/\/s\/[^/]+$/);

  await test.step("answer it on the public page", async () => {
    // A separate context: the respondent is nobody who is signed in.
    const respondent = await browser.newContext();
    try {
      const survey = await respondent.newPage();
      await survey.goto(surveyURL);
      await expect(survey.getByRole("heading", { name: surveyName, level: 1 })).toBeVisible();
      await survey.getByRole("textbox", { name: DEFAULT_QUESTION }).fill(answer);
      await survey.getByRole("button", { name: "Next" }).click();
      await expect(survey.getByRole("heading", { name: "Thank you!" })).toBeVisible();
    } finally {
      await respondent.close();
    }
  });

  const rows = page.getByRole("main").getByRole("table").getByRole("row").filter({ hasText: answer });

  await test.step("see the response", async () => {
    await page.goto(page.url().replace(/\/summary.*$/, "/responses"));
    await expect(rows).toHaveCount(1);
    await expect(rows).toContainText("Completed");
  });

  await test.step("filter by completion", async () => {
    const applyCompletion = async (option: string) => {
      await page.getByRole("button", { name: "Filter" }).click();
      const filter = page.getByRole("dialog");
      await filter
        .getByRole("combobox", { name: "Show all responses that match" })
        .selectOption({ label: option });
      await filter.getByRole("button", { name: "Apply filters" }).click();
      await expect(filter).toBeHidden();
    };
    await applyCompletion("Partial responses");
    await expect(rows).toHaveCount(0);
    await applyCompletion("Complete responses");
    await expect(rows).toHaveCount(1);
  });

  await test.step("export the filtered responses as CSV", async () => {
    await page.getByRole("button", { name: "Download" }).click();
    const [download] = await Promise.all([
      page.waitForEvent("download"),
      page.getByRole("menuitem", { name: "Filtered responses (CSV)" }).click(),
    ]);
    expect(download.suggestedFilename()).toMatch(/\.csv$/);
    const csv = await readFile(await download.path(), "utf8");
    expect(csv).toContain(DEFAULT_QUESTION);
    expect(csv).toContain(answer);
  });
});
