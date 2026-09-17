import { afterEach, describe, expect, test, vi } from "vitest";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/constants";
import { TSurveyCreateInputWithWorkspaceId, TSurveyQuestionTypeEnum } from "@forma/types/surveys/types";
import { responses } from "@/lib/api/response";
import { getIsSpamProtectionEnabled } from "@/modules/license-check/lib/utils";
import { getSurveyFollowUpsPermission } from "@/modules/survey/follow-ups/lib/utils";
import { getExternalUrlsPermission } from "@/modules/survey/lib/permission";
import { checkFeaturePermissions } from "./utils";

// Mock dependencies
vi.mock("@/lib/api/response", () => ({
  responses: {
    forbiddenResponse: vi.fn((message) => new Response(message, { status: 403 })),
  },
}));

vi.mock("@/modules/license-check/lib/utils", () => ({
  getIsSpamProtectionEnabled: vi.fn(),
}));

vi.mock("@/modules/survey/follow-ups/lib/utils", () => ({
  getSurveyFollowUpsPermission: vi.fn(),
}));

vi.mock("@/modules/survey/lib/permission", () => ({
  getExternalUrlsPermission: vi.fn().mockResolvedValue(true),
}));

vi.mock("@/lib/survey/utils", () => ({
  getElementsFromBlocks: vi.fn((blocks: any[]) => blocks.flatMap((block: any) => block.elements)),
}));

const mockFollowUp: TSurveyCreateInputWithWorkspaceId["followUps"][number] = {
  id: "followup1",
  surveyId: "mockSurveyId",
  name: "Test Follow-up",
  trigger: {
    type: "response",
    properties: null,
  },
  action: {
    type: "send-email",
    properties: {
      to: "mockQuestion1Id",
      from: "noreply@example.com",
      replyTo: [],
      subject: "Follow-up Subject",
      body: "Follow-up Body",
      attachResponseData: false,
    },
  },
};

const mockLanguage: TSurveyCreateInputWithWorkspaceId["languages"][number] = {
  language: {
    id: "lang1",
    code: "en",
    alias: "English",
    createdAt: new Date(),
    workspaceId: "mockWorkspaceId",
    updatedAt: new Date(),
  },
  default: true,
  enabled: true,
};

const baseSurveyData: TSurveyCreateInputWithWorkspaceId = {
  name: "Test Survey",
  workspaceId: "mockWorkspaceId",
  questions: [
    {
      id: "q1",
      type: TSurveyQuestionTypeEnum.OpenText,
      headline: { default: "Q1" },
      required: false,
      charLimit: {},
      inputType: "text",
    },
  ],
  endings: [],
  languages: [],
  type: "link",
  welcomeCard: { enabled: false, showResponseCount: false, timeToFinish: false },
  blocks: [],
  followUps: [],
  isAutoProgressingEnabled: false,
};

describe("checkFeaturePermissions", () => {
  vi.mocked(getExternalUrlsPermission).mockResolvedValue(true);

  afterEach(() => {
    vi.clearAllMocks();
    vi.mocked(getExternalUrlsPermission).mockResolvedValue(true);
  });

  test("should return null if no restricted features are used", async () => {
    const surveyData = { ...baseSurveyData };
    const result = await checkFeaturePermissions(surveyData);
    expect(result).toBeNull();
  });

  // Recaptcha tests
  test("should return forbiddenResponse if recaptcha is enabled but permission denied", async () => {
    vi.mocked(getIsSpamProtectionEnabled).mockResolvedValue(false);
    const surveyData = { ...baseSurveyData, recaptcha: { enabled: true, threshold: 0.5 } };
    const result = await checkFeaturePermissions(surveyData);
    expect(result).toBeInstanceOf(Response);
    expect(result?.status).toBe(403);
    expect(responses.forbiddenResponse).toHaveBeenCalledWith(
      "Spam protection is not enabled for this organization"
    );
  });

  test("should return null if recaptcha is enabled and permission granted", async () => {
    vi.mocked(getIsSpamProtectionEnabled).mockResolvedValue(true);
    const surveyData: TSurveyCreateInputWithWorkspaceId = {
      ...baseSurveyData,
      recaptcha: { enabled: true, threshold: 0.5 },
    };
    const result = await checkFeaturePermissions(surveyData);
    expect(result).toBeNull();
  });

  // Follow-ups tests

  test("should return null if follow-ups are used and permission granted", async () => {
    vi.mocked(getSurveyFollowUpsPermission).mockResolvedValue(true);
    const surveyData = { ...baseSurveyData, followUps: [mockFollowUp] }; // Add minimal follow-up data
    const result = await checkFeaturePermissions(surveyData);
    expect(result).toBeNull();
  });

  // Combined tests
  test("should return null if multiple features are used and all permissions granted", async () => {
    vi.mocked(getIsSpamProtectionEnabled).mockResolvedValue(true);
    vi.mocked(getSurveyFollowUpsPermission).mockResolvedValue(true);
    const surveyData = {
      ...baseSurveyData,
      recaptcha: { enabled: true, threshold: 0.5 },
      followUps: [mockFollowUp],
      languages: [mockLanguage],
    };
    const result = await checkFeaturePermissions(surveyData);
    expect(result).toBeNull();
  });

  test("should return forbiddenResponse for the first denied feature (recaptcha)", async () => {
    vi.mocked(getIsSpamProtectionEnabled).mockResolvedValue(false); // Denied
    vi.mocked(getSurveyFollowUpsPermission).mockResolvedValue(true);
    const surveyData = {
      ...baseSurveyData,
      recaptcha: { enabled: true, threshold: 0.5 },
      followUps: [mockFollowUp],
      languages: [mockLanguage],
    };
    const result = await checkFeaturePermissions(surveyData);
    expect(result).toBeInstanceOf(Response);
    expect(result?.status).toBe(403);
    expect(responses.forbiddenResponse).toHaveBeenCalledWith(
      "Spam protection is not enabled for this organization"
    );
    expect(responses.forbiddenResponse).toHaveBeenCalledTimes(1); // Ensure it stops at the first failure
  });

  // External URLs - ending card button link tests

  test("should allow ending buttonLink when permission is granted", async () => {
    vi.mocked(getExternalUrlsPermission).mockResolvedValue(true);
    const surveyData = {
      ...baseSurveyData,
      endings: [
        {
          id: "ending1",
          type: "endScreen" as const,
          headline: { default: "Thanks" },
          subheader: { default: "" },
          buttonLink: "https://example.com",
          buttonLabel: { default: "Click" },
        },
      ],
    };
    const result = await checkFeaturePermissions(surveyData);
    expect(result).toBeNull();
  });

  // External URLs - CTA external button tests

  test("should allow CTA external button when permission is granted", async () => {
    vi.mocked(getExternalUrlsPermission).mockResolvedValue(true);
    const surveyData = {
      ...baseSurveyData,
      blocks: [
        {
          id: "block1",
          name: "Block 1",
          elements: [
            {
              id: "cta1",
              type: TSurveyElementTypeEnum.CTA,
              headline: { default: "CTA" },
              required: false,
              buttonExternal: true,
              buttonUrl: "https://example.com",
              ctaButtonLabel: { default: "Click" },
            },
          ],
          buttonLabel: { default: "Next" },
        },
      ],
    };
    const result = await checkFeaturePermissions(surveyData as any);
    expect(result).toBeNull();
  });
});
