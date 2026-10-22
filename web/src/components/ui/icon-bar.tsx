import type { LucideIcon } from "lucide-react";
import { Button } from "./button";
import { TooltipRenderer } from "./tooltip";

export interface IconAction {
  icon: LucideIcon;
  tooltip: string;
  onClick?: () => void;
  isVisible?: boolean;
  disabled?: boolean;
  isLoading?: boolean;
}

/** forma2's IconBar: a row of icon buttons, each named by its tooltip; hidden actions are left out. */
export const IconBar = ({ actions, ariaLabel }: { actions: IconAction[]; ariaLabel: string }) => {
  const visibleActions = actions.filter((action) => action.isVisible);
  if (visibleActions.length === 0) return null;

  return (
    <div
      className="flex items-center justify-center divide-x rounded-md border border-slate-300 bg-white"
      role="toolbar"
      aria-label={ariaLabel}>
      {visibleActions.map((action) => (
        <span key={action.tooltip}>
          <TooltipRenderer tooltipContent={action.tooltip}>
            <Button
              variant="ghost"
              className="border-none hover:bg-slate-50"
              size="icon"
              onClick={action.onClick}
              disabled={action.disabled}
              loading={action.isLoading}
              aria-label={action.tooltip}>
              <action.icon />
            </Button>
          </TooltipRenderer>
        </span>
      ))}
    </div>
  );
};
