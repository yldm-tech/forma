import { CalendarClockIcon, CheckCircle2Icon, HelpCircleIcon, PauseCircleIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { Wordmark } from "../../components/ui/logo";
import type { TInactiveState } from "../lib/state";

const CREATE_YOUR_OWN_URL =
  "https://forma.yldm.ai?utm_source=forma-app&utm_medium=survey&utm_campaign=create_your_own_cta";
const POWERED_BY_URL =
  "https://forma.yldm.ai?utm_source=forma-app&utm_medium=survey&utm_campaign=powered_by_badge";

/** forma2's SurveyInactive: a paused, scheduled or closed survey, an invalid or expired link, or a contact who already answered. */
export const SurveyInactive = ({ status, isScheduled, surveyClosedMessage, workspace }: TInactiveState) => {
  const { t } = useTranslation();
  const icons = {
    paused: isScheduled ? <CalendarClockIcon className="size-20" /> : <PauseCircleIcon className="size-20" />,
    completed: <CheckCircle2Icon className="size-20" />,
    "link invalid": <HelpCircleIcon className="size-20" />,
    "response submitted": <CheckCircle2Icon className="size-20" />,
    "link expired": <CalendarClockIcon className="size-20" />,
  };
  const descriptions = {
    paused: isScheduled ? t("s.scheduled") : t("s.paused"),
    completed: t("s.completed"),
    "link invalid": t("s.link_invalid"),
    "response submitted": t("s.response_submitted"),
    "link expired": t("c.link_expired_description"),
  };
  const headings = {
    paused: isScheduled ? t("common.survey_scheduled") : t("s.paused_heading"),
    completed: t("s.completed_heading"),
    "link invalid": t("s.this_looks_fishy"),
    "response submitted": t("s.survey_already_answered_heading"),
    // forma2's personal link page passed c.link_expired as the closed message's heading.
    "link expired": t("c.link_expired"),
  };

  const title =
    status === "completed" && surveyClosedMessage ? surveyClosedMessage.heading : headings[status];
  const description =
    status === "completed" && surveyClosedMessage ? surveyClosedMessage.subheading : descriptions[status];
  const showCTA =
    status !== "link invalid" &&
    status !== "link expired" &&
    status !== "response submitted" &&
    ((status !== "paused" && status !== "completed") || workspace?.linkSurveyBranding || !workspace) &&
    !(status === "completed" && surveyClosedMessage);

  return (
    <main className="flex h-full flex-col items-center justify-between bg-linear-to-br from-slate-200 to-slate-50 px-4 py-8 text-center">
      <div className="my-auto flex flex-col items-center gap-y-3 text-slate-300">
        {icons[status]}
        <h1 className="text-4xl font-bold text-slate-800">{title}</h1>
        <p className="text-lg leading-10 text-slate-500">{description}</p>
        {showCTA && (
          <Button className="mt-2" asChild>
            <a href={CREATE_YOUR_OWN_URL}>{t("s.create_your_own")}</a>
          </Button>
        )}
      </div>
      {(!workspace || workspace.linkSurveyBranding) && (
        <div>
          <a
            href={POWERED_BY_URL}
            className="inline-flex rounded-md p-1 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-slate-900">
            <Wordmark className="text-2xl" />
          </a>
        </div>
      )}
    </main>
  );
};
