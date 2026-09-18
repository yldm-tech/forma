import type { TFunction } from "i18next";
import {
  BarChart3Icon,
  type LucideIcon,
  MousePointerClickIcon,
  TrendingDownIcon,
  UsersIcon,
} from "lucide-react";

export const AI_SURVEY_PROMPT_MIN_LENGTH = 4;
export const AI_SURVEY_PROMPT_MAX_LENGTH = 1200;

export const getHelperPrompts = (
  t: TFunction
): {
  label: string;
  prompt: string;
  Icon: LucideIcon;
}[] => [
  {
    label: t("workspace.surveys.ai_create.prompt_helper_onboarding_label"),
    prompt: t("workspace.surveys.ai_create.prompt_helper_onboarding"),
    Icon: MousePointerClickIcon,
  },
  {
    label: t("workspace.surveys.ai_create.prompt_helper_churn_label"),
    prompt: t("workspace.surveys.ai_create.prompt_helper_churn"),
    Icon: UsersIcon,
  },
  {
    label: t("workspace.surveys.ai_create.prompt_helper_pmf_label"),
    prompt: t("workspace.surveys.ai_create.prompt_helper_pmf"),
    Icon: BarChart3Icon,
  },
  {
    label: t("workspace.surveys.ai_create.prompt_helper_website_label"),
    prompt: t("workspace.surveys.ai_create.prompt_helper_website"),
    Icon: TrendingDownIcon,
  },
];

/**
 * The language an AI draft is generated in when the author picks nothing.
 *
 * English rather than the signed-in person's locale: a survey's audience is not its author, and
 * the common case is writing for readers elsewhere.
 *
 * Restated here rather than imported from the route's schemas: production code under `modules/`
 * does not import from `app/`, and the route keeps its own fallback for callers that send no
 * language at all. The picker always sends one, so the two cannot disagree in practice.
 */
export const DEFAULT_AI_SURVEY_LANGUAGE = "en-US" as const;
