import type { ReactNode } from "react";
import { cn } from "../../lib/cn";
import { H4, Small } from "./typography";

export const PageContentWrapper = ({
  children,
  className,
  width = "full",
}: {
  children: ReactNode;
  className?: string;
  /** settings is the reading column every settings surface shares. */
  width?: "full" | "settings";
}) => <div className={cn("min-h-full space-y-6 p-6", width === "settings" && "max-w-settings", className)}>{children}</div>;

export const PageHeader = ({ pageTitle, cta, children }: { pageTitle: ReactNode; cta?: ReactNode; children?: ReactNode }) => (
  <div className="border-b border-slate-200">
    <div className="flex items-center justify-between gap-x-4 pb-4">
      <h1 className="text-3xl font-bold text-slate-800">{pageTitle}</h1>
      {cta}
    </div>
    {children}
  </div>
);

/**
 * forma2's SettingsCard. The body is padded by default; "flush" lets an edge-to-edge table meet the card's border.
 */
export const SettingsCard = ({
  title,
  description,
  children,
  bodyVariant = "padded",
  width = "settings",
  className,
  cta,
}: {
  title: string;
  description: string;
  children: ReactNode;
  bodyVariant?: "padded" | "flush";
  width?: "settings" | "full";
  className?: string;
  cta?: ReactNode;
}) => (
  <section
    aria-label={title}
    className={cn(
      "relative my-4 w-full rounded-xl border border-slate-200 bg-white py-4 text-left shadow-xs",
      width === "settings" && "max-w-4xl",
      bodyVariant === "flush" && "overflow-hidden",
      className
    )}>
    <div className="flex justify-between border-b border-slate-200 px-4 pb-4">
      <div>
        <H4 className="font-medium tracking-normal">{title}</H4>
        <Small color="muted" className="mt-1">
          {description}
        </Small>
      </div>
      {cta}
    </div>
    <div className={bodyVariant === "padded" ? "px-4 pt-4" : "-mb-4"}>{children}</div>
  </section>
);

/** One line of placeholder while a page's data loads. */
export const LoadingBlock = ({ className }: { className?: string }) => (
  <div aria-hidden="true" className={cn("h-8 w-80 animate-pulse rounded-full bg-slate-200", className)} />
);
