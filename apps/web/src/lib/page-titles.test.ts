import { QueryClient } from "@tanstack/react-query";
import { createRouter } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { describe, expect, test } from "vite-plus/test";
import enUS from "../../locales/en-US.json";
import jaJP from "../../locales/ja-JP.json";
import zhHansCN from "../../locales/zh-Hans-CN.json";
import zhHantTW from "../../locales/zh-Hant-TW.json";
import { analysisSurveyQuery } from "../features/survey-analysis/api";
import { routeTree } from "../routeTree.gen";
import { PAGE_TITLES, documentTitle } from "./page-titles";

// forma2's page titles under its "%s | Forma" template.

const t = ((key: string, values?: Record<string, string>) =>
  values ? `${key}:${JSON.stringify(values)}` : key) as unknown as TFunction;
const WS = "/_authenticated/workspaces/$workspaceId";

const title = (routeIds: string[], params: Record<string, string> = {}, queryClient = new QueryClient()) =>
  documentTitle(
    routeIds.map((routeId) => ({ routeId, params })),
    { t, queryClient }
  );

describe("documentTitle", () => {
  test("translates the page's own heading in the template", () => {
    expect(title(["__root__", "/auth", "/auth/login"])).toBe("auth.login.login_to_your_account | Forma");
    expect(title(["__root__", "/_authenticated", WS, `${WS}/surveys/`])).toBe("common.surveys | Forma");
  });

  test("names the auth, invite, onboarding and account pages forma2 left untitled", () => {
    const ORG = "/_authenticated/organizations/$organizationId";
    expect(title(["__root__", "/auth", "/auth/signup"])).toBe("auth.signup.title | Forma");
    expect(title(["__root__", "/auth", "/auth/forgot-password/"])).toBe("auth.forgot-password.title | Forma");
    expect(title(["__root__", "/invite"])).toBe("common.invite | Forma");
    expect(title(["__root__", "/_authenticated", ORG, `${ORG}/workspaces/new`, `${ORG}/workspaces/new/survey`])).toBe(
      "organizations.workspaces.new.survey.title | Forma"
    );
    expect(title(["__root__", "/_authenticated", "/_authenticated/account/settings/profile"])).toBe(
      "common.profile | Forma"
    );
  });

  test("translates a settings page's heading", () => {
    expect(title(["__root__", "/_authenticated", WS, `${WS}/settings/workspace/look`])).toBe(
      "common.appearance | Forma"
    );
  });

  test("is Forma for a page without a title", () => {
    expect(title(["__root__", "/auth", "/auth/verify-email"])).toBe("Forma");
  });

  test("names a survey's results from its loaded survey, under every analysis view", () => {
    const routes = [
      "__root__",
      "/_authenticated",
      WS,
      `${WS}/surveys/$surveyId`,
      `${WS}/surveys/$surveyId/summary`,
    ];
    expect(title(routes, { surveyId: "s1" })).toBe("common.survey | Forma");
    const queryClient = new QueryClient();
    queryClient.setQueryData(analysisSurveyQuery("s1").queryKey, { name: "NPS" } as never);
    expect(title(routes, { surveyId: "s1" }, queryClient)).toBe(
      'workspace.surveys.summary.survey_results:{"surveyName":"NPS"} | Forma'
    );
  });
});

test("every titled route exists", () => {
  const router = createRouter({ routeTree, context: { queryClient: new QueryClient() } });
  const missing = Object.keys(PAGE_TITLES).filter((routeId) => !(routeId in router.routesById));
  expect(missing).toEqual([]);
});

test("every translated title exists in all four catalogues", () => {
  const lookup = (catalogue: unknown, key: string) =>
    key
      .split(".")
      .reduce<unknown>((node, part) => (node as Record<string, unknown> | undefined)?.[part], catalogue);
  const keys = Object.values(PAGE_TITLES).flatMap((source) =>
    typeof source !== "function" && "key" in source ? [source.key] : []
  );
  const missing = [enUS, jaJP, zhHansCN, zhHantTW].flatMap((catalogue) =>
    keys.filter((key) => typeof lookup(catalogue, key) !== "string")
  );
  expect(keys.length).toBeGreaterThan(10);
  expect(missing).toEqual([]);
});
