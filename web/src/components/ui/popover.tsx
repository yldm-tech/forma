import * as PopoverPrimitive from "@radix-ui/react-popover";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { cn } from "../../lib/cn";
import { usePortalContainer } from "./dialog";

export const Popover = PopoverPrimitive.Root;
export const PopoverTrigger = PopoverPrimitive.Trigger;

export const PopoverContent = ({
  className,
  align = "center",
  sideOffset = 4,
  ...props
}: ComponentPropsWithoutRef<typeof PopoverPrimitive.Content>) => (
  <PopoverPrimitive.Portal container={usePortalContainer()}>
    <PopoverPrimitive.Content
      align={align}
      sideOffset={sideOffset}
      className={cn("z-50 w-72 rounded-md border border-slate-100 bg-white p-4 shadow-md outline-hidden", className)}
      {...props}
    />
  </PopoverPrimitive.Portal>
);

/** A control that is not available, with the reason one click away: forma2 showed it this way for disabled navigation entries and the add-workspace action. */
export const DisabledReason = ({ reason, children }: { reason: string; children: ReactNode }) => (
  <Popover>
    <PopoverTrigger asChild>{children}</PopoverTrigger>
    <PopoverContent className="w-fit max-w-72 px-3 py-2 text-sm text-slate-700">{reason}</PopoverContent>
  </Popover>
);
