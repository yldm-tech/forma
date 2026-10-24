import { Link } from "@tanstack/react-router";
import { ArrowLeftIcon, Loader2, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
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
            : "cursor-pointer hover:scale-[1.03] hover:border-slate-300 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-hidden"
        )}>
        <span className="flex h-full w-full flex-col items-center gap-y-4 text-center">
          <span className="flex h-16 w-16 shrink-0 items-center justify-center">
            <Icon aria-hidden="true" className="size-16 text-slate-600" strokeWidth={1} absoluteStrokeWidth />
          </span>
          {/* Top-aligned, so the titles of a row of cards line up whatever the length of their descriptions. */}
          <span className="flex flex-1 flex-col items-center justify-start text-center">
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

/** The way back from a later onboarding step to the first one. */
export const OnboardingBackLink = ({ organizationId }: { organizationId: string }) => {
  const { t } = useTranslation();
  return (
    <Link
      to="/organizations/$organizationId/workspaces/new/survey"
      params={{ organizationId }}
      className="focus-visible:ring-ring inline-flex items-center gap-1 rounded-md px-2 py-1 text-sm font-medium text-slate-600 hover:text-slate-900 focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-hidden">
      <ArrowLeftIcon className="size-4" aria-hidden="true" />
      {t("common.back")}
    </Link>
  );
};

/** forma2's onboarding step: a headline over a grid of options, with a way back on the later steps. It fills the onboarding layout under its header. */
export const OnboardingOptions = ({
  title,
  options,
  back,
}: {
  title: string;
  options: OnboardingOption[];
  back?: ReactNode;
}) => (
  <main className="flex w-full flex-1 flex-col items-center justify-center gap-y-12 p-6">
    <div className="flex flex-col items-center gap-y-4">
      {back}
      <h1 className="text-center text-4xl font-medium text-balance text-slate-800">{title}</h1>
    </div>
    <div className="grid w-full max-w-5xl grid-cols-1 gap-8 text-center sm:grid-cols-2 lg:grid-cols-3">
      {options.map((option) => (
        <div key={option.title} className="flex h-full flex-col items-center gap-2">
          <OptionCard option={option} />
          {option.disabled && option.disabledDescription && (
            <p className="max-w-80 text-xs text-slate-600">{option.disabledDescription}</p>
          )}
        </div>
      ))}
    </div>
  </main>
);
