import type { HTMLAttributes } from "react";
import { cn } from "../../lib/cn";

export const H4 = ({ className, children, ...props }: HTMLAttributes<HTMLHeadingElement>) => (
  <h4 className={cn("scroll-m-20 text-base tracking-tight text-slate-800", className)} {...props}>
    {children}
  </h4>
);

export const Small = ({
  className,
  color = "default",
  ...props
}: HTMLAttributes<HTMLParagraphElement> & { color?: "default" | "muted" }) => (
  <p
    className={cn(
      "text-sm leading-none",
      color === "muted" ? "text-slate-500" : "font-medium text-slate-800",
      className
    )}
    {...props}
  />
);

export const Muted = ({ className, ...props }: HTMLAttributes<HTMLSpanElement>) => (
  <span className={cn("text-sm text-slate-500", className)} {...props} />
);
