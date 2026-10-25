import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import type { TSurveyBlock, TSurveyBlockLogic } from "@forma/types/surveys/blocks";
import { type TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { ConditionalLogic } from "./conditional-logic";

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

const question = {
  id: "q1",
  type: TSurveyElementTypeEnum.OpenText,
  headline: { default: "What should we build next?" },
  required: false,
  inputType: "text",
  charLimit: { enabled: false },
} as TSurveyElement;

/** A rule with a single condition ("q1 is submitted") that jumps to the ending. */
const rule = (id: string): TSurveyBlockLogic => ({
  id,
  conditions: {
    id: `${id}-group`,
    connector: "and",
    conditions: [
      { id: `${id}-condition`, leftOperand: { value: "q1", type: "element" }, operator: "isSubmitted" },
    ],
  },
  actions: [{ id: `${id}-action`, objective: "jumpToBlock", target: "ending1" }],
});

/** The logic section of a block, holding the block's rules in state as the editor does. */
const renderLogic = (rules: TSurveyBlockLogic[]) => {
  let latest = rules;
  const Section = () => {
    const [logic, setLogic] = useState(rules);
    const block: TSurveyBlock = { id: "block1", name: "Block 1", elements: [question], logic };
    const survey = {
      id: "survey1",
      name: "Feedback",
      type: "link",
      blocks: [block],
      endings: [{ id: "ending1", type: "endScreen", headline: { default: "Thanks" } }],
      hiddenFields: { enabled: true, fieldIds: [] },
      variables: [],
      languages: [],
    } as unknown as TSurvey;
    return (
      <ConditionalLogic
        localSurvey={survey}
        blockIdx={0}
        block={block}
        updateBlockLogic={(_blockIdx, next) => {
          latest = next;
          setLogic(next);
        }}
        updateBlockLogicFallback={() => {}}
      />
    );
  };
  render(
    <I18nextProvider i18n={i18n}>
      <Section />
    </I18nextProvider>
  );
  fireEvent.click(screen.getByRole("button", { name: i18n.t("workspace.surveys.edit.conditional_logic") }));
  return { rules: () => latest.map((item) => item.id) };
};

const ruleMenus = () =>
  screen.getAllByRole("button", { name: i18n.t("workspace.surveys.edit.logic.rule_options") });

const choose = (trigger: HTMLElement, item: string) => {
  fireEvent.keyDown(trigger, { key: "Enter" });
  fireEvent.click(within(screen.getByRole("menu")).getByRole("menuitem", { name: item }));
};

describe("ConditionalLogic", () => {
  test("a rule with a single condition has its own menu, and it removes the rule", () => {
    const logic = renderLogic([rule("rule1")]);

    expect(ruleMenus()).toHaveLength(1);
    choose(ruleMenus()[0]!, i18n.t("common.remove"));

    expect(logic.rules()).toEqual([]);
  });

  test("single-condition rules can be reordered and duplicated from the rule menu", () => {
    const logic = renderLogic([rule("rule1"), rule("rule2")]);

    choose(ruleMenus()[1]!, i18n.t("common.move_up"));
    expect(logic.rules()).toEqual(["rule2", "rule1"]);

    choose(ruleMenus()[0]!, i18n.t("common.duplicate"));
    expect(logic.rules()).toHaveLength(3);
    expect(logic.rules()[0]).toBe("rule2");
    expect(logic.rules()[2]).toBe("rule1");
  });

  test("the rule, condition and action menu buttons are named", () => {
    renderLogic([rule("rule1")]);

    for (const key of ["rule_options", "condition_options", "action_options"]) {
      expect(
        screen.getAllByRole("button", { name: i18n.t(`workspace.surveys.edit.logic.${key}`) })
      ).toHaveLength(1);
    }
  });

  test("a condition group's menu button is named and is not a button inside a button", () => {
    const grouped = rule("rule1");
    grouped.conditions.conditions = [
      {
        id: "group2",
        connector: "or",
        conditions: [
          { id: "c1", leftOperand: { value: "q1", type: "element" }, operator: "isSubmitted" },
          { id: "c2", leftOperand: { value: "q1", type: "element" }, operator: "isSkipped" },
        ],
      },
    ];
    renderLogic([grouped]);

    const groupMenu = screen.getByRole("button", {
      name: i18n.t("workspace.surveys.edit.logic.condition_group_options"),
    });
    expect(groupMenu.querySelector("button")).toBeNull();
    expect(groupMenu.parentElement?.closest("button")).toBeNull();
  });
});
