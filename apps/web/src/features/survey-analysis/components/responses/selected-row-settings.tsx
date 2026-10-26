import type { TResponseFilterCriteria, TResponseTableData } from "@forma/types/responses";
import type { TTag } from "@forma/types/tags";
import type { Table } from "@tanstack/react-table";
import { ArrowDownToLineIcon, Loader2Icon, TagIcon, TagsIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { DeleteDialog } from "../../../../components/ui/delete-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../../components/ui/dropdown-menu";
import { ApiError } from "../../../../lib/api";
import { errorMessage } from "../../../../lib/errors";
import { TAG_NAME_ALREADY_EXISTS } from "../../../../lib/queries/tags";
import { toast } from "../../../../lib/toast";
import {
  BULK_TAG_MAX_RESPONSE_IDS,
  type BulkTagOperation,
  type BulkTagResult,
  type ExportFormat,
} from "../../api";
import { DecrementQuotasCheckbox } from "../response-values/response-values";
import { BulkTagPicker } from "./bulk-tag-picker";

/** forma2's bulk delete runs the single delete five at a time. */
export const BULK_DELETE_CHUNK_SIZE = 5;

/** Runs the deletes in chunks of BULK_DELETE_CHUNK_SIZE, each chunk in parallel; the first failure stops the rest. */
export const deleteInChunks = async (
  ids: string[],
  deleteOne: (id: string) => Promise<unknown>
): Promise<void> => {
  for (let i = 0; i < ids.length; i += BULK_DELETE_CHUNK_SIZE) {
    await Promise.all(ids.slice(i, i + BULK_DELETE_CHUNK_SIZE).map(deleteOne));
  }
};

/** What a bulk action covers: the loaded rows the member ticked, or every response matching the view's filter ("select all N matching"), loaded or not. */
export type ResponseSelection =
  | { kind: "rows"; responseIds: string[] }
  | { kind: "allMatching"; criteria: TResponseFilterCriteria; count: number };

/** The criteria of the requests one bulk tag change makes: the view's filter once, or the ticked rows' ids in chunks the server accepts. */
export const bulkTagCriteria = (selection: ResponseSelection): TResponseFilterCriteria[] => {
  if (selection.kind === "allMatching") return [selection.criteria];
  const chunks: TResponseFilterCriteria[] = [];
  for (let i = 0; i < selection.responseIds.length; i += BULK_TAG_MAX_RESPONSE_IDS) {
    chunks.push({ responseIds: selection.responseIds.slice(i, i + BULK_TAG_MAX_RESPONSE_IDS) });
  }
  return chunks;
};

/** Sends the bulk tag change one request after another and resolves to how many responses changed; the first failure stops the rest, the earlier chunks staying applied. */
export const changeTagInChunks = async (
  selection: ResponseSelection,
  change: (criteria: TResponseFilterCriteria) => Promise<BulkTagResult>
): Promise<number> => {
  let changed = 0;
  for (const criteria of bulkTagCriteria(selection)) {
    changed += (await change(criteria)).changed;
  }
  return changed;
};

/** The tags the ticked rows carry, each once, by name: what "Remove tag" offers for a selection of rows. */
export const tagsOfRows = (rows: TResponseTableData[]): TTag[] => {
  const byId = new Map<string, TTag>();
  for (const row of rows) for (const tag of row.tags) byId.set(tag.id, tag);
  return [...byId.values()].sort((a, b) => a.name.localeCompare(b.name));
};

const Separator = () => <div aria-hidden="true">|</div>;

// The toolbar sits on the primary colour, so its buttons are ghost buttons in white; the enabled:hover: prefix lets them replace the variant's hover colours.
const TOOLBAR_BUTTON = "h-6 px-2 text-white enabled:hover:bg-white/15 enabled:hover:text-white";

/** forma2's SelectedRowSettings for responses: the selection's count, select all or none, add or remove a tag, download and delete. Past the loaded rows, "Select all N matching" widens the selection to every response matching the view's filter; tagging and download then cover all of them, and delete, which runs per response, is not offered. */
export const SelectedRowSettings = ({
  table,
  matchingCount,
  allMatching,
  filterCriteria,
  onSelectAllMatching,
  environmentTags,
  onDeleted,
  deleteResponse,
  downloadResponses,
  changeTag,
  createTag,
  onTagsChanged,
}: {
  table: Table<TResponseTableData>;
  /** How many responses match the view's filter, when known. */
  matchingCount: number | undefined;
  /** Whether the selection is every response matching the filter rather than the ticked rows. */
  allMatching: boolean;
  filterCriteria: TResponseFilterCriteria;
  onSelectAllMatching: () => void;
  environmentTags: TTag[];
  onDeleted: (responseIds: string[]) => void;
  deleteResponse: (responseId: string, decrementQuotas: boolean) => Promise<unknown>;
  downloadResponses: (criteria: TResponseFilterCriteria, format: ExportFormat) => Promise<void>;
  changeTag: (
    operation: BulkTagOperation,
    tagId: string,
    criteria: TResponseFilterCriteria
  ) => Promise<BulkTagResult>;
  createTag: (name: string) => Promise<TTag>;
  /** Called after a bulk tag change, applied in full or in part, to read the responses and the tags again. */
  onTagsChanged: () => void;
}) => {
  const { t } = useTranslation();
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isDownloading, setIsDownloading] = useState(false);
  const [isTagging, setIsTagging] = useState(false);
  const selectedRows = table.getFilteredSelectedRowModel().rows;
  const selectedRowCount = selectedRows.length;
  const loadedRowCount = table.getRowModel().rows.length;
  const hasQuotas = selectedRows.some((row) => (row.original.quotas?.length ?? 0) > 0);
  // Checked by default when the selection holds a response with quotas, until the member decides.
  const [decrementChoice, setDecrementQuotas] = useState<boolean | null>(null);
  const decrementQuotas = decrementChoice ?? hasQuotas;
  const isAllMatching = allMatching && matchingCount !== undefined;
  const selection: ResponseSelection = isAllMatching
    ? { kind: "allMatching", criteria: filterCriteria, count: matchingCount }
    : { kind: "rows", responseIds: selectedRows.map((row) => row.id) };
  const canSelectAllMatching =
    !isAllMatching && matchingCount !== undefined && matchingCount > loadedRowCount;
  const removableTags = isAllMatching ? environmentTags : tagsOfRows(selectedRows.map((row) => row.original));

  const handleDelete = async () => {
    setIsDeleting(true);
    const ids = selectedRows.map((row) => row.id);
    try {
      await deleteInChunks(ids, (id) => deleteResponse(id, decrementQuotas));
      onDeleted(ids);
      toast.success(t("workspace.surveys.responses.responses_deleted", { count: ids.length }));
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsDeleting(false);
      setIsDeleteDialogOpen(false);
    }
  };

  const handleDownload = async (format: ExportFormat) => {
    setIsDownloading(true);
    try {
      await downloadResponses(
        selection.kind === "allMatching" ? selection.criteria : { responseIds: selection.responseIds },
        format
      );
    } finally {
      setIsDownloading(false);
    }
  };

  const handleTag = async (operation: BulkTagOperation, tag: TTag) => {
    setIsTagging(true);
    try {
      const changed = await changeTagInChunks(selection, (criteria) =>
        changeTag(operation, tag.id, criteria)
      );
      toast.success(
        operation === "add"
          ? t("workspace.surveys.responses.bulk.tag_added", { tag: tag.name, count: changed })
          : t("workspace.surveys.responses.bulk.tag_removed", { tag: tag.name, count: changed })
      );
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsTagging(false);
      onTagsChanged();
    }
  };

  const handleCreateTag = async (name: string) => {
    let tag: TTag;
    try {
      tag = await createTag(name);
    } catch (error) {
      toast.error(
        error instanceof ApiError && error.code === TAG_NAME_ALREADY_EXISTS
          ? t("workspace.surveys.responses.tag_already_exists")
          : t("workspace.surveys.responses.an_error_occurred_creating_the_tag")
      );
      return;
    }
    await handleTag("add", tag);
  };

  return (
    <>
      <div className="bg-primary flex flex-wrap items-center gap-x-2 rounded-md p-1 px-2 text-xs text-white">
        <div aria-live="polite">
          {isAllMatching
            ? t("workspace.surveys.responses.bulk.all_matching_selected", { count: matchingCount })
            : t("workspace.surveys.responses.selected_count", { count: selectedRowCount })}
        </div>
        <Separator />
        <Button
          variant="ghost"
          size="sm"
          className={TOOLBAR_BUTTON}
          onClick={() => table.toggleAllPageRowsSelected(true)}>
          {t("common.select_all")}
        </Button>
        {canSelectAllMatching && (
          <Button variant="ghost" size="sm" className={TOOLBAR_BUTTON} onClick={onSelectAllMatching}>
            {t("workspace.surveys.responses.bulk.select_all_matching", { count: matchingCount })}
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          className={TOOLBAR_BUTTON}
          onClick={() => table.toggleAllPageRowsSelected(false)}>
          {t("common.clear_selection")}
        </Button>
        <Separator />
        <BulkTagPicker
          label={t("workspace.surveys.responses.bulk.add_tag")}
          icon={isTagging ? <Loader2Icon className="size-4 animate-spin" /> : <TagIcon />}
          tags={environmentTags}
          onPick={(tag) => void handleTag("add", tag)}
          onCreate={(name) => void handleCreateTag(name)}
          emptyText={t("workspace.surveys.responses.bulk.type_to_create_tag")}
          disabled={isTagging}
          className={TOOLBAR_BUTTON}
        />
        <BulkTagPicker
          label={t("workspace.surveys.responses.bulk.remove_tag")}
          icon={<TagsIcon />}
          tags={removableTags}
          onPick={(tag) => void handleTag("remove", tag)}
          emptyText={t("workspace.surveys.responses.bulk.no_tags_to_remove")}
          disabled={isTagging}
          className={TOOLBAR_BUTTON}
        />
        <Separator />
        <DropdownMenu>
          <DropdownMenuTrigger asChild disabled={isDownloading}>
            <Button variant="ghost" size="sm" className={`${TOOLBAR_BUTTON} gap-1`}>
              {isDownloading ? <Loader2Icon className="size-4 animate-spin" /> : <ArrowDownToLineIcon />}
              {t("common.download")}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem onClick={() => void handleDownload("csv")}>
              <p className="text-slate-700">{t("workspace.surveys.summary.selected_responses_csv")}</p>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => void handleDownload("xlsx")}>
              <p>{t("workspace.surveys.summary.selected_responses_excel")}</p>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        {!isAllMatching && (
          <Button
            variant="secondary"
            size="sm"
            className="h-6 gap-1 px-2"
            onClick={() => setIsDeleteDialogOpen(true)}>
            {t("common.delete")}
            <Trash2Icon />
          </Button>
        )}
      </div>
      <DeleteDialog
        open={isDeleteDialogOpen}
        setOpen={setIsDeleteDialogOpen}
        deleteWhat={t("common.count_responses", { count: selectedRowCount })}
        onDelete={() => void handleDelete()}
        isDeleting={isDeleting}
        text={t("workspace.surveys.responses.delete_responses_confirmation", { count: selectedRowCount })}>
        {hasQuotas && (
          <DecrementQuotasCheckbox
            title={t("workspace.surveys.responses.bulk_delete_response_quotas")}
            checked={decrementQuotas}
            onCheckedChange={setDecrementQuotas}
          />
        )}
      </DeleteDialog>
    </>
  );
};
