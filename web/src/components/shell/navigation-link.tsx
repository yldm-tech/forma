import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { cn } from "../../lib/cn";
import { DisabledReason } from "../ui/popover";

/** One entry of a sidebar: active, plain, or disabled with the reason shown on click. Collapsed, only the icon shows and the label becomes its accessible name and tooltip. */
export const NavigationLink = ({
  href,
  isActive,
  isCollapsed = false,
  label,
  icon,
  disabledReason,
  indent = false,
}: {
  href: string;
  isActive: boolean;
  isCollapsed?: boolean;
  label: string;
  icon: ReactNode;
  disabledReason?: string;
  /** Settings entries sit deeper than main navigation entries. */
  indent?: boolean;
}) => {
  const padding = isCollapsed ? "ml-1 justify-center" : indent ? "pl-8" : "pl-5";
  if (disabledReason) {
    return (
      <li className={cn("mb-1 rounded-l-md border-r-4 border-transparent py-2 text-sm text-slate-400", padding)}>
        <DisabledReason reason={disabledReason}>
          <button
            type="button"
            aria-disabled="true"
            aria-label={isCollapsed ? label : undefined}
            className={cn("flex w-full cursor-not-allowed items-center", isCollapsed && "justify-center")}>
            {icon}
            {!isCollapsed && <span className="ml-2">{label}</span>}
          </button>
        </DisabledReason>
      </li>
    );
  }
  return (
    <li
      className={cn(
        "mb-1 rounded-l-md border-r-4 py-2 text-sm",
        padding,
        isActive
          ? "border-brand-dark bg-slate-50 font-semibold text-slate-900"
          : "border-transparent text-slate-600 transition-all duration-150 ease-in-out hover:border-slate-300 hover:bg-slate-50 hover:text-slate-900"
      )}>
      <Link
        to={href}
        title={isCollapsed ? label : undefined}
        aria-label={isCollapsed ? label : undefined}
        aria-current={isActive ? "page" : undefined}
        className={cn("flex items-center", isCollapsed && "justify-center")}>
        {icon}
        {!isCollapsed && <span className="ml-2">{label}</span>}
      </Link>
    </li>
  );
};
