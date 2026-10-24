import { getComputedEmbeddedFields, getIngestedEmbeddedFields } from "@forma/types/embedded-data-resolver";
import type { TResponseTableData } from "@forma/types/responses";
import type { TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { EyeOffIcon, FileDigitIcon, FileType2Icon, MailIcon, TagIcon } from "lucide-react";
import type { ReactNode } from "react";
import { IdBadge } from "../../../../components/ui/id-badge";
import { cn } from "../../../../lib/cn";
import { getLocalizedValue } from "../../../../lib/survey/i18n";
import { recallToHeadline } from "../../../../lib/survey/recall";
import { formatDateForDisplay } from "../../../../lib/time";
import { getElementsFromBlocks } from "../../../survey-editor/lib/client-utils";
import { getElementIconMap } from "../../../survey-editor/lib/elements";
import {
  RESERVED_COLUMN_ENTRIES,
  getAddressFieldLabel,
  getContactInfoFieldLabel,
  getReservedColumnIcon,
  getReservedColumnLabel,
  reservedColumnId,
} from "../../lib/response-table";
import { extractChoiceIdsFromResponse, getContactIdentifier } from "../../lib/responses";
import { RenderResponse } from "../response-card/render-response";
import { ResponseBadges } from "../response-values/response-values";
import { getSelectionColumn } from "./data-table-parts";

const ADDRESS_FIELDS = ["addressLine1", "addressLine2", "city", "state", "zip", "country"];
const CONTACT_INFO_FIELDS = ["firstName", "lastName", "email", "phone", "company"];

const HeaderWithIcon = ({ icon, title }: { icon: ReactNode; title: string }) => (
  <div className="flex items-center justify-between">
    <div className="flex items-center gap-x-2 overflow-hidden">
      <span className="size-4">{icon}</span>
      <span className="truncate">{title}</span>
    </div>
  </div>
);

/** The table's date: short enough that the time fits the pinned column. */
const RESPONSE_DATE_OPTIONS: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "short",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
};

const TextCell = ({ value }: { value: unknown }) =>
  typeof value === "string" ? <p className="text-slate-900">{value}</p> : null;

/** The columns of one element: a matrix one per row, an address or contact info one per field, a choice element its answer and its option ids. The ids are the persisted column state's keys. */
const getElementColumns = (
  element: TSurveyElement,
  survey: TSurvey,
  isExpanded: boolean,
  locale: string,
  t: TFunction
): ColumnDef<TResponseTableData>[] => {
  const icons = getElementIconMap(t);
  const icon = (type: TSurveyElementTypeEnum) => icons[type];
  const headline = getTextContent(
    getLocalizedValue(recallToHeadline(element.headline, survey, false, "default"), "default")
  );
  const answerCell: ColumnDef<TResponseTableData>["cell"] = ({ row }) => (
    <RenderResponse
      element={element}
      survey={survey}
      responseData={row.original.responseData[element.id]}
      language={row.original.language}
      locale={locale}
      isExpanded={isExpanded}
      showId={false}
    />
  );

  switch (element.type) {
    case "matrix":
      return element.rows.map((matrixRow) => ({
        accessorKey: `ELEMENT_${element.id}_${matrixRow.label.default}`,
        header: () => (
          <HeaderWithIcon
            icon={icon(element.type)}
            title={`${getTextContent(getLocalizedValue(element.headline, "default"))} - ${getLocalizedValue(matrixRow.label, "default")}`}
          />
        ),
        cell: ({ row }) => <TextCell value={row.original.responseData[matrixRow.label.default ?? ""]} />,
      }));
    case "address":
      return ADDRESS_FIELDS.map((field) => ({
        accessorKey: `ELEMENT_${element.id}_${field}`,
        header: () => (
          <HeaderWithIcon icon={icon(element.type)} title={getAddressFieldLabel(field, t) ?? field} />
        ),
        cell: ({ row }) => <TextCell value={row.original.responseData[field]} />,
      }));
    case "contactInfo":
      return CONTACT_INFO_FIELDS.map((field) => ({
        accessorKey: `ELEMENT_${element.id}_${field}`,
        header: () => (
          <HeaderWithIcon icon={icon(element.type)} title={getContactInfoFieldLabel(field, t) ?? field} />
        ),
        cell: ({ row }) => <TextCell value={row.original.responseData[field]} />,
      }));
    case "multipleChoiceMulti":
    case "multipleChoiceSingle":
    case "ranking":
    case "pictureSelection":
      return [
        {
          accessorKey: `ELEMENT_${element.id}`,
          header: () => <HeaderWithIcon icon={icon(element.type)} title={headline} />,
          cell: answerCell,
        },
        {
          accessorKey: `ELEMENT_${element.id}optionIds`,
          header: () => (
            <HeaderWithIcon icon={icon(element.type)} title={`${headline} - ${t("common.option_id")}`} />
          ),
          cell: ({ row }) => {
            const value = row.original.responseData[element.id];
            if (typeof value !== "string" && !Array.isArray(value)) return null;
            const choiceIds = extractChoiceIdsFromResponse(
              value,
              element,
              row.original.language || undefined
            );
            if (choiceIds.length === 0) return null;
            return (
              <div className={cn("flex w-full gap-x-1", isExpanded && "flex-wrap gap-y-1")}>
                {choiceIds.map((choiceId, index) => (
                  // A ranking repeats no id, but an Other answer maps several labels to "other".
                  // oxlint-disable-next-line react/no-array-index-key -- see above
                  <IdBadge key={`${choiceId}-${index}`} id={choiceId} />
                ))}
              </div>
            );
          },
        },
      ];
    default:
      return [
        {
          accessorKey: `ELEMENT_${element.id}`,
          header: () => <HeaderWithIcon icon={icon(element.type)} title={headline} />,
          cell: answerCell,
        },
      ];
  }
};

/**
 * The response table's columns in their default order: selection (unless read-only), date, person, status, tags, quotas (when the survey has any), verified email (when the survey verifies), the elements' columns, the variables, the hidden fields, then the ids and the reserved fields. The answers come before the ids, so the first screen shows what people said. The order only applies to a survey whose table has no saved order; reconcileColumnOrder keeps a saved one.
 */
export const generateResponseTableColumns = (
  survey: TSurvey,
  isExpanded: boolean,
  isReadOnly: boolean,
  locale: string,
  t: TFunction,
  showQuotasColumn: boolean
): ColumnDef<TResponseTableData>[] => {
  const elementColumns = getElementsFromBlocks(survey.blocks).flatMap((element) =>
    getElementColumns(element, survey, isExpanded, locale, t)
  );

  const personColumn: ColumnDef<TResponseTableData> = {
    accessorKey: "personId",
    header: () => <div className="flex items-center gap-x-1.5">{t("common.person")}</div>,
    size: 275,
    cell: ({ row }) => (
      <p className="truncate text-slate-900">
        {row.original.person
          ? getContactIdentifier(row.original.person, row.original.contactAttributes)
          : t("common.anonymous")}
      </p>
    ),
  };

  const singleUseIdColumn: ColumnDef<TResponseTableData> = {
    accessorKey: "singleUseId",
    header: () => <div className="gap-x-1.5">{t("workspace.surveys.responses.single_use_id")}</div>,
    cell: ({ row }) => <p className="truncate text-slate-900">{row.original.singleUseId}</p>,
  };

  const responseIdColumn: ColumnDef<TResponseTableData> = {
    accessorKey: "responseId",
    header: () => <div className="gap-x-1.5">{t("common.response_id")}</div>,
    cell: ({ row }) => <IdBadge id={row.original.responseId} />,
  };

  // The date is the row's one keyboard stop that opens the response: the click reaches the cell, which opens it for every column.
  const dateColumn: ColumnDef<TResponseTableData> = {
    accessorKey: "createdAt",
    header: () => t("common.date"),
    size: 240,
    cell: ({ row }) => {
      const date = formatDateForDisplay(row.original.createdAt, locale, RESPONSE_DATE_OPTIONS);
      return (
        <button
          type="button"
          className="focus-visible:ring-ring cursor-pointer truncate rounded-sm text-left text-slate-900 hover:underline focus-visible:ring-2 focus-visible:outline-hidden"
          aria-label={t("workspace.surveys.responses.open_response_from", { date })}>
          {date}
        </button>
      );
    },
  };

  const quotasColumn: ColumnDef<TResponseTableData> = {
    accessorKey: "quota",
    header: t("common.quota"),
    size: 200,
    cell: ({ row }) => (
      <ResponseBadges items={(row.original.quotas ?? []).map((quota) => ({ value: quota }))} showId={false} />
    ),
  };

  const statusColumn: ColumnDef<TResponseTableData> = {
    accessorKey: "status",
    size: 200,
    header: () => <div className="gap-x-1.5">{t("common.status")}</div>,
    cell: ({ row }) => <ResponseBadges items={[{ value: row.original.status }]} showId={false} />,
  };

  const verifiedEmailColumn: ColumnDef<TResponseTableData> = {
    accessorKey: "verifiedEmail",
    header: () => (
      <HeaderWithIcon icon={<MailIcon className="size-4" />} title={t("common.verified_email")} />
    ),
  };

  const variableColumns: ColumnDef<TResponseTableData>[] = getComputedEmbeddedFields(survey).map(
    ({ field, link }) => ({
      accessorKey: `VARIABLE_${link.storageKey}`,
      header: () => (
        <HeaderWithIcon
          icon={
            field.dataType === "number" ? (
              <FileDigitIcon className="size-4" />
            ) : (
              <FileType2Icon className="size-4" />
            )
          }
          title={field.name}
        />
      ),
      cell: ({ row }) => {
        const value = row.original.variables[link.storageKey];
        return typeof value === "string" || typeof value === "number" ? (
          <div className="text-slate-900">{value}</div>
        ) : null;
      },
    })
  );

  const hiddenFieldColumns: ColumnDef<TResponseTableData>[] = getIngestedEmbeddedFields(survey).map(
    ({ field, link }) => ({
      accessorKey: `HIDDEN_FIELD_${link.storageKey}`,
      header: () => <HeaderWithIcon icon={<EyeOffIcon className="size-4" />} title={field.name} />,
      cell: ({ row }) => <TextCell value={row.original.responseData[link.storageKey]} />,
    })
  );

  const metadataColumns: ColumnDef<TResponseTableData>[] = RESERVED_COLUMN_ENTRIES.map((entry) => {
    const Icon = getReservedColumnIcon(entry.name);
    const columnId = reservedColumnId(entry.name);
    return {
      accessorKey: columnId,
      header: () => (
        <div className="flex items-center gap-x-2 overflow-hidden">
          <span className="size-4">{Icon && <Icon className="h-4 w-4" />}</span>
          <span className="truncate">{getReservedColumnLabel(entry.name, t)}</span>
        </div>
      ),
      cell: ({ row }) => {
        const value = row.original.reservedValues[columnId];
        return value ? <div className="truncate text-slate-900">{value}</div> : null;
      },
    };
  });

  const tagsColumn: ColumnDef<TResponseTableData> = {
    accessorKey: "tags",
    header: () => <div className="gap-x-1.5">{t("common.tags")}</div>,
    cell: ({ row }) => (
      <ResponseBadges
        items={row.original.tags.map((tag) => ({ value: tag.name }))}
        isExpanded={isExpanded}
        icon={<TagIcon className="size-4 text-slate-500" />}
        showId={false}
      />
    ),
  };

  const baseColumns = [
    dateColumn,
    personColumn,
    statusColumn,
    tagsColumn,
    ...(showQuotasColumn ? [quotasColumn] : []),
    ...(survey.isVerifyEmailEnabled ? [verifiedEmailColumn] : []),
    ...elementColumns,
    ...variableColumns,
    ...hiddenFieldColumns,
    singleUseIdColumn,
    responseIdColumn,
    ...metadataColumns,
  ];

  return isReadOnly
    ? baseColumns
    : [getSelectionColumn<TResponseTableData>(t("common.select_all"), t("common.select")), ...baseColumns];
};

/** The columns the table starts hidden, among columnIds: the single-use id unless the survey uses single-use links (it is empty for every other response), and the option-id twin of each choice question, which repeats its answer as ids. seedColumnVisibility applies them only to columns an author has never shown or hidden. */
export const getDefaultHiddenColumnIds = (survey: TSurvey, columnIds: readonly string[]): string[] =>
  columnIds.filter(
    (id) =>
      (id === "singleUseId" && !survey.singleUse?.enabled) || (id.startsWith("ELEMENT_") && id.endsWith("optionIds"))
  );
