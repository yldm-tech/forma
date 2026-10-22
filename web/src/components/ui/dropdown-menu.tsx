import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";
import { Check, ChevronLeft, ChevronRight, Circle } from "lucide-react";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { cn } from "../../lib/cn";
import { usePortalContainer } from "./dialog";

export const DropdownMenu = DropdownMenuPrimitive.Root;
export const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger;
export const DropdownMenuGroup = DropdownMenuPrimitive.Group;
export const DropdownMenuRadioGroup = DropdownMenuPrimitive.RadioGroup;
export const DropdownMenuSub = DropdownMenuPrimitive.Sub;

export const DropdownMenuSubTrigger = ({
  className,
  inset,
  chevronSide = "right",
  children,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.SubTrigger> & {
  inset?: boolean;
  chevronSide?: "left" | "right";
}) => (
  <DropdownMenuPrimitive.SubTrigger
    className={cn(
      "flex cursor-default items-center rounded-lg px-2 py-1.5 text-sm font-medium text-slate-600 outline-hidden select-none hover:text-slate-700 focus:bg-slate-100 data-[state=open]:bg-slate-100",
      inset && "pl-8",
      className
    )}
    {...props}>
    {chevronSide === "left" && <ChevronLeft className="mr-2 size-4" />}
    {children}
    {chevronSide === "right" && <ChevronRight className="ml-auto size-4" />}
  </DropdownMenuPrimitive.SubTrigger>
);

export const DropdownMenuSubContent = ({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.SubContent>) => (
  <DropdownMenuPrimitive.Portal container={usePortalContainer()}>
    <DropdownMenuPrimitive.SubContent
      className={cn(
        "z-50 max-h-[min(20rem,var(--radix-dropdown-menu-content-available-height))] min-w-32 overflow-x-hidden overflow-y-auto rounded-lg border border-slate-200 bg-white p-1 font-medium text-slate-600 shadow-xs hover:text-slate-700",
        className
      )}
      {...props}
    />
  </DropdownMenuPrimitive.Portal>
);

export const DropdownMenuContent = ({
  className,
  sideOffset = 4,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Content>) => (
  <DropdownMenuPrimitive.Portal container={usePortalContainer()}>
    <DropdownMenuPrimitive.Content
      sideOffset={sideOffset}
      className={cn(
        "z-50 max-h-[min(20rem,var(--radix-dropdown-menu-content-available-height))] min-w-32 overflow-x-hidden overflow-y-auto rounded-lg border border-slate-200 bg-white p-1 font-medium text-slate-700 shadow-xs",
        className
      )}
      {...props}
    />
  </DropdownMenuPrimitive.Portal>
);

export const DropdownMenuItem = ({
  className,
  children,
  icon,
  asChild,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item> & { icon?: ReactNode }) => (
  <DropdownMenuPrimitive.Item
    asChild={asChild}
    className={cn(
      "relative flex w-full cursor-pointer items-center gap-x-2 rounded-lg px-2 py-1.5 text-sm font-medium outline-hidden select-none focus:bg-slate-100 data-disabled:pointer-events-none data-disabled:opacity-50",
      className
    )}
    {...props}>
    {/* With asChild the item becomes its one child element (a link), which carries its own icon. */}
    {asChild ? (
      children
    ) : (
      <>
        {icon}
        {children}
      </>
    )}
  </DropdownMenuPrimitive.Item>
);

export const DropdownMenuCheckboxItem = ({
  className,
  children,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.CheckboxItem>) => (
  <DropdownMenuPrimitive.CheckboxItem
    className={cn(
      "relative flex cursor-pointer items-center rounded-lg py-1.5 pr-2 pl-8 text-sm font-medium outline-hidden select-none focus:bg-slate-100 data-disabled:pointer-events-none data-disabled:opacity-50",
      className
    )}
    {...props}>
    <span className="absolute left-2 flex size-3.5 items-center justify-center">
      <DropdownMenuPrimitive.ItemIndicator>
        <Check className="size-4" />
      </DropdownMenuPrimitive.ItemIndicator>
    </span>
    {children}
  </DropdownMenuPrimitive.CheckboxItem>
);

export const DropdownMenuRadioItem = ({
  className,
  children,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.RadioItem>) => (
  <DropdownMenuPrimitive.RadioItem
    className={cn(
      "relative flex cursor-pointer items-center rounded-lg py-1.5 pr-2 pl-8 text-sm font-medium outline-hidden select-none focus:bg-slate-100 data-disabled:pointer-events-none data-disabled:opacity-50",
      className
    )}
    {...props}>
    <span className="absolute left-2 flex size-3.5 items-center justify-center">
      <DropdownMenuPrimitive.ItemIndicator>
        <Circle className="size-2 fill-current" />
      </DropdownMenuPrimitive.ItemIndicator>
    </span>
    {children}
  </DropdownMenuPrimitive.RadioItem>
);

export const DropdownMenuSeparator = ({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Separator>) => (
  <DropdownMenuPrimitive.Separator className={cn("-mx-1 my-1 h-px bg-slate-100", className)} {...props} />
);

export const DropdownMenuLabel = ({
  className,
  inset,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Label> & { inset?: boolean }) => (
  <DropdownMenuPrimitive.Label
    className={cn("px-2 py-1.5 text-sm font-semibold text-slate-900", inset && "pl-8", className)}
    {...props}
  />
);
