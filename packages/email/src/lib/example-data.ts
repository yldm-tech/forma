// Mock data for email templates to use in React Email preview server
import { TOrganization } from "@forma/types/organizations";
import { TResponse } from "@forma/types/responses";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { embedSurveyPreviewEmailHtml } from "./fixtures/embed-survey-preview-email-html";

export const exampleData = {
  verificationEmail: {
    verifyLink: "https://app.forma.ylam.ai/auth/verify?token=example-verification-token",
    verificationRequestLink: "https://app.forma.ylam.ai/auth/verification-requested",
  },

  forgotPasswordEmail: {
    verifyLink: "https://app.forma.ylam.ai/auth/forgot-password/reset?token=example-reset-token",
    linkValidityInMinutes: 30,
  },

  deleteAccountEmail: {
    deleteLink:
      "https://app.forma.ylam.ai/api/auth/delete-user/callback?token=example-delete-token&callbackURL=/",
    linkValidityInMinutes: 60,
  },

  newEmailVerification: {
    verifyLink: "https://app.forma.ylam.ai/verify-email-change?token=example-email-change-token",
  },

  passwordResetNotifyEmail: {
    // No props needed
  },

  ssoRecoveryFactorsRemovedEmail: {
    passwordRemoved: true,
    twoFactorRemoved: true,
    securitySettingsLink: "https://app.forma.ylam.ai/account/settings/profile",
  },

  inviteEmail: {
    inviteeName: "Jane Smith",
    inviterName: "John Doe",
    verifyLink: "https://app.forma.ylam.ai/invite?token=example-invite-token",
  },

  inviteAcceptedEmail: {
    inviterName: "John Doe",
    inviteeName: "Jane Smith",
  },

  linkSurveyEmail: {
    surveyName: "Customer Satisfaction Survey",
    surveyLink:
      "https://app.forma.ylam.ai/s/example-survey-id?verify=example-token&suId=example-single-use-id",
  },

  embedSurveyPreviewEmail: {
    html: embedSurveyPreviewEmailHtml,
    workspaceId: "workspace-123",
  },

  responseFinishedEmail: {
    survey: {
      id: "survey-123",
      name: "Customer Feedback Survey",
      variables: [
        {
          id: "var-1",
          name: "Customer ID",
          type: "text" as const,
        },
      ],
      hiddenFields: {
        enabled: true,
        fieldIds: ["userId"],
      },
      welcomeCard: {
        enabled: false,
      },
      questions: [
        {
          id: "q1",
          type: "openText" as const,
          headline: { default: "What did you like most?" },
          required: true,
          inputType: "text" as const,
        },
        {
          id: "q2",
          type: "rating" as const,
          headline: { default: "How would you rate your experience?" },
          required: true,
          scale: "number" as const,
          range: 5,
        },
      ],
      endings: [],
      styling: {},
      createdBy: null,
    } as unknown as TSurvey,
    responseCount: 15,
    response: {
      id: "response-123",
      createdAt: new Date(),
      updatedAt: new Date(),
      surveyId: "survey-123",
      finished: true,
      data: {
        q1: "The customer service was excellent!",
        q2: 5,
        userId: "user-abc-123",
      },
      variables: {
        "var-1": "CUST-456",
      },
      contactAttributes: {
        email: "customer@example.com",
      },
      meta: {
        userAgent: {},
        url: "https://example.com",
      },
      tags: [],
      ttc: {},
      singleUseId: null,
      language: "default",
      displayId: null,
    } as unknown as TResponse,
    WEBAPP_URL: "https://app.forma.ylam.ai",
    workspaceId: "workspace-123",
    organization: {
      id: "org-123",
      name: "Acme Corporation",
      createdAt: new Date(),
      updatedAt: new Date(),
      billing: {
        stripeCustomerId: null,
        subscriptionStatus: null,
        features: {
          inAppSurvey: { status: "active" as const, unlimited: true },
          linkSurvey: { status: "active" as const, unlimited: true },
          userTargeting: { status: "active" as const, unlimited: true },
        },
        limits: {
          monthly: {
            responses: 1000,
          },
        },
      },
      isAISmartToolsEnabled: false,
    } as unknown as TOrganization,
  },

  followUpEmail: {
    body: "<p>Thank you for your feedback! We've received your response and will review it shortly.</p><p>Here's a summary of what you submitted:</p>",
    responseData: [
      {
        element: "What did you like most?",
        response: "The customer service was excellent!",
        type: TSurveyElementTypeEnum.OpenText,
      },
      {
        element: "How would you rate your experience?",
        response: "5",
        type: TSurveyElementTypeEnum.Rating,
      },
    ],
    variables: [
      {
        id: "var-1",
        name: "Customer ID",
        type: "text" as const,
        value: "CUST-456",
      },
    ],
    hiddenFields: [
      {
        id: "userId",
        value: "user-abc-123",
      },
    ],
  },

  emailCustomizationPreviewEmail: {
    userName: "Alex Johnson",
  },

  legalProps: {
    privacyUrl: "https://forma.ylam.ai/privacy",
    termsUrl: "https://forma.ylam.ai/terms",
    imprintUrl: "https://forma.ylam.ai/imprint",
    imprintAddress: "Forma GmbH, Example Street 123, 12345 Berlin, Germany",
  },
};

export type ExampleDataKeys = keyof typeof exampleData;
export type ExampleData<K extends ExampleDataKeys> = (typeof exampleData)[K];
