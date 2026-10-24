import AxeBuilder from "@axe-core/playwright";
import { type Page, expect, test } from "@playwright/test";
import { DEFAULT_QUESTION, finishOnboarding, signUpAndSignIn } from "./helpers";

// forma2's survey-accessibility.spec.ts and the signed-out checks of its signup.spec.ts, on the three surfaces this suite gates: the login page, the survey editor and a public survey page, each at a desktop and a phone viewport. Only WCAG 2.0, 2.1 and 2.2 A and AA rules fail; axe tags each rule with exactly one of these, so all six are listed.
const WCAG_AA_TAGS = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22a", "wcag22aa"];

const VIEWPORTS = {
  desktop: { width: 1280, height: 800 },
  mobile: { width: 390, height: 844 },
} as const;

type Viewport = keyof typeof VIEWPORTS;
type Surface = "login" | "editor" | "survey";

/**
 * Violations known on a surface, by axe rule id, recorded rather than hidden as forma2's allowlist did. The list may only shrink: a rule not listed fails, and so does a listed rule that no longer fires, so a fix has to delete its entry. Rules, not nodes: axe names nodes by generated class names, which change with any styling edit.
 */
const KNOWN_VIOLATIONS: Record<Surface, Record<Viewport, string[]>> = {
  login: { desktop: [], mobile: [] },
  // The editor carried over from forma2 still has ARIA attributes on elements that do not take them. forma2 never ran axe on its editor.
  editor: {
    desktop: ["aria-allowed-attr", "color-contrast"],
    // The low-contrast workspace name in the menu bar is not shown below the md breakpoint.
    mobile: ["aria-allowed-attr"],
  },
  survey: { desktop: [], mobile: [] },
};

/** The WCAG A and AA violations on the page as it is now, one line each, so a failure names the rule and the nodes. */
const scan = async (page: Page) => {
  const results = await new AxeBuilder({ page }).withTags(WCAG_AA_TAGS).analyze();
  return results.violations.map((v) => ({
    rule: v.id,
    line: `${v.id} (${v.impact ?? "n/a"}): ${v.help} at ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`,
  }));
};

/** Fails on a violation the surface's entry does not record, and on a recorded rule that did not fire. */
const expectOnlyKnownViolations = async (page: Page, surface: Surface, viewport: Viewport): Promise<void> => {
  const known = KNOWN_VIOLATIONS[surface][viewport];
  const found = await scan(page);
  expect(found.filter((v) => !known.includes(v.rule)).map((v) => v.line)).toEqual([]);
  const fixed = known.filter((rule) => !found.some((v) => v.rule === rule));
  expect(
    fixed.map(
      (rule) => `${rule} no longer fires on the ${surface} at ${viewport}: delete it from KNOWN_VIOLATIONS`
    )
  ).toEqual([]);
};

for (const viewport of Object.keys(VIEWPORTS) as Viewport[]) {
  test.describe(`at a ${viewport} viewport`, () => {
    test.use({ viewport: VIEWPORTS[viewport] });

    test("the login page meets WCAG AA", async ({ page }) => {
      await page.goto("/app/auth/login");
      await expect(page.getByRole("heading", { name: "Log in to your account" })).toBeVisible();
      await expectOnlyKnownViolations(page, "login", viewport);
      // The email form only exists once it is opened.
      await page.getByRole("button", { name: "Log in with Email" }).click();
      await expect(page.getByRole("textbox", { name: "Email" })).toBeVisible();
      await expectOnlyKnownViolations(page, "login", viewport);
    });

    test("the survey editor meets WCAG AA", async ({ page, baseURL }) => {
      // Onboarding at a desktop width: the scan, not the setup, is what this test measures.
      await page.setViewportSize(VIEWPORTS.desktop);
      await signUpAndSignIn(page, baseURL!, `a11y-editor-${viewport}`);
      const { workspaceId, surveyId } = await finishOnboarding(page);

      await page.setViewportSize(VIEWPORTS[viewport]);
      await page.goto(`/app/workspaces/${workspaceId}/surveys/${surveyId}/edit`);
      await expect(page.getByRole("main").getByRole("heading", { name: DEFAULT_QUESTION })).toBeVisible();
      await expectOnlyKnownViolations(page, "editor", viewport);
    });

    test("a public survey page meets WCAG AA", async ({ page, browser, baseURL }) => {
      await page.setViewportSize(VIEWPORTS.desktop);
      await signUpAndSignIn(page, baseURL!, `a11y-survey-${viewport}`);
      const { surveyId } = await finishOnboarding(page);

      // The respondent is signed out.
      const respondent = await browser.newContext({ viewport: VIEWPORTS[viewport] });
      try {
        const survey = await respondent.newPage();
        await survey.goto(`/s/${surveyId}`);
        await expect(survey.getByRole("textbox", { name: DEFAULT_QUESTION })).toBeVisible();
        await expectOnlyKnownViolations(survey, "survey", viewport);
        // The ending card, after an answer.
        await survey.getByRole("textbox", { name: DEFAULT_QUESTION }).fill("Accessible enough");
        await survey.getByRole("button", { name: "Next" }).click();
        await expect(survey.getByRole("heading", { name: "Thank you!" })).toBeVisible();
        await expectOnlyKnownViolations(survey, "survey", viewport);
      } finally {
        await respondent.close();
      }
    });
  });
}
