import { cleanup, render } from "@testing-library/preact";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/constants";
import type { TSurveyCalElement } from "@forma/types/surveys/elements";
import { CalElement } from "./cal-element";

// The scheduler itself is Cal.com's cross-origin iframe; only the header around it is under test.
vi.mock("@/components/general/cal-embed", () => ({
  CalEmbed: () => <div data-testid="cal-embed" />,
}));

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

const element: TSurveyCalElement = {
  id: "cal",
  type: TSurveyElementTypeEnum.Cal,
  headline: { default: "Book a call" },
  subheader: { default: "Pick a time that suits you" },
  required: true,
  calUserName: "forma",
};

const renderCal = () =>
  render(
    <CalElement
      element={element}
      value=""
      onChange={vi.fn()}
      languageCode="default"
      ttc={{}}
      setTtc={vi.fn()}
      currentElementId="cal"
    />
  );

describe("CalElement", () => {
  // Every other question renders its prompt with survey-ui's ElementHeader, which the theme's headline size, weight and color apply to; the old Headline was bold whatever the theme said.
  test("renders its prompt with the same header as every other question", () => {
    const { container } = renderCal();
    const heading = container.querySelector('[data-slot="element-headline"]');
    expect(heading?.tagName).toBe("H2");
    expect(heading?.textContent).toBe("Book a call");
    expect(heading?.querySelector('[data-variant="headline"]')).not.toBeNull();
    expect(container.querySelector('[data-variant="description"]')?.textContent).toBe(
      "Pick a time that suits you"
    );
    expect(container.querySelector('[data-testid="fb__surveys__headline-text-test"]')).toBeNull();
    expect(container.querySelector(".font-semibold")).toBeNull();
  });

  // vitestSetup's useTranslation returns the bare key.
  test("marks a required booking with the required label", () => {
    const { container } = renderCal();
    expect(container.querySelector(".label-card")?.textContent).toBe("common.required");
  });
});
