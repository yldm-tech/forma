import { describe, expect, test } from "vite-plus/test";
import type { SurveyListItem } from "../api";
import { surveyLink, surveyPreviewLink, surveyRowRules } from "./row-rules";

type Row = Pick<SurveyListItem, "id" | "status" | "type" | "archivedAt" | "publishOn">;
const row = (overrides: Partial<Row> = {}): Row => ({
  id: "s1",
  status: "inProgress",
  type: "link",
  archivedAt: null,
  publishOn: null,
  ...overrides,
});
const archivedAt = "2026-10-01T00:00:00.000Z";

// forma2's SurveyCard and SurveyDropDownMenu rules.
describe("surveyRowRules", () => {
  test("a draft opens its editor, any other survey its summary, an archived one always its summary", () => {
    expect(surveyRowRules(row({ status: "draft" }), "ws", false).href).toBe("/workspaces/ws/surveys/s1/edit");
    expect(surveyRowRules(row(), "ws", false).href).toBe("/workspaces/ws/surveys/s1/summary");
    expect(surveyRowRules(row({ status: "completed" }), "ws", true).href).toBe(
      "/workspaces/ws/surveys/s1/summary"
    );
    expect(surveyRowRules(row({ archivedAt }), "ws", false).href).toBe("/workspaces/ws/surveys/s1/summary");
    expect(surveyRowRules(row({ status: "draft", archivedAt }), "ws", false).href).toBe(
      "/workspaces/ws/surveys/s1/summary"
    );
  });

  test("only a read-only draft or an archived draft is not clickable; only a read-only draft disables its menu", () => {
    expect(surveyRowRules(row({ status: "draft" }), "ws", true)).toMatchObject({
      isClickable: false,
      isMenuDisabled: true,
    });
    expect(surveyRowRules(row({ status: "draft", archivedAt }), "ws", false)).toMatchObject({
      isClickable: false,
      isMenuDisabled: false,
    });
    expect(surveyRowRules(row({ status: "completed" }), "ws", true)).toMatchObject({
      isClickable: true,
      isMenuDisabled: false,
    });
    expect(surveyRowRules(row({ archivedAt }), "ws", false).isClickable).toBe(true);
    expect(surveyRowRules(row({ status: "draft" }), "ws", false).isClickable).toBe(true);
  });

  test("preview and copy link are for published link surveys that are not archived", () => {
    expect(surveyRowRules(row(), "ws", true).canPreviewOrCopyLink).toBe(true);
    expect(surveyRowRules(row({ type: "app" }), "ws", false).canPreviewOrCopyLink).toBe(false);
    expect(surveyRowRules(row({ status: "draft" }), "ws", false).canPreviewOrCopyLink).toBe(false);
    expect(surveyRowRules(row({ archivedAt }), "ws", false).canPreviewOrCopyLink).toBe(false);
  });

  test("the status can be changed by a writer on a published survey that is not archived", () => {
    expect(surveyRowRules(row({ status: "paused" }), "ws", false).canChangeStatus).toBe(true);
    expect(surveyRowRules(row({ status: "paused" }), "ws", true).canChangeStatus).toBe(false);
    expect(surveyRowRules(row({ status: "draft" }), "ws", false).canChangeStatus).toBe(false);
    expect(surveyRowRules(row({ archivedAt }), "ws", false).canChangeStatus).toBe(false);
  });

  test("a read-only member sees the menu only when they can preview or copy the link", () => {
    expect(surveyRowRules(row({ type: "app" }), "ws", true).hasVisibleActions).toBe(false);
    expect(surveyRowRules(row(), "ws", true).hasVisibleActions).toBe(true);
    // Restore and delete are the only actions on an archived survey, and both need write access.
    expect(surveyRowRules(row({ archivedAt }), "ws", true).hasVisibleActions).toBe(false);
    expect(surveyRowRules(row({ archivedAt }), "ws", false).hasVisibleActions).toBe(true);
  });

  test("a paused survey with a publish date is scheduled, unless it is archived", () => {
    const publishOn = "2026-11-01T00:00:00.000Z";
    expect(surveyRowRules(row({ status: "paused", publishOn }), "ws", false).isScheduled).toBe(true);
    expect(surveyRowRules(row({ status: "paused" }), "ws", false).isScheduled).toBe(false);
    expect(surveyRowRules(row({ status: "paused", publishOn, archivedAt }), "ws", false).isScheduled).toBe(
      false
    );
  });
});

describe("survey links", () => {
  test("are built on the public domain, the preview flagged", () => {
    expect(surveyLink("https://surveys.example.com", "s1")).toBe("https://surveys.example.com/s/s1");
    expect(surveyPreviewLink("https://surveys.example.com", "s1")).toBe(
      "https://surveys.example.com/s/s1?preview=true"
    );
  });
});
