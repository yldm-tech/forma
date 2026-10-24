import { expect, test } from "@playwright/test";
import { DEFAULT_QUESTION, PASSWORD, uniqueEmail } from "./helpers";

// forma2's signup.spec.ts and onboarding.spec.ts: a visitor signs up with email and password, logs in, and finishes onboarding by starting their first survey from scratch.
test("signs up, logs in and finishes onboarding with a survey from scratch", async ({ page }) => {
  const email = uniqueEmail("signup");

  await test.step("sign up", async () => {
    await page.goto("/app/auth/signup");
    await page.getByRole("button", { name: "Continue with Email" }).click();
    const submit = page.getByRole("button", { name: "Continue with Email" });
    await page.getByRole("textbox", { name: "Full name" }).fill("Playwright User");
    // Mixed case on purpose: the account is stored and shown under the lower-cased address.
    await page.getByRole("textbox", { name: "Email" }).fill(email.replace("signup", "SignUp"));
    await page.getByRole("textbox", { name: "Password" }).fill("short");
    await expect(submit).toBeDisabled();
    await page.getByRole("textbox", { name: "Password" }).fill(PASSWORD);
    await submit.click();
    await page.waitForURL(/\/app\/auth\/signup-without-verification-success/);
    await expect(page.getByText(email)).toBeVisible();
  });

  await test.step("log in", async () => {
    await page.getByRole("link", { name: "Log in" }).click();
    await page.getByRole("button", { name: "Log in with Email" }).click();
    await page.getByRole("textbox", { name: "Email" }).fill(email);
    await page.getByRole("textbox", { name: "Password" }).fill(PASSWORD);
    await page.getByRole("button", { name: "Log in with Email" }).click();
    await page.waitForURL(/\/app\/organizations\/[^/]+\/workspaces\/new\/survey/);
    await expect(page.getByRole("heading", { name: "Create your first survey" })).toBeVisible();
  });

  await test.step("start the first survey from scratch", async () => {
    await page.getByRole("button", { name: /Start from scratch/ }).click();
    await page.waitForURL(/\/app\/workspaces\/[^/]+\/surveys\/[^/]+\/edit/);
    await expect(page.getByRole("main").getByRole("heading", { name: DEFAULT_QUESTION })).toBeVisible();
    await page.getByRole("button", { name: "Save & Close" }).click();
    await page.waitForURL(/\/app\/workspaces\/[^/]+\/surveys\/[^/]+\/summary/);
    const share = page.getByRole("dialog", { name: "Share survey" });
    await expect(share.getByRole("textbox", { name: "URL" })).toHaveValue(/\/s\/[^/]+$/);
    await page.keyboard.press("Escape");
    await expect(share).toBeHidden();
  });

  await test.step("find it live in the survey list", async () => {
    await page.getByRole("link", { name: "Surveys" }).click();
    // The survey's name is the row's link; the row holds the status and type cells beside it.
    const row = page
      .getByRole("main")
      .getByRole("row")
      .filter({ has: page.getByRole("link", { name: "Start from scratch", exact: true }) });
    await expect(row).toBeVisible();
    await expect(row).toContainText("In Progress");
    await expect(row).toContainText("Link");
  });
});

test("refuses a wrong password", async ({ page }) => {
  await page.goto("/app/auth/login");
  await page.getByRole("button", { name: "Log in with Email" }).click();
  await page.getByRole("textbox", { name: "Email" }).fill(uniqueEmail("nobody"));
  await page.getByRole("textbox", { name: "Password" }).fill(PASSWORD);
  await page.getByRole("button", { name: "Log in with Email" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page).toHaveURL(/\/app\/auth\/login/);
});

// The admin pages moved under /app; a link to their old root address (an email, a bookmark) lands on the same page there, its query kept, and the root leads to the app.
test("sends old admin addresses and the root to the app", async ({ page }) => {
  await page.goto("/auth/login?email=old%40example.com");
  await expect(page).toHaveURL(/\/app\/auth\/login\?email=old/);
  await expect(page.getByRole("button", { name: "Log in with Email" })).toBeVisible();

  await page.goto("/");
  await expect(page).toHaveURL(/\/app\/auth\/login/);
});
