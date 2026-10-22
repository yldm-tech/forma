import { getOriginalFileNameFromUrl } from "@forma/survey-ui/storage";
import type { TContactAttributes } from "@forma/types/contact-attribute";
import type { TResponseContact } from "@forma/types/responses";
import type {
  TSurvey,
  TSurveyElementSummaryAddress,
  TSurveyElementSummaryContactInfo,
  TSurveyElementSummaryDate,
  TSurveyElementSummaryFileUpload,
  TSurveyElementSummaryHiddenFields,
  TSurveyElementSummaryOpenText,
} from "@forma/types/surveys/types";
import { Link } from "@tanstack/react-router";
import { DownloadIcon, FileIcon, InboxIcon, MessageSquareTextIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "../../../../components/ui/avatar";
import { Button } from "../../../../components/ui/button";
import { EmptyState } from "../../../../components/ui/empty-state";
import { IdBadge } from "../../../../components/ui/id-badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../../../../components/ui/table";
import { formatStoredDateForDisplay } from "../../../../lib/survey/date-display";
import { timeSince } from "../../../../lib/time";
import { renderHyperlinkedContent } from "../../lib/response-display";
import { getContactIdentifier } from "../../lib/responses";
import { ArrayResponse } from "../response-values/response-values";
import { ResponseSampleModal } from "./response-sample-modal";
import { ElementSummaryHeader, SampleTruncationNotice } from "./summary-parts";

const PAGE = 10;

/** Who gave a sample answer: the contact with a link to them, or "Anonymous". */
export const SampleUser = ({
  workspaceId,
  contact,
  contactAttributes,
}: {
  workspaceId: string;
  contact: TResponseContact | null;
  contactAttributes: TContactAttributes | null;
}) => {
  const { t } = useTranslation();
  if (!contact) {
    return (
      <div className="group flex items-center">
        <div className="hidden md:flex">
          <PersonAvatar personId="anonymous" />
        </div>
        <p className="break-all text-slate-600 md:ml-2">{t("common.anonymous")}</p>
      </div>
    );
  }
  return (
    <Link
      className="ph-no-capture group flex items-center"
      to="/workspaces/$workspaceId/contacts/$contactId"
      params={{ workspaceId, contactId: contact.id }}
      onClick={(e) => e.stopPropagation()}>
      <div className="hidden md:flex">
        <PersonAvatar personId={contact.id} />
      </div>
      <p className="ph-no-capture break-all text-slate-600 group-hover:underline md:ml-2">
        {getContactIdentifier(contact, contactAttributes)}
      </p>
    </Link>
  );
};

interface Sample {
  updatedAt: Date;
  contact: TResponseContact | null;
  contactAttributes: TContactAttributes | null;
}

/** The user / response / time grid the sample cards share, ten rows at a time, with the truncation notice under the last page. */
const SampleGrid = <T extends Sample>({
  samples,
  responseCount,
  workspaceId,
  locale,
  paged,
  sampleKey,
  renderValue,
}: {
  samples: T[];
  responseCount: number;
  workspaceId: string;
  locale: string;
  paged: boolean;
  sampleKey: (sample: T, index: number) => string;
  renderValue: (sample: T) => ReactNode;
}) => {
  const { t } = useTranslation();
  const [visible, setVisible] = useState(PAGE);
  const shown = paged ? samples.slice(0, visible) : samples;
  return (
    <div>
      <div className="grid h-10 grid-cols-4 items-center border-y border-slate-200 bg-slate-100 text-sm font-bold text-slate-600">
        <div className="pl-4 md:pl-6">{t("common.user")}</div>
        <div className="col-span-2 pl-4 md:pl-6">{t("common.response")}</div>
        <div className="px-4 md:px-6">{t("common.time")}</div>
      </div>
      <div className="max-h-[62vh] w-full overflow-y-auto">
        {samples.length === 0 ? (
          <div className="p-8">
            <EmptyState text={t("workspace.surveys.summary.no_responses_found")} variant="simple" />
          </div>
        ) : (
          shown.map((sample, index) => (
            <div
              key={sampleKey(sample, index)}
              className="grid grid-cols-4 items-center border-b border-slate-100 py-2 text-sm text-slate-800 last:border-transparent md:text-base">
              <div className="pl-4 md:pl-6">
                <SampleUser
                  workspaceId={workspaceId}
                  contact={sample.contact}
                  contactAttributes={sample.contactAttributes}
                />
              </div>
              {renderValue(sample)}
              <div className="px-4 text-slate-500 md:px-6">
                {timeSince(sample.updatedAt.toISOString(), locale)}
              </div>
            </div>
          ))
        )}
      </div>
      {paged && visible < samples.length ? (
        <div className="flex justify-center py-4">
          <Button
            onClick={() => setVisible((current) => Math.min(current + PAGE, samples.length))}
            variant="secondary"
            size="sm">
            {t("common.load_more")}
          </Button>
        </div>
      ) : (
        <SampleTruncationNotice sampleCount={samples.length} responseCount={responseCount} />
      )}
    </div>
  );
};

/** forma2's AddressSummary and ContactInfoSummary: the answers' fields, one per line. */
export const ArraySamplesSummary = ({
  elementSummary,
  survey,
  locale,
}: {
  elementSummary: TSurveyElementSummaryAddress | TSurveyElementSummaryContactInfo;
  survey: TSurvey;
  locale: string;
}) => (
  <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
    <ElementSummaryHeader elementSummary={elementSummary} survey={survey} />
    <SampleGrid
      samples={elementSummary.samples}
      responseCount={elementSummary.responseCount}
      workspaceId={survey.workspaceId}
      locale={locale}
      paged={false}
      sampleKey={(sample) => sample.id}
      renderValue={(sample) => (
        <div className="ph-no-capture col-span-2 pl-6 font-semibold">
          <ArrayResponse value={sample.value} />
        </div>
      )}
    />
  </div>
);

/** forma2's DateElementSummary: the dates in the element's display format. */
export const DateElementSummary = ({
  elementSummary,
  survey,
  locale,
}: {
  elementSummary: TSurveyElementSummaryDate;
  survey: TSurvey;
  locale: string;
}) => {
  const { t } = useTranslation();
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader elementSummary={elementSummary} survey={survey} />
      <SampleGrid
        samples={elementSummary.samples}
        responseCount={elementSummary.responseCount}
        workspaceId={survey.workspaceId}
        locale={locale}
        paged
        sampleKey={(sample) => sample.id}
        renderValue={(sample) => (
          <div className="ph-no-capture col-span-2 pl-6 font-semibold whitespace-pre-wrap">
            {formatStoredDateForDisplay(sample.value, elementSummary.element.format, locale) ??
              t("common.invalid_date_with_value", { value: sample.value })}
          </div>
        )}
      />
    </div>
  );
};

/** forma2's FileUploadSummary: the uploaded files, each downloadable. */
export const FileUploadSummary = ({
  elementSummary,
  survey,
  locale,
}: {
  elementSummary: TSurveyElementSummaryFileUpload;
  survey: TSurvey;
  locale: string;
}) => {
  const { t } = useTranslation();
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader elementSummary={elementSummary} survey={survey} />
      <SampleGrid
        samples={elementSummary.files}
        responseCount={elementSummary.responseCount}
        workspaceId={survey.workspaceId}
        locale={locale}
        paged
        sampleKey={(sample) => sample.id}
        renderValue={(sample) => (
          <div className="col-span-2 grid">
            {sample.value.length > 0 ? (
              sample.value.map((fileUrl) => (
                <div className="relative m-2 rounded-lg bg-slate-200" key={fileUrl}>
                  <a
                    href={fileUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    aria-label={t("common.download")}>
                    <div className="absolute top-0 right-0 m-2">
                      <div className="flex size-8 items-center justify-center rounded-lg bg-slate-50 hover:bg-white">
                        <DownloadIcon className="h-6 text-slate-500" />
                      </div>
                    </div>
                  </a>
                  <div className="flex flex-col items-center justify-center p-2">
                    <FileIcon className="h-6 text-slate-500" />
                    <p className="mt-2 text-sm text-slate-500">{getOriginalFileNameFromUrl(fileUrl)}</p>
                  </div>
                </div>
              ))
            ) : (
              <div className="flex w-full flex-col items-center justify-center p-2">
                <p className="mt-2 text-sm font-semibold text-slate-500">{t("common.skipped")}</p>
              </div>
            )}
          </div>
        )}
      />
    </div>
  );
};

/** forma2's HiddenFieldsSummary: the values one ingested field took. */
export const HiddenFieldsSummary = ({
  elementSummary,
  workspaceId,
  locale,
}: {
  elementSummary: TSurveyElementSummaryHiddenFields;
  workspaceId: string;
  locale: string;
}) => {
  const { t } = useTranslation();
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <div className="space-y-2 px-4 pt-6 pb-5 md:px-6">
        <div className="align-center flex justify-between gap-4">
          <h3 className="pb-1 text-lg font-semibold text-slate-900 md:text-xl">{elementSummary.id}</h3>
        </div>
        <div className="flex gap-x-2 text-xs font-semibold text-slate-600 md:text-sm">
          <div className="flex items-center rounded-lg bg-slate-100 p-2">
            <MessageSquareTextIcon className="mr-2 size-4" />
            {t("common.hidden_field")}
          </div>
          <div className="flex items-center rounded-lg bg-slate-100 p-2">
            <InboxIcon className="mr-2 size-4" />
            {t("common.count_responses", { count: elementSummary.responseCount })}
          </div>
        </div>
      </div>
      <div className="rounded-b-lg bg-white">
        <SampleGrid
          samples={elementSummary.samples}
          responseCount={elementSummary.responseCount}
          workspaceId={workspaceId}
          locale={locale}
          paged
          sampleKey={(sample, index) => `${sample.value}-${index}`}
          renderValue={(sample) => (
            <div className="ph-no-capture col-span-2 pl-6 font-semibold whitespace-pre-wrap">
              {sample.value}
            </div>
          )}
        />
      </div>
    </div>
  );
};

/** forma2's OpenTextSummary: the text answers, ten at a time, each opening its response. */
export const OpenTextSummary = ({
  elementSummary,
  survey,
  locale,
  isReadOnly,
}: {
  elementSummary: TSurveyElementSummaryOpenText;
  survey: TSurvey;
  locale: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const [visibleResponses, setVisibleResponses] = useState(PAGE);
  const [selectedResponseId, setSelectedResponseId] = useState<string | null>(null);

  return (
    <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader elementSummary={elementSummary} survey={survey} />
      <div className="border-t border-slate-200" />
      {elementSummary.samples.length === 0 ? (
        <div className="p-8">
          <EmptyState text={t("workspace.surveys.summary.no_responses_found")} variant="simple" />
        </div>
      ) : (
        <div className="max-h-[40vh] overflow-y-auto">
          <Table>
            <TableHeader className="bg-slate-100">
              <TableRow>
                <TableHead className="w-1/5">{t("common.user")}</TableHead>
                <TableHead className="w-2/5">{t("common.response")}</TableHead>
                <TableHead className="w-1/5">{t("common.time")}</TableHead>
                <TableHead className="w-1/5">{t("common.response_id")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {elementSummary.samples.slice(0, visibleResponses).map((response) => (
                <TableRow
                  key={response.id}
                  role="button"
                  tabIndex={0}
                  aria-label={t("workspace.surveys.summary.open_response_details")}
                  className="cursor-pointer hover:bg-slate-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-slate-400"
                  onClick={() => setSelectedResponseId(response.id)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      setSelectedResponseId(response.id);
                    }
                  }}>
                  <TableCell className="w-1/5">
                    <SampleUser
                      workspaceId={survey.workspaceId}
                      contact={response.contact}
                      contactAttributes={response.contactAttributes}
                    />
                  </TableCell>
                  <TableCell className="w-2/5 font-medium">
                    {renderHyperlinkedContent(response.value)}
                  </TableCell>
                  <TableCell className="w-1/5">
                    {timeSince(response.updatedAt.toISOString(), locale)}
                  </TableCell>
                  <TableCell className="w-1/5" onClick={(e) => e.stopPropagation()}>
                    <IdBadge id={response.id} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {visibleResponses < elementSummary.samples.length ? (
            <div className="flex justify-center py-4">
              <Button
                onClick={() =>
                  setVisibleResponses((current) => Math.min(current + PAGE, elementSummary.samples.length))
                }
                variant="secondary"
                size="sm">
                {t("common.load_more")}
              </Button>
            </div>
          ) : (
            <SampleTruncationNotice
              sampleCount={elementSummary.samples.length}
              responseCount={elementSummary.responseCount}
            />
          )}
        </div>
      )}
      <ResponseSampleModal
        responseId={selectedResponseId}
        onClose={() => setSelectedResponseId(null)}
        survey={survey}
        isReadOnly={isReadOnly}
        locale={locale}
      />
    </div>
  );
};
