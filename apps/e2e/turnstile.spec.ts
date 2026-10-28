import { type Server, createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { type BrowserContextOptions, type Page, devices, expect, test } from "@playwright/test";
import {
  DEFAULT_QUESTION,
  PASSWORD,
  finishOnboarding,
  nameSurvey,
  post,
  publish,
  startSurveyFromScratch,
  uniqueEmail,
} from "./helpers";

// Cloudflare Turnstile as the spam protection of public surveys, with Cloudflare's documented test keys (developers.cloudflare.com/turnstile/troubleshooting/testing): the site key 1x00000000000000000000AA renders a widget that always passes with the dummy token XXXX.DUMMY.TOKEN.XXXX, which the secret 1x0000000000000000000000000000000AA accepts and 2x0000000000000000000000000000000AA refuses. The instance needs those keys, which every other spec runs without, so these run only when E2E_TURNSTILE names the secret the server was restarted with (e2e.yml: `pass`, then `fail`).
const mode = process.env.E2E_TURNSTILE;

const DUMMY_TOKEN = "XXXX.DUMMY.TOKEN.XXXX";

/** Waits for Turnstile's challenge frame, which Cloudflare serves from challenges.cloudflare.com inside a closed shadow root that locators cannot reach, so it is found among the page's frames. */
const expectTurnstileFrame = (page: Page) =>
  expect
    .poll(() => page.frames().some((frame) => frame.url().startsWith("https://challenges.cloudflare.com/")))
    .toBe(true);

/** The hidden field Turnstile writes the widget's token into once the challenge is passed. */
const turnstileToken = (page: Page) => page.locator('input[name="cf-turnstile-response"]');

/** Content-Security-Policy violations the page reports, so a policy that blocks Turnstile's script or frame fails the spec instead of only its widget. */
const watchPolicy = (page: Page): string[] => {
  const violations: string[] = [];
  page.on("console", (message) => {
    if (message.text().includes("Content Security Policy")) violations.push(message.text());
  });
  return violations;
};

/** Turns spam protection on in the editor's Response Options, where the switch names Turnstile and offers no score threshold, and publishes the survey; returns its public URL. */
const publishSpamProtectedSurvey = async (page: Page, workspaceId: string): Promise<string> => {
  await startSurveyFromScratch(page, workspaceId);
  await nameSurvey(page, `Spam protected ${Date.now()}`);
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  const spamSwitch = page.getByRole("switch", { name: /^Spam protection/ });
  await spamSwitch.scrollIntoViewIfNeeded();
  await expect(page.getByText(/uses Cloudflare Turnstile/)).toBeVisible();
  await spamSwitch.click();
  await expect(spamSwitch).toBeChecked();
  await expect(page.getByText("Response threshold", { exact: false })).toHaveCount(0);
  await publish(page);
  const share = page.getByRole("dialog", { name: "Share survey" });
  const url = share.getByRole("textbox", { name: "URL" });
  await expect(url).toHaveValue(/\/s\/[^/]+$/);
  return url.inputValue();
};

/** Opens the survey as a respondent nobody signed in, on the device given, answers its question and returns the response create the page sent. */
const answerAsRespondent = async (
  page: Page,
  surveyURL: string,
  answer: string,
  device: BrowserContextOptions = {}
) => {
  const respondent = await page.context().browser()!.newContext(device);
  const survey = await respondent.newPage();
  const violations = watchPolicy(survey);
  await survey.goto(surveyURL);
  await expect(survey.locator("#forma-turnstile-script")).toHaveCount(1);
  await expectTurnstileFrame(survey);
  await expect(turnstileToken(survey)).toHaveValue(DUMMY_TOKEN);
  await survey.getByRole("textbox", { name: DEFAULT_QUESTION }).fill(answer);
  const created = survey.waitForResponse(
    (response) =>
      response.request().method() === "POST" && /\/api\/v[12]\/client\/[^/]+\/responses$/.test(response.url())
  );
  await survey.getByRole("button", { name: /^(Next|Finish)$/ }).click();
  const response = await created;
  return { respondent, survey, response, violations };
};

/** Signs the seeded owner in (`forma seed`, internal/seed), which needs no Turnstile token, and returns the seeded workspace. */
const signInSeededOwner = async (page: Page, baseURL: string): Promise<string> => {
  await post(page.request, baseURL, "/api/app/auth/sign-in", {
    email: "admin@forma.yldm.ai",
    password: "Password#123",
  });
  await page.goto("/app");
  await page.waitForURL(/\/app\/workspaces\/[^/]+/);
  return /\/workspaces\/([^/?]+)/.exec(page.url())![1];
};

/** A customer's page with the SDK snippet the app-connection page hands out (sdk.spec.ts). */
const hostPage = (appUrl: string, workspaceId: string) => `<!doctype html>
<html lang="en">
  <head>
    <title>Customer site</title>
    <script type="text/javascript">
      !(function () {
        var t = document.createElement("script");
        (t.type = "text/javascript"), (t.async = !0), (t.src = "${appUrl}/js/forma.umd.cjs");
        var e = document.getElementsByTagName("script")[0];
        e.parentNode.insertBefore(t, e);
        t.onload = function () {
          window.forma.setup({ workspaceId: "${workspaceId}", appUrl: "${appUrl}" });
        };
      })();
    </script>
  </head>
  <body>
    <main><h1>A page with the Forma SDK</h1></main>
  </body>
</html>`;

let server: Server | undefined;

test.afterEach(async () => {
  await new Promise<void>((resolve) => (server ? server.close(() => resolve()) : resolve()));
  server = undefined;
});

test.describe("Turnstile spam protection", () => {
  test.skip(!mode, "needs an instance with Cloudflare's Turnstile test keys (E2E_TURNSTILE, see e2e.yml)");

  test("signs up through Turnstile, and a protected survey takes a response with the widget's token", async ({
    page,
  }) => {
    test.skip(mode !== "pass", "the secret accepts the dummy token only in the pass run");
    const policy = watchPolicy(page);
    const answer = `A person answered ${Math.random().toString(36).slice(2, 8)}`;

    await test.step("sign up: the page loads Turnstile under its Content-Security-Policy", async () => {
      await page.goto("/app/auth/signup");
      await page.getByRole("textbox", { name: "Full name" }).fill("Playwright User");
      await page.getByRole("textbox", { name: "Email" }).fill(uniqueEmail("turnstile"));
      await page.getByRole("textbox", { name: "Password" }).fill(PASSWORD);
      await expectTurnstileFrame(page);
      // The always-pass key issues the token on its own, without the visitor doing anything.
      await expect(turnstileToken(page)).toHaveValue(DUMMY_TOKEN);
      const signup = page.waitForRequest(
        (request) => request.method() === "POST" && request.url().endsWith("/api/app/auth/signup")
      );
      await page.getByRole("button", { name: "Continue with Email" }).click();
      expect((await signup).postDataJSON().turnstileToken).toBe(DUMMY_TOKEN);
      await page.waitForURL(/\/app\/organizations\/[^/]+\/workspaces\/new\/survey/);
    });

    const workspaceId = await test.step("finish onboarding", async () => {
      await page.getByRole("button", { name: /Start from scratch/ }).click();
      await page.waitForURL(/\/app\/workspaces\/[^/]+\/surveys\/[^/]+\/edit/);
      return /\/workspaces\/([^/]+)\//.exec(page.url())![1];
    });

    const surveyURL = await test.step("turn spam protection on and publish", () =>
      publishSpamProtectedSurvey(page, workspaceId));

    await test.step("answer it on a phone: the create carries the token and is taken", async () => {
      const { respondent, survey, response, violations } = await answerAsRespondent(
        page,
        surveyURL,
        answer,
        devices["Pixel 7"]
      );
      try {
        expect(response.request().postDataJSON().turnstileToken).toBe(DUMMY_TOKEN);
        expect(response.status()).toBe(200);
        await expect(survey.getByRole("heading", { name: "Thank you!" })).toBeVisible();
        expect(violations).toEqual([]);
      } finally {
        await respondent.close();
      }
    });

    await test.step("find the response", async () => {
      await page.goto(page.url().replace(/\/summary.*$/, "/responses"));
      const rows = page.getByRole("main").getByRole("table").getByRole("row").filter({ hasText: answer });
      await expect(rows).toHaveCount(1);
    });
    expect(policy).toEqual([]);
  });

  test("the SDK shows a protected app survey on a customer's page and sends the widget's token", async ({
    page,
    baseURL,
  }) => {
    test.skip(mode !== "pass", "the secret accepts the dummy token only in the pass run");
    const answer = `Answered in the widget ${Math.random().toString(36).slice(2, 8)}`;
    // A new account through the API, with the token the always-pass key issues, so its workspace shows no survey but this one.
    const email = uniqueEmail("turnstile-sdk");
    await post(page.request, baseURL!, "/api/app/auth/signup", {
      name: "Playwright User",
      email,
      password: PASSWORD,
      turnstileToken: DUMMY_TOKEN,
    });
    await post(page.request, baseURL!, "/api/app/auth/sign-in", { email, password: PASSWORD });
    const { workspaceId } = await finishOnboarding(page);

    await test.step("publish an app survey with spam protection, shown on every page view", async () => {
      await startSurveyFromScratch(page, workspaceId);
      await nameSurvey(page, `Protected app survey ${Date.now()}`);
      await page.getByRole("button", { name: "Settings", exact: true }).click();
      await page.getByText("Survey Type", { exact: true }).click();
      await page.getByRole("radio", { name: /Website & App Survey/ }).click();
      await page.getByText("Survey Trigger", { exact: true }).click();
      await page.getByRole("button", { name: "Add action" }).click();
      const modal = page.getByRole("dialog", { name: "Add action" });
      await modal.getByRole("button", { name: "Capture new action" }).click();
      await modal.getByText("Page View", { exact: true }).click();
      await modal.getByLabel("What did your user do?").fill(`Visit ${Date.now()}`);
      await modal.getByRole("button", { name: "Create action" }).click();
      await expect(modal).toBeHidden();
      // The card stays as it opened for the link survey this started as: open, unless something closed it.
      const spamSwitch = page.getByRole("switch", { name: /^Spam protection/ });
      if (!(await spamSwitch.isVisible())) await page.getByText("Response Options", { exact: true }).click();
      await spamSwitch.click();
      await expect(spamSwitch).toBeChecked();
      await publish(page);
    });

    const host = await test.step("serve a customer page carrying the SDK", async () => {
      server = createServer((_, res) => {
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(hostPage(baseURL!, workspaceId));
      });
      await new Promise<void>((resolve) => server!.listen(0, "127.0.0.1", resolve));
      return `http://127.0.0.1:${(server!.address() as AddressInfo).port}/`;
    });

    await test.step("answer it in the widget", async () => {
      const visitor = await page.context().browser()!.newContext();
      try {
        const site = await visitor.newPage();
        await site.goto(host);
        const widget = site.locator("#forma-modal-container");
        await expect(widget.getByRole("dialog")).toBeVisible();
        await expectTurnstileFrame(site);
        await expect(turnstileToken(site)).toHaveValue(DUMMY_TOKEN);
        await widget.getByRole("textbox", { name: DEFAULT_QUESTION }).fill(answer);
        const created = site.waitForResponse(
          (response) =>
            response.request().method() === "POST" &&
            /\/api\/v[12]\/client\/[^/]+\/responses$/.test(response.url())
        );
        await widget.getByRole("button", { name: /^(Next|Finish)$/ }).click();
        const response = await created;
        expect(response.request().postDataJSON().turnstileToken).toBe(DUMMY_TOKEN);
        expect(response.status()).toBe(200);
        await expect(widget.getByRole("heading", { name: "Thank you!" })).toBeVisible();
      } finally {
        await visitor.close();
      }
    });
  });

  test("a token Cloudflare refuses keeps the response out and tells the respondent", async ({
    page,
    baseURL,
  }) => {
    test.skip(mode !== "fail", "the secret refuses every token only in the fail run");
    // Sign-up refuses every token too, so the seeded owner signs in, which needs no token.
    const workspaceId = await signInSeededOwner(page, baseURL!);

    const surveyURL = await publishSpamProtectedSurvey(page, workspaceId);
    const { respondent, survey, response, violations } = await answerAsRespondent(
      page,
      surveyURL,
      "Refused by Cloudflare"
    );
    try {
      expect(response.request().postDataJSON().turnstileToken).toBe(DUMMY_TOKEN);
      expect(response.status()).toBe(400);
      expect(await response.json()).toMatchObject({
        message: "Turnstile verification failed",
        details: { code: "recaptcha_verification_failed" },
      });
      await expect(survey.getByText("We could not verify that you are human.")).toBeVisible();
      await expect(survey.getByRole("heading", { name: "Thank you!" })).toHaveCount(0);
      expect(violations).toEqual([]);
    } finally {
      await respondent.close();
    }
  });
});
