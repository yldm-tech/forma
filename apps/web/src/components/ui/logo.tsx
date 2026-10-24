import { cn } from "../../lib/cn";

/** forma2's product mark: a card with a filled field, a shorter one and a chosen option. */
export const FormaLogo = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 48 48"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    className={className}
    aria-hidden="true">
    <rect x="6" y="4" width="36" height="40" rx="10" fill="#038178" />
    <rect x="14" y="17" width="20" height="5" rx="2.5" fill="#00E6CA" />
    <rect x="14" y="27" width="12" height="5" rx="2.5" fill="#FFFFFF" />
    <rect x="29" y="27" width="5" height="5" rx="2.5" fill="#00E6CA" />
  </svg>
);

/** The product name beside its mark, in the application's font. */
export const Wordmark = ({ className }: { className?: string }) => (
  <span className={cn("inline-flex items-center gap-2", className)}>
    <FormaLogo className="h-[1.15em] w-auto shrink-0" />
    <span className="text-[1em] leading-none font-bold tracking-tight text-slate-900">Forma</span>
  </span>
);
