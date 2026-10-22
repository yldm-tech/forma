import type { TFunction } from "i18next";
import {
  BarChart3Icon,
  type LucideIcon,
  MousePointerClickIcon,
  TrendingDownIcon,
  UsersIcon,
} from "lucide-react";
import { ZUserLocale } from "@forma/types/user";

/** forma2's prompt bounds, the generate route's (server/internal/survey/v3generate.go). */
export const AI_SURVEY_PROMPT_MIN_LENGTH = 4;
export const AI_SURVEY_PROMPT_MAX_LENGTH = 1200;

/** The language a draft is generated in when the author picks nothing: English rather than the author's locale, since a survey's audience is not its author. */
export const DEFAULT_AI_SURVEY_LANGUAGE = "en-US";

/** Every locale the generate route accepts, wider than the four the admin UI is translated into. */
export const AI_SURVEY_LANGUAGES: readonly string[] = ZUserLocale.options;

/** forma2's getHelperPrompts: the example prompts under the textarea. */
export const getHelperPrompts = (t: TFunction): { label: string; prompt: string; Icon: LucideIcon }[] => [
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
