import { ChevronDownIcon } from "lucide-react";
import { type SelectHTMLAttributes, forwardRef } from "react";
import { cn } from "../../lib/cn";

/** A native select styled like the form inputs, for single choices from a short or long list. */
export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(
  ({ className, children, ...props }, ref) => (
    <div className="relative w-full">
      <select
        ref={ref}
        className={cn(
          "flex h-10 w-full appearance-none rounded-md border border-slate-300 bg-white px-3 py-2 pr-9 text-sm text-slate-800 focus:border-brand-dark focus:ring-2 focus:ring-slate-400 focus:ring-offset-2 focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50",
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
