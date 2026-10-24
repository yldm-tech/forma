import { createColumnHelper } from "@tanstack/react-table";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vite-plus/test";
import { DataTable } from "./data-table";

afterEach(cleanup);

interface Item {
  id: string;
  name: string;
}

const column = createColumnHelper<Item>();

const renderTable = (onRowClick: (item: Item) => void, onOpen: (item: Item) => void) =>
  render(
    <DataTable
      aria-label="Items"
      emptyMessage="None"
      getRowId={(item) => item.id}
      data={[{ id: "1", name: "First" }]}
      onRowClick={onRowClick}
      columns={[
        column.accessor("name", {
          header: "Name",
          cell: (info) => (
            <button type="button" onClick={() => onOpen(info.row.original)}>
              {info.getValue()}
            </button>
          ),
        }),
        column.display({ id: "note", header: "Note", cell: () => <span>plain text</span> }),
      ]}
    />
  );

test("a row opens from a click anywhere, and from its own button without opening twice", () => {
  const onRowClick = vi.fn();
  const onOpen = vi.fn();
  renderTable(onRowClick, onOpen);

  fireEvent.click(screen.getByText("plain text"));
  expect(onRowClick).toHaveBeenCalledTimes(1);

  // The cell's button is the keyboard's way in; its click is not handled by the row as well.
  fireEvent.click(screen.getByRole("button", { name: "First" }));
  expect(onOpen).toHaveBeenCalledTimes(1);
  expect(onRowClick).toHaveBeenCalledTimes(1);
});
