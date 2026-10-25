/**
 * @vitest-environment jsdom
 */
import type { SurveyContainerProps } from "@forma/types/forma-surveys";
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { SurveyInline } from "./survey-inline";

const APP_URL = "https://forma.example";

const props = {
  appUrl: APP_URL,
  survey: { id: "survey-1", type: "link", languages: [] },
  styling: {},
  isBrandingEnabled: false,
  languageCode: "default",
} as unknown as Omit<SurveyContainerProps, "containerId">;

afterEach(() => {
  cleanup();
  document.head.innerHTML = "";
  delete (globalThis as { formaSurveys?: unknown }).formaSurveys;
});

describe("SurveyInline", () => {
  // The server renders the runtime's script tag with a URL pinned to the build (apps/server/internal/linksurvey); a second tag with the plain URL would download the runtime twice.
  test("waits for the runtime script the server rendered, pinned to the build, instead of adding another", async () => {
    const rendered = document.createElement("script");
    rendered.async = true;
    rendered.src = `${APP_URL}/js/surveys.umd.js?hash=0123456789abcdef`;
    document.head.appendChild(rendered);

    render(<SurveyInline {...props} />);
    const renderSurvey = vi.fn();
    (globalThis as { formaSurveys?: unknown }).formaSurveys = { renderSurvey };
    rendered.dispatchEvent(new Event("load"));

    await waitFor(() => expect(renderSurvey).toHaveBeenCalledTimes(1));
    expect(document.querySelectorAll('script[src*="/js/surveys.umd.js"]')).toHaveLength(1);
  });
});
