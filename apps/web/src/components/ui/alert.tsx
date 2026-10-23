import { type VariantProps, cva } from "class-variance-authority";
import { AlertCircleIcon, AlertTriangleIcon, CheckCircle2Icon, InfoIcon } from "lucide-react";
import { type HTMLAttributes, createContext, forwardRef, useContext, useMemo } from "react";
import { cn } from "../../lib/cn";
import { Button, type ButtonProps } from "./button";

const alertVariants = cva("relative w-full rounded-lg border bg-white [&>svg]:size-4", {
  variants: {
    variant: {
      default: "text-slate-900 border-slate-200",
      error:
        "text-error-foreground [&>svg]:text-error border-error/50 [&_button]:bg-error-background [&_button]:text-error-foreground [&_button:hover]:bg-error-background-muted [&_a]:bg-error-background [&_a]:text-error-foreground [&_a:hover]:bg-error-background-muted",
      warning:
        "text-warning-foreground [&>svg]:text-warning border-warning/50 [&_button]:bg-warning-background [&_button]:text-warning-foreground [&_button:hover]:bg-warning-background-muted [&_a]:bg-warning-background [&_a]:text-warning-foreground [&_a:hover]:bg-warning-background-muted",
      info: "text-info-foreground [&>svg]:text-info border-info/50 [&_button]:bg-info-background [&_button]:text-info-foreground [&_button:hover]:bg-info-background-muted [&_a]:bg-info-background [&_a]:text-info-foreground [&_a:hover]:bg-info-background-muted",
      success:
        "text-success-foreground [&>svg]:text-success border-success/50 [&_button]:bg-success-background [&_button]:text-success-foreground [&_button:hover]:bg-success-background-muted [&_a]:bg-success-background [&_a]:text-success-foreground [&_a:hover]:bg-success-background-muted",
    },
    size: {
      default:
        "py-3 px-4 text-sm grid grid-cols-[2fr_auto] grid-rows-[auto_auto] gap-y-0.5 gap-x-3 [&>svg]:absolute [&>svg]:left-4 [&>svg]:top-4 [&>svg~*]:pl-7",
      small:
        "px-4 py-2 text-xs flex items-center gap-2 [&>svg]:shrink-0 [&_button]:bg-transparent [&_button:hover]:bg-transparent [&_a]:bg-transparent [&_a:hover]:bg-transparent [&>svg~*]:pl-0",
    },
  },
  defaultVariants: { variant: "default", size: "default" },
});

type AlertVariants = VariantProps<typeof alertVariants>;

// The variant and size, shared with the title, description and button so they lay themselves out to match.
const AlertContext = createContext<AlertVariants>({ variant: "default", size: "default" });

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
  size,
  role = "alert",
  children,
  ...props
}: HTMLAttributes<HTMLDivElement> & AlertVariants) => {
  const context = useMemo(() => ({ variant, size }), [variant, size]);
  return (
    <AlertContext.Provider value={context}>
      <div role={role} className={cn(alertVariants({ variant, size }), className)} {...props}>
        {ICONS[variant ?? "default"]}
        {children}
      </div>
    </AlertContext.Provider>
  );
};

export const AlertTitle = ({ className, children, ...props }: HTMLAttributes<HTMLHeadingElement>) => {
  const { size } = useContext(AlertContext);
  return (
    <h5
      className={cn(
        "col-start-1 row-start-1 tracking-tight",
        size === "small" ? "shrink truncate font-normal" : "font-medium",
        className
      )}
      {...props}>
      {children}
    </h5>
  );
};

export const AlertDescription = ({ className, ...props }: HTMLAttributes<HTMLDivElement>) => {
  const { size } = useContext(AlertContext);
  return (
    <div
      className={cn(
        "[&_p]:leading-relaxed",
        size === "small" ? "shrink grow-0 truncate" : "col-start-1 row-start-2",
        className
      )}
      {...props}
    />
  );
};

/** The alert's action: a secondary button beside a default alert, a link at the end of a small one. */
export const AlertButton = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, children, ...props }, ref) => {
    const { size: alertSize } = useContext(AlertContext);
    const buttonVariant = variant ?? (alertSize === "small" ? "link" : "secondary");
    return (
      <div
        className={cn(
          "self-end",
          alertSize === "small"
            ? "-my-2 -mr-4 ml-auto shrink-0"
            : "col-start-2 row-span-2 row-start-1 flex items-center justify-center"
        )}>
        <Button
          ref={ref}
          variant={buttonVariant}
          size={size ?? (alertSize === "small" ? "sm" : "default")}
          className={cn(
            alertSize === "small" &&
              buttonVariant === "link" &&
              "bg-transparent font-normal underline-offset-4 hover:bg-transparent hover:underline",
            className
          )}
          {...props}>
          {children}
        </Button>
      </div>
    );
  }
);
AlertButton.displayName = "AlertButton";
