import {
  ArrowDownToLineIcon,
  Code2Icon,
  EyeIcon,
  LogOutIcon,
  MousePointerClickIcon,
  TimerIcon,
  ZapIcon,
} from "lucide-react";

const TRIGGER_ICONS = {
  code: Code2Icon,
  automatic: ZapIcon,
  click: MousePointerClickIcon,
  pageView: EyeIcon,
  exitIntent: LogOutIcon,
  fiftyPercentScroll: ArrowDownToLineIcon,
  pageDwell: TimerIcon,
} as const;

type TriggerIconKey = keyof typeof TRIGGER_ICONS;

const isTriggerIconKey = (value: string): value is TriggerIconKey => Object.hasOwn(TRIGGER_ICONS, value);

/** What an action's icon shows: code, automatic, or its no-code trigger kind (click, page view, exit intent, scroll, time on page). */
export const actionIconKey = (actionClass: {
  type: string;
  noCodeConfig?: { type: string } | null;
}): TriggerIconKey => {
  if (actionClass.type === "code" || actionClass.type === "automatic") return actionClass.type;
  const kind = actionClass.noCodeConfig?.type ?? "click";
  return isTriggerIconKey(kind) ? kind : "click";
};

/** The icon for what triggers an action, so a page view, a click and an exit intent look different in lists and details. */
export const ActionClassIcon = ({
  actionClass,
  className = "size-4",
}: {
  actionClass: { type: string; noCodeConfig?: { type: string } | null };
  className?: string;
}) => {
  const Icon = TRIGGER_ICONS[actionIconKey(actionClass)];
  return <Icon className={className} aria-hidden="true" />;
};
