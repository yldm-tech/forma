import { type InputHTMLAttributes, forwardRef } from "react";
import { cn } from "../../lib/cn";

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  isInvalid?: boolean;
}

export const inputClassName =
  "flex h-10 w-full rounded-md border border-slate-300 bg-transparent px-3 py-2 text-base text-slate-800 placeholder:text-slate-500 focus:border-ink focus:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 sm:text-sm";

export const Input = forwardRef<HTMLInputElement, InputProps>(({ className, isInvalid, ...props }, ref) => (
  <input
    ref={ref}
    aria-invalid={isInvalid || undefined}
    className={cn(inputClassName, className, isInvalid && "border border-red-500 focus:border-red-500")}
    {...props}
  />
));
Input.displayName = "Input";
