import type { HTMLAttributes, LiHTMLAttributes } from "react";
import { cn } from "../../lib/cn";

export const Breadcrumb = (props: HTMLAttributes<HTMLElement>) => <nav aria-label="breadcrumb" {...props} />;

export const BreadcrumbList = ({ className, ...props }: HTMLAttributes<HTMLOListElement>) => (
  <ol
    className={cn(
      "flex flex-wrap items-center gap-1.5 text-sm wrap-break-word text-slate-500 hover:text-slate-700",
      className
    )}
    {...props}
  />
);

export const BreadcrumbItem = ({
  className,
  isActive,
  ...props
}: LiHTMLAttributes<HTMLLIElement> & { isActive?: boolean }) => (
  <li
    className={cn(
      "inline-flex items-center gap-1.5 space-x-1 rounded-md px-1.5 py-1 hover:bg-white hover:outline-slate-300 hover:outline-solid",
      isActive && "bg-slate-100 outline-slate-300 outline-solid",
      className
    )}
    {...props}
  />
);
