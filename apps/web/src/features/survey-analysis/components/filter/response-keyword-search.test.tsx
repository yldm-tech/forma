import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import { type Context, type Dispatch, type SetStateAction, useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import type { SelectedFilterValue } from "../../lib/filter-types";
import * as filterContext from "./response-filter-context";
import { ResponseKeywordSearch, hasSearchableAnswers } from "./response-keyword-search";

// The shared filter comes from a plain context the harness below provides, in place of the provider, which needs the router.
vi.mock("./response-filter-context", async () => {
  const { createContext, useContext } = await import("react");
  const TestFilterContext = createContext<unknown>(undefined);
  return { TestFilterContext, useResponseFilter: () => useContext(TestFilterContext) };
});

const { TestFilterContext } = filterContext as unknown as {
  TestFilterContext: Context<
    | {
        selectedFilter: SelectedFilterValue;
        setSelectedFilter: Dispatch<SetStateAction<SelectedFilterValue>>;
      }
    | undefined
  >;
};

let i18n: I18n;

/** The search box over a filter held in state, with the applied keyword shown and a reset standing in for a chip or Clear all. */
const Harness = () => {
  const [selectedFilter, setSelectedFilter] = useState<SelectedFilterValue>({
    filter: [],
    responseStatus: "all",
  });
  return (
    <TestFilterContext.Provider value={{ selectedFilter, setSelectedFilter }}>
      <ResponseKeywordSearch />
      <output data-testid="applied">{selectedFilter.keyword ?? "(none)"}</output>
      <button type="button" onClick={() => setSelectedFilter({ filter: [], responseStatus: "all" })}>
        reset
      </button>
    </TestFilterContext.Provider>
  );
};

const applied = () => screen.getByTestId("applied").textContent;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

beforeEach(() => {
  vi.useFakeTimers();
  render(
    <I18nextProvider i18n={i18n}>
      <Harness />
    </I18nextProvider>
  );
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const box = () => screen.getByRole("searchbox", { name: i18n.t("workspace.analysis.search.placeholder") });

describe("ResponseKeywordSearch", () => {
  test("applies the typed keyword, trimmed, once typing pauses", () => {
    fireEvent.change(box(), { target: { value: "onboard" } });
    act(() => {
      vi.advanceTimersByTime(100);
    });
    fireEvent.change(box(), { target: { value: " onboarding " } });
    expect(applied()).toBe("(none)");
    act(() => {
      vi.advanceTimersByTime(400);
    });
    expect(applied()).toBe("onboarding");
    // The trailing space the user typed stays, so typing can go on.
    expect(box()).toHaveProperty("value", " onboarding ");
  });

  test("Enter applies at once", () => {
    fireEvent.change(box(), { target: { value: "pricing" } });
    fireEvent.keyDown(box(), { key: "Enter" });
    expect(applied()).toBe("pricing");
  });

  test("a keyword cleared from outside, by its chip or Clear all, empties the box", () => {
    fireEvent.change(box(), { target: { value: "pricing" } });
    fireEvent.keyDown(box(), { key: "Enter" });
    fireEvent.click(screen.getByRole("button", { name: "reset" }));
    expect(box()).toHaveProperty("value", "");
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(applied()).toBe("(none)");
  });

  test("the clear button removes the keyword", () => {
    fireEvent.change(box(), { target: { value: "pricing" } });
    fireEvent.keyDown(box(), { key: "Enter" });
    fireEvent.click(screen.getByRole("button", { name: i18n.t("workspace.analysis.search.clear") }));
    expect(applied()).toBe("(none)");
    expect(box()).toHaveProperty("value", "");
  });
});

describe("hasSearchableAnswers", () => {
  const surveyOf = (type: TSurveyElementTypeEnum) =>
    ({ blocks: [{ id: "b1", name: "B", elements: [{ id: "q1", type }] }] }) as unknown as TSurvey;

  test("is true for open text, address and contact info answers only", () => {
    expect(hasSearchableAnswers(surveyOf(TSurveyElementTypeEnum.OpenText))).toBe(true);
    expect(hasSearchableAnswers(surveyOf(TSurveyElementTypeEnum.Address))).toBe(true);
    expect(hasSearchableAnswers(surveyOf(TSurveyElementTypeEnum.ContactInfo))).toBe(true);
    expect(hasSearchableAnswers(surveyOf(TSurveyElementTypeEnum.NPS))).toBe(false);
  });
});
