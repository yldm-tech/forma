import { UserIcon } from "lucide-react";
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

const PERSON_COLORS = ["bg-teal-500", "bg-teal-100", "bg-slate-700"];

/** A respondent's avatar: a colour picked from the contact id, so the same contact looks the same in every list; anonymous respondents share one. */
export const PersonAvatar = ({ personId, className }: { personId: string; className?: string }) => {
  let hash = 0;
  for (const char of personId) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  const color = PERSON_COLORS[hash % PERSON_COLORS.length];
  return (
    <span
      aria-hidden="true"
      className={cn(
        "flex size-10 shrink-0 items-center justify-center rounded-full",
        color,
        color === "bg-teal-100" ? "text-teal-700" : "text-white",
        className
      )}>
      <UserIcon className="size-5" />
    </span>
  );
};
