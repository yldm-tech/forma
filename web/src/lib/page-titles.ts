import type { QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { analysisSurveyQuery } from "../features/survey-analysis/api";

// The browser tab titles forma2's pages set through Next's metadata, by route id: its root layout's "%s | Forma" template around the page's own title, "Forma" for a page without one. A layout's title covers the pages under it unless they have their own, as Next resolved metadata, so the deepest matched route with an entry wins. Settings and integration pages name themselves with the key of the heading they render (forma2's getSettingsPageMetadata); the few untranslated titles are forma2's literal strings.

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
  const survey = params.surveyId ? queryClient.getQueryData(analysisSurveyQuery(params.surveyId).queryKey) : undefined;
  return survey ? t("workspace.surveys.summary.survey_results", { surveyName: survey.name }) : t("common.survey");
};

const WS = "/_authenticated/workspaces/$workspaceId";

export const PAGE_TITLES: Record<string, TitleSource> = {
  "/auth/login": { text: "Login" },
  "/setup/intro": { text: "Intro" },
  "/setup/signup": { text: "Sign up" },
  "/_authenticated/setup/organization/create": { text: "Create Organization" },
  "/_authenticated/setup/organization/$organizationId/invite": { text: "Invite" },
  [`${WS}/surveys/`]: { text: "Your Surveys" },
  [`${WS}/surveys/$surveyId`]: surveyResultsTitle,
  [`${WS}/integrations/`]: { key: "common.integrations" },
  [`${WS}/integrations/app-connection`]: { key: "common.web_and_mobile_sdk" },
  [`${WS}/integrations/airtable`]: { key: "workspace.integrations.airtable.airtable_integration" },
  [`${WS}/integrations/google-sheets`]: { key: "workspace.integrations.google_sheets.google_sheets_integration" },
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
