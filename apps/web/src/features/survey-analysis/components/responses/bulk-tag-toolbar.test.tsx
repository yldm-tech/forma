import type { TResponseFilterCriteria, TResponseTableData } from "@forma/types/responses";
import type { TTag } from "@forma/types/tags";
import { type RowSelectionState, getCoreRowModel, useReactTable } from "@tanstack/react-table";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { toast } from "../../../../lib/toast";
import type { BulkTagOperation, BulkTagResult } from "../../api";
import { SelectedRowSettings } from "./selected-row-settings";

// The selection toolbar's bulk tagging: ticked rows go to POST /api/app/surveys/{id}/responses/tags/{add,remove} as responseIds, "select all N matching" as the view's filter (apps/server/internal/response/bulk_tags.go).

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // cmdk scrolls the selected item into view and measures its list; jsdom lays nothing out.
  Element.prototype.scrollIntoView ??= () => {};
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

const tag = (id: string, name: string): TTag => ({
  id,
  name,
  workspaceId: "w",
  createdAt: new Date(0),
  updatedAt: new Date(0),
});
// Real tag ids are UUIDs, whose hex letters and digits a search must not match.
const VIP = tag("0b1c2d3e-4f50-4a6b-8c7d-9e0fa1b2c3d4", "vip");
const CHURN = tag("5e6f7a8b-9c0d-4e1f-a2b3-c4d5e6f7a8b9", "churn");
const REFUND = tag("a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", "refund");
const TAGS = [CHURN, REFUND, VIP];

const row = (responseId: string, tags: TTag[]) =>
  ({ responseId, tags, quotas: [] }) as unknown as TResponseTableData;
const ROWS = [row("r1", [VIP]), row("r2", [CHURN]), row("r3", [])];
const FILTER: TResponseFilterCriteria = { finished: true };

type ChangeTag = (
  operation: BulkTagOperation,
  tagId: string,
  criteria: TResponseFilterCriteria
) => Promise<BulkTagResult>;

const Harness = ({
  selected,
  allMatching,
  environmentTags,
  changeTag,
  createTag,
  onTagsChanged,
  onSelectAllMatching,
}: {
  selected: RowSelectionState;
  allMatching: boolean;
  environmentTags: TTag[];
  changeTag: ChangeTag;
  createTag: (name: string) => Promise<TTag>;
  onTagsChanged: () => void;
  onSelectAllMatching: () => void;
}) => {
  const [rowSelection, setRowSelection] = useState(selected);
  // The table the response table builds, reduced to its rows and their selection.
  // oxlint-disable-next-line react/incompatible-library -- the SPA is not built with the React Compiler
  const table = useReactTable({
    data: ROWS,
    columns: [],
    getRowId: (r) => r.responseId,
    getCoreRowModel: getCoreRowModel(),
    onRowSelectionChange: setRowSelection,
    state: { rowSelection },
  });
  return (
    <SelectedRowSettings
      table={table}
      matchingCount={30}
      allMatching={allMatching}
      filterCriteria={FILTER}
      onSelectAllMatching={onSelectAllMatching}
      environmentTags={environmentTags}
      onDeleted={vi.fn()}
      deleteResponse={vi.fn()}
      downloadResponses={vi.fn()}
      changeTag={changeTag}
      createTag={createTag}
      onTagsChanged={onTagsChanged}
    />
  );
};

const renderToolbar = (selected: RowSelectionState, allMatching = false, environmentTags = TAGS) => {
  const changeTag = vi.fn<ChangeTag>((_operation, tagId, criteria) =>
    Promise.resolve({
      tagId,
      matched: criteria.responseIds?.length ?? 30,
      changed: criteria.responseIds?.length ?? 30,
    })
  );
  const createTag = vi.fn((name: string) => Promise.resolve(tag("t-new", name)));
  const onTagsChanged = vi.fn();
  const onSelectAllMatching = vi.fn();
  render(
    <I18nextProvider i18n={i18n}>
      <Harness
        selected={selected}
        allMatching={allMatching}
        environmentTags={environmentTags}
        changeTag={changeTag}
        createTag={createTag}
        onTagsChanged={onTagsChanged}
        onSelectAllMatching={onSelectAllMatching}
      />
    </I18nextProvider>
  );
  return { changeTag, createTag, onTagsChanged, onSelectAllMatching };
};

const toastMessages = () => toast.getSnapshot().map((shown) => shown.message);

// The options a user can see: getByText would also find those in a group cmdk has hidden.
const visibleOptions = () => screen.queryAllByRole("option").map((option) => option.textContent?.trim());

const search = (value: string) =>
  fireEvent.change(screen.getByRole("combobox", { name: "Search Tags…" }), { target: { value } });

describe("bulk tagging of ticked rows", () => {
  test("adds the picked tag to the ticked rows by id", async () => {
    const { changeTag, onTagsChanged } = renderToolbar({ r1: true, r2: true });
    expect(screen.getByText("2 responses selected")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }));
    fireEvent.click(screen.getByText("refund"));
    await waitFor(() => expect(onTagsChanged).toHaveBeenCalled());
    expect(changeTag).toHaveBeenCalledWith("add", REFUND.id, { responseIds: ["r1", "r2"] });
    expect(toastMessages()).toContain("Added “refund” to 2 responses");
  });

  test("offers to remove only the tags the ticked rows carry", async () => {
    const { changeTag, onTagsChanged } = renderToolbar({ r1: true, r3: true });
    fireEvent.click(screen.getByRole("button", { name: "Remove tag" }));
    expect(screen.getByText("vip")).toBeTruthy();
    expect(screen.queryByText("churn")).toBeNull();
    fireEvent.click(screen.getByText("vip"));
    await waitFor(() => expect(onTagsChanged).toHaveBeenCalled());
    expect(changeTag).toHaveBeenCalledWith("remove", VIP.id, { responseIds: ["r1", "r3"] });
  });

  test("searches the tag names, not their ids", () => {
    renderToolbar({ r1: true });
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }));
    expect(visibleOptions()).toEqual(["churn", "refund", "vip"]);
    search("E");
    expect(visibleOptions()).toEqual(["refund", "+ Create tag “E”"]);
    search("2");
    expect(visibleOptions()).toEqual(["+ Create tag “2”"]);
    search("bad");
    expect(visibleOptions()).toEqual(["+ Create tag “bad”"]);
  });

  test("shows the create option for a name no tag has, also when the workspace has no tags", () => {
    renderToolbar({ r1: true });
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }));
    search("urgent");
    expect(visibleOptions()).toEqual(["+ Create tag “urgent”"]);
    cleanup();
    renderToolbar({ r1: true }, false, []);
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }));
    expect(screen.getByText("Type a name to create a tag")).toBeTruthy();
    search("urgent");
    expect(visibleOptions()).toEqual(["+ Create tag “urgent”"]);
  });

  test("says when no tag to remove matches the search", () => {
    renderToolbar({ r1: true, r2: true });
    fireEvent.click(screen.getByRole("button", { name: "Remove tag" }));
    expect(visibleOptions()).toEqual(["churn", "vip"]);
    search("c");
    expect(visibleOptions()).toEqual(["churn"]);
    search("urgent");
    expect(visibleOptions()).toEqual([]);
    expect(screen.getByText(i18n.t("workspace.tags.no_tag_found"))).toBeTruthy();
  });

  test("creates a tag from the search and adds it", async () => {
    const { changeTag, createTag, onTagsChanged } = renderToolbar({ r2: true });
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }));
    search("  urgent ");
    fireEvent.click(screen.getByRole("option", { name: "+ Create tag “urgent”" }));
    await waitFor(() => expect(onTagsChanged).toHaveBeenCalled());
    expect(createTag).toHaveBeenCalledWith("urgent");
    expect(changeTag).toHaveBeenCalledWith("add", "t-new", { responseIds: ["r2"] });
  });

  test("offers to widen the selection past the loaded rows", () => {
    const { onSelectAllMatching } = renderToolbar({ r1: true, r2: true, r3: true });
    fireEvent.click(screen.getByRole("button", { name: "Select all 30 matching" }));
    expect(onSelectAllMatching).toHaveBeenCalled();
  });

  test("reports a failed change and still reads the responses again", async () => {
    const { changeTag, onTagsChanged } = renderToolbar({ r1: true });
    changeTag.mockRejectedValueOnce(new Error("offline"));
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }));
    fireEvent.click(screen.getByText("churn"));
    await waitFor(() => expect(onTagsChanged).toHaveBeenCalled());
    expect(toastMessages()).toContain(i18n.t("common.something_went_wrong_please_try_again"));
  });
});

describe("bulk tagging of all matching responses", () => {
  test("sends the view's filter and offers every tag for removal, but no delete", async () => {
    const { changeTag, onTagsChanged } = renderToolbar({ r1: true, r2: true, r3: true }, true);
    expect(screen.getByText("All 30 matching responses selected")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Select all 30 matching" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Delete/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove tag" }));
    fireEvent.click(screen.getByText("refund"));
    await waitFor(() => expect(onTagsChanged).toHaveBeenCalled());
    expect(changeTag).toHaveBeenCalledWith("remove", REFUND.id, FILTER);
    expect(toastMessages()).toContain("Removed “refund” from 30 responses");
  });
});
