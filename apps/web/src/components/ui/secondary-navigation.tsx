import { Link } from "@tanstack/react-router";
import { cn } from "../../lib/cn";

export interface SecondaryNavItem {
  id: string;
  label: string;
  href: string;
}

/** forma2's SecondaryNavigation (tabs variant): links side by side, the active one underlined. */
export const SecondaryNavigation = ({
  navigation,
  activeId,
  ariaLabel,
}: {
  navigation: SecondaryNavItem[];
  activeId: string;
  ariaLabel: string;
}) => (
  <nav className="flex h-10 w-full items-center gap-x-4" aria-label={ariaLabel}>
    {navigation.map((item) => {
      const isActive = item.id === activeId;
      return (
        <div key={item.id} className="group flex h-full flex-col truncate">
          <Link
            to={item.href}
            aria-current={isActive ? "page" : undefined}
            className={cn(
              isActive ? "font-semibold text-slate-900" : "text-slate-500 hover:text-slate-700",
              "flex h-full items-center px-3 text-sm font-medium"
            )}>
            {item.label}
          </Link>
          <div
            className={cn(
              "bottom-0 mt-auto h-[2px] w-full rounded-t-lg transition-all duration-150 ease-in-out",
              isActive ? "bg-brand-dark" : "bg-transparent group-hover:bg-slate-300"
            )}
          />
        </div>
      );
    })}
  </nav>
);
