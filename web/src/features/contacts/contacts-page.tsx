import { useInfiniteQuery, useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  type ColumnDef,
  type RowSelectionState,
  type VisibilityState,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { ExpandIcon, RefreshCwIcon, SettingsIcon, ShrinkIcon, Trash2Icon } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import { Dialog, DialogBody, DialogHeader, DialogTitle } from "../../components/ui/dialog";
import { HighlightedText } from "../../components/ui/highlighted-text";
import { IdBadge } from "../../components/ui/id-badge";
import { Input } from "../../components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../components/ui/table";
import { cn } from "../../lib/cn";
import {
  type ContactTableRow,
  contactTableRows,
  defaultColumnVisibility,
  formatAttributeValue,
} from "../../lib/contacts";
import { errorMessage } from "../../lib/errors";
import { meQuery } from "../../lib/queries/auth";
import { attributeKeysQuery, contactsQuery, useDeleteContacts } from "../../lib/queries/contacts";
import { readStorage, writeStorage } from "../../lib/storage";
import { toast } from "../../lib/toast";
import { useDebouncedValue } from "../../lib/use-debounced-value";
import { ContactsPageLayout } from "./contacts-layout";
import { UploadContactsButton } from "./upload-contacts";

/** forma2's contacts page: the workspace's contacts, searchable, thirty at a time. */
export const ContactsPage = ({ workspaceId, isReadOnly }: { workspaceId: string; isReadOnly: boolean }) => {
  const { t } = useTranslation();
  const keys = useQuery(attributeKeysQuery(workspaceId));
  return (
    <ContactsPageLayout
      pageTitle={t("common.contacts")}
      activeId="contacts"
      workspaceId={workspaceId}
      isReadOnly={isReadOnly}
      cta={keys.data && <UploadContactsButton workspaceId={workspaceId} attributeKeys={keys.data} />}>
      <ContactsTable workspaceId={workspaceId} isReadOnly={isReadOnly} />
    </ContactsPageLayout>
  );
};

const selectionColumn: ColumnDef<ContactTableRow> = {
  id: "select",
  header: ({ table }) => (
    <input
      type="checkbox"
      aria-label="Select all"
      className="size-4 cursor-pointer"
      checked={table.getIsAllPageRowsSelected()}
      onChange={(e) => table.toggleAllPageRowsSelected(e.target.checked)}
    />
  ),
  cell: ({ row }) => (
    <input
      type="checkbox"
      aria-label="Select row"
      className="size-4 cursor-pointer"
      checked={row.getIsSelected()}
      onChange={(e) => row.toggleSelected(e.target.checked)}
    />
  ),
};

/** The persisted table preferences forma2 kept per workspace. */
const visibilityStorageKey = (workspaceId: string) => `${workspaceId}-columnVisibility`;
const expandStorageKey = (workspaceId: string) => `${workspaceId}-rowExpand`;

const readJson = <T,>(key: string): T | undefined => {
  const raw = readStorage(key);
  if (raw === null) return undefined;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return undefined;
  }
};

const ContactsTable = ({ workspaceId, isReadOnly }: { workspaceId: string; isReadOnly: boolean }) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: me } = useSuspenseQuery(meQuery);
  const [searchValue, setSearchValue] = useState("");
  const search = useDebouncedValue(searchValue, 300);
  const keys = useQuery(attributeKeysQuery(workspaceId));
  const contacts = useInfiniteQuery(contactsQuery(workspaceId, search));
  const remove = useDeleteContacts(workspaceId);
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [isExpanded, setIsExpanded] = useState(
    () => readJson<boolean>(expandStorageKey(workspaceId)) ?? false
  );
  const [visibilityOverride, setVisibilityOverride] = useState<VisibilityState | undefined>(() =>
    readJson<VisibilityState>(visibilityStorageKey(workspaceId))
  );

  const rows = useMemo(
    () => contactTableRows(contacts.data?.pages.flat() ?? [], keys.data ?? []),
    [contacts.data, keys.data]
  );

  const columns = useMemo<ColumnDef<ContactTableRow>[]>(() => {
    const otherKeys = rows[0]?.attributes ?? [];
    const base: ColumnDef<ContactTableRow>[] = [
      {
        id: "contactsTableUser",
        header: t("common.id"),
        cell: ({ row }) => <HighlightedText value={row.original.id} searchValue={search} />,
      },
      {
        id: "userId",
        header: t("common.user_id"),
        cell: ({ row }) => (row.original.userId ? <IdBadge id={row.original.userId} /> : null),
      },
      {
        id: "email",
        header: t("common.email"),
        cell: ({ row }) => <HighlightedText value={row.original.email} searchValue={search} />,
      },
      {
        id: "firstName",
        header: t("common.first_name"),
        cell: ({ row }) => <HighlightedText value={row.original.firstName} searchValue={search} />,
      },
      {
        id: "lastName",
        header: t("common.last_name"),
        cell: ({ row }) => <HighlightedText value={row.original.lastName} searchValue={search} />,
      },
      ...otherKeys.map<ColumnDef<ContactTableRow>>((attribute) => ({
        id: attribute.key,
        header: attribute.name ?? attribute.key,
        cell: ({ row }) => {
          const value = row.original.attributes.find((a) => a.key === attribute.key);
          return value ? (
            <HighlightedText
              value={formatAttributeValue(value.value, value.dataType, me.locale)}
              searchValue={search}
            />
          ) : null;
        },
      })),
    ];
    return isReadOnly ? base : [selectionColumn, ...base];
  }, [rows, t, search, isReadOnly, me.locale]);

  const columnIds = columns.map((column) => column.id as string);
  // A stored choice applies only while it still covers exactly the table's columns, as forma2 checked it.
  const columnVisibility =
    visibilityOverride && Object.keys(visibilityOverride).length === columnIds.length
      ? visibilityOverride
      : defaultColumnVisibility(columnIds, rows);

  const setColumnVisibility = (next: VisibilityState) => {
    setVisibilityOverride(next);
    writeStorage(visibilityStorageKey(workspaceId), JSON.stringify(next));
  };

  // The SPA is not built with the React Compiler, so nothing memoizes the table's functions.
  // oxlint-disable-next-line react/incompatible-library -- see above
  const table = useReactTable({
    data: rows,
    columns,
    getRowId: (row) => row.id,
    getCoreRowModel: getCoreRowModel(),
    enableRowSelection: !isReadOnly,
    state: { rowSelection, columnVisibility },
    onRowSelectionChange: setRowSelection,
    onColumnVisibilityChange: (updater) =>
      setColumnVisibility(typeof updater === "function" ? updater(columnVisibility) : updater),
  });
  const selectedIds = Object.keys(rowSelection).filter((id) => rowSelection[id]);

  const deleteSelected = async () => {
    try {
      await remove.mutateAsync(selectedIds);
      setRowSelection({});
      toast.success(t("common.table_items_deleted_successfully", { type: "Contact" }));
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setConfirmDelete(false);
    }
  };

  const refresh = async () => {
    await contacts.refetch();
    toast.success(t("common.data_refreshed_successfully"));
  };

  return (
    <div className="w-full">
      <div className="mb-4 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <div className="w-64">
            <Input
              type="search"
              aria-label={t("workspace.contacts.search_contact")}
              placeholder={t("workspace.contacts.search_contact")}
              value={searchValue}
              onChange={(e) => {
                setSearchValue(e.target.value);
                setRowSelection({});
              }}
            />
          </div>
          {selectedIds.length > 0 && (
            <div className="bg-primary flex items-center gap-x-2 rounded-md p-1 px-2 text-xs text-white">
              <div className="lowercase">{`${selectedIds.length} ${t("common.contacts")} ${t("common.selected")}`}</div>
              <Button
                variant="secondary"
                size="sm"
                className="h-6 px-2"
                onClick={() => table.toggleAllPageRowsSelected(true)}>
                {t("common.select_all")}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                className="h-6 px-2"
                onClick={() => table.toggleAllPageRowsSelected(false)}>
                {t("common.clear_selection")}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                className="h-6 gap-1 px-2"
                onClick={() => setConfirmDelete(true)}>
                {t("common.delete")}
                <Trash2Icon className="size-3" />
              </Button>
            </div>
          )}
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon" aria-label={t("common.refresh")} onClick={() => void refresh()}>
            <RefreshCwIcon className={cn("size-4", contacts.isRefetching && "animate-spin")} />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label={t("common.table_settings")}
            onClick={() => setSettingsOpen(true)}>
            <SettingsIcon className="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label={isExpanded ? t("common.collapse_rows") : t("common.expand_rows")}
            aria-pressed={isExpanded}
            onClick={() => {
              writeStorage(expandStorageKey(workspaceId), JSON.stringify(!isExpanded));
              setIsExpanded(!isExpanded);
            }}>
            {isExpanded ? <ShrinkIcon className="size-4" /> : <ExpandIcon className="size-4" />}
          </Button>
        </div>
      </div>
      <div className="w-full overflow-x-auto rounded-xl border border-slate-200">
        <Table aria-label={t("common.contacts")} aria-busy={contacts.isPending || undefined}>
          <TableHeader>
            {table.getHeaderGroups().map((group) => (
              <TableRow key={group.id}>
                {group.headers.map((header) => (
                  <TableHead key={header.id} className={header.column.id === "select" ? "w-12" : undefined}>
                    {header.isPlaceholder
                      ? null
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows.map((row) => (
              <TableRow
                key={row.id}
                data-state={row.getIsSelected() ? "selected" : undefined}
                className="group cursor-pointer">
                {row.getVisibleCells().map((cell) => (
                  <TableCell
                    key={cell.id}
                    onClick={() => {
                      if (cell.column.id !== "select")
                        void navigate({ href: `/workspaces/${workspaceId}/contacts/${row.id}` });
                    }}
                    className={cn(
                      "border-slate-200 bg-white px-4 py-2 group-hover:bg-slate-100",
                      row.getIsSelected() && "bg-slate-100"
                    )}>
                    <div className={cn("flex flex-1 items-center truncate", isExpanded ? "h-10" : "h-full")}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </div>
                  </TableCell>
                ))}
              </TableRow>
            ))}
            {table.getRowModel().rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={columns.length} className="h-24 text-center">
                  <p className="text-slate-400">
                    {contacts.isPending
                      ? t("common.loading")
                      : contacts.isError
                        ? errorMessage(t, contacts.error)
                        : t("common.no_results")}
                  </p>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      {contacts.hasNextPage && rows.length > 0 && (
        <div className="mt-4 flex justify-center">
          <Button onClick={() => void contacts.fetchNextPage()} loading={contacts.isFetchingNextPage}>
            {t("common.load_more")}
          </Button>
        </div>
      )}
      <DeleteDialog
        open={confirmDelete}
        setOpen={setConfirmDelete}
        deleteWhat={t("common.count_contacts", { count: selectedIds.length })}
        onDelete={() => void deleteSelected()}
        isDeleting={remove.isPending}
        text={t("workspace.contacts.delete_contact_confirmation_with_quotas", { value: selectedIds.length })}
      />
      <Dialog open={settingsOpen} onOpenChange={setSettingsOpen} width="narrow">
        <DialogHeader>
          <DialogTitle>{t("common.table_settings")}</DialogTitle>
        </DialogHeader>
        <DialogBody>
          <ul className="space-y-2">
            {table
              .getAllLeafColumns()
              .filter((column) => column.id !== "select")
              .map((column) => (
                <li key={column.id} className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id={`column-${column.id}`}
                    className="size-4"
                    checked={column.getIsVisible()}
                    onChange={(e) => column.toggleVisibility(e.target.checked)}
                  />
                  <label htmlFor={`column-${column.id}`} className="text-sm text-slate-700">
                    {typeof column.columnDef.header === "string" ? column.columnDef.header : column.id}
                  </label>
                </li>
              ))}
          </ul>
        </DialogBody>
      </Dialog>
    </div>
  );
};
