import { cleanup, fireEvent, render, screen } from "@testing-library/preact";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/constants";
import type { TSurveySliderElement } from "@forma/types/surveys/elements";
import { SliderElement } from "./slider-element";

// vitestSetup's useTranslation returns the bare key, so the assertions read which text was chosen.

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

const element: TSurveySliderElement = {
  id: "s",
  type: TSurveyElementTypeEnum.Slider,
  headline: { default: "How many hours?" },
  required: true,
  min: 0,
  max: 10,
  step: 0.5,
};

const renderSlider = (value?: number, onChange = vi.fn()) => {
  render(
    <SliderElement
      element={element}
      value={value}
      onChange={onChange}
      languageCode="default"
      ttc={{}}
      setTtc={vi.fn()}
      currentElementId="s"
    />
  );
  return { onChange, input: screen.getByRole("slider") as HTMLInputElement };
};

describe("SliderElement", () => {
  test("an untouched slider rests in the middle and says it is not answered", () => {
    const { input, onChange } = renderSlider();
    expect(input.value).toBe("5");
    expect(input.getAttribute("aria-valuetext")).toBe("slider.not_answered");
    expect(onChange).not.toHaveBeenCalled();
  });

  test("moving it reports the value on the scale", () => {
    const { input, onChange } = renderSlider();
    fireEvent.input(input, { target: { value: "7.5" } });
    expect(onChange).toHaveBeenLastCalledWith({ s: 7.5 });
  });

  test("pressing it where it rests answers with that value", () => {
    const { input, onChange } = renderSlider();
    fireEvent.click(input);
    expect(onChange).toHaveBeenLastCalledWith({ s: 5 });
  });

  test("an answered slider announces its value", () => {
    const { input } = renderSlider(2.5);
    expect(input.value).toBe("2.5");
    expect(input.getAttribute("aria-valuetext")).toBe("2.5");
  });
});
