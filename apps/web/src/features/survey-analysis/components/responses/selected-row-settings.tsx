import type { TResponseTableData } from "@forma/types/responses";
import type { Table } from "@tanstack/react-table";
import { ArrowDownToLineIcon, Loader2Icon, Trash2Icon } from "lucide-react";
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
import { errorMessage } from "../../../../lib/errors";
import { toast } from "../../../../lib/toast";
import type { ExportFormat } from "../../api";
import { DecrementQuotasCheckbox } from "../response-values/response-values";

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

const Separator = () => <div aria-hidden="true">|</div>;

/** forma2's SelectedRowSettings for responses: the selection's count, select all or none, download and delete. */
export const SelectedRowSettings = ({
  table,
  onDeleted,
  deleteResponse,
  downloadResponses,
}: {
  table: Table<TResponseTableData>;
  onDeleted: (responseIds: string[]) => void;
  deleteResponse: (responseId: string, decrementQuotas: boolean) => Promise<unknown>;
  downloadResponses: (responseIds: string[], format: ExportFormat) => Promise<void>;
}) => {
  const { t } = useTranslation();
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isDownloading, setIsDownloading] = useState(false);
  const selectedRows = table.getFilteredSelectedRowModel().rows;
  const selectedRowCount = selectedRows.length;
  const hasQuotas = selectedRows.some((row) => (row.original.quotas?.length ?? 0) > 0);
  // Checked by default when the selection holds a response with quotas, until the member decides.
  const [decrementChoice, setDecrementQuotas] = useState<boolean | null>(null);
  const decrementQuotas = decrementChoice ?? hasQuotas;

  const handleDelete = async () => {
    setIsDeleting(true);
    const ids = selectedRows.map((row) => row.id);
    try {
      await deleteInChunks(ids, (id) => deleteResponse(id, decrementQuotas));
      onDeleted(ids);
      toast.success(t("common.table_items_deleted_successfully", { type: "Response" }));
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
        selectedRows.map((row) => row.id),
        format
      );
    } finally {
      setIsDownloading(false);
    }
  };

  return (
    <>
      <div className="bg-primary flex items-center gap-x-2 rounded-md p-1 px-2 text-xs text-white">
        <div className="lowercase">{`${selectedRowCount} ${t("common.responses")} ${t("common.selected")}`}</div>
        <Separator />
        <Button
          variant="outline"
          size="sm"
          className="h-6 border-none px-2"
          onClick={() => table.toggleAllPageRowsSelected(true)}>
          {t("common.select_all")}
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="h-6 border-none px-2"
          onClick={() => table.toggleAllPageRowsSelected(false)}>
          {t("common.clear_selection")}
        </Button>
        <Separator />
        <DropdownMenu>
          <DropdownMenuTrigger asChild disabled={isDownloading}>
            <Button variant="outline" size="sm" className="h-6 gap-1 border-none px-2">
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
        <Button
          variant="secondary"
          size="sm"
          className="h-6 gap-1 px-2"
          onClick={() => setIsDeleteDialogOpen(true)}>
          {t("common.delete")}
          <Trash2Icon />
        </Button>
      </div>
      <DeleteDialog
        open={isDeleteDialogOpen}
        setOpen={setIsDeleteDialogOpen}
        deleteWhat={t("common.count_responses", { count: selectedRowCount })}
        onDelete={() => void handleDelete()}
        isDeleting={isDeleting}
        text={t("workspace.surveys.responses.delete_response_confirmation")}>
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
