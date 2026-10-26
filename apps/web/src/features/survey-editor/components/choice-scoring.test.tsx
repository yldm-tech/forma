import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/constants";
import type { TSurveyMultipleChoiceElement } from "@forma/types/surveys/elements";
import { getScoreVariable } from "@forma/types/surveys/scoring";
import type { TSurvey, TSurveyVariable } from "@forma/types/surveys/types";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import type { TElementUpdate, TElementUpdater } from "../types/editor";
import { ChoiceScoreInput, ChoiceScoringToggle, parseChoiceScore } from "./choice-scoring";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
});

const element: TSurveyMultipleChoiceElement = {
  id: "q",
  type: TSurveyElementTypeEnum.MultipleChoiceSingle,
  headline: { default: "Capital of France?" },
  required: true,
  choices: [
    { id: "paris", label: { default: "Paris" } },
    { id: "lyon", label: { default: "Lyon" }, score: 2 },
  ],
};

const survey = (variables: TSurveyVariable[], el: TSurveyMultipleChoiceElement): TSurvey =>
  ({
    languages: [],
    variables,
    blocks: [{ id: "block", name: "Block 1", elements: [el] }],
    endings: [],
  }) as unknown as TSurvey;

/** The toggle over a survey that takes its updates, as the editor does: setLocalSurvey replaces the survey, updateElement applies its updater over the latest one. */
const renderToggle = (variables: TSurveyVariable[], initial: TSurveyMultipleChoiceElement) => {
  const state = { current: survey(variables, initial) };
  const Harness = () => {
    const [current, setCurrent] = useState(state.current);
    const el = current.blocks[0]!.elements[0] as TSurveyMultipleChoiceElement;
    const set = (next: TSurvey) => {
      state.current = next;
      setCurrent(next);
    };
    return (
      <ChoiceScoringToggle
        localSurvey={current}
        element={el}
        elementIdx={0}
        setLocalSurvey={set}
        updateElement={(_idx, update: TElementUpdate | TElementUpdater) => {
          const latest = state.current.blocks[0]!.elements[0]!;
          const attributes = typeof update === "function" ? update(latest) : update;
          set({
            ...state.current,
            blocks: [
              {
                ...state.current.blocks[0]!,
                elements: [{ ...latest, ...attributes } as TSurveyMultipleChoiceElement],
              },
            ],
          });
        }}
      />
    );
  };
  render(
    <I18nextProvider i18n={i18n}>
      <Harness />
    </I18nextProvider>
  );
  return state;
};

const choicesOf = (s: TSurvey) => (s.blocks[0]!.elements[0] as TSurveyMultipleChoiceElement).choices;

describe("ChoiceScoringToggle", () => {
  test("turning scoring on scores every choice and adds the total score variable", () => {
    const unscored = { ...element, choices: element.choices.map(({ id, label }) => ({ id, label })) };
    const state = renderToggle([{ id: "bonus", name: "score", type: "number", value: 0 }], unscored);
    const toggle = screen.getByRole("switch", { name: /Score answers/ });
    expect(toggle.getAttribute("aria-checked")).toBe("false");

    fireEvent.click(toggle);

    expect(choicesOf(state.current).map((c) => c.score)).toEqual([0, 0]);
    const added = getScoreVariable(state.current.variables);
    // "score" is taken by another variable, so the new one gets the next free name.
    expect(added).toMatchObject({ name: "score_2", type: "number", value: 0, isScore: true });
    expect(state.current.variables).toHaveLength(2);
    expect(
      screen.getByText("The points add up in the variable score_2, which logic, recall and quotas can use.")
    ).toBeTruthy();
  });

  test("turning scoring off removes the scores and keeps the variable", () => {
    const scoreVariable: TSurveyVariable = {
      id: "total",
      name: "score",
      type: "number",
      value: 0,
      isScore: true,
    };
    const state = renderToggle([scoreVariable], element);
    fireEvent.click(screen.getByRole("switch", { name: /Score answers/ }));
    expect(choicesOf(state.current).every((c) => !("score" in c))).toBe(true);
    expect(state.current.variables).toEqual([scoreVariable]);
  });

  test("a scored element without a total score variable says so and offers to add one", () => {
    const state = renderToggle([], element);
    expect(
      screen.getByText("No variable holds the total score yet, so these points are not added up.")
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add total score variable" }));
    expect(getScoreVariable(state.current.variables)?.name).toBe("score");
    expect(choicesOf(state.current).map((c) => c.score)).toEqual([undefined, 2]);
  });
});

describe("ChoiceScoreInput", () => {
  test("takes a number within the bound, and nothing from an empty field", () => {
    expect(parseChoiceScore("2.5")).toBe(2.5);
    expect(parseChoiceScore("-3")).toBe(-3);
    expect(parseChoiceScore(" ")).toBeNull();
    expect(parseChoiceScore("1e7")).toBeNull();
    expect(parseChoiceScore("abc")).toBeNull();
  });

  test("a cleared field keeps the saved score and shows it again when left", () => {
    const saved: number[] = [];
    render(<ChoiceScoreInput score={3} label="Score of option 1" onChange={(score) => saved.push(score)} />);
    const field = screen.getByLabelText("Score of option 1") as HTMLInputElement;
    fireEvent.change(field, { target: { value: "" } });
    expect(field.value).toBe("");
    expect(saved).toEqual([]);
    fireEvent.blur(field);
    expect(field.value).toBe("3");
    fireEvent.change(field, { target: { value: "-1.5" } });
    expect(saved).toEqual([-1.5]);
  });
});
