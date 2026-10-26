import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import type { TSurvey, TSurveyEndScreenCard } from "@forma/types/surveys/types";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { EndScreenForm } from "./end-screen-form";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
});

const ending: TSurveyEndScreenCard = {
  id: "end",
  type: "endScreen",
  headline: { default: "Thank you!" },
  subheader: { default: "We appreciate your feedback." },
};

const renderForm = () => {
  const updates: Partial<TSurveyEndScreenCard>[] = [];
  render(
    <I18nextProvider i18n={i18n}>
      <EndScreenForm
        localSurvey={{ languages: [], blocks: [], endings: [ending] } as unknown as TSurvey}
        endingCardIndex={0}
        isInvalid={false}
        selectedLanguageCode="default"
        updateSurvey={(input) => updates.push(input as Partial<TSurveyEndScreenCard>)}
        endingCard={ending}
        locale="en-US"
      />
    </I18nextProvider>
  );
  return updates;
};

describe("EndScreenForm", () => {
  test("turning the button on leaves its label and link for the author to write, with no link to Forma", () => {
    const updates = renderForm();
    fireEvent.click(screen.getByRole("switch", { name: /Show button/ }));
    expect(updates).toEqual([{ buttonLabel: { default: "" }, buttonLink: "" }]);
  });
});
