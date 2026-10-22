import { cn } from "../../lib/cn";

const COLORS = ["bg-teal-600", "bg-slate-700", "bg-cyan-700", "bg-emerald-700", "bg-sky-700"];

/** The initials of a name on a colour picked from the user id, so a person keeps their colour everywhere. */
export const ProfileAvatar = ({
  userId,
  name,
  className,
}: {
  userId: string;
  name: string;
  className?: string;
}) => {
  const initials = name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join("");
  let hash = 0;
  for (const char of userId) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  return (
    <span
      aria-hidden="true"
      className={cn(
        "flex size-9 items-center justify-center rounded-full text-xs font-semibold text-white",
        COLORS[hash % COLORS.length],
        className
      )}>
      {initials || "?"}
    </span>
  );
};
