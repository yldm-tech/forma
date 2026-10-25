import { cleanup, render, waitFor } from "@testing-library/react";
import type { SurveyContainerProps } from "@forma/types/forma-surveys";
import type { TSurvey } from "@forma/types/surveys/types";
import type { TWorkspace } from "@forma/types/workspace";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import type { SurveyTemplate } from "../../../features/surveys/api";
import { templatePreviewSurvey } from "../../../features/surveys/templates/preview";
import { initI18n } from "../../../lib/i18n";
import { PreviewSurvey } from "./index";

// The preview starts over when it is handed another survey (the template gallery switching templates), and never when the survey it shows is edited.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const workspace = {
  id: "0190a000-0000-7000-8000-000000000002",
  createdAt: new Date("2026-01-01T00:00:00Z"),
  updatedAt: new Date("2026-01-01T00:00:00Z"),
  name: "Main",
  organizationId: "0190a000-0000-7000-8000-000000000001",
  styling: { allowStyleOverwrite: true },
  recontactDays: 7,
  inAppSurveyBranding: true,
  linkSurveyBranding: false,
  config: { channel: null, industry: null },
  placement: "bottomRight",
  clickOutsideClose: true,
  overlay: "none",
  languages: [],
  appSetupCompleted: false,
  logo: null,
  customHeadScripts: null,
} as unknown as TWorkspace;

const block = (id: string, headline: string) => ({
  id,
  name: id,
  elements: [{ id: `${id}-q`, type: "openText", headline: { default: headline }, required: false }],
});

const survey = (id: string, blocks: ReturnType<typeof block>[]): TSurvey =>
  templatePreviewSurvey(
    { id, name: id, description: "", preset: { blocks } as SurveyTemplate["preset"] },
    "link"
  );

/**
 * Stubs the respondent runtime with the one piece of its state the preview drives: the block it shows. The preview moves it through the setter the runtime hands back with getSetBlockId; a respondent answering moves it too.
 */
const stubRuntime = () => {
  const runtime = { blockId: "", setBlockId: vi.fn((id: string) => void (runtime.blockId = id)) };
  const renderSurvey = vi.fn((props: SurveyContainerProps) => {
    props.getSetBlockId?.(runtime.setBlockId);
  });
  vi.stubGlobal("formaSurveys", { renderSurvey });
  return { runtime, renderSurvey };
};

const preview = (previewed: TSurvey) => (
  <I18nextProvider i18n={i18n}>
    <PreviewSurvey
      survey={previewed}
      workspace={workspace}
      previewType="fullwidth"
      languageCode="default"
      isSpamProtectionAllowed={false}
    />
  </I18nextProvider>
);

describe("PreviewSurvey", () => {
  test("starts over on the first block when it is handed another survey", async () => {
    const { runtime, renderSurvey } = stubRuntime();
    const { rerender } = render(preview(survey("nps", [block("nps-1", "Score"), block("nps-2", "Why")])));
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    // The respondent has moved on to the second block.
    runtime.blockId = "nps-2";

    rerender(preview(survey("csat", [block("csat-1", "Satisfied?"), block("csat-2", "Why")])));

    await waitFor(() => expect(runtime.blockId).toBe("csat-1"));
    await waitFor(() => expect(renderSurvey.mock.calls.at(-1)?.[0].survey.id).toBe("csat"));
  });

  test("an edit of the survey it shows does not start it over", async () => {
    const { runtime, renderSurvey } = stubRuntime();
    const { rerender } = render(preview(survey("draft", [block("b-1", "Score"), block("b-2", "Why")])));
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    runtime.blockId = "b-2";

    rerender(preview(survey("draft", [block("b-1", "Score"), block("b-2", "Why exactly?")])));

    await waitFor(() =>
      expect(renderSurvey.mock.calls.at(-1)?.[0].survey.blocks[1]?.elements[0]?.headline).toEqual({
        default: "Why exactly?",
      })
    );
    expect(runtime.setBlockId).not.toHaveBeenCalled();
    expect(runtime.blockId).toBe("b-2");
  });
});
