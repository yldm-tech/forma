import { expect, test } from "vite-plus/test";
import type { TWorkflowDefinition } from "../definition";
import {
  getProtectedAttributeWrites,
  isRespondentSourcedValue,
  writesProtectedAttributeFromResponse,
} from "./protected-attributes";

test("a value is respondent-sourced when it fills an answer, a hidden field or a variable", () => {
  expect(isRespondentSourcedValue("{{answers.plan}}")).toBe(true);
  expect(isRespondentSourcedValue("tier-{{ answers.utm_source }}")).toBe(true);
  expect(isRespondentSourcedValue("{{variables.score}}")).toBe(true);
  expect(isRespondentSourcedValue("{{answers.pmf.choiceId}}")).toBe(true);
  expect(isRespondentSourcedValue("enterprise")).toBe(false);
  expect(isRespondentSourcedValue("{{survey.name}} {{contact.attributes.plan}}")).toBe(false);
  expect(isRespondentSourcedValue("{{answers.}}")).toBe(false);
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
    ],
  } as unknown as TWorkflowDefinition;
  expect(getProtectedAttributeWrites(definition, ["plan", "seats"]).map((node) => node.id)).toEqual([
    "a",
    "c",
  ]);
  expect(getProtectedAttributeWrites(definition, [])).toEqual([]);
  expect(getProtectedAttributeWrites(null, ["plan"])).toEqual([]);
});
