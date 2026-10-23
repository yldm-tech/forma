import { expect, test } from "@playwright/test";
import { DEFAULT_QUESTION, PASSWORD, uniqueEmail } from "./helpers";

// forma2's signup.spec.ts and onboarding.spec.ts: a visitor signs up with email and password, logs in, and finishes onboarding by starting their first survey from scratch.
test("signs up, logs in and finishes onboarding with a survey from scratch", async ({ page }) => {
  const email = uniqueEmail("signup");

  await test.step("sign up", async () => {
    await page.goto("/auth/signup");
    await page.getByRole("button", { name: "Continue with Email" }).click();
    const submit = page.getByRole("button", { name: "Continue with Email" });
    await page.getByRole("textbox", { name: "Full name" }).fill("Playwright User");
    // Mixed case on purpose: the account is stored and shown under the lower-cased address.
    await page.getByRole("textbox", { name: "Email" }).fill(email.replace("signup", "SignUp"));
    await page.getByRole("textbox", { name: "Password" }).fill("short");
    await expect(submit).toBeDisabled();
    await page.getByRole("textbox", { name: "Password" }).fill(PASSWORD);
    await submit.click();
    await page.waitForURL(/\/auth\/signup-without-verification-success/);
    await expect(page.getByText(email)).toBeVisible();
  });

  await test.step("log in", async () => {
    await page.getByRole("link", { name: "Log in" }).click();
    await page.getByRole("button", { name: "Log in with Email" }).click();
    await page.getByRole("textbox", { name: "Email" }).fill(email);
    await page.getByRole("textbox", { name: "Password" }).fill(PASSWORD);
    await page.getByRole("button", { name: "Log in with Email" }).click();
    await page.waitForURL(/\/organizations\/[^/]+\/workspaces\/new\/survey/);
    await expect(page.getByRole("heading", { name: "Create your first survey" })).toBeVisible();
  });

  await test.step("start the first survey from scratch", async () => {
    await page.getByRole("button", { name: /Start from scratch/ }).click();
    await page.waitForURL(/\/surveys\/[^/]+\/edit/);
    await expect(page.getByRole("main").getByRole("heading", { name: DEFAULT_QUESTION })).toBeVisible();
    await page.getByRole("button", { name: "Save & Close" }).click();
    await page.waitForURL(/\/workspaces\/[^/]+\/surveys\/[^/]+\/summary/);
    const share = page.getByRole("dialog", { name: "Share survey" });
    await expect(share.getByRole("textbox", { name: "URL" })).toHaveValue(/\/s\/[^/]+$/);
    await page.keyboard.press("Escape");
    await expect(share).toBeHidden();
  });

  await test.step("find it live in the survey list", async () => {
    await page.getByRole("link", { name: "Surveys" }).click();
    const row = page.getByRole("main").getByRole("link", { name: /^Start from scratch/ });
    await expect(row).toBeVisible();
    await expect(row).toContainText("In Progress");
    await expect(row).toContainText("Link");
  });
});

test("refuses a wrong password", async ({ page }) => {
  await page.goto("/auth/login");
  await page.getByRole("button", { name: "Log in with Email" }).click();
  await page.getByRole("textbox", { name: "Email" }).fill(uniqueEmail("nobody"));
  await page.getByRole("textbox", { name: "Password" }).fill(PASSWORD);
  await page.getByRole("button", { name: "Log in with Email" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page).toHaveURL(/\/auth\/login/);
});
