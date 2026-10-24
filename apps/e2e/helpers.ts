import { type APIRequestContext, type Page, expect } from "@playwright/test";

/** The password every account a spec creates signs in with; it meets the sign-up rules (mixed case, a number, 8+ characters). */
export const PASSWORD = "Playwright#2026";

/** An address no other spec, run or retry uses, so specs can share one instance. */
export const uniqueEmail = (prefix: string): string =>
  `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}@example.com`;

/** The headers every /api/app mutation needs: the SPA's origin and its CSRF header. */
const appHeaders = (baseURL: string) => ({ Origin: baseURL, "X-Requested-With": "XMLHttpRequest" });

/**
 * Creates an account and signs the page's browser context in, through the same /api/app endpoints the sign-up and login forms call. Specs about something other than signing up start here; auth.spec.ts covers the forms themselves.
 */
export const signUpAndSignIn = async (page: Page, baseURL: string, prefix: string): Promise<string> => {
  const email = uniqueEmail(prefix);
  await post(page.request, baseURL, "/api/app/auth/signup", {
    name: "Playwright User",
    email,
    password: PASSWORD,
  });
  await post(page.request, baseURL, "/api/app/auth/sign-in", { email, password: PASSWORD });
  return email;
};

/** POSTs to /api/app as the signed-in browser does and returns the answer's JSON. */
export const post = async <T = unknown>(
  request: APIRequestContext,
  baseURL: string,
  path: string,
  data: unknown
): Promise<T> => {
  const response = await request.post(path, { data, headers: appHeaders(baseURL) });
  expect(response.ok()).toBe(true);
  return (await response.json()) as T;
};

export interface OnboardedSurvey {
  workspaceId: string;
  surveyId: string;
}

export interface Onboarded extends OnboardedSurvey {
  organizationId: string;
}

/**
 * Finishes a new account's onboarding the way forma2's finishOnboarding did: the first-survey page, "Start from scratch", then "Save & Close", which publishes that survey as a link survey. Returns where it landed.
 */
export const finishOnboarding = async (page: Page): Promise<Onboarded> => {
  await page.goto("/");
  await page.waitForURL(/\/app\/organizations\/[^/]+\/workspaces\/new\/survey/);
  const organizationId = /\/organizations\/([^/]+)/.exec(page.url())![1];
  await page.getByRole("button", { name: /Start from scratch/ }).click();
  await page.waitForURL(/\/app\/workspaces\/[^/]+\/surveys\/[^/]+\/edit/);
  await page.getByRole("button", { name: "Save & Close" }).click();
  await page.waitForURL(/\/app\/workspaces\/[^/]+\/surveys\/[^/]+\/summary/);
  return { organizationId, ...surveyFromURL(page.url()) };
};

/** The workspace and survey of a /workspaces/{workspaceId}/surveys/{surveyId}/... URL. */
export const surveyFromURL = (url: string): OnboardedSurvey => {
  const match = /\/workspaces\/([^/]+)\/surveys\/([^/?]+)/.exec(url);
  if (!match) throw new Error(`not a survey URL: ${url}`);
  return { workspaceId: match[1], surveyId: match[2] };
};

/** Opens the workspace's survey list and starts a survey from scratch in the full editor (with Settings and Publish). */
export const startSurveyFromScratch = async (page: Page, workspaceId: string): Promise<void> => {
  await page.goto(`/app/workspaces/${workspaceId}/surveys`);
  await page.getByRole("button", { name: "New Survey" }).click();
  await page.getByRole("menuitem", { name: /Start from scratch/ }).click();
  await page.waitForURL(/\/app\/workspaces\/[^/]+\/surveys\/[^/]+\/edit/);
  await expect(page.getByRole("button", { name: "Publish", exact: true })).toBeVisible();
};

/** Names the survey open in the editor; the name field is the menu bar's first text box. The menu bar mounts again once the editor has loaded everything, which drops a name typed before that, so the name is typed until it stays. */
export const nameSurvey = async (page: Page, name: string): Promise<void> => {
  const field = page.getByRole("textbox").first();
  await expect(async () => {
    await field.fill(name);
    await expect(field).toHaveValue(name, { timeout: 1_000 });
  }).toPass();
};

/** Publishes the survey open in the editor and waits for its summary. */
export const publish = async (page: Page): Promise<OnboardedSurvey> => {
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await page.waitForURL(/\/app\/workspaces\/[^/]+\/surveys\/[^/]+\/summary/);
  return surveyFromURL(page.url());
};

/** The headline of the question a survey started from scratch begins with. */
export const DEFAULT_QUESTION = "What would you like to know?";
