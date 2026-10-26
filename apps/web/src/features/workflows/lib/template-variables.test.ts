import type { TFunction } from "i18next";
import { expect, test } from "vite-plus/test";
import { buildWorkflowTemplateVariables, insertAtSelection, toTemplateToken } from "./template-variables";

const t = ((key: string) => key) as unknown as TFunction;

test("without a bound survey only the response's and survey's own values are offered", () => {
  expect(buildWorkflowTemplateVariables(null, t).map((variable) => variable.token)).toEqual([
    "{{survey.name}}",
    "{{response.id}}",
    "{{response.data}}",
    "{{contact.id}}",
    "{{contact.attributes.email}}",
  ]);
  expect(toTemplateToken("answers.q1")).toBe("{{answers.q1}}");
});

test("insertAtSelection puts the text at the caret, over a selection, or at the end", () => {
  expect(insertAtSelection('{"a":""}', "{{x}}", { start: 6, end: 6 })).toEqual({
    value: '{"a":"{{x}}"}',
    caret: 11,
  });
  expect(insertAtSelection("Hi NAME!", "{{x}}", { start: 3, end: 7 })).toEqual({
    value: "Hi {{x}}!",
    caret: 8,
  });
  expect(insertAtSelection("Hi", "{{x}}", null)).toEqual({ value: "Hi{{x}}", caret: 7 });
  expect(insertAtSelection("Hi", "{{x}}", { start: 99, end: 99 })).toEqual({ value: "Hi{{x}}", caret: 7 });
});
