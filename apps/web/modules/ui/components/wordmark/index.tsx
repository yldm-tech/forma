import { cn } from "@/lib/cn";
import { FormaLogo } from "@/modules/ui/components/forma-logo";

interface WordmarkProps {
  /** Rendered above the name, for the survey footer's "powered by" line. */
  eyebrow?: string;
  className?: string;
}

/**
 * The product name next to its mark, set in the application's own font.
 *
 * It replaces four copies of an inherited wordmark whose letter outlines still spelled the upstream product's name — a nav header, an onboarding sidebar, the sign-in page and the survey footer that respondents see. Vector lettering is invisible to a text search, which is why those four outlived every other mention of that name in the repository.
 *
 * Drawn as a mark plus live text rather than as a new set of outlines: nobody here can redraw a typeface, and text in the app font is honest about being text. It also means the name can change again without anyone touching a path.
 */
export const Wordmark = ({ eyebrow, className }: Readonly<WordmarkProps>) => (
  <span className={cn("inline-flex items-center gap-2", className)}>
    <FormaLogo className="h-[1.15em] w-auto shrink-0" />
    <span className="flex flex-col leading-none">
      {eyebrow && <span className="text-[0.4em] font-medium tracking-wide text-slate-500">{eyebrow}</span>}
      <span className="text-[1em] font-bold tracking-tight text-slate-900">Forma</span>
    </span>
  </span>
);
