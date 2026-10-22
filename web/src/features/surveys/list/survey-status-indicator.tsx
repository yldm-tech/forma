import { CalendarClockIcon, CheckIcon, type LucideIcon, PauseIcon, PencilIcon } from "lucide-react";
import type { SurveyStatus } from "./filters";

const IconIndicator = ({ className, Icon }: { className: string; Icon: LucideIcon }) => (
  <span className={className}>
    <Icon className="size-3 text-slate-600" />
  </span>
);

/** forma2's SurveyStatusIndicator: a pulsing dot while a survey collects responses, an icon for every other status. */
export const SurveyStatusIndicator = ({
  status,
  isScheduled = false,
}: {
  status: SurveyStatus;
  isScheduled?: boolean;
}) => {
  switch (status) {
    case "inProgress":
      return (
        <span className="relative flex size-3" aria-hidden="true">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-green-500 opacity-75" />
          <span className="relative inline-flex size-3 rounded-full bg-green-500" />
        </span>
      );
    case "paused":
      return (
        <IconIndicator
          className="flex rounded-full bg-slate-300 p-1"
          Icon={isScheduled ? CalendarClockIcon : PauseIcon}
        />
      );
    case "completed":
      return <IconIndicator className="flex rounded-full bg-slate-200 p-1" Icon={CheckIcon} />;
    case "draft":
      return <IconIndicator className="flex rounded-full bg-slate-300 p-1" Icon={PencilIcon} />;
  }
};
