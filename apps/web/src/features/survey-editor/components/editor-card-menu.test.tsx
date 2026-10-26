import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { type TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { TooltipProvider } from "../../../components/ui/tooltip";
import { EditorCardMenu } from "./editor-card-menu";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterEach(cleanup);

const scoredChoice = {
  id: "q1",
  type: TSurveyElementTypeEnum.MultipleChoiceSingle,
  headline: { default: "Capital of France?" },
  required: true,
  choices: [
    { id: "paris", label: { default: "Paris" }, score: 2 },
    { id: "lyon", label: { default: "Lyon" }, score: 0 },
    { id: "nice", label: { default: "Nice" } },
  ],
} as TSurveyElement;

/** Changes the card's type through its menu and returns what the menu asked the editor to update. */
const changeType = (card: TSurveyElement, typeLabel: string) => {
  const updateCard = vi.fn();
  const survey = {
    id: "survey1",
    type: "link",
    blocks: [{ id: "block1", name: "Block 1", elements: [card, { ...card, id: "q2" }] }],
    endings: [],
  } as unknown as TSurvey;
  render(
    <I18nextProvider i18n={i18n}>
      <TooltipProvider>
        <EditorCardMenu
          survey={survey}
          cardIdx={0}
          lastCard={false}
          duplicateCard={() => {}}
          deleteCard={() => {}}
          moveCard={() => {}}
          card={card}
          updateCard={updateCard}
          cardType="element"
        />
      </TooltipProvider>
    </I18nextProvider>
  );
  fireEvent.keyDown(screen.getByRole("button", { name: i18n.t("workspace.surveys.edit.more_actions") }), {
    key: "Enter",
  });
  fireEvent.keyDown(
    within(screen.getByRole("menu")).getByRole("menuitem", {
      name: i18n.t("workspace.surveys.edit.change_question_type"),
    }),
    { key: "ArrowRight" }
  );
  const typeMenu = screen.getAllByRole("menu").at(-1)!;
  fireEvent.click(within(typeMenu).getByRole("menuitem", { name: typeLabel }));
  expect(updateCard).toHaveBeenCalledTimes(1);
  return updateCard.mock.calls[0]![1] as { type: TSurveyElementTypeEnum; choices: unknown[] };
};

describe("EditorCardMenu change question type", () => {
  test("drops the choices' scores when a scored question becomes a ranking, which is not scored", () => {
    const update = changeType(scoredChoice, i18n.t("templates.ranking"));

    expect(update.type).toBe(TSurveyElementTypeEnum.Ranking);
    expect(update.choices).toEqual([
      { id: "paris", label: { default: "Paris" } },
      { id: "lyon", label: { default: "Lyon" } },
      { id: "nice", label: { default: "Nice" } },
    ]);
  });

  test("keeps the choices' scores between single and multiple choice, which are both scored", () => {
    const update = changeType(scoredChoice, i18n.t("templates.multi_select"));

    expect(update.type).toBe(TSurveyElementTypeEnum.MultipleChoiceMulti);
    expect(update.choices).toEqual(
      (scoredChoice as Extract<TSurveyElement, { type: TSurveyElementTypeEnum.MultipleChoiceSingle }>).choices
    );
  });
});
