import "server-only";

export const AUTHORIZATION_RESOURCE_CATEGORIES = {
  AUTHENTICATION_OR_APPLICATION: "authentication_or_application",
  DIRECT_AUTHORIZATION_RESOURCE: "direct_authorization_resource",
  PARENT_DERIVED_OR_DATA_INTEGRITY: "parent_derived_or_data_integrity",
  PUBLIC_OR_OUT_OF_SCOPE: "public_or_out_of_scope",
  RELATIONSHIP_OR_GRANT_SOURCE: "relationship_or_grant_source",
  WORKSPACE_INHERITED_RESOURCE: "workspace_inherited_resource",
} as const;

type TResourceCategory =
  (typeof AUTHORIZATION_RESOURCE_CATEGORIES)[keyof typeof AUTHORIZATION_RESOURCE_CATEGORIES];

/**
 * Non-runtime review inventory for every Prisma model.
 *
 * Workflows inherit workspace authorization.
 */
export const PRISMA_AUTHORIZATION_RESOURCE_INVENTORY = {
  Account: "authentication_or_application",
  ActionClass: "workspace_inherited_resource",
  ApiKey: "relationship_or_grant_source",
  ApiKeyWorkspace: "relationship_or_grant_source",
  AuthzedProjectionOutbox: "authentication_or_application",
  Contact: "workspace_inherited_resource",
  ContactAttribute: "parent_derived_or_data_integrity",
  ContactAttributeKey: "workspace_inherited_resource",
  DataMigration: "public_or_out_of_scope",
  Display: "parent_derived_or_data_integrity",
  EmbeddedData: "parent_derived_or_data_integrity",
  Integration: "workspace_inherited_resource",
  Invite: "authentication_or_application",
  Language: "parent_derived_or_data_integrity",
  Membership: "relationship_or_grant_source",
  Organization: "direct_authorization_resource",
  OrganizationBilling: "authentication_or_application",
  // This is a Prisma model name and a classification label, not a credential or credential value.
  PasswordResetToken: "authentication_or_application", // NOSONAR
  Response: "direct_authorization_resource",
  ResponsePipelineOutbox: "authentication_or_application",
  ResponseQuotaLink: "parent_derived_or_data_integrity",
  Segment: "workspace_inherited_resource",
  Session: "authentication_or_application",
  Survey: "direct_authorization_resource",
  SurveyAttributeFilter: "parent_derived_or_data_integrity",
  SurveyEmbeddedData: "parent_derived_or_data_integrity",
  SurveyFollowUp: "parent_derived_or_data_integrity",
  SurveyLanguage: "parent_derived_or_data_integrity",
  SurveyQuota: "parent_derived_or_data_integrity",
  SurveyTrigger: "parent_derived_or_data_integrity",
  Tag: "workspace_inherited_resource",
  TagsOnResponses: "parent_derived_or_data_integrity",
  Team: "direct_authorization_resource",
  TeamUser: "relationship_or_grant_source",
  TwoFactor: "authentication_or_application",
  User: "authentication_or_application",
  VerificationToken: "authentication_or_application",
  Webhook: "workspace_inherited_resource",
  Workflow: "workspace_inherited_resource",
  WorkflowRun: "parent_derived_or_data_integrity",
  WorkflowRunLog: "parent_derived_or_data_integrity",
  WorkflowVersion: "parent_derived_or_data_integrity",
  Workspace: "direct_authorization_resource",
  WorkspaceTeam: "relationship_or_grant_source",
  jwks: "authentication_or_application",
  oauthAccessToken: "authentication_or_application",
  oauthClient: "authentication_or_application",
  oauthClientAssertion: "authentication_or_application",
  oauthClientResource: "authentication_or_application",
  oauthConsent: "authentication_or_application",
  oauthRefreshToken: "authentication_or_application",
  oauthResource: "authentication_or_application",
} as const satisfies Readonly<Record<string, TResourceCategory>>;

/** Audit-only targets use a separate namespace so each target and each Prisma model is classified once. */
export const AUDIT_TARGET_AUTHORIZATION_RESOURCE_INVENTORY = {
  actionClass: "workspace_inherited_resource",
  apiKey: "relationship_or_grant_source",
  contact: "workspace_inherited_resource",
  contactAttributeKey: "workspace_inherited_resource",
  cubeQuery: "parent_derived_or_data_integrity",
  file: "workspace_inherited_resource",
  integration: "workspace_inherited_resource",
  invite: "authentication_or_application",
  language: "parent_derived_or_data_integrity",
  membership: "relationship_or_grant_source",
  organization: "direct_authorization_resource",
  quota: "parent_derived_or_data_integrity",
  response: "direct_authorization_resource",
  segment: "workspace_inherited_resource",
  survey: "direct_authorization_resource",
  tag: "workspace_inherited_resource",
  team: "direct_authorization_resource",
  twoFactorAuth: "authentication_or_application",
  user: "authentication_or_application",
  webhook: "workspace_inherited_resource",
  workflow: "workspace_inherited_resource",
  workspace: "direct_authorization_resource",
  workspaceTeam: "relationship_or_grant_source",
} as const satisfies Readonly<Record<string, TResourceCategory>>;
