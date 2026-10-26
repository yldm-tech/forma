import { expect, test } from "vite-plus/test";
import { alertRows, isUnsubscribeLink, unsubscribeSubject, unsubscribeTarget } from "./notifications";

const organizations = [
  {
    id: "o1",
    name: "Acme",
    autoSubscribe: true,
    weeklySummary: true,
    surveys: [
      { id: "s1", name: "NPS", workspaceId: "w1", workspaceName: "Main", alert: true },
      { id: "s2", name: "CSAT", workspaceId: "w1", workspaceName: "Main", alert: false },
    ],
  },
  { id: "o2", name: "Other", autoSubscribe: false, weeklySummary: false, surveys: [] },
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
  expect(
    unsubscribeTarget({ type: "unsubscribedWeeklySummaryOrganizationIds", elementId: "o1" }, organizations)
  ).toEqual({ type: "unsubscribedWeeklySummaryOrganizationIds", id: "o1" });
  expect(
    unsubscribeTarget({ type: "unsubscribedWeeklySummaryOrganizationIds", elementId: "o2" }, organizations)
  ).toBeNull();
  expect(unsubscribeTarget({ type: "weekly", elementId: "o1" }, organizations)).toBeNull();
  expect(unsubscribeTarget({}, organizations)).toBeNull();
});

test("an unsubscribe link names the switch it is for, on or off, with its survey's or organization's name", () => {
  expect(unsubscribeSubject({ type: "alert", elementId: "s2" }, organizations)).toEqual({
    type: "alert",
    id: "s2",
    name: "CSAT",
    enabled: false,
  });
  expect(unsubscribeSubject({ type: "unsubscribedOrganizationIds", elementId: "o1" }, organizations)).toEqual(
    {
      type: "unsubscribedOrganizationIds",
      id: "o1",
      name: "Acme",
      enabled: true,
    }
  );
  expect(
    unsubscribeSubject({ type: "unsubscribedWeeklySummaryOrganizationIds", elementId: "o2" }, organizations)
  ).toEqual({ type: "unsubscribedWeeklySummaryOrganizationIds", id: "o2", name: "Other", enabled: false });
  expect(unsubscribeSubject({ type: "alert", elementId: "o1" }, organizations)).toBeNull();
  expect(
    unsubscribeSubject({ type: "unsubscribedOrganizationIds", elementId: "s1" }, organizations)
  ).toBeNull();
  expect(unsubscribeSubject({ type: "weekly", elementId: "o1" }, organizations)).toBeNull();
  expect(unsubscribeSubject({ type: "alert" }, organizations)).toBeNull();
});

test("only a link with both a type and an id is an unsubscribe link", () => {
  expect(isUnsubscribeLink({ type: "alert", elementId: "s1" })).toBe(true);
  expect(isUnsubscribeLink({ type: "alert" })).toBe(false);
  expect(isUnsubscribeLink({ elementId: "s1" })).toBe(false);
  expect(isUnsubscribeLink({ type: "", elementId: "s1" })).toBe(false);
  expect(isUnsubscribeLink({})).toBe(false);
});

const survey = (id: string, name: string, workspaceName: string) => ({
  id,
  name,
  workspaceId: workspaceName,
  workspaceName,
  alert: false,
});

test("alert rows keep a workspace's surveys together, by workspace then survey name", () => {
  const rows = alertRows([
    survey("s1", "NPS Survey", "Website"),
    survey("s2", "Onboarding Feedback", "Docs Portal"),
    survey("s3", "Churn Survey", "Website"),
  ]);
  expect(rows.map((row) => [row.workspaceName, row.name])).toEqual([
    ["Docs Portal", "Onboarding Feedback"],
    ["Website", "Churn Survey"],
    ["Website", "NPS Survey"],
  ]);
});

test("alert rows order same-named surveys by id, so their rows cannot swap", () => {
  const duplicates = [survey("s2", "NPS Survey", "Website"), survey("s1", "NPS Survey", "Website")];
  expect(alertRows(duplicates).map((row) => row.id)).toEqual(["s1", "s2"]);
  expect(alertRows([...duplicates].reverse()).map((row) => row.id)).toEqual(["s1", "s2"]);
});

test("alert rows order numbered workspaces the way a reader counts them", () => {
  const rows = alertRows([survey("s1", "NPS", "Workspace 10"), survey("s2", "NPS", "Workspace 2")]);
  expect(rows.map((row) => row.workspaceName)).toEqual(["Workspace 2", "Workspace 10"]);
});
