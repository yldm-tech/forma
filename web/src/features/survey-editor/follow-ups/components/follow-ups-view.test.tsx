import { renderToStaticMarkup } from "react-dom/server";
import type { TSurvey } from "@forma/types/surveys/types";
import { describe, expect, test } from "vite-plus/test";
import { FollowUpsView } from "./follow-ups-view";

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
