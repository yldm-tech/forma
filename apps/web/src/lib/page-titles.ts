import type { QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { analysisSurveyQuery } from "../features/survey-analysis/api";

// The browser tab titles, by route id: forma2's root layout's "%s | Forma" template around the page's own title, "Forma" for a page without one. A layout's title covers the pages under it unless they have their own, as Next resolved metadata, so the deepest matched route with an entry wins. Every page names itself with the key of a heading it renders or of its name in the navigation, so the title is in the user's language (forma2 left a few as English literals, and most auth, onboarding and account pages untitled).

export const BASE_TITLE = "Forma";

interface TitleContext {
  t: TFunction;
  params: Record<string, string>;
  queryClient: QueryClient;
}

/** A translation key, a literal title, or a title made from what the page has loaded. */
export type TitleSource = { key: string } | { text: string } | ((context: TitleContext) => string);

/** forma2's survey analysis layout: the survey's results, or the neutral "Survey" while its name is unknown. */
const surveyResultsTitle = ({ t, params, queryClient }: TitleContext): string => {
  // The analysis views' own cache of the survey, which their route loads; read, never fetched, so a title cannot request data the page would not.
  const survey = params.surveyId
    ? queryClient.getQueryData(analysisSurveyQuery(params.surveyId).queryKey)
    : undefined;
  return survey
    ? t("workspace.surveys.summary.survey_results", { surveyName: survey.name })
    : t("common.survey");
};

const WS = "/_authenticated/workspaces/$workspaceId";

const ORG = "/_authenticated/organizations/$organizationId";

export const PAGE_TITLES: Record<string, TitleSource> = {
  "/auth/login": { key: "auth.login.login_to_your_account" },
  "/auth/signup": { key: "auth.signup.title" },
  "/auth/forgot-password/": { key: "auth.forgot-password.title" },
  "/auth/forgot-password/reset/": { key: "auth.forgot-password.reset_password" },
  "/invite": { key: "common.invite" },
  "/results": { key: "workspace.analysis.result_shares.shared_results" },
  "/unsubscribe": { key: "workspace.settings.notifications.unsubscribe_page.unsubscribe" },
  "/setup/intro": { key: "setup.intro.welcome_to_forma" },
  "/setup/signup": { key: "setup.signup.create_administrator" },
  "/_authenticated/setup/organization/create": { key: "setup.organization.create.title" },
  "/_authenticated/setup/organization/$organizationId/invite": {
    key: "setup.invite.invite_your_organization_members",
  },
  [`${ORG}/workspaces/new/plan`]: { key: "workspace.settings.billing.plan_selection_title" },
  [`${ORG}/workspaces/new/survey`]: { key: "organizations.workspaces.new.survey.title" },
  [`${ORG}/workspaces/new/templates`]: { key: "workspace.xm-templates.headline" },
  [`${ORG}/workspaces/new/ai`]: { key: "workspace.surveys.ai_create.dialog_title" },
  [`${ORG}/two-factor`]: { key: "workspace.settings.profile.two_factor_authentication" },
  [`${ORG}/settings/general`]: { key: "workspace.settings.general.organization_settings" },
  [`${ORG}/settings/teams`]: { key: "common.members_and_teams" },
  [`${ORG}/settings/api-keys`]: { key: "common.api_keys" },
  [`${ORG}/settings/domain`]: { key: "common.branding_and_links" },
  [`${ORG}/settings/billing`]: { key: "common.billing" },
  "/_authenticated/account/settings/profile": { key: "common.profile" },
  "/_authenticated/account/settings/notifications": { key: "common.notifications" },
  [`${WS}/surveys/`]: { key: "common.surveys" },
  "/_authenticated/workspaces/$workspaceId_/surveys/templates": {
    key: "workspace.surveys.templates.create_a_new_survey",
  },
  [`${WS}/surveys/$surveyId`]: surveyResultsTitle,
  [`${WS}/integrations/`]: { key: "common.integrations" },
  [`${WS}/integrations/app-connection`]: { key: "common.web_and_mobile_sdk" },
  [`${WS}/integrations/airtable`]: { key: "workspace.integrations.airtable.airtable_integration" },
  [`${WS}/integrations/google-sheets`]: {
    key: "workspace.integrations.google_sheets.google_sheets_integration",
  },
  [`${WS}/integrations/notion`]: { key: "workspace.integrations.notion.notion_integration" },
  [`${WS}/integrations/slack`]: { key: "workspace.integrations.slack.slack_integration" },
  [`${WS}/integrations/webhooks`]: { key: "common.webhooks" },
  [`${WS}/settings/workspace/general`]: { key: "common.workspace_settings" },
  [`${WS}/settings/workspace/languages`]: { key: "common.survey_languages" },
  [`${WS}/settings/workspace/look`]: { key: "common.appearance" },
  [`${WS}/settings/workspace/teams`]: { key: "common.team_access" },
  [`${WS}/tags`]: { key: "common.tags" },
  [`${WS}/user-actions`]: { key: "common.user_actions" },
};

/** The document title for the matched routes, outermost first. */
export const documentTitle = (
  matches: { routeId: string; params: Record<string, string> }[],
  context: Omit<TitleContext, "params">
): string => {
  for (let i = matches.length - 1; i >= 0; i--) {
    const match = matches[i];
    const source = match && PAGE_TITLES[match.routeId];
    if (!match || !source) continue;
    const title =
      typeof source === "function"
        ? source({ ...context, params: match.params })
        : "key" in source
          ? context.t(source.key)
          : source.text;
    return `${title} | ${BASE_TITLE}`;
  }
  return BASE_TITLE;
};
