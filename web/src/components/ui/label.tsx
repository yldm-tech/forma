import type { LabelHTMLAttributes } from "react";
import { cn } from "../../lib/cn";

export const Label = ({ className, ...props }: LabelHTMLAttributes<HTMLLabelElement>) => (
  <label
    className={cn(
      "text-sm leading-none font-medium text-slate-800 peer-disabled:cursor-not-allowed peer-disabled:opacity-70",
      className
    )}
    {...props}
  />
);
