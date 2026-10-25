import { describe, expect, test } from "vite-plus/test";
import type { TActionClassNoCodeConfig } from "@forma/types/action-classes";
import type { TSurvey } from "@forma/types/surveys/types";
import { resetsCooldownOnEveryPageView } from "./cooldown-reset";

const trigger = (noCodeConfig: TActionClassNoCodeConfig | null) => ({
  actionClass: {
    id: "a1",
    name: "Page view",
    type: noCodeConfig ? "noCode" : "code",
    key: null,
    noCodeConfig,
  },
});
const everyPage = trigger({ type: "pageView", urlFilters: [] });

// The seeded "Website Pulse": ignores the cooldown, shows until answered, on every page view.
const pulse = (overrides: Partial<TSurvey> = {}): TSurvey =>
  ({
    type: "app",
    recontactDays: 0,
    displayOption: "displayMultiple",
    triggers: [everyPage],
    ...overrides,
  }) as unknown as TSurvey;

describe("resetsCooldownOnEveryPageView", () => {
  test("an ignore-cooldown survey shown again and again on every page view keeps restarting the workspace cooldown", () => {
    expect(resetsCooldownOnEveryPageView(pulse())).toBe(true);
    expect(resetsCooldownOnEveryPageView(pulse({ displayOption: "respondMultiple" }))).toBe(true);
    expect(resetsCooldownOnEveryPageView(pulse({ displayOption: "displaySome", displayLimit: 3 }))).toBe(
      true
    );
  });

  test.each([
    ["it uses the workspace cooldown", { recontactDays: null }],
    ["it has a cooldown of its own", { recontactDays: 7 }],
    ["it shows only once", { displayOption: "displayOnce" }],
    [
      "its page view is limited to some pages",
      { triggers: [trigger({ type: "pageView", urlFilters: [{ value: "/pricing", rule: "contains" }] })] },
    ],
    ["it is triggered by a code action", { triggers: [trigger(null)] }],
    [
      "it is triggered by a click",
      { triggers: [trigger({ type: "click", urlFilters: [], elementSelector: { cssSelector: "#buy" } })] },
    ],
    ["it is a link survey", { type: "link" }],
  ] as [string, Partial<TSurvey>][])("not when %s", (_, overrides) => {
    expect(resetsCooldownOnEveryPageView(pulse(overrides))).toBe(false);
  });
});
