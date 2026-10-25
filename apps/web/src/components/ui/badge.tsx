import type { LucideIcon } from "lucide-react";
import { cn } from "../../lib/cn";

interface BadgeProps {
  text: string;
  /** An icon before the text, decorative: the text says what the badge means. */
  icon?: LucideIcon;
  type: "warning" | "success" | "error" | "gray";
  size?: "tiny" | "normal" | "large";
  className?: string;
  title?: string;
}

const COLORS: Record<BadgeProps["type"], string> = {
  warning: "bg-amber-100 border-amber-200 text-amber-800",
  success: "bg-green-50 border-green-600 text-green-800",
  error: "bg-red-100 border-red-200 text-red-800",
  gray: "bg-slate-100 border-slate-200 text-slate-600",
};

const PADDING = { tiny: "px-1.5 py-0.5", normal: "px-2.5 py-0.5", large: "px-3.5 py-1" };

export const Badge = ({ text, icon: Icon, type, size = "normal", className, title }: BadgeProps) => (
  <span
    title={title}
    className={cn(
      "inline-flex cursor-default items-center gap-1.5 rounded-full border font-medium",
      COLORS[type],
      PADDING[size],
      size === "large" ? "text-sm" : "text-xs",
      className
    )}>
    {Icon && <Icon className="size-3.5 shrink-0" aria-hidden />}
    {text}
  </span>
);
