"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useTranslation } from "react-i18next";
import { getSampleTruncation } from "../lib/sample-truncation";

interface SampleTruncationNoticeProps {
  sampleCount: number;
  responseCount: number;
}

/**
 * Renders "Shown: 50 / 312 responses" plus a way out to the Responses tab when a summary sample list
 * was capped server-side. Without it the card header advertises the full count while the list below
 * stops silently, so a truncated list reads as complete.
 *
 * The link is deliberately unfiltered: the Responses tab is the only view that holds every row.
 */
export const SampleTruncationNotice = ({
  sampleCount,
  responseCount,
}: Readonly<SampleTruncationNoticeProps>) => {
  const { t } = useTranslation();
  const params = useParams();

  const truncation = getSampleTruncation(sampleCount, responseCount);
  if (!truncation) return null;

  const workspaceId = typeof params?.workspaceId === "string" ? params.workspaceId : null;
  const surveyId = typeof params?.surveyId === "string" ? params.surveyId : null;
  const responsesHref =
    workspaceId && surveyId ? `/workspaces/${workspaceId}/surveys/${surveyId}/responses?referer=true` : null;

  return (
    <div className="flex flex-wrap items-center justify-center gap-x-2 gap-y-1 py-3 text-sm text-slate-500">
      <span>
        {t("common.shown")}: {truncation.shown} / {t("common.count_responses", { count: truncation.total })}
      </span>
      {responsesHref && (
        <Link href={responsesHref} className="font-medium text-slate-700 underline underline-offset-2">
          {t("common.responses")}
        </Link>
      )}
    </div>
  );
};
