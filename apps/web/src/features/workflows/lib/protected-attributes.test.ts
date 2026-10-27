import { expect, test } from "vite-plus/test";
import type { TWorkflowDefinition } from "../definition";
import {
  getProtectedAttributeWrites,
  isRespondentSourcedValue,
  needsProtectedWriteConfirmation,
  writesProtectedAttributeFromResponse,
} from "./protected-attributes";

test("a value is respondent-sourced when it fills an answer, a hidden field or a variable", () => {
  expect(isRespondentSourcedValue("{{answers.plan}}", [])).toBe(true);
  expect(isRespondentSourcedValue("tier-{{ answers.utm_source }}", [])).toBe(true);
  expect(isRespondentSourcedValue("{{variables.score}}", [])).toBe(true);
  expect(isRespondentSourcedValue("{{answers.pmf.default}}", [])).toBe(true);
  expect(isRespondentSourcedValue("enterprise", [])).toBe(false);
  expect(isRespondentSourcedValue("{{survey.name}} {{response.id}}", [])).toBe(false);
  expect(isRespondentSourcedValue("{{answers.}}", [])).toBe(false);
  expect(isRespondentSourcedValue("{{variables.}}", [])).toBe(false);
});

test("all answers are respondent-sourced", () => {
  expect(isRespondentSourcedValue("{{response.data}}", [])).toBe(true);
  expect(isRespondentSourcedValue("{{ response.data }}", ["plan"])).toBe(true);
});

test("a choice id is not respondent-sourced, since it is always one of the author's choice ids", () => {
  expect(isRespondentSourcedValue("{{answers.pmf.choiceId}}", [])).toBe(false);
  expect(isRespondentSourcedValue("tier-{{ answers.plan.choiceId }}", ["plan"])).toBe(false);
  expect(isRespondentSourcedValue("{{answers.plan.choiceId}}-{{answers.note}}", [])).toBe(true);
});

test("a contact attribute is respondent-sourced unless it is itself protected from the SDK", () => {
  expect(isRespondentSourcedValue("{{contact.attributes.nickname}}", ["plan"])).toBe(true);
  expect(isRespondentSourcedValue("{{contact.attributes.email}}", [])).toBe(true);
  expect(isRespondentSourcedValue("{{contact.attributes.tier}}", ["plan", "tier"])).toBe(false);
  expect(isRespondentSourcedValue("{{survey.name}} {{contact.attributes.plan}}", ["plan"])).toBe(false);
  expect(isRespondentSourcedValue("{{contact.attributes.}}", [])).toBe(false);
  expect(isRespondentSourcedValue("{{contact.id}} {{contact.userId}}", [])).toBe(false);
});

test("only a protected key written from the response needs a confirmation", () => {
  expect(
    writesProtectedAttributeFromResponse({ attributeKey: "plan", value: "{{answers.q1}}" }, ["plan"])
  ).toBe(true);
  expect(writesProtectedAttributeFromResponse({ attributeKey: "plan", value: "enterprise" }, ["plan"])).toBe(
    false
  );
  expect(
    writesProtectedAttributeFromResponse({ attributeKey: "nps", value: "{{answers.q1}}" }, ["plan"])
  ).toBe(false);
  expect(
    writesProtectedAttributeFromResponse({ attributeKey: "plan", value: "{{answers.q1.choiceId}}" }, ["plan"])
  ).toBe(false);
  expect(
    writesProtectedAttributeFromResponse({ attributeKey: "plan", value: "{{contact.attributes.nickname}}" }, [
      "plan",
    ])
  ).toBe(true);
});

test("a protected write is confirmed when it starts and again when it moves to another protected attribute", () => {
  const keys = ["plan", "seats"];
  const fromAnswer = (attributeKey: string) => ({ attributeKey, value: "{{answers.q1}}" });
  expect(
    needsProtectedWriteConfirmation({ attributeKey: "plan", value: "pro" }, fromAnswer("plan"), keys)
  ).toBe(true);
  expect(
    needsProtectedWriteConfirmation(
      { attributeKey: "nps", value: "{{answers.q1}}" },
      fromAnswer("plan"),
      keys
    )
  ).toBe(true);
  expect(needsProtectedWriteConfirmation(fromAnswer("plan"), fromAnswer("seats"), keys)).toBe(true);
  expect(
    needsProtectedWriteConfirmation(
      fromAnswer("plan"),
      { attributeKey: "plan", value: "{{answers.q2}}" },
      keys
    )
  ).toBe(false);
  expect(needsProtectedWriteConfirmation(fromAnswer("plan"), fromAnswer("nps"), keys)).toBe(false);
  expect(
    needsProtectedWriteConfirmation(fromAnswer("plan"), { attributeKey: "seats", value: "10" }, keys)
  ).toBe(false);
});

test("the definition's protected writes are listed in order", () => {
  const attribute = (id: string, attributeKey: string, value: string) => ({
    id,
    type: "action" as const,
    actionType: "set_contact_attribute" as const,
    config: { attributeKey, value },
  });
  const definition = {
    schemaVersion: 1,
    trigger: null,
    entryNodeId: "trigger",
    edges: [],
    nodes: [
      attribute("a", "plan", "{{answers.plan}}"),
      attribute("b", "nps", "{{answers.nps}}"),
      attribute("c", "seats", "{{variables.seats}}"),
      attribute("d", "seats", "10"),
      attribute("e", "plan", "{{answers.plan.choiceId}}"),
      attribute("f", "seats", "{{response.data}}"),
    ],
  } as unknown as TWorkflowDefinition;
  expect(getProtectedAttributeWrites(definition, ["plan", "seats"]).map((node) => node.id)).toEqual([
    "a",
    "c",
    "f",
  ]);
  expect(getProtectedAttributeWrites(definition, [])).toEqual([]);
  expect(getProtectedAttributeWrites(null, ["plan"])).toEqual([]);
});
