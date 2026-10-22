import { renderToStaticMarkup } from "react-dom/server";
import type { TSurvey } from "@forma/types/surveys/types";
import { describe, expect, test, vi } from "vite-plus/test";
import { FollowUpsView } from "./follow-ups-view";

// Under Vitest, react-i18next resolves a React of its own, whose hooks cannot run inside this render; the view's text is not what is tested.
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

describe("FollowUpsView", () => {
  // The Workflows pages are not ported, so a link to them would open the not-found page.
  test("links to no page this app does not have", () => {
    const html = renderToStaticMarkup(
      <FollowUpsView
        localSurvey={{ followUps: [] } as unknown as TSurvey}
        setLocalSurvey={() => {}}
        selectedLanguageCode="default"
        userEmail="me@example.com"
        teamMemberDetails={[]}
        locale="en-US"
      />
    );
    expect(html).not.toMatch(/href="\/workspaces\//);
    expect(html).toContain('href="https://forma.yldm.ai/docs/workflows/overview"');
  });
});
