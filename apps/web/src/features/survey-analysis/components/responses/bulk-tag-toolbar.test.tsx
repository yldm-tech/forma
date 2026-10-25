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
const VIP = tag("t-vip", "vip");
const CHURN = tag("t-churn", "churn");
const REFUND = tag("t-refund", "refund");

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
  changeTag,
  createTag,
  onTagsChanged,
  onSelectAllMatching,
}: {
  selected: RowSelectionState;
  allMatching: boolean;
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
      environmentTags={[CHURN, REFUND, VIP]}
      onDeleted={vi.fn()}
      deleteResponse={vi.fn()}
      downloadResponses={vi.fn()}
      changeTag={changeTag}
      createTag={createTag}
      onTagsChanged={onTagsChanged}
    />
  );
};

const renderToolbar = (selected: RowSelectionState, allMatching = false) => {
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

  test("creates a tag from the search and adds it", async () => {
    const { changeTag, createTag, onTagsChanged } = renderToolbar({ r2: true });
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Search Tags…" }), {
      target: { value: "  urgent " },
    });
    fireEvent.click(screen.getByText("+ Create tag “urgent”"));
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
