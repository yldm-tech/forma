import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import type { TSurveyElement } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { afterEach, beforeAll, expect, test } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { UpdateElementId } from "./update-element-id";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

const renderField = (status: TSurvey["status"], isDraft?: boolean) => {
  const element = {
    id: "deuz2df62r9uzoqxzqgf3bpg",
    type: "openText",
    headline: { default: "Your email?" },
    required: false,
    inputType: "email",
    charLimit: { enabled: false },
    ...(isDraft === undefined ? {} : { isDraft }),
  } as TSurveyElement;
  const survey = {
    status,
    blocks: [{ id: "b1", name: "Block", elements: [element] }],
    endings: [],
    hiddenFields: { enabled: true, fieldIds: [] },
  } as unknown as TSurvey;
  render(
    <I18nextProvider i18n={i18n}>
      <UpdateElementId localSurvey={survey} element={element} elementIdx={0} updateElement={() => {}} />
    </I18nextProvider>
  );
  const input = screen.getByRole<HTMLInputElement>("textbox", { name: "Question ID" });
  const help = document.getElementById(input.getAttribute("aria-describedby") ?? "");
  return { input, description: help?.textContent };
};

// Zapier, Make and n8n users map webhook answers by this id, so the field says where it shows up and that it locks on publish.
test("the question ID explains that webhooks and the API key answers by it and that it locks once published", () => {
  const { input, description } = renderField("draft");
  expect(description).toBe(
    "Webhooks and the API identify this question's answer by this ID; each webhook also lists it under fields with the question's text. Once the survey is published and this question is saved, it can't be changed."
  );
  expect(input.disabled).toBe(false);
});

test("a published survey's question ID is read-only and still explains why", () => {
  const { input, description } = renderField("inProgress");
  expect(input.disabled).toBe(true);
  expect(description).toMatch(
    /Once the survey is published and this question is saved, it can't be changed\./
  );
});

// A question added to a published survey stays editable until the survey is saved (isDraft is stripped on save), so the copy must not claim it is already locked.
test("a question just added to a published survey keeps an editable ID and the copy says it locks once saved", () => {
  const { input, description } = renderField("inProgress", true);
  expect(input.disabled).toBe(false);
  expect(description).toMatch(
    /Once the survey is published and this question is saved, it can't be changed\./
  );
});
