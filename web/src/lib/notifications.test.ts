import { expect, test } from "vite-plus/test";
import { unsubscribeTarget } from "./notifications";

const organizations = [
  {
    id: "o1",
    name: "Acme",
    autoSubscribe: true,
    surveys: [
      { id: "s1", name: "NPS", workspaceId: "w1", workspaceName: "Main", alert: true },
      { id: "s2", name: "CSAT", workspaceId: "w1", workspaceName: "Main", alert: false },
    ],
  },
  { id: "o2", name: "Other", autoSubscribe: false, surveys: [] },
];

test("an unsubscribe link switches off only what is on and on this page", () => {
  expect(unsubscribeTarget({ type: "alert", elementId: "s1" }, organizations)).toEqual({
    type: "alert",
    id: "s1",
  });
  expect(unsubscribeTarget({ type: "alert", elementId: "s2" }, organizations)).toBeNull();
  expect(unsubscribeTarget({ type: "alert", elementId: "elsewhere" }, organizations)).toBeNull();
  expect(unsubscribeTarget({ type: "unsubscribedOrganizationIds", elementId: "o1" }, organizations)).toEqual({
    type: "unsubscribedOrganizationIds",
    id: "o1",
  });
  expect(
    unsubscribeTarget({ type: "unsubscribedOrganizationIds", elementId: "o2" }, organizations)
  ).toBeNull();
  expect(unsubscribeTarget({ type: "weekly", elementId: "o1" }, organizations)).toBeNull();
  expect(unsubscribeTarget({}, organizations)).toBeNull();
});
