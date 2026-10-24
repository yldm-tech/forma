import * as SelectPrimitive from "@radix-ui/react-select";
import { ChevronDown } from "lucide-react";
import { type ComponentPropsWithoutRef, type ComponentRef, forwardRef } from "react";
import { cn } from "../../lib/cn";
import { usePortalContainer } from "./dialog";

// forma2's Radix Select, for the editor's pickers whose options carry icons or need a custom trigger. Plain single choices use the native Select in ./select.

export const SelectMenu = SelectPrimitive.Root;
export const SelectGroup = SelectPrimitive.Group;
export const SelectValue = SelectPrimitive.Value;

export const SelectTrigger = forwardRef<
  ComponentRef<typeof SelectPrimitive.Trigger>,
  ComponentPropsWithoutRef<typeof SelectPrimitive.Trigger> & { hideArrow?: boolean }
>(({ className, hideArrow, children, ...props }, ref) => (
  <SelectPrimitive.Trigger
    ref={ref}
    className={cn(
      "flex h-9 w-full items-center justify-between gap-2 rounded-md border border-slate-300 bg-transparent px-3 py-2 text-sm focus:ring-2 focus:ring-slate-400 focus:ring-offset-1 focus:outline-hidden hover:enabled:border-slate-400 disabled:cursor-not-allowed disabled:opacity-50 data-placeholder:text-slate-400 [&>span]:truncate",
      className
    )}
    {...props}>
    {children}
    {!hideArrow ? <ChevronDown className="size-4 shrink-0 opacity-50" /> : null}
  </SelectPrimitive.Trigger>
));
SelectTrigger.displayName = SelectPrimitive.Trigger.displayName;

export const SelectContent = forwardRef<
  ComponentRef<typeof SelectPrimitive.Content>,
  ComponentPropsWithoutRef<typeof SelectPrimitive.Content>
>(({ className, children, position = "popper", ...props }, ref) => (
  <SelectPrimitive.Portal container={usePortalContainer()}>
    <SelectPrimitive.Content
      ref={ref}
      position={position}
      className={cn(
        "relative z-50 max-h-[min(20rem,var(--radix-select-content-available-height))] min-w-32 overflow-x-hidden overflow-y-auto rounded-md border border-slate-100 bg-white text-slate-700 shadow-md",
        position === "popper" &&
          "data-[side=bottom]:translate-y-1 data-[side=left]:-translate-x-1 data-[side=right]:translate-x-1 data-[side=top]:-translate-y-1",
        className
      )}
      {...props}>
      <SelectPrimitive.Viewport
        className={cn(
          "p-1",
          position === "popper" &&
            "h-(--radix-select-trigger-height) w-full min-w-(--radix-select-trigger-width)"
        )}>
        {children}
      </SelectPrimitive.Viewport>
    </SelectPrimitive.Content>
  </SelectPrimitive.Portal>
));
SelectContent.displayName = SelectPrimitive.Content.displayName;

export const SelectLabel = forwardRef<
  ComponentRef<typeof SelectPrimitive.Label>,
  ComponentPropsWithoutRef<typeof SelectPrimitive.Label>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.Label
    ref={ref}
    className={cn("py-1.5 pr-2 pl-8 text-sm font-semibold text-slate-900", className)}
    {...props}
  />
));
SelectLabel.displayName = SelectPrimitive.Label.displayName;

export const SelectItem = forwardRef<
  ComponentRef<typeof SelectPrimitive.Item>,
  ComponentPropsWithoutRef<typeof SelectPrimitive.Item>
>(({ className, children, ...props }, ref) => (
  <SelectPrimitive.Item
    ref={ref}
    className={cn(
      "relative flex cursor-pointer items-center rounded-md py-1.5 pr-2 pl-2 text-sm font-medium outline-hidden select-none focus:bg-slate-100 data-disabled:pointer-events-none data-disabled:opacity-50",
      className
    )}
    {...props}>
    <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
  </SelectPrimitive.Item>
));
SelectItem.displayName = SelectPrimitive.Item.displayName;

export const SelectSeparator = forwardRef<
  ComponentRef<typeof SelectPrimitive.Separator>,
  ComponentPropsWithoutRef<typeof SelectPrimitive.Separator>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.Separator ref={ref} className={cn("-mx-1 my-1 h-px bg-slate-100", className)} {...props} />
));
SelectSeparator.displayName = SelectPrimitive.Separator.displayName;
