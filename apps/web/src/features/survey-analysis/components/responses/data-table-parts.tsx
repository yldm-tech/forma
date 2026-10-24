import {
  DndContext,
  type DragEndEvent,
  PointerSensor,
  closestCorners,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { SortableContext, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  type Column,
  type ColumnDef,
  type Header,
  type HeaderContext,
  type Table,
  flexRender,
} from "@tanstack/react-table";
import {
  EllipsisVerticalIcon,
  EyeOffIcon,
  GripVertical,
  GripVerticalIcon,
  MoveVerticalIcon,
  SettingsIcon,
} from "lucide-react";
import { type CSSProperties, type ReactNode, useId } from "react";
import { useTranslation } from "react-i18next";
import { Checkbox } from "../../../../components/ui/checkbox";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "../../../../components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../../components/ui/dropdown-menu";
import { Switch } from "../../../../components/ui/switch";
import { TooltipRenderer } from "../../../../components/ui/tooltip";
import { cn } from "../../../../lib/cn";

// forma2's data-table components, as the response table uses them: the sortable header, the column menu, the table settings and the toolbar.

/** The columns pinned to the left of the response table: the selection and the date, so a row keeps its anchor when the table scrolls sideways. */
export const PINNED_COLUMN_IDS = ["select", "createdAt"];

export const isPinnedColumn = (columnId: string) => PINNED_COLUMN_IDS.includes(columnId);

/** The sticky position of a column pinned to the left. */
export const getCommonPinningStyles = <T,>(column: Column<T>): CSSProperties => ({
  left: `${column.getStart("left") - 1}px`,
  position: "sticky",
  width: column.getSize(),
  zIndex: 1,
});

/** The checkbox column that selects rows for the bulk actions. */
export const getSelectionColumn = <T extends object>(
  selectAllLabel: string,
  selectRowLabel: string
): ColumnDef<T, unknown> => ({
  id: "select",
  accessorKey: "select",
  size: 60,
  enableResizing: false,
  header: ({ table }) => (
    <div className="flex w-full items-center justify-center">
      <Checkbox
        checked={table.getIsAllPageRowsSelected()}
        onCheckedChange={(value) => table.toggleAllPageRowsSelected(value === true)}
        aria-label={selectAllLabel}
      />
    </div>
  ),
  cell: ({ row }) => (
    <div className="flex w-full items-center justify-center">
      <Checkbox
        checked={row.getIsSelected()}
        onCheckedChange={(value) => row.toggleSelected(value === true)}
        aria-label={selectRowLabel}
      />
    </div>
  ),
});

/** A column's menu. It is named "Column options" followed by the column's title (aria-labelledby on itself and the title), and it is out of the tab order: both of its actions are in the toolbar's table settings, and one stop per column would put dozens of stops between the toolbar and the first row. */
const ColumnSettingsDropdown = <T,>({
  column,
  setIsTableSettingsModalOpen,
  titleId,
}: {
  column: Column<T>;
  setIsTableSettingsModalOpen: (open: boolean) => void;
  titleId: string;
}) => {
  const { t } = useTranslation();
  const triggerId = useId();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          id={triggerId}
          type="button"
          tabIndex={-1}
          className="py-2 focus:outline-hidden"
          aria-label={t("workspace.surveys.responses.column_options")}
          aria-labelledby={`${triggerId} ${titleId}`}>
          <EllipsisVerticalIcon className="size-4" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem
          onClick={() => column.toggleVisibility(false)}
          icon={<EyeOffIcon className="size-4" />}>
          <span>{t("common.hide_column")}</span>
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => setIsTableSettingsModalOpen(true)}
          icon={<SettingsIcon className="size-4" />}>
          <span>{t("common.table_settings")}</span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};

/** A column header that can be dragged to reorder, hidden, and resized; the selection and date columns stay put. */
export const DataTableHeader = <T,>({
  header,
  setIsTableSettingsModalOpen,
}: {
  header: Header<T, unknown>;
  setIsTableSettingsModalOpen: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const titleId = useId();
  const { attributes, isDragging, listeners, setNodeRef, transform } = useSortable({ id: header.column.id });
  const pinned = isPinnedColumn(header.column.id);
  // Every header cell sticks to the top of the table's scroll container; the pinned ones also to its left, above the other headers and the pinned body cells.
  const style: CSSProperties = {
    opacity: isDragging ? 0.8 : 1,
    transform: CSS.Translate.toString(transform),
    transition: "width transform 0.2s ease-in-out",
    whiteSpace: "nowrap",
    width: header.column.getSize(),
    ...(pinned ? getCommonPinningStyles(header.column) : {}),
    position: "sticky",
    top: 0,
    zIndex: isDragging ? 4 : pinned ? 3 : 2,
  };

  return (
    <th
      colSpan={header.colSpan}
      ref={setNodeRef}
      style={style}
      className={cn(
        "group h-10 border-b border-slate-200 bg-white px-4 text-center align-middle font-medium shadow-[inset_0_-1px_0_var(--color-slate-200)]",
        {
          "border-r": !header.column.getIsLastColumn(),
          "border-l": !header.column.getIsFirstColumn(),
        }
      )}>
      <div className="flex items-center gap-1">
        {header.column.id !== "select" && header.column.id !== "createdAt" && (
          <div className="flex shrink-0">
            <ColumnSettingsDropdown
              column={header.column}
              setIsTableSettingsModalOpen={setIsTableSettingsModalOpen}
              titleId={titleId}
            />
            {/* dnd-kit's attributes make the handle a tab stop; tabIndex comes after them to take it out. Keyboard reordering is in the table settings. */}
            <button
              type="button"
              {...attributes}
              {...listeners}
              tabIndex={-1}
              className="cursor-move"
              aria-label={t("common.reorder_and_hide_columns")}>
              <GripVerticalIcon className="size-4" />
            </button>
          </div>
        )}
        <div id={titleId} className="w-full truncate text-left font-semibold">
          {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
        </div>
        {header.column.getCanResize() && (
          <button
            type="button"
            aria-hidden="true"
            tabIndex={-1}
            onDoubleClick={() => header.column.resetSize()}
            onMouseDown={header.getResizeHandler()}
            onTouchStart={header.getResizeHandler()}
            data-testid="column-resize-handle"
            className={cn(
              "absolute top-0 right-0 hidden h-full w-1 cursor-col-resize group-hover:block",
              header.column.getIsResizing() ? "bg-black" : "bg-slate-500"
            )}
          />
        )}
      </div>
    </th>
  );
};

const DataTableSettingsModalItem = <T,>({
  column,
  table,
}: {
  column: Column<T, unknown>;
  table: Table<T>;
}) => {
  const { t } = useTranslation();
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: column.id,
  });
  const style = {
    transition: transition ?? "transform 100ms ease",
    transform: CSS.Translate.toString(transform),
    zIndex: isDragging ? 10 : 1,
  };
  // A minimal header context renders a column's header whether or not the column is visible: the header groups hold visible columns only.
  const headerContext = { column, header: null, table } as unknown as HeaderContext<T, unknown>;

  return (
    <div ref={setNodeRef} style={style} id={column.id}>
      <div {...listeners} {...attributes}>
        <div className="flex w-full items-center justify-between gap-4 rounded-md p-1.5 hover:cursor-move hover:bg-slate-100">
          <div className="flex items-center gap-x-2 overflow-hidden">
            <GripVertical className="size-4 shrink-0" aria-hidden="true" />
            {flexRender(column.columnDef.header, headerContext)}
          </div>
          <Switch
            id={column.id}
            aria-label={t("common.hide_column")}
            checked={column.getIsVisible()}
            onCheckedChange={(value) => column.toggleVisibility(value)}
          />
        </div>
      </div>
    </div>
  );
};

/** forma2's DataTableSettingsModal: every column but the selection and the date, reorderable and each hideable. */
export const DataTableSettingsModal = <T,>({
  open,
  setOpen,
  table,
  columnOrder,
  handleDragEnd,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  table: Table<T>;
  columnOrder: string[];
  handleDragEnd: (event: DragEndEvent) => void;
}) => {
  const { t } = useTranslation();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const tableColumns = table.getAllColumns();
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogHeader>
        <SettingsIcon className="size-6" />
        <DialogTitle>{t("common.table_settings")}</DialogTitle>
        <DialogDescription>{t("common.reorder_and_hide_columns")}</DialogDescription>
      </DialogHeader>
      <DialogBody className="max-h-[75vh] space-y-2 overflow-auto">
        <DndContext
          id="table-settings"
          sensors={sensors}
          onDragEnd={handleDragEnd}
          collisionDetection={closestCorners}>
          <SortableContext items={columnOrder} strategy={verticalListSortingStrategy}>
            {columnOrder.map((columnId) => {
              if (columnId === "select" || columnId === "createdAt") return null;
              const column = tableColumns.find((candidate) => candidate.id === columnId);
              if (!column) return null;
              return <DataTableSettingsModalItem column={column} table={table} key={column.id} />;
            })}
          </SortableContext>
        </DndContext>
      </DialogBody>
    </Dialog>
  );
};

/** forma2's DataTableToolbar: the selection's bulk actions on the left, table settings and row expansion on the right. */
export const DataTableToolbar = ({
  selection,
  setIsTableSettingsModalOpen,
  setIsExpanded,
  isExpanded,
}: {
  selection: ReactNode;
  setIsTableSettingsModalOpen: (open: boolean) => void;
  setIsExpanded: (expanded: boolean) => void;
  isExpanded: boolean;
}) => {
  const { t } = useTranslation();
  return (
    <div className="flex w-full items-center justify-between py-2">
      <div>{selection}</div>
      <div className="flex gap-x-2">
        <TooltipRenderer tooltipContent={t("common.table_settings")}>
          <button
            type="button"
            aria-label={t("common.table_settings")}
            onClick={() => setIsTableSettingsModalOpen(true)}
            className="cursor-pointer rounded-md border bg-white hover:border-slate-400">
            <SettingsIcon strokeWidth={1.5} className="m-1 size-6 p-0.5" />
          </button>
        </TooltipRenderer>
        <TooltipRenderer tooltipContent={isExpanded ? t("common.collapse_rows") : t("common.expand_rows")}>
          <button
            type="button"
            aria-label={isExpanded ? t("common.collapse_rows") : t("common.expand_rows")}
            aria-pressed={isExpanded}
            onClick={() => setIsExpanded(!isExpanded)}
            className={cn(
              "cursor-pointer rounded-md border bg-white hover:border-slate-400",
              isExpanded && "bg-black text-white"
            )}>
            <MoveVerticalIcon strokeWidth={1.5} className="m-1 size-6 p-0.5" />
          </button>
        </TooltipRenderer>
      </div>
    </div>
  );
};
