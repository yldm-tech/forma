import type { TSurvey } from "@forma/types/surveys/types";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { type CustomScriptsAvailability, instanceQuery } from "../../../../lib/queries/instance";
import { CustomHtmlTab } from "./custom-html-tab";

const survey = {
  id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d12",
  customHeadScripts: "<script>window.survey=1</script>",
  customHeadScriptsMode: "add",
} as TSurvey;

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

const renderTab = ({
  customScripts,
  isOwnerOrManager,
  isReadOnly = false,
}: {
  customScripts: CustomScriptsAvailability;
  isOwnerOrManager: boolean;
  isReadOnly?: boolean;
}) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(instanceQuery.queryKey, {
    storageConfigured: true,
    customScripts,
    version: "0.2.10",
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <CustomHtmlTab
          survey={survey}
          workspaceCustomScripts={null}
          isReadOnly={isReadOnly}
          isOwnerOrManager={isOwnerOrManager}
        />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const scripts = () =>
  screen.getByLabelText<HTMLTextAreaElement>(
    i18n.t("workspace.surveys.share.custom_html.survey_scripts_label")
  );

describe("CustomHtmlTab", () => {
  test("an owner or manager edits the survey's scripts where the instance runs them", () => {
    renderTab({ customScripts: "available", isOwnerOrManager: true });
    expect(scripts().disabled).toBe(false);
    expect(screen.queryByText(i18n.t("workspace.custom_scripts_access.managers_only"))).toBeNull();
  });

  test("a member who may edit the survey sees its scripts but cannot change them", () => {
    renderTab({ customScripts: "available", isOwnerOrManager: false });
    expect(scripts().disabled).toBe(true);
    expect(scripts().value).toBe(survey.customHeadScripts);
    screen.getByText(i18n.t("workspace.custom_scripts_access.managers_only"));
  });

  test.each<[CustomScriptsAvailability, string]>([
    ["requires_public_url", "workspace.custom_scripts_access.requires_public_url"],
    ["forma_cloud", "workspace.custom_scripts_access.unavailable_cloud"],
  ])(
    "on an instance that does not run them (%s) nobody changes them, and the tab says why",
    (customScripts, key) => {
      renderTab({ customScripts, isOwnerOrManager: true });
      expect(scripts().disabled).toBe(true);
      screen.getByText(i18n.t(key));
    }
  );

  test("the operator learns what to set", () => {
    expect(i18n.t("workspace.custom_scripts_access.requires_public_url")).toContain("PUBLIC_URL");
  });
});
