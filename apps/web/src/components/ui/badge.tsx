import { cn } from "../../lib/cn";

interface BadgeProps {
  text: string;
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

export const Badge = ({ text, type, size = "normal", className, title }: BadgeProps) => (
  <span
    title={title}
    className={cn(
      "inline-flex cursor-default items-center rounded-full border font-medium",
      COLORS[type],
      PADDING[size],
      size === "large" ? "text-sm" : "text-xs",
      className
    )}>
    {text}
  </span>
);
