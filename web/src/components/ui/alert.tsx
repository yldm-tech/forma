import { type VariantProps, cva } from "class-variance-authority";
import { AlertCircleIcon, AlertTriangleIcon, CheckCircle2Icon, InfoIcon } from "lucide-react";
import type { HTMLAttributes } from "react";
import { cn } from "../../lib/cn";

const alertVariants = cva(
  "relative w-full rounded-lg border bg-white py-3 px-4 text-sm grid grid-cols-[2fr_auto] grid-rows-[auto_auto] gap-y-0.5 gap-x-3 [&>svg]:absolute [&>svg]:left-4 [&>svg]:top-4 [&>svg]:size-4 [&>svg~*]:pl-7",
  {
    variants: {
      variant: {
        default: "text-slate-900 border-slate-200",
        error: "text-error-foreground [&>svg]:text-error border-error/50",
        warning: "text-warning-foreground [&>svg]:text-warning border-warning/50",
        info: "text-info-foreground [&>svg]:text-info border-info/50",
        success: "text-success-foreground [&>svg]:text-success border-success/50",
      },
    },
    defaultVariants: { variant: "default" },
  }
);

const ICONS = {
  default: null,
  error: <AlertCircleIcon />,
  warning: <AlertTriangleIcon />,
  info: <InfoIcon />,
  success: <CheckCircle2Icon />,
};

/**
 * forma2's Alert. role="alert" is an assertive live region for messages that appear in response to something; a banner already on the page when it renders takes role="status".
 */
export const Alert = ({
  className,
  variant,
  role = "alert",
  children,
  ...props
}: HTMLAttributes<HTMLDivElement> & VariantProps<typeof alertVariants>) => (
  <div role={role} className={cn(alertVariants({ variant }), className)} {...props}>
    {ICONS[variant ?? "default"]}
    {children}
  </div>
);

export const AlertTitle = ({ className, children, ...props }: HTMLAttributes<HTMLHeadingElement>) => (
  <h5 className={cn("col-start-1 row-start-1 font-medium tracking-tight", className)} {...props}>
    {children}
  </h5>
);

export const AlertDescription = ({ className, ...props }: HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("col-start-1 row-start-2 [&_p]:leading-relaxed", className)} {...props} />
);
