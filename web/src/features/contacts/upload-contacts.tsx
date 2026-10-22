import { isSafeIdentifier } from "@forma/types/safe-identifier";
import { ArrowUpFromLineIcon, ChevronDownIcon, FileUpIcon, PlusIcon } from "lucide-react";
import { type DragEvent, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert } from "../../components/ui/alert";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../components/ui/dialog";
import { Input } from "../../components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "../../components/ui/popover";
import { ApiError } from "../../lib/api";
import {
  type AttributeKey,
  RESERVED_ATTRIBUTE_KEYS_TEXT,
  attributeKeyLabel,
  isReservedAttributeKey,
} from "../../lib/attribute-keys";
import { cn } from "../../lib/cn";
import {
  type CsvContacts,
  type DuplicateContactsAction,
  EXAMPLE_CSV,
  MAX_CSV_BYTES,
  type TypeMismatch,
  attributeMapErrors,
  csvRecordsError,
  duplicateMappedColumns,
  isCsvFile,
  isNewAttributeTarget,
  mappableColumns,
  parseContactsCsv,
  suggestAttributeMap,
  typeMismatchMessage,
} from "../../lib/contacts-csv";
import { errorMessage } from "../../lib/errors";
import { useImportContacts } from "../../lib/queries/contacts";
import { toast } from "../../lib/toast";
import { TabToggle } from "../actions/action-form";

/** forma2's UploadContactsCSVButton: the button and the import dialog it opens. */
export const UploadContactsButton = ({
  workspaceId,
  attributeKeys,
}: {
  workspaceId: string;
  attributeKeys: AttributeKey[];
}) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button size="sm" onClick={() => setOpen(true)}>
        {t("common.upload")} CSV
        <PlusIcon />
      </Button>
      {open && (
        <UploadContactsDialog workspaceId={workspaceId} attributeKeys={attributeKeys} setOpen={setOpen} />
      )}
    </>
  );
};

interface PickedFile {
  file: File;
  parsed: CsvContacts;
}

const UploadContactsDialog = ({
  workspaceId,
  attributeKeys,
  setOpen,
}: {
  workspaceId: string;
  attributeKeys: AttributeKey[];
  setOpen: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const upload = useImportContacts(workspaceId);
  const [picked, setPicked] = useState<PickedFile | null>(null);
  const [attributeMap, setAttributeMap] = useState<Record<string, string>>({});
  const [duplicateAction, setDuplicateAction] = useState<DuplicateContactsAction>("skip");
  const [fileError, setFileError] = useState("");
  const [uploadError, setUploadError] = useState("");
  const columns = picked?.parsed.columns ?? [];
  const mappingErrors = picked ? attributeMapErrors(columns, attributeMap, attributeKeys, t) : [];
  const error = fileError || uploadError || mappingErrors.join("\n");

  const pickFile = async (file: File) => {
    setUploadError("");
    if (!isCsvFile(file)) {
      setFileError(t("workspace.contacts.upload_contacts_error_invalid_file_type"));
      return;
    }
    if (file.size > MAX_CSV_BYTES) {
      setFileError(t("workspace.contacts.upload_contacts_error_file_too_large"));
      return;
    }
    let parsed: CsvContacts;
    try {
      parsed = parseContactsCsv(await file.text());
    } catch (err) {
      setFileError(
        err instanceof Error ? err.message : t("workspace.contacts.upload_contacts_error_generic")
      );
      return;
    }
    const recordsError = csvRecordsError(parsed.records);
    if (recordsError) {
      setFileError(recordsError);
      return;
    }
    if (parsed.records.length === 0) {
      setFileError(t("workspace.contacts.upload_contacts_error_no_valid_contacts"));
      return;
    }
    setFileError("");
    setPicked({ file, parsed });
    setAttributeMap(suggestAttributeMap(parsed.columns, attributeKeys));
  };

  const reset = () => {
    setPicked(null);
    setAttributeMap({});
    setDuplicateAction("skip");
    setFileError("");
    setUploadError("");
  };

  const submit = async () => {
    if (!picked) return;
    setUploadError("");
    const duplicates = duplicateMappedColumns(attributeMap);
    if (duplicates.length > 0) {
      setUploadError(
        t("workspace.contacts.upload_contacts_error_duplicate_mappings", {
          attributes: duplicates.join(", "),
        })
      );
      return;
    }
    try {
      await upload.mutateAsync({ file: picked.file, duplicateContactsAction: duplicateAction, attributeMap });
      toast.success(t("workspace.contacts.upload_contacts_success"));
      setOpen(false);
    } catch (err) {
      const mismatches =
        err instanceof ApiError ? (err.details?.validationErrors as TypeMismatch[] | undefined) : undefined;
      setUploadError(
        mismatches
          ? mismatches.map((mismatch) => typeMismatchMessage(mismatch, t)).join("\n")
          : errorMessage(t, err)
      );
    }
  };

  const downloadExample = () => {
    const link = document.createElement("a");
    link.href = `data:text/csv;charset=utf-8,${encodeURIComponent(EXAMPLE_CSV)}`;
    link.download = "example.csv";
    document.body.appendChild(link);
    link.click();
    link.remove();
  };

  const onDrop = (e: DragEvent<HTMLLabelElement>) => {
    e.preventDefault();
    const file = e.dataTransfer.files[0];
    if (file) void pickFile(file);
  };

  return (
    <Dialog open onOpenChange={setOpen} disableCloseOnOutsideClick width="wide">
      <DialogHeader>
        <FileUpIcon />
        <DialogTitle>{t("common.upload")} CSV</DialogTitle>
        <DialogDescription>{t("workspace.contacts.upload_contacts_modal_description")}</DialogDescription>
      </DialogHeader>
      <DialogBody>
        <div className="flex flex-col gap-4">
          {error && (
            <Alert variant="error" className="whitespace-pre-line">
              {error}
            </Alert>
          )}
          <div className="flex flex-col gap-2">
            <div className="rounded-md border-2 border-dashed border-slate-300 bg-slate-50 p-4">
              {picked ? (
                <div className="flex flex-col items-center gap-4">
                  <h3 className="font-medium text-slate-500">
                    {t("workspace.contacts.upload_contacts_modal_preview")}
                  </h3>
                  <CsvPreview parsed={picked.parsed} />
                </div>
              ) : (
                <label
                  htmlFor="contacts-file"
                  className="relative flex cursor-pointer flex-col items-center justify-center rounded-lg hover:bg-slate-100"
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={onDrop}>
                  <div className="flex flex-col items-center justify-center pt-5 pb-6">
                    <ArrowUpFromLineIcon className="h-6 text-slate-500" />
                    <p className="mt-2 text-center text-sm font-semibold text-slate-500">
                      {t("common.upload_input_description")}
                    </p>
                    <input
                      type="file"
                      id="contacts-file"
                      accept=".csv"
                      className="hidden"
                      onChange={(e) => {
                        const file = e.target.files?.[0];
                        if (file) void pickFile(file);
                        e.target.value = "";
                      }}
                    />
                  </div>
                </label>
              )}
            </div>
            {!picked && (
              <div className="flex justify-start">
                <Button onClick={downloadExample} variant="secondary">
                  {t("workspace.contacts.upload_contacts_modal_download_example_csv")}
                </Button>
              </div>
            )}
          </div>
          {picked && (
            <div className="flex flex-col">
              <h3 className="font-medium text-slate-900">
                {t("workspace.contacts.upload_contacts_modal_attributes_title")}
              </h3>
              <p className="mb-2 text-slate-500">
                {t("workspace.contacts.upload_contacts_modal_attributes_description")}
              </p>
              <div className="grid grid-cols-[minmax(150px,1fr)_minmax(200px,2fr)] gap-x-4 gap-y-3">
                <div className="font-medium text-slate-900">
                  {t("workspace.contacts.upload_contacts_modal_csv_column_header")}
                </div>
                <div className="font-medium text-slate-900">
                  {t("workspace.contacts.upload_contacts_modal_attribute_header")}
                </div>
                {mappableColumns(columns, attributeKeys).map((column) => (
                  <ColumnMapping
                    key={column}
                    column={column}
                    target={attributeMap[column]}
                    attributeKeys={attributeKeys}
                    onChange={(target) => {
                      setUploadError("");
                      setAttributeMap((current) => ({ ...current, [column]: target }));
                    }}
                  />
                ))}
              </div>
            </div>
          )}
          <div className="flex flex-col gap-2">
            <h3 className="font-medium text-slate-900">
              {t("workspace.contacts.upload_contacts_modal_duplicates_title")}
            </h3>
            <p className="text-slate-500">
              {t("workspace.contacts.upload_contacts_modal_duplicates_description")}
            </p>
            <div className="max-w-[400px]">
              <TabToggle<DuplicateContactsAction>
                label={t("workspace.contacts.upload_contacts_modal_duplicates_title")}
                value={duplicateAction}
                onChange={setDuplicateAction}
                options={[
                  {
                    value: "skip",
                    label: t("workspace.contacts.upload_contacts_modal_duplicates_skip_title"),
                  },
                  {
                    value: "update",
                    label: t("workspace.contacts.upload_contacts_modal_duplicates_update_title"),
                  },
                  {
                    value: "overwrite",
                    label: t("workspace.contacts.upload_contacts_modal_duplicates_overwrite_title"),
                  },
                ]}
              />
            </div>
            <p className="text-xs font-medium text-slate-500">
              {duplicateAction === "skip" &&
                t("workspace.contacts.upload_contacts_modal_duplicates_skip_description")}
              {duplicateAction === "update" &&
                t("workspace.contacts.upload_contacts_modal_duplicates_update_description")}
              {duplicateAction === "overwrite" &&
                t("workspace.contacts.upload_contacts_modal_duplicates_overwrite_description")}
            </p>
          </div>
        </div>
      </DialogBody>
      <DialogFooter>
        {picked && (
          <Button variant="secondary" onClick={reset}>
            {t("workspace.contacts.upload_contacts_modal_pick_different_file")}
          </Button>
        )}
        <Button
          onClick={() => void submit()}
          loading={upload.isPending}
          disabled={upload.isPending || !picked || !!fileError || mappingErrors.length > 0}>
          {t("workspace.contacts.upload_contacts_modal_upload_btn")}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};

/** The first ten rows of the picked file. */
const CsvPreview = ({ parsed }: { parsed: CsvContacts }) => (
  <div className="max-h-[300px] w-full overflow-auto rounded-md border border-slate-300">
    <table className="w-full text-left text-xs">
      <thead className="bg-slate-100">
        <tr>
          {parsed.columns.map((column) => (
            <th key={column} className="px-2 py-1 font-medium text-slate-700">
              {column}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {parsed.records.slice(0, 10).map((record, i) => (
          // oxlint-disable-next-line react/no-array-index-key -- preview rows are positional
          <tr key={i} className="border-t border-slate-200 bg-white">
            {parsed.columns.map((column) => (
              <td key={column} className="px-2 py-1 text-slate-600">
                {record[column]}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  </div>
);

/** One row of the mapping step: the column and the key it fills, chosen among the workspace's keys or typed as a new one. */
const ColumnMapping = ({
  column,
  target,
  attributeKeys,
  onChange,
}: {
  column: string;
  target: string | undefined;
  attributeKeys: AttributeKey[];
  onChange: (target: string) => void;
}) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const query = search.trim();
  const current = attributeKeys.find((key) => key.id === target);
  const matches = useMemo(
    () => attributeKeys.filter((key) => attributeKeyLabel(key).toLowerCase().includes(search.toLowerCase())),
    [attributeKeys, search]
  );
  const canCreate =
    query !== "" && isSafeIdentifier(query) && !isReservedAttributeKey(query) && matches.length === 0;

  const choose = (value: string) => {
    onChange(value);
    setOpen(false);
    setSearch("");
  };

  return (
    <>
      <span className="overflow-hidden font-medium text-ellipsis text-slate-700">{column}</span>
      <div className="flex items-center gap-2">
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button
              variant="secondary"
              size="sm"
              className="w-60 justify-between"
              aria-label={`${column}: ${t("workspace.contacts.select_attribute")}`}>
              <span className="truncate">
                {current ? attributeKeyLabel(current) : target || t("workspace.contacts.select_attribute")}
              </span>
              <ChevronDownIcon className="size-4 shrink-0" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-60 p-1">
            <Input
              aria-label={t("workspace.contacts.upload_contacts_modal_attributes_search_or_add")}
              placeholder={t("workspace.contacts.upload_contacts_modal_attributes_search_or_add")}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="mb-1"
            />
            <ul className="max-h-48 overflow-y-auto">
              {matches.map((key) => (
                <li key={key.id}>
                  <button
                    type="button"
                    className={cn(
                      "w-full rounded-sm px-2 py-1.5 text-left text-sm text-slate-700 hover:bg-slate-100",
                      key.id === target && "font-semibold"
                    )}
                    onClick={() => choose(key.id)}>
                    {attributeKeyLabel(key)}
                  </button>
                </li>
              ))}
            </ul>
            {query !== "" && matches.length === 0 && (
              <div className="px-2 py-1 text-xs">
                {canCreate ? (
                  <button
                    type="button"
                    className="h-8 w-full text-left text-sm hover:bg-slate-50"
                    onClick={() => choose(query)}>
                    + {t("common.add")} {query}
                  </button>
                ) : isReservedAttributeKey(query) ? (
                  <span className="text-red-500">
                    {t("workspace.contacts.attribute_key_reserved_future_default", {
                      reservedKeys: RESERVED_ATTRIBUTE_KEYS_TEXT,
                    })}
                  </span>
                ) : (
                  <span className="text-red-500">
                    {t("workspace.contacts.attribute_key_safe_identifier_required")}
                  </span>
                )}
              </div>
            )}
          </PopoverContent>
        </Popover>
        {isNewAttributeTarget(target, attributeKeys) && (
          <Badge
            size="normal"
            type="success"
            className="rounded-md"
            text={t("workspace.contacts.upload_contacts_modal_attributes_new")}
          />
        )}
      </div>
    </>
  );
};
