import {
  type ColumnDef,
  type OnChangeFn,
  type RowSelectionState,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import type { ReactNode } from "react";
import { cn } from "../../lib/cn";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./table";

/** A column's extra presentation: header and cell classes and alignment. */
export interface DataTableColumnMeta {
  headerClassName?: string;
  cellClassName?: string;
  align?: "left" | "center" | "right";
}

declare module "@tanstack/react-table" {
  interface ColumnMeta<TData, TValue> extends DataTableColumnMeta {}
}

const alignClass = { left: "text-left", center: "text-center", right: "text-right" } as const;

/** Whether a click on a clickable row or cell started on a control inside it (a link, button, checkbox or label), which handles the click itself rather than the row. */
export const isFromInteractiveElement = (target: EventTarget | null, container: Element): boolean => {
  if (target === null || !("closest" in target)) return false;
  const control = (target as Element).closest("a, button, input, select, textarea, label");
  return control !== null && container.contains(control);
};

/**
 * forma2's SettingsTable and DataTable on TanStack Table: a header row, one row per item, a loading skeleton and an empty message. A row can open something when clicked, and rows can be selected when the caller owns the selection state. A clickable row is only a mouse convenience: give one of its cells a real button or link that does the same, so the keyboard and screen readers can reach it; clicks on such controls do not reach the row.
 */
export const DataTable = <T,>({
  columns,
  data,
  getRowId,
  isLoading = false,
  emptyMessage,
  onRowClick,
  rowSelection,
  onRowSelectionChange,
  footer,
  "aria-label": ariaLabel,
}: {
  // TanStack's column helpers produce columns with different value types; the table only renders them.
  // oxlint-disable-next-line typescript/no-explicit-any -- ColumnDef's value parameter differs per column
  columns: ColumnDef<T, any>[];
  data: T[];
  getRowId: (row: T) => string;
  isLoading?: boolean;
  emptyMessage: string;
  onRowClick?: (row: T) => void;
  rowSelection?: RowSelectionState;
  onRowSelectionChange?: OnChangeFn<RowSelectionState>;
  footer?: ReactNode;
  "aria-label": string;
}) => {
  // The SPA is not built with the React Compiler, so nothing memoizes the table's functions.
  // oxlint-disable-next-line react/incompatible-library -- see above
  const table = useReactTable({
    data,
    columns,
    getRowId,
    getCoreRowModel: getCoreRowModel(),
    enableRowSelection: rowSelection !== undefined,
    state: rowSelection ? { rowSelection } : {},
    onRowSelectionChange,
  });
  const columnCount = table.getVisibleLeafColumns().length;

  return (
    <>
      <Table aria-label={ariaLabel} aria-busy={isLoading || undefined}>
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id} className="bg-slate-50">
              {group.headers.map((header) => {
                const meta = header.column.columnDef.meta;
                return (
                  <TableHead
                    key={header.id}
                    className={cn(
                      alignClass[meta?.align ?? "left"],
                      "text-slate-600",
                      meta?.headerClassName
                    )}>
                    {header.isPlaceholder
                      ? null
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                );
              })}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {isLoading ? (
            Array.from({ length: 3 }, (_, i) => (
              // oxlint-disable-next-line react/no-array-index-key -- skeleton rows are positional
              <TableRow key={i}>
                {Array.from({ length: columnCount }, (_, j) => (
                  // oxlint-disable-next-line react/no-array-index-key -- skeleton cells are positional
                  <TableCell key={j}>
                    <div aria-hidden="true" className="h-4 w-24 animate-pulse rounded-full bg-slate-200" />
                  </TableCell>
                ))}
              </TableRow>
            ))
          ) : table.getRowModel().rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={columnCount} className="py-8 text-center text-slate-500">
                {emptyMessage}
              </TableCell>
            </TableRow>
          ) : (
            table.getRowModel().rows.map((row) => (
              <TableRow
                key={row.id}
                data-state={row.getIsSelected() ? "selected" : undefined}
                className={cn(onRowClick && "cursor-pointer hover:bg-slate-50")}
                onClick={
                  onRowClick
                    ? (e) => {
                        // The row is a mouse convenience; a column that renders its own button or link (the keyboard's way in) handles the click itself.
                        if (!isFromInteractiveElement(e.target, e.currentTarget)) onRowClick(row.original);
                      }
                    : undefined
                }>
                {row.getVisibleCells().map((cell) => {
                  const meta = cell.column.columnDef.meta;
                  return (
                    <TableCell
                      key={cell.id}
                      className={cn(alignClass[meta?.align ?? "left"], meta?.cellClassName)}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  );
                })}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
      {footer}
    </>
  );
};
