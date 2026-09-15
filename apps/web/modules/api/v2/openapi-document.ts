import * as yaml from "yaml";
import { createDocument } from "zod-openapi";
import { ZApiKeyData } from "@forma/database/zod/api-keys";
import { ZContact } from "@forma/database/zod/contact";
import { ZContactAttributeKey } from "@forma/database/zod/contact-attribute-keys";
import { ZContactAttribute } from "@forma/database/zod/contact-attributes";
import { ZResponse } from "@forma/database/zod/responses";
import { ZRoles } from "@forma/database/zod/roles";
import { ZSurveyWithoutQuestionType } from "@forma/database/zod/surveys";
import { ZTeam } from "@forma/database/zod/teams";
import { ZUser } from "@forma/database/zod/users";
import { ZWebhook, ZWebhookWithoutSecret } from "@forma/database/zod/webhooks";
import { ZWorkspaceTeam } from "@forma/database/zod/workspace-teams";
import { healthPaths } from "@/modules/api/v2/health/lib/openapi";
import { ZOverallHealthStatus } from "@/modules/api/v2/health/types/health-status";
import { contactAttributeKeyPaths } from "@/modules/api/v2/management/contact-attribute-keys/lib/openapi";
import { responsePaths } from "@/modules/api/v2/management/responses/lib/openapi";
import { surveyContactLinksBySegmentPaths } from "@/modules/api/v2/management/surveys/[surveyId]/contact-links/segments/lib/openapi";
import { surveyPaths } from "@/modules/api/v2/management/surveys/lib/openapi";
import { webhookPaths } from "@/modules/api/v2/management/webhooks/lib/openapi";
import { mePaths } from "@/modules/api/v2/me/lib/openapi";
import { teamPaths } from "@/modules/api/v2/organizations/[organizationId]/teams/lib/openapi";
import { userPaths } from "@/modules/api/v2/organizations/[organizationId]/users/lib/openapi";
import { workspaceTeamPaths } from "@/modules/api/v2/organizations/[organizationId]/workspace-teams/lib/openapi";
import { rolePaths } from "@/modules/api/v2/roles/lib/openapi";
import { bulkContactPaths } from "@/modules/ee/contacts/api/v2/management/contacts/bulk/lib/openapi";
import { contactPaths } from "@/modules/ee/contacts/api/v2/management/contacts/lib/openapi";

const document = createDocument({
  openapi: "3.1.0",
  info: {
    title: "Forma API",
    description: "Manage Forma resources programmatically.",
    version: "2.0.0",
  },
  paths: {
    ...healthPaths,
    ...rolePaths,
    ...mePaths,
    ...responsePaths,
    ...bulkContactPaths,
    ...contactPaths,
    ...contactAttributeKeyPaths,
    ...surveyPaths,
    ...surveyContactLinksBySegmentPaths,
    ...webhookPaths,
    ...teamPaths,
    ...workspaceTeamPaths,
    ...userPaths,
  },
  servers: [
    {
      url: "https://app.forma.ylam.ai/api/v2",
      description: "Forma Cloud",
    },
  ],
  tags: [
    {
      name: "Health",
      description: "Operations for checking critical application dependencies health status.",
    },
    {
      name: "Roles",
      description: "Operations for managing roles.",
    },
    {
      name: "Me",
      description: "Operations for managing your API key.",
    },
    {
      name: "Management API - Responses",
      description: "Operations for managing responses.",
    },
    {
      name: "Management API - Contacts",
      description: "Operations for managing contacts.",
    },
    {
      name: "Management API - Contact Attributes",
      description: "Operations for managing contact attributes.",
    },
    {
      name: "Management API - Contact Attribute Keys",
      description: "Operations for managing contact attribute keys.",
    },
    {
      name: "Management API - Surveys",
      description: "Operations for managing surveys.",
    },
    {
      name: "Management API - Surveys - Contact Links",
      description: "Operations for generating personalized survey links for contacts.",
    },
    {
      name: "Management API - Webhooks",
      description: "Operations for managing webhooks.",
    },
    {
      name: "Organizations API - Teams",
      description: "Operations for managing teams.",
    },
    {
      name: "Organizations API - Workspace Teams",
      description: "Operations for managing workspace teams.",
    },
    {
      name: "Organizations API - Users",
      description: "Operations for managing users.",
    },
  ],
  components: {
    securitySchemes: {
      apiKeyAuth: {
        type: "apiKey",
        in: "header",
        name: "x-api-key",
        description: "Use your Forma x-api-key to authenticate.",
      },
    },
    schemas: {
      health: ZOverallHealthStatus,
      role: ZRoles,
      me: ZApiKeyData,
      response: ZResponse,
      contact: ZContact,
      contactAttribute: ZContactAttribute,
      contactAttributeKey: ZContactAttributeKey,
      survey: ZSurveyWithoutQuestionType,
      webhook: ZWebhook,
      webhookWithoutSecret: ZWebhookWithoutSecret,
      team: ZTeam,
      workspaceTeam: ZWorkspaceTeam,
      user: ZUser,
    },
  },
  security: [
    {
      apiKeyAuth: [],
    },
  ],
});

// do not replace this with logger.info
console.log(yaml.stringify(document));
