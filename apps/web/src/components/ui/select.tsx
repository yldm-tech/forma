import { ChevronDownIcon } from "lucide-react";
import { type SelectHTMLAttributes, forwardRef } from "react";
import { cn } from "../../lib/cn";

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  /** Classes for the wrapper that holds the select and its chevron. It is full width by default; a select sized to a column (w-44 shrink-0) is sized here, so the chevron stays inside its box. */
  containerClassName?: string;
}

/** A native select styled like the form inputs, for single choices from a short or long list. */
export const Select = forwardRef<HTMLSelectElement, SelectProps>(
  ({ className, containerClassName, children, ...props }, ref) => (
    <div className={cn("relative w-full", containerClassName)}>
      <select
        ref={ref}
        className={cn(
          "flex h-10 w-full appearance-none rounded-md border border-slate-300 bg-white px-3 py-2 pr-9 text-sm text-slate-800 focus:border-brand-dark focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50",
          className
        )}
        {...props}>
        {children}
      </select>
      <ChevronDownIcon
        aria-hidden="true"
        className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 text-slate-500"
      />
    </div>
  )
);
Select.displayName = "Select";
