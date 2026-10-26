import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/constants";
import type { TSurveySliderElement } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { SliderElementForm } from "./slider-element-form";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
});

const initial: TSurveySliderElement = {
  id: "s",
  type: TSurveyElementTypeEnum.Slider,
  headline: { default: "How many hours?" },
  required: true,
  min: 0,
  max: 10,
  step: 1,
};

/** The form over an element that takes the form's updates, as the survey editor does. */
const Harness = ({ saved }: { saved: TSurveySliderElement[] }) => {
  const [element, setElement] = useState(initial);
  return (
    <SliderElementForm
      localSurvey={
        {
          languages: [],
          blocks: [{ id: "block", name: "Block 1", elements: [element] }],
          endings: [],
        } as unknown as TSurvey
      }
      element={element}
      elementIdx={0}
      updateElement={(_idx, attributes) => {
        const next = { ...element, ...attributes } as TSurveySliderElement;
        saved.push(next);
        setElement(next);
      }}
      isInvalid={false}
      locale="en-US"
    />
  );
};

const renderForm = () => {
  const saved: TSurveySliderElement[] = [];
  render(
    <I18nextProvider i18n={i18n}>
      <Harness saved={saved} />
    </I18nextProvider>
  );
  return { saved, step: screen.getByLabelText("Step") as HTMLInputElement };
};

describe("SliderElementForm", () => {
  test("a step that does not fit the range is saved and its issue shown", () => {
    const { saved, step } = renderForm();
    fireEvent.change(step, { target: { value: "3" } });
    expect(saved.at(-1)?.step).toBe(3);
    expect(screen.getByRole("alert").textContent).toBe(
      "The step must fit a whole number of times between the minimum and the maximum."
    );
  });

  test("a cleared box asks for a number instead of talking about the number it no longer shows", () => {
    const { saved, step } = renderForm();
    fireEvent.change(step, { target: { value: "3" } });
    fireEvent.change(step, { target: { value: "" } });
    expect(saved.at(-1)?.step).toBe(3);
    expect(screen.getByRole("alert").textContent).toBe(
      "Enter a number for the minimum, the maximum and the step."
    );
    expect(step.getAttribute("aria-invalid")).toBe("true");
  });

  test("leaving a cleared box puts back the number that is saved", () => {
    const { saved, step } = renderForm();
    fireEvent.change(step, { target: { value: "2" } });
    fireEvent.change(step, { target: { value: "" } });
    fireEvent.blur(step);
    expect(step.value).toBe("2");
    expect(saved.at(-1)?.step).toBe(2);
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
