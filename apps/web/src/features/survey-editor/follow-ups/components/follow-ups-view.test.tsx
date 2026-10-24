import { renderToStaticMarkup } from "react-dom/server";
import type { TSurvey } from "@forma/types/surveys/types";
import { describe, expect, test } from "vite-plus/test";
import { FollowUpsView } from "./follow-ups-view";

describe("FollowUpsView", () => {
  test("links New workflow to the survey's workspace workflows and the deprecation notice to the docs", () => {
    const html = renderToStaticMarkup(
      <FollowUpsView
        localSurvey={
          { followUps: [], workspaceId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f" } as unknown as TSurvey
        }
        setLocalSurvey={() => {}}
        selectedLanguageCode="default"
        userEmail="me@example.com"
        teamMemberDetails={[]}
        locale="en-US"
      />
    );
    expect(html).toContain('href="/app/workspaces/0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f/workflows"');
    expect(html).toContain('href="https://forma.yldm.ai/docs/workflows/overview"');
  });
});
