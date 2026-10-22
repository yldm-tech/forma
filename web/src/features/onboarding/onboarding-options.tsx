import { Loader2, type LucideIcon } from "lucide-react";
import { cn } from "../../lib/cn";

export interface OnboardingOption {
  title: string;
  description: string;
  icon: LucideIcon;
  onSelect: () => void;
  isLoading?: boolean;
  disabled?: boolean;
  /** Said under a disabled card: why it cannot be chosen. */
  disabledDescription?: string;
}

/** forma2's OptionCard (size md): a large clickable card with an icon, a title and a description, covered by a spinner while its action runs. */
const OptionCard = ({ option }: { option: OnboardingOption }) => {
  const Icon = option.icon;
  const loading = option.isLoading ?? false;
  const unavailable = loading || (option.disabled ?? false);
  return (
    <div className="relative h-full w-full">
      <button
        type="button"
        disabled={unavailable}
        onClick={option.onSelect}
        aria-busy={loading || undefined}
        className={cn(
          "flex h-full w-80 flex-col items-center rounded-xl border border-slate-200 bg-white p-6 shadow-lg transition-transform duration-200",
          unavailable
            ? "cursor-not-allowed opacity-50"
            : "cursor-pointer hover:scale-[1.03] hover:border-slate-300 focus-visible:ring-2 focus-visible:ring-slate-400 focus-visible:ring-offset-2 focus-visible:outline-hidden"
        )}>
        <span className="flex h-full w-full flex-col items-center gap-y-4 text-center">
          <span className="flex h-16 w-16 shrink-0 items-center justify-center">
            <Icon aria-hidden="true" className="size-16 text-slate-600" strokeWidth={1} absoluteStrokeWidth />
          </span>
          <span className="flex flex-1 flex-col items-center justify-center text-center">
            <span className="text-lg font-medium text-slate-800">{option.title}</span>
            <span className="text-sm text-balance text-slate-500">{option.description}</span>
          </span>
        </span>
      </button>
      {loading && (
        <div
          className="pointer-events-none absolute inset-0 flex items-center justify-center bg-slate-100/70"
          aria-hidden="true">
          <Loader2 className="size-6 animate-spin text-slate-600" />
        </div>
      )}
    </div>
  );
};

/** forma2's onboarding step: a headline over a grid of options. */
export const OnboardingOptions = ({ title, options }: { title: string; options: OnboardingOption[] }) => (
  <main className="flex min-h-screen min-w-full flex-col items-center justify-center gap-y-12 bg-slate-50 p-6">
    <h1 className="text-center text-4xl font-medium text-slate-800">{title}</h1>
    <div className="grid w-full max-w-5xl grid-cols-1 gap-8 text-center sm:grid-cols-2 lg:grid-cols-3">
      {options.map((option) => (
        <div key={option.title} className="flex h-full flex-col items-center gap-2">
          <OptionCard option={option} />
          {option.disabled && option.disabledDescription && (
            <p className="max-w-80 text-xs text-slate-400">{option.disabledDescription}</p>
          )}
        </div>
      ))}
    </div>
  </main>
);
