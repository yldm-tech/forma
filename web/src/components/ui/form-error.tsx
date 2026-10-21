import type { ReactNode } from "react";
import { cn } from "../../lib/cn";

/** A field's validation message, announced when it appears. */
export const FormError = ({
  id,
  children,
  className,
}: {
  id?: string;
  children?: ReactNode;
  className?: string;
}) =>
  children ? (
    <p id={id} role="alert" className={cn("text-sm text-red-500", className)}>
      {children}
    </p>
  ) : null;
