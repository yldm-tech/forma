import { Slot } from "@radix-ui/react-slot";
import { type VariantProps, cva } from "class-variance-authority";
import { Loader2 } from "lucide-react";
import { type ButtonHTMLAttributes, forwardRef } from "react";
import { cn } from "../../lib/cn";

export const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground enabled:hover:bg-primary-hover",
        destructive: "bg-destructive text-destructive-foreground enabled:hover:bg-destructive-hover",
        outline:
          "border border-slate-300 bg-white enabled:hover:bg-accent enabled:hover:text-accent-foreground",
        secondary:
          "bg-secondary text-secondary-foreground enabled:hover:bg-secondary/30 border border-ink/5 hover:border-ink/20",
        ghost: "enabled:hover:bg-accent enabled:hover:text-accent-foreground text-ink",
        link: "text-primary underline-offset-4 enabled:hover:underline",
      },
      size: {
        default: "h-9 px-4 py-2",
        sm: "h-8 rounded-md px-3 text-xs",
        lg: "h-10 rounded-md px-8",
        icon: "h-9 w-9",
        tall: "h-10 rounded-md px-3 text-xs",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  }
);

export interface ButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean;
  loading?: boolean;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, loading, asChild = false, disabled, children, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp
        className={cn(
          buttonVariants({ variant, size }),
          loading && "relative cursor-not-allowed opacity-50",
          className
        )}
        ref={ref}
        aria-busy={loading || undefined}
        {...props}
        disabled={loading || disabled}>
        {loading ? (
          // The label keeps its place, invisibly, so the button does not change width while it spins.
          <>
            <Loader2 className="absolute inset-0 m-auto animate-spin" />
            <span className="invisible inline-flex items-center gap-2">{children}</span>
          </>
        ) : (
          children
        )}
      </Comp>
    );
  }
);
Button.displayName = "Button";
