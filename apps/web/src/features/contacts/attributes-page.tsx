import { formatSnakeCaseToTitleCase, toSafeIdentifier } from "@forma/types/safe-identifier";
import type { TContactAttributeDataType } from "@forma/types/contact-attribute-key";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import {
  type ColumnDef,
  type RowSelectionState,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { CalendarIcon, ExpandIcon, HashIcon, PlusIcon, ShrinkIcon, TagIcon, Trash2Icon } from "lucide-react";
import { type FormEvent, type MouseEvent, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { isFromInteractiveElement } from "../../components/ui/data-table";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../components/ui/dialog";
import { FormError } from "../../components/ui/form-error";
import { HighlightedText } from "../../components/ui/highlighted-text";
import { IdBadge } from "../../components/ui/id-badge";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Select } from "../../components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../components/ui/table";
import { type AttributeKey, filterAttributeKeys, newAttributeKeyError } from "../../lib/attribute-keys";
import { cn } from "../../lib/cn";
import { errorMessage } from "../../lib/errors";
import { meQuery } from "../../lib/queries/auth";
import {
  attributeKeysQuery,
  segmentsQuery,
  useCreateAttributeKey,
  useDeleteAttributeKeys,
  useUpdateAttributeKey,
} from "../../lib/queries/contacts";
import { formatDateForDisplay, timeSince } from "../../lib/time";
import { toast } from "../../lib/toast";
import { attributeKeyUsage } from "./attribute-usage";
import { ContactsPageLayout } from "./contacts-layout";

const DATA_TYPE_ICONS = { date: CalendarIcon, number: HashIcon, string: TagIcon } as const;

/** forma2's attributes page: the workspace's attribute keys; custom ones can be edited and deleted, new ones created. */
export const AttributesPage = ({ workspaceId, isReadOnly }: { workspaceId: string; isReadOnly: boolean }) => {
  const { t } = useTranslation();
  return (
    <ContactsPageLayout
      pageTitle={t("common.contacts")}
      activeId="attributes"
      workspaceId={workspaceId}
      isReadOnly={isReadOnly}
      cta={<CreateAttributeButton workspaceId={workspaceId} />}>
      <AttributesTable workspaceId={workspaceId} isReadOnly={isReadOnly} />
    </ContactsPageLayout>
  );
};

const AttributesTable = ({ workspaceId, isReadOnly }: { workspaceId: string; isReadOnly: boolean }) => {
  const { t } = useTranslation();
  const { data: me } = useSuspenseQuery(meQuery);
  const keys = useQuery(attributeKeysQuery(workspaceId));
  const remove = useDeleteAttributeKeys(workspaceId);
  const [search, setSearch] = useState("");
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});
  const [isExpanded, setIsExpanded] = useState(false);
  const [editing, setEditing] = useState<AttributeKey | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleteAnyway, setDeleteAnyway] = useState(false);
  // Segments, private ones included, to warn before deleting a key their filters use.
  const segments = useQuery({ ...segmentsQuery(workspaceId), enabled: !isReadOnly });
  const rows = useMemo(() => filterAttributeKeys(keys.data ?? [], search), [keys.data, search]);

  const columns = useMemo<ColumnDef<AttributeKey>[]>(() => {
    const dataTypeLabel = (dataType: TContactAttributeDataType) =>
      dataType === "date" ? t("common.date") : dataType === "number" ? t("common.number") : t("common.text");
    const base: ColumnDef<AttributeKey>[] = [
      {
        id: "createdAt",
        header: t("common.created_at"),
        cell: ({ row }) =>
          formatDateForDisplay(row.original.createdAt, me.locale, {
            year: "numeric",
            month: "long",
            day: "numeric",
          }),
      },
      {
        id: "name",
        header: t("common.label"),
        cell: ({ row }) => {
          const label = (
            <HighlightedText value={row.original.name ?? row.original.key} searchValue={search} />
          );
          // An editable key's label is the button that opens it, so the keyboard can reach it; the row click stays as a mouse convenience.
          return !isReadOnly && row.original.type === "custom" ? (
            <button
              type="button"
              className="rounded-sm text-left hover:underline"
              onClick={() => setEditing(row.original)}>
              {label}
            </button>
          ) : (
            label
          );
        },
      },
      { id: "key", header: t("common.key"), cell: ({ row }) => <IdBadge id={row.original.key} /> },
      {
        id: "description",
        header: t("common.description"),
        cell: ({ row }) =>
          row.original.description ? (
            <div className={isExpanded ? "wrap-break-word whitespace-normal" : "truncate"}>
              <HighlightedText value={row.original.description} searchValue={search} />
            </div>
          ) : (
            <span className="text-slate-400">-</span>
          ),
      },
      {
        id: "dataType",
        header: t("workspace.contacts.data_type"),
        cell: ({ row }) => {
          const Icon = DATA_TYPE_ICONS[row.original.dataType];
          return (
            <div className="flex items-center gap-2">
              <Icon className="size-4 text-slate-500" />
              <Badge type="gray" size="normal" text={dataTypeLabel(row.original.dataType)} />
            </div>
          );
        },
      },
      {
        id: "updatedAt",
        header: t("common.updated_at"),
        cell: ({ row }) => timeSince(row.original.updatedAt, me.locale),
      },
    ];
    if (isReadOnly) return base;
    return [
      {
        id: "select",
        header: ({ table }) => (
          <input
            type="checkbox"
            aria-label={t("workspace.contacts.select_all_rows")}
            className="size-4 cursor-pointer"
            checked={table.getIsAllPageRowsSelected()}
            onChange={(e) => table.toggleAllPageRowsSelected(e.target.checked)}
          />
        ),
        // The default keys cannot be selected or deleted.
        cell: ({ row }) =>
          row.getCanSelect() ? (
            <input
              type="checkbox"
              aria-label={t("workspace.contacts.select_row_named", {
                name: row.original.name ?? row.original.key,
              })}
              className="size-4 cursor-pointer"
              checked={row.getIsSelected()}
              onChange={(e) => row.toggleSelected(e.target.checked)}
            />
          ) : null,
      },
      ...base,
    ];
  }, [t, me.locale, search, isExpanded, isReadOnly]);

  // The SPA is not built with the React Compiler, so nothing memoizes the table's functions.
  // oxlint-disable-next-line react/incompatible-library -- see above
  const table = useReactTable({
    data: rows,
    columns,
    getRowId: (row) => row.id,
    getCoreRowModel: getCoreRowModel(),
    enableRowSelection: (row) => !isReadOnly && row.original.type === "custom",
    state: { rowSelection },
    onRowSelectionChange: setRowSelection,
  });
  const selectedIds = Object.keys(rowSelection).filter((id) => rowSelection[id]);
  const usage = attributeKeyUsage(
    segments.data ?? [],
    (keys.data ?? []).filter((key) => selectedIds.includes(key.id)).map((key) => key.key)
  );
  const isUsed = usage.segments.length > 0 || usage.surveys.length > 0;

  const openDeleteDialog = (open: boolean) => {
    setDeleteAnyway(false);
    setConfirmDelete(open);
  };

  const deleteSelected = async () => {
    try {
      await remove.mutateAsync(selectedIds);
      toast.success(t("workspace.contacts.attributes_deleted_successfully", { count: selectedIds.length }));
      setRowSelection({});
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      openDeleteDialog(false);
    }
  };

  return (
    <div className="w-full">
      <div className="mb-4 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <div className="w-64">
            <Input
              type="search"
              aria-label={t("workspace.contacts.search_attribute_keys")}
              placeholder={t("workspace.contacts.search_attribute_keys")}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          {selectedIds.length > 0 && (
            <div className="bg-primary flex items-center gap-x-2 rounded-md p-1 px-2 text-xs text-white">
              <div>{t("workspace.contacts.attributes_selected", { count: selectedIds.length })}</div>
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
                onClick={() => openDeleteDialog(true)}>
                {t("common.delete")}
                <Trash2Icon className="size-3" />
              </Button>
            </div>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon"
          aria-label={isExpanded ? t("common.collapse_rows") : t("common.expand_rows")}
          aria-pressed={isExpanded}
          onClick={() => setIsExpanded(!isExpanded)}>
          {isExpanded ? <ShrinkIcon className="size-4" /> : <ExpandIcon className="size-4" />}
        </Button>
      </div>
      <div className="w-full overflow-x-auto rounded-xl border border-slate-200">
        <Table aria-label={t("common.attributes")} aria-busy={keys.isPending || undefined}>
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
            {table.getRowModel().rows.map((row) => {
              const isEditable = !isReadOnly && row.original.type === "custom";
              return (
                <TableRow key={row.id} className={cn(isEditable ? "group cursor-pointer" : "cursor-default")}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      onClick={(e: MouseEvent<HTMLTableCellElement>) => {
                        if (
                          cell.column.id === "select" ||
                          isFromInteractiveElement(e.target, e.currentTarget)
                        )
                          return;
                        if (isEditable) setEditing(row.original);
                      }}
                      className={cn(
                        "border-slate-200 bg-white px-4 py-2",
                        isEditable && "group-hover:bg-slate-100",
                        row.getIsSelected() && "bg-slate-100"
                      )}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              );
            })}
            {table.getRowModel().rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={columns.length} className="h-24 text-center text-slate-400">
                  {keys.isPending
                    ? t("common.loading")
                    : keys.isError
                      ? errorMessage(t, keys.error)
                      : t("common.no_results")}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      <DeleteDialog
        open={confirmDelete}
        setOpen={openDeleteDialog}
        deleteWhat={t("common.count_attributes", { count: selectedIds.length })}
        onDelete={() => void deleteSelected()}
        isDeleting={remove.isPending}
        disabled={segments.isPending || (isUsed && !deleteAnyway)}
        text={t("workspace.contacts.delete_attribute_confirmation", { value: selectedIds.length })}>
        {isUsed && (
          <AttributeUsageWarning
            usage={usage}
            count={selectedIds.length}
            deleteAnyway={deleteAnyway}
            setDeleteAnyway={setDeleteAnyway}
          />
        )}
      </DeleteDialog>
      {editing && (
        <EditAttributeModal
          key={editing.id}
          workspaceId={workspaceId}
          attribute={editing}
          close={() => setEditing(null)}
        />
      )}
    </div>
  );
};

/** What deleting keys breaks: the segments and surveys whose filters use them, and the confirmation it takes to go ahead. */
const AttributeUsageWarning = ({
  usage,
  count,
  deleteAnyway,
  setDeleteAnyway,
}: {
  usage: { segments: string[]; surveys: string[] };
  count: number;
  deleteAnyway: boolean;
  setDeleteAnyway: (value: boolean) => void;
}) => {
  const { t } = useTranslation();
  const list = (names: string[]) => (
    <ul className="mt-1 ml-4 list-disc">
      {names.map((name) => (
        <li key={name}>{name}</li>
      ))}
    </ul>
  );
  return (
    <Alert variant="warning" role="status" className="mt-3">
      <AlertTitle>{t("workspace.contacts.delete_attribute_in_use", { count })}</AlertTitle>
      <AlertDescription className="space-y-3 text-slate-800">
        {usage.segments.length > 0 && (
          <div>
            <p>{t("workspace.contacts.delete_attribute_used_by_segments")}</p>
            {list(usage.segments)}
          </div>
        )}
        {usage.surveys.length > 0 && (
          <div>
            <p>{t("workspace.contacts.delete_attribute_used_by_surveys")}</p>
            {list(usage.surveys)}
          </div>
        )}
        <label className="flex items-center gap-2 font-medium">
          <input
            type="checkbox"
            className="size-4"
            checked={deleteAnyway}
            onChange={(e) => setDeleteAnyway(e.target.checked)}
          />
          {t("workspace.contacts.delete_attribute_anyway")}
        </label>
      </AlertDescription>
    </Alert>
  );
};

/** forma2's CreateAttributeModal: the key follows the label until edited by hand. */
const CreateAttributeButton = ({ workspaceId }: { workspaceId: string }) => {
  const { t } = useTranslation();
  const create = useCreateAttributeKey(workspaceId);
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({
    key: "",
    name: "",
    description: "",
    dataType: "string" as TContactAttributeDataType,
  });
  const [keyError, setKeyError] = useState("");

  const close = () => {
    setForm({ key: "", name: "", description: "", dataType: "string" });
    setKeyError("");
    setOpen(false);
  };

  const changeName = (name: string) => {
    const previousAutoKey = toSafeIdentifier(form.name);
    const autoKey = toSafeIdentifier(name);
    const follows = !form.key || form.key === previousAutoKey;
    setForm((current) => ({ ...current, name, key: follows ? autoKey : current.key }));
    if (follows && keyError) setKeyError(autoKey ? newAttributeKeyError(autoKey, t) : "");
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const error = newAttributeKeyError(form.key, t);
    setKeyError(error);
    if (error) return;
    try {
      await create.mutateAsync({
        key: form.key,
        name: form.name || formatSnakeCaseToTitleCase(form.key),
        description: form.description || undefined,
        dataType: form.dataType,
      });
      toast.success(t("workspace.contacts.attribute_created_successfully"));
      close();
    } catch (err) {
      toast.error(errorMessage(t, err));
    }
  };

  return (
    <>
      <Button onClick={() => setOpen(true)} size="sm">
        {t("workspace.contacts.create_attribute")}
        <PlusIcon />
      </Button>
      <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : close())} width="narrow">
        <DialogHeader>
          <DialogTitle>{t("workspace.contacts.create_new_attribute")}</DialogTitle>
          <DialogDescription>{t("workspace.contacts.create_new_attribute_description")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit}>
          <DialogBody>
            <div className="flex flex-col gap-4">
              <div className="flex flex-col gap-2">
                <Label htmlFor="attribute-label">{t("workspace.contacts.attribute_label")}</Label>
                <Input
                  id="attribute-label"
                  value={form.name}
                  onChange={(e) => changeName(e.target.value)}
                  placeholder={t("workspace.contacts.attribute_label_placeholder")}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="attribute-key">{t("workspace.contacts.attribute_key")}</Label>
                <Input
                  id="attribute-key"
                  value={form.key}
                  isInvalid={!!keyError}
                  onChange={(e) => {
                    setForm((current) => ({ ...current, key: e.target.value }));
                    setKeyError(newAttributeKeyError(e.target.value, t));
                  }}
                  placeholder={t("workspace.contacts.attribute_key_placeholder")}
                />
                <FormError>{keyError}</FormError>
                <p className="text-xs text-slate-500">{t("workspace.contacts.attribute_key_hint")}</p>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="attribute-data-type">{t("workspace.contacts.data_type")}</Label>
                <Select
                  id="attribute-data-type"
                  value={form.dataType}
                  onChange={(e) =>
                    setForm((current) => ({
                      ...current,
                      dataType: e.target.value as TContactAttributeDataType,
                    }))
                  }>
                  <option value="string">{t("common.string")}</option>
                  <option value="number">{t("common.number")}</option>
                  <option value="date">{t("common.date")}</option>
                </Select>
                <p className="text-xs text-slate-500">{t("workspace.contacts.data_type_description")}</p>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="attribute-description">
                  {t("workspace.contacts.attribute_description")} ({t("common.optional")})
                </Label>
                <Input
                  id="attribute-description"
                  value={form.description}
                  onChange={(e) => setForm((current) => ({ ...current, description: e.target.value }))}
                  placeholder={t("workspace.contacts.attribute_description_placeholder")}
                />
              </div>
            </div>
          </DialogBody>
          <DialogFooter className="mt-4">
            <Button onClick={close} type="button" variant="secondary">
              {t("common.cancel")}
            </Button>
            <Button disabled={!form.key || !form.name || !!keyError} loading={create.isPending} type="submit">
              {t("workspace.contacts.create_attribute")}
            </Button>
          </DialogFooter>
        </form>
      </Dialog>
    </>
  );
};

/** forma2's EditAttributeModal: a custom key's label and description; the key and data type are fixed. */
const EditAttributeModal = ({
  workspaceId,
  attribute,
  close,
}: {
  workspaceId: string;
  attribute: AttributeKey;
  close: () => void;
}) => {
  const { t } = useTranslation();
  const update = useUpdateAttributeKey(workspaceId);
  const [name, setName] = useState(attribute.name ?? "");
  const [description, setDescription] = useState(attribute.description ?? "");
  const Icon = DATA_TYPE_ICONS[attribute.dataType];

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      await update.mutateAsync({
        id: attribute.id,
        name: name || undefined,
        description: description || undefined,
      });
      toast.success(t("workspace.contacts.attribute_updated_successfully"));
      close();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && close()} width="narrow">
      <DialogHeader>
        <DialogTitle>{t("workspace.contacts.edit_attribute")}</DialogTitle>
        <DialogDescription>{t("workspace.contacts.edit_attribute_description")}</DialogDescription>
      </DialogHeader>
      <form onSubmit={submit}>
        <DialogBody>
          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <Label htmlFor="edit-attribute-key">{t("workspace.contacts.attribute_key")}</Label>
              <Input id="edit-attribute-key" value={attribute.key} disabled className="bg-slate-50" />
              <p className="text-xs text-slate-500">
                {t("workspace.contacts.attribute_key_cannot_be_changed")}
              </p>
            </div>
            <div className="flex flex-col gap-2">
              <span className="text-sm font-medium text-slate-900">{t("workspace.contacts.data_type")}</span>
              <div className="flex h-10 items-center gap-2 rounded-md border border-slate-200 bg-slate-50 px-3">
                <Icon className="size-4" />
                <Badge
                  text={
                    attribute.dataType === "date"
                      ? t("common.date")
                      : attribute.dataType === "number"
                        ? t("common.number")
                        : t("common.string")
                  }
                  type="gray"
                  size="tiny"
                />
              </div>
              <p className="text-xs text-slate-500">{t("workspace.contacts.data_type_cannot_be_changed")}</p>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="edit-attribute-label">{t("workspace.contacts.attribute_label")}</Label>
              <Input
                id="edit-attribute-label"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t("workspace.contacts.attribute_label_placeholder")}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="edit-attribute-description">
                {t("workspace.contacts.attribute_description")} ({t("common.optional")})
              </Label>
              <Input
                id="edit-attribute-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t("workspace.contacts.attribute_description_placeholder")}
              />
            </div>
          </div>
        </DialogBody>
        <DialogFooter className="mt-4">
          <Button onClick={close} type="button" variant="secondary">
            {t("common.cancel")}
          </Button>
          <Button disabled={!name} loading={update.isPending} type="submit">
            {t("common.save")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
