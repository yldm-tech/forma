import {
  DndContext,
  type DragEndEvent,
  KeyboardSensor,
  type Modifier,
  MouseSensor,
  TouchSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { SortableContext, arrayMove, horizontalListSortingStrategy } from "@dnd-kit/sortable";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import type { TResponseTableData, TResponseWithQuotas } from "@forma/types/responses";
import type { TSurvey } from "@forma/types/surveys/types";
import type { TTag } from "@forma/types/tags";
import {
  type Cell,
  type Row,
  type VisibilityState,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { Maximize2Icon } from "lucide-react";
import { type ReactNode, memo, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { TableCell, TableRow } from "../../../../components/ui/table";
import { cn } from "../../../../lib/cn";
import { readStorage, writeStorage } from "../../../../lib/storage";
import { toast } from "../../../../lib/toast";
import { type ExportFormat, analysisApi } from "../../api";
import { reconcileColumnOrder, seedColumnVisibility } from "../../lib/column-state";
import { saveExport } from "../../lib/download";
import { mapResponsesToTableData } from "../../lib/response-table";
import {
  DataTableHeader,
  DataTableSettingsModal,
  DataTableToolbar,
  PINNED_COLUMN_IDS,
  getCommonPinningStyles,
  isPinnedColumn,
} from "./data-table-parts";
import { ResponseCardModal } from "./response-card-modal";
import { generateResponseTableColumns, getDefaultHiddenColumnIds } from "./response-table-columns";
import { SelectedRowSettings } from "./selected-row-settings";

/** Keeps a dragged column on its row, as dnd-kit's restrictToHorizontalAxis did in forma2. */
const restrictToHorizontalAxis: Modifier = ({ transform }) => ({ ...transform, y: 0 });

const readJson = <T,>(key: string): T | undefined => {
  const raw = readStorage(key);
  if (raw === null) return undefined;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return undefined;
  }
};

const ResponseTableCell = memo(
  ({
    cell,
    row,
    isSelected,
    isExpanded,
    onOpen,
    expandLabel,
  }: {
    cell: Cell<TResponseTableData, unknown>;
    row: Row<TResponseTableData>;
    /** The row's selection, passed so the memo re-renders the checkbox: the cell and row objects stay the same when only the selection changes. */
    isSelected: boolean;
    isExpanded: boolean;
    onOpen: (responseId: string) => void;
    expandLabel: string;
  }) => {
    const isSelect = cell.column.id === "select";
    const open = () => {
      if (!isSelect) onOpen(row.id);
    };
    return (
      <TableCell
        className={cn(
          "border-b border-slate-200 bg-white shadow-none group-hover:bg-slate-100",
          isSelected && "bg-slate-100",
          !cell.column.getIsLastColumn() && "border-r",
          isSelect && "px-2"
        )}
        style={{
          width: `${cell.column.getSize()}px`,
          ...(isPinnedColumn(cell.column.id) ? getCommonPinningStyles(cell.column) : {}),
        }}
        onClick={open}>
        <div className="flex w-full items-center">
          {/* The selection cell is not truncated: overflow hidden would clip the checkbox's border. */}
          <div
            className={cn(
              "flex flex-1 items-center",
              !isSelect && "min-w-0 truncate",
              isExpanded ? "h-full" : "h-10"
            )}>
            {flexRender(cell.column.columnDef.cell, cell.getContext())}
          </div>
          {/* A mouse affordance only: the date itself is the row's keyboard stop that opens the response. */}
          {cell.column.id === "createdAt" && (
            <button
              type="button"
              aria-hidden="true"
              tabIndex={-1}
              title={expandLabel}
              className="hidden shrink-0 cursor-pointer items-center rounded-md border border-slate-200 bg-white p-2 group-focus-within:flex group-hover:flex hover:border-slate-300 focus:outline-hidden"
              onClick={(event) => {
                event.stopPropagation();
                open();
              }}>
              <Maximize2Icon className="size-4" />
            </button>
          )}
        </div>
      </TableCell>
    );
  }
);
ResponseTableCell.displayName = "ResponseTableCell";

/**
 * forma2's ResponseTable: the responses as a TanStack table whose columns can be reordered, hidden and resized, with the order, the visibility and the row expansion kept per survey in localStorage; rows can be selected for bulk download and delete, and a row opens its response card.
 */
export const ResponseTable = ({
  survey,
  responses,
  environmentTags,
  isReadOnly,
  fetchNextPage,
  hasMore,
  isFetchingFirstPage,
  totalCount,
  countSummary,
  emptyState,
  showQuotasColumn,
  locale,
  onResponseChanged,
  onResponsesDeleted,
}: {
  survey: TSurvey;
  responses: TResponseWithQuotas[];
  environmentTags: TTag[];
  isReadOnly: boolean;
  /** Loads the next page and resolves to every loaded response, the new page included. */
  fetchNextPage: () => Promise<TResponseWithQuotas[]>;
  hasMore: boolean;
  isFetchingFirstPage: boolean;
  /** How many responses match the current filter, when known: the response card's position reads "n of total". */
  totalCount: number | undefined;
  /** How many responses are loaded of those matching, shown in the toolbar while no row is selected. */
  countSummary: ReactNode;
  /** What shows under the header row when no response matches: it sits outside the table, which is as wide as all its columns, so it stays in view. */
  emptyState: ReactNode;
  showQuotasColumn: boolean;
  locale: string;
  onResponseChanged: (responseId: string) => void;
  onResponsesDeleted: (responseIds: string[]) => void;
}) => {
  const { t } = useTranslation();
  const orderKey = `${survey.id}-columnOrder`;
  const visibilityKey = `${survey.id}-columnVisibility`;
  const expandKey = `${survey.id}-rowExpand`;
  const [savedVisibility, setSavedVisibility] = useState<VisibilityState>(
    () => readJson<VisibilityState>(visibilityKey) ?? {}
  );
  const [rowSelection, setRowSelection] = useState({});
  const [isTableSettingsModalOpen, setIsTableSettingsModalOpen] = useState(false);
  const [isExpanded, setIsExpanded] = useState(() => readJson<boolean>(expandKey) ?? false);
  const [selectedResponseId, setSelectedResponseId] = useState<string | null>(null);
  const [parent] = useAutoAnimate();

  const columns = useMemo(
    () => generateResponseTableColumns(survey, isExpanded, isReadOnly, locale, t, showQuotasColumn),
    [survey, isExpanded, isReadOnly, locale, t, showQuotasColumn]
  );
  const defaultColumnOrder = useMemo(
    () =>
      columns.map(
        (column) =>
          column.id ?? ("accessorKey" in column ? String(column.accessorKey).replaceAll(".", "_") : "")
      ),
    [columns]
  );
  // A column the saved state has never seen gets its default visibility; one the author has shown or hidden keeps that choice.
  const columnVisibility = useMemo(
    () => seedColumnVisibility(savedVisibility, getDefaultHiddenColumnIds(survey, defaultColumnOrder)),
    [savedVisibility, survey, defaultColumnOrder]
  );
  const [savedOrder, setSavedOrder] = useState<string[]>(() => readJson<string[]>(orderKey) ?? []);
  // A saved order is reconciled with the table's real columns: stale ids drop, new ones take their default place.
  const columnOrder = useMemo(
    () => (savedOrder.length > 0 ? reconcileColumnOrder(savedOrder, defaultColumnOrder) : defaultColumnOrder),
    [savedOrder, defaultColumnOrder]
  );

  useEffect(() => {
    if (savedOrder.length > 0) writeStorage(orderKey, JSON.stringify(columnOrder));
  }, [columnOrder, orderKey, savedOrder.length]);
  useEffect(() => {
    if (Object.keys(columnVisibility).length > 0)
      writeStorage(visibilityKey, JSON.stringify(columnVisibility));
  }, [columnVisibility, visibilityKey]);
  useEffect(() => {
    writeStorage(expandKey, JSON.stringify(isExpanded));
  }, [isExpanded, expandKey]);

  const data = useMemo(() => mapResponsesToTableData(responses, survey, t), [responses, survey, t]);
  // While the first page loads, ten placeholder rows stand in, every cell a pulsing bar.
  const tableData = useMemo(
    () =>
      isFetchingFirstPage
        ? Array.from(
            { length: 10 },
            (_, index) => ({ responseId: `skeleton-${index}` }) as TResponseTableData
          )
        : data,
    [data, isFetchingFirstPage]
  );
  const tableColumns = useMemo(
    () =>
      isFetchingFirstPage
        ? columns.map((column) => ({
            ...column,
            cell: () => <div className="h-6 w-full animate-pulse rounded-full bg-slate-200" />,
          }))
        : columns,
    [columns, isFetchingFirstPage]
  );

  // The SPA is not built with the React Compiler, so nothing memoizes the table's functions.
  // oxlint-disable-next-line react/incompatible-library -- see above
  const table = useReactTable({
    data: tableData,
    columns: tableColumns,
    getRowId: (row) => row.responseId,
    getCoreRowModel: getCoreRowModel(),
    onColumnVisibilityChange: (updater) =>
      setSavedVisibility(typeof updater === "function" ? updater(columnVisibility) : updater),
    onRowSelectionChange: setRowSelection,
    onColumnOrderChange: (updater) =>
      setSavedOrder((current) =>
        typeof updater === "function"
          ? updater(current.length > 0 ? columnOrder : defaultColumnOrder)
          : updater
      ),
    columnResizeMode: "onChange",
    columnResizeDirection: "ltr",
    manualPagination: true,
    defaultColumn: { size: 300 },
    state: { columnOrder, columnVisibility, rowSelection, columnPinning: { left: PINNED_COLUMN_IDS } },
  });

  const sensors = useSensors(
    useSensor(MouseSensor, {}),
    useSensor(TouchSensor, {}),
    useSensor(KeyboardSensor, {})
  );

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const oldIndex = columnOrder.indexOf(String(active.id));
    const newIndex = columnOrder.indexOf(String(over.id));
    setSavedOrder(arrayMove(columnOrder, oldIndex, newIndex));
  };

  const downloadSelectedRows = async (responseIds: string[], format: ExportFormat) => {
    try {
      saveExport(await analysisApi.exportResponses(survey.id, format, { responseIds }), format);
    } catch {
      toast.error(t("workspace.surveys.responses.error_downloading_responses"));
    }
  };

  const handleDeleted = (responseIds: string[]) => {
    setRowSelection({});
    onResponsesDeleted(responseIds);
  };

  const rows = table.getRowModel().rows;
  const isEmpty = !isFetchingFirstPage && rows.length === 0;

  return (
    <div>
      <DndContext
        id="response-table"
        collisionDetection={closestCenter}
        modifiers={[restrictToHorizontalAxis]}
        onDragEnd={handleDragEnd}
        sensors={sensors}>
        <DataTableToolbar
          selection={
            table.getFilteredSelectedRowModel().rows.length > 0 ? (
              <SelectedRowSettings
                table={table}
                onDeleted={handleDeleted}
                deleteResponse={(responseId, decrementQuotas) =>
                  analysisApi.deleteResponse(responseId, decrementQuotas)
                }
                downloadResponses={downloadSelectedRows}
              />
            ) : (
              countSummary
            )
          }
          setIsExpanded={setIsExpanded}
          setIsTableSettingsModalOpen={setIsTableSettingsModalOpen}
          isExpanded={isExpanded}
        />
        {/* The one scroll container of the table, both ways, at most a viewport tall: the header row sticks to its top and the horizontal scrollbar stays in view. */}
        <div className="max-h-[max(20rem,calc(100dvh-22rem))] w-fit max-w-full overflow-auto rounded-xl border border-slate-200">
          <div className="w-full">
            <table
              // Separate borders belong to their cells, so the pinned columns carry theirs when the table scrolls sideways; collapsed borders stay behind and let the scrolled cells show through.
              className="w-full caption-bottom border-separate border-spacing-0 text-sm"
              style={{ tableLayout: "fixed" }}
              id="response-table">
              <thead className="text-slate-800 [&_tr]:border-b">
                {table.getHeaderGroups().map((headerGroup) => (
                  <TableRow key={headerGroup.id}>
                    <SortableContext items={columnOrder} strategy={horizontalListSortingStrategy}>
                      {headerGroup.headers.map((header) => (
                        <DataTableHeader
                          key={header.id}
                          header={header}
                          setIsTableSettingsModalOpen={setIsTableSettingsModalOpen}
                        />
                      ))}
                    </SortableContext>
                  </TableRow>
                ))}
              </thead>
              {/* Auto animation is off past 200 responses: animating that many rows costs more than it shows. */}
              <tbody
                ref={responses.length > 200 ? undefined : parent}
                className="[&_tr:last-child_td]:border-b-0">
                {rows.map((row) => (
                  <TableRow
                    key={row.id}
                    data-state={row.getIsSelected() ? "selected" : undefined}
                    className="group cursor-pointer">
                    {row.getVisibleCells().map((cell) => (
                      <ResponseTableCell
                        key={cell.id}
                        cell={cell}
                        row={row}
                        isSelected={row.getIsSelected()}
                        isExpanded={isExpanded}
                        onOpen={setSelectedResponseId}
                        expandLabel={t("workspace.surveys.summary.open_response_details")}
                      />
                    ))}
                  </TableRow>
                ))}
              </tbody>
            </table>
          </div>
          {isEmpty && <div className="sticky left-0">{emptyState}</div>}
        </div>

        {hasMore && data.length > 0 && (
          <div className="mt-4 flex justify-center">
            <Button onClick={() => void fetchNextPage()}>{t("common.load_more")}</Button>
          </div>
        )}

        <DataTableSettingsModal
          open={isTableSettingsModalOpen}
          setOpen={setIsTableSettingsModalOpen}
          table={table}
          columnOrder={columnOrder}
          handleDragEnd={handleDragEnd}
        />

        <ResponseCardModal
          survey={survey}
          responses={responses}
          hasMore={hasMore}
          fetchNextPage={fetchNextPage}
          totalCount={totalCount}
          environmentTags={environmentTags}
          isReadOnly={isReadOnly}
          onResponseChanged={onResponseChanged}
          onResponsesDeleted={onResponsesDeleted}
          selectedResponseId={selectedResponseId}
          setSelectedResponseId={setSelectedResponseId}
          locale={locale}
        />
      </DndContext>
    </div>
  );
};
