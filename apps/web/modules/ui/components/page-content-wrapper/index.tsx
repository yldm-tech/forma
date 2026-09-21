import { cn } from "@/lib/cn";

/**
 * How wide the page's content column is allowed to grow:
 * - `full` — the default; the column fills the main area. For tables, grids and dashboards, which
 *   earn the extra room.
 * - `settings` — the reading column shared by every settings surface, matching the width
 *   SettingsCard caps itself at. Constraining it here rather than on each card is what keeps
 *   PageHeader's rule flush with the content below it.
 */
export type TPageWidth = "full" | "settings";

const WIDTH_CLASSES: Record<TPageWidth, string> = {
  full: "",
  settings: "max-w-settings",
};

interface PageContentWrapperProps {
  children: React.ReactNode;
  className?: string;
  width?: TPageWidth;
}

export const PageContentWrapper = ({
  children,
  className,
  width = "full",
}: Readonly<PageContentWrapperProps>) => {
  return <div className={cn("min-h-full space-y-6 p-6", WIDTH_CLASSES[width], className)}>{children}</div>;
};
