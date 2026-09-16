-- Initial schema for Forma.
--
-- This fork squashed upstream's 202 migrations into one baseline: it has no deployment to upgrade,
-- and the old history carried the Formbricks brand in two directory names plus the Unify Feedback
-- and Dashboards tables this fork removed. Upgrading a database that ran upstream's migrations is
-- therefore not a supported path from here.
--
-- The three ignored rules all describe changing a table that already holds rows: building an index
-- without CONCURRENTLY, and adding a foreign key that has to validate existing rows. Creating an
-- empty schema can do neither, so they are noise here rather than findings.
-- squawk-ignore-file require-concurrent-index-creation, constraint-missing-not-valid, adding-foreign-key-constraint

BEGIN;

SET LOCAL lock_timeout = '5s';

-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "public";

-- CreateExtension
CREATE EXTENSION IF NOT EXISTS "vector";

-- CreateEnum
CREATE TYPE "PipelineTriggers" AS ENUM ('responseCreated', 'responseUpdated', 'responseFinished');

-- CreateEnum
CREATE TYPE "WebhookSource" AS ENUM ('user', 'zapier', 'make', 'n8n', 'activepieces');

-- CreateEnum
CREATE TYPE "ContactAttributeType" AS ENUM ('default', 'custom');

-- CreateEnum
CREATE TYPE "ContactAttributeDataType" AS ENUM ('string', 'number', 'date');

-- CreateEnum
CREATE TYPE "SurveyStatus" AS ENUM ('draft', 'inProgress', 'paused', 'completed');

-- CreateEnum
CREATE TYPE "SurveyAttributeFilterCondition" AS ENUM ('equals', 'notEquals');

-- CreateEnum
CREATE TYPE "SurveyQuotaAction" AS ENUM ('endSurvey', 'continueSurvey');

-- CreateEnum
CREATE TYPE "ResponseQuotaLinkStatus" AS ENUM ('screenedIn', 'screenedOut');

-- CreateEnum
CREATE TYPE "SurveyType" AS ENUM ('link', 'app');

-- CreateEnum
CREATE TYPE "displayOptions" AS ENUM ('displayOnce', 'displayMultiple', 'displaySome', 'respondMultiple');

-- CreateEnum
CREATE TYPE "SurveyScriptMode" AS ENUM ('add', 'replace');

-- CreateEnum
CREATE TYPE "ActionType" AS ENUM ('code', 'noCode');

-- CreateEnum
CREATE TYPE "IntegrationType" AS ENUM ('googleSheets', 'notion', 'airtable', 'slack');

-- CreateEnum
CREATE TYPE "DataMigrationStatus" AS ENUM ('pending', 'applied', 'failed');

-- CreateEnum
CREATE TYPE "WidgetPlacement" AS ENUM ('bottomLeft', 'bottomRight', 'topLeft', 'topRight', 'center');

-- CreateEnum
CREATE TYPE "SurveyOverlay" AS ENUM ('none', 'light', 'dark');

-- CreateEnum
CREATE TYPE "OrganizationRole" AS ENUM ('owner', 'manager', 'member', 'billing');

-- CreateEnum
CREATE TYPE "ApiKeyPermission" AS ENUM ('read', 'write', 'manage');

-- CreateEnum
CREATE TYPE "IdentityProvider" AS ENUM ('email', 'github', 'google', 'azuread', 'openid', 'saml');

-- CreateEnum
CREATE TYPE "EmbeddedDataSource" AS ENUM ('computed', 'ingested', 'reserved');

-- CreateEnum
CREATE TYPE "EmbeddedDataType" AS ENUM ('string', 'number', 'boolean', 'date');

-- CreateEnum
CREATE TYPE "TeamUserRole" AS ENUM ('admin', 'contributor');

-- CreateEnum
CREATE TYPE "WorkspaceTeamPermission" AS ENUM ('read', 'readWrite', 'manage');

-- CreateEnum
CREATE TYPE "ChartType" AS ENUM ('area', 'bar', 'pie', 'big_number');

-- CreateEnum
CREATE TYPE "WorkflowStatus" AS ENUM ('draft', 'enabled', 'disabled', 'archived');

-- CreateEnum
CREATE TYPE "WorkflowRunStatus" AS ENUM ('queued', 'running', 'completed', 'failed', 'canceled');

-- CreateEnum
CREATE TYPE "WorkflowRunLogStatus" AS ENUM ('pending', 'running', 'succeeded', 'failed', 'skipped');

-- CreateTable
CREATE TABLE "Webhook" (
    "id" TEXT NOT NULL,
    "name" TEXT,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "url" TEXT NOT NULL,
    "source" "WebhookSource" NOT NULL DEFAULT 'user',
    "workspaceId" TEXT NOT NULL,
    "triggers" "PipelineTriggers"[],
    "surveyIds" TEXT[],
    "secret" TEXT,

    CONSTRAINT "Webhook_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "ContactAttribute" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "attributeKeyId" TEXT NOT NULL,
    "contactId" TEXT NOT NULL,
    "value" TEXT NOT NULL,
    "valueNumber" DOUBLE PRECISION,
    "valueDate" TIMESTAMP(3),

    CONSTRAINT "ContactAttribute_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "ContactAttributeKey" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "isUnique" BOOLEAN NOT NULL DEFAULT false,
    "key" TEXT NOT NULL,
    "name" TEXT,
    "description" TEXT,
    "type" "ContactAttributeType" NOT NULL DEFAULT 'custom',
    "dataType" "ContactAttributeDataType" NOT NULL DEFAULT 'string',
    "workspaceId" TEXT NOT NULL,

    CONSTRAINT "ContactAttributeKey_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Contact" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "workspaceId" TEXT NOT NULL,

    CONSTRAINT "Contact_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Response" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "finished" BOOLEAN NOT NULL DEFAULT false,
    "surveyId" TEXT NOT NULL,
    "contactId" TEXT,
    "endingId" TEXT,
    "data" JSONB NOT NULL DEFAULT '{}',
    "variables" JSONB NOT NULL DEFAULT '{}',
    "ttc" JSONB NOT NULL DEFAULT '{}',
    "meta" JSONB NOT NULL DEFAULT '{}',
    "ingest_flags" JSONB,
    "contactAttributes" JSONB,
    "singleUseId" TEXT,
    "language" TEXT,
    "displayId" TEXT,

    CONSTRAINT "Response_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Tag" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "workspaceId" TEXT NOT NULL,

    CONSTRAINT "Tag_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "TagsOnResponses" (
    "responseId" TEXT NOT NULL,
    "tagId" TEXT NOT NULL,

    CONSTRAINT "TagsOnResponses_pkey" PRIMARY KEY ("responseId","tagId")
);

-- CreateTable
CREATE TABLE "Display" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "surveyId" TEXT NOT NULL,
    "contactId" TEXT,

    CONSTRAINT "Display_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SurveyTrigger" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "surveyId" TEXT NOT NULL,
    "actionClassId" TEXT NOT NULL,

    CONSTRAINT "SurveyTrigger_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SurveyAttributeFilter" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "attributeKeyId" TEXT NOT NULL,
    "surveyId" TEXT NOT NULL,
    "condition" "SurveyAttributeFilterCondition" NOT NULL,
    "value" TEXT NOT NULL,

    CONSTRAINT "SurveyAttributeFilter_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Survey" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "redirectUrl" TEXT,
    "type" "SurveyType" NOT NULL DEFAULT 'app',
    "workspaceId" TEXT NOT NULL,
    "createdBy" TEXT,
    "status" "SurveyStatus" NOT NULL DEFAULT 'draft',
    "welcomeCard" JSONB NOT NULL DEFAULT '{"enabled": false}',
    "questions" JSONB NOT NULL DEFAULT '[]',
    "blocks" JSONB[] DEFAULT ARRAY[]::JSONB[],
    "endings" JSONB[] DEFAULT ARRAY[]::JSONB[],
    "hiddenFields" JSONB NOT NULL DEFAULT '{"enabled": false}',
    "variables" JSONB NOT NULL DEFAULT '[]',
    "displayOption" "displayOptions" NOT NULL DEFAULT 'displayOnce',
    "recontactDays" INTEGER,
    "displayLimit" INTEGER,
    "inlineTriggers" JSONB,
    "autoClose" INTEGER,
    "autoComplete" INTEGER,
    "delay" INTEGER NOT NULL DEFAULT 0,
    "publishOn" TIMESTAMP(3),
    "closeOn" TIMESTAMP(3),
    "archivedAt" TIMESTAMP(3),
    "surveyClosedMessage" JSONB,
    "segmentId" TEXT,
    "workspaceOverwrites" JSONB,
    "styling" JSONB,
    "singleUse" JSONB DEFAULT '{"enabled": false, "isEncrypted": true}',
    "isVerifyEmailEnabled" BOOLEAN NOT NULL DEFAULT false,
    "isBackButtonHidden" BOOLEAN NOT NULL DEFAULT false,
    "isAutoProgressingEnabled" BOOLEAN NOT NULL DEFAULT false,
    "isCaptureIpEnabled" BOOLEAN NOT NULL DEFAULT false,
    "is_anonymize_responses_enabled" BOOLEAN NOT NULL DEFAULT false,
    "pin" TEXT,
    "displayPercentage" DECIMAL(65,30),
    "showLanguageSwitch" BOOLEAN,
    "recaptcha" JSONB DEFAULT '{"enabled": false, "threshold":0.1}',
    "metadata" JSONB NOT NULL DEFAULT '{}',
    "slug" TEXT,
    "customHeadScripts" TEXT,
    "customHeadScriptsMode" "SurveyScriptMode" DEFAULT 'add',

    CONSTRAINT "Survey_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SurveyQuota" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "surveyId" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "limit" INTEGER NOT NULL,
    "logic" JSONB NOT NULL DEFAULT '{}',
    "action" "SurveyQuotaAction" NOT NULL,
    "endingCardId" TEXT,
    "countPartialSubmissions" BOOLEAN NOT NULL DEFAULT false,

    CONSTRAINT "SurveyQuota_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "ResponseQuotaLink" (
    "responseId" TEXT NOT NULL,
    "quotaId" TEXT NOT NULL,
    "status" "ResponseQuotaLinkStatus" NOT NULL,

    CONSTRAINT "ResponseQuotaLink_pkey" PRIMARY KEY ("responseId","quotaId")
);

-- CreateTable
CREATE TABLE "SurveyFollowUp" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "surveyId" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "trigger" JSONB NOT NULL,
    "action" JSONB NOT NULL,

    CONSTRAINT "SurveyFollowUp_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "ActionClass" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "description" TEXT,
    "type" "ActionType" NOT NULL,
    "key" TEXT,
    "noCodeConfig" JSONB,
    "workspaceId" TEXT NOT NULL,

    CONSTRAINT "ActionClass_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Integration" (
    "id" TEXT NOT NULL,
    "type" "IntegrationType" NOT NULL,
    "config" JSONB NOT NULL,
    "workspaceId" TEXT NOT NULL,

    CONSTRAINT "Integration_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "DataMigration" (
    "id" TEXT NOT NULL,
    "started_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "finished_at" TIMESTAMP(3),
    "name" TEXT NOT NULL,
    "status" "DataMigrationStatus" NOT NULL,

    CONSTRAINT "DataMigration_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Workspace" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "legacyEnvironmentId" TEXT,
    "organizationId" TEXT NOT NULL,
    "styling" JSONB NOT NULL DEFAULT '{"allowStyleOverwrite":true}',
    "config" JSONB NOT NULL DEFAULT '{}',
    "recontactDays" INTEGER NOT NULL DEFAULT 7,
    "linkSurveyBranding" BOOLEAN NOT NULL DEFAULT true,
    "inAppSurveyBranding" BOOLEAN NOT NULL DEFAULT true,
    "placement" "WidgetPlacement" NOT NULL DEFAULT 'bottomRight',
    "clickOutsideClose" BOOLEAN NOT NULL DEFAULT true,
    "overlay" "SurveyOverlay" NOT NULL DEFAULT 'none',
    "logo" JSONB,
    "appSetupCompleted" BOOLEAN NOT NULL DEFAULT false,
    "customHeadScripts" TEXT,

    CONSTRAINT "Workspace_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Organization" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "whitelabel" JSONB NOT NULL DEFAULT '{}',
    "isAISmartToolsEnabled" BOOLEAN NOT NULL DEFAULT false,
    "displayTimeZone" TEXT,

    CONSTRAINT "Organization_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "OrganizationBilling" (
    "organization_id" TEXT NOT NULL,
    "stripe_customer_id" TEXT,
    "limits" JSONB NOT NULL,
    "usage_cycle_anchor" TIMESTAMP(3),
    "stripe" JSONB,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "OrganizationBilling_pkey" PRIMARY KEY ("organization_id")
);

-- CreateTable
CREATE TABLE "Membership" (
    "organizationId" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "accepted" BOOLEAN NOT NULL DEFAULT false,
    "role" "OrganizationRole" NOT NULL DEFAULT 'member',

    CONSTRAINT "Membership_pkey" PRIMARY KEY ("userId","organizationId")
);

-- CreateTable
CREATE TABLE "Invite" (
    "id" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "name" TEXT,
    "organizationId" TEXT NOT NULL,
    "creatorId" TEXT NOT NULL,
    "acceptorId" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "expiresAt" TIMESTAMP(3) NOT NULL,
    "role" "OrganizationRole" NOT NULL DEFAULT 'member',
    "teamIds" TEXT[] DEFAULT ARRAY[]::TEXT[],

    CONSTRAINT "Invite_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "ApiKey" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "createdBy" TEXT,
    "lastUsedAt" TIMESTAMP(3),
    "label" TEXT NOT NULL,
    "hashedKey" TEXT NOT NULL,
    "lookupHash" TEXT,
    "organizationId" TEXT NOT NULL,
    "organizationAccess" JSONB NOT NULL DEFAULT '{}',

    CONSTRAINT "ApiKey_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "ApiKeyWorkspace" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "apiKeyId" TEXT NOT NULL,
    "workspaceId" TEXT NOT NULL,
    "permission" "ApiKeyPermission" NOT NULL,

    CONSTRAINT "ApiKeyWorkspace_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Account" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "userId" TEXT NOT NULL,
    "type" TEXT,
    "provider" TEXT NOT NULL,
    "providerAccountId" TEXT NOT NULL,
    "access_token" TEXT,
    "refresh_token" TEXT,
    "expires_at" INTEGER,
    "ext_expires_in" INTEGER,
    "token_type" TEXT,
    "scope" TEXT,
    "id_token" TEXT,
    "session_state" TEXT,
    "password" TEXT,
    "accessTokenExpiresAt" TIMESTAMP(3),
    "refreshTokenExpiresAt" TIMESTAMP(3),
    "issuer" TEXT,

    CONSTRAINT "Account_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Session" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "sessionToken" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "expires" TIMESTAMP(3) NOT NULL,
    "ipAddress" TEXT,
    "userAgent" TEXT,

    CONSTRAINT "Session_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "VerificationToken" (
    "identifier" TEXT NOT NULL,
    "token" TEXT NOT NULL,
    "expires" TIMESTAMP(3) NOT NULL
);

-- CreateTable
CREATE TABLE "TwoFactor" (
    "id" TEXT NOT NULL,
    "secret" TEXT NOT NULL,
    "backupCodes" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "verified" BOOLEAN NOT NULL DEFAULT true,
    "failedVerificationCount" INTEGER NOT NULL DEFAULT 0,
    "lockedUntil" TIMESTAMP(3),

    CONSTRAINT "TwoFactor_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "jwks" (
    "id" TEXT NOT NULL,
    "publicKey" TEXT NOT NULL,
    "privateKey" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL,
    "expiresAt" TIMESTAMP(3),
    "alg" TEXT,
    "crv" TEXT,

    CONSTRAINT "jwks_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "oauthClient" (
    "id" TEXT NOT NULL,
    "clientId" TEXT NOT NULL,
    "clientSecret" TEXT,
    "disabled" BOOLEAN DEFAULT false,
    "skipConsent" BOOLEAN,
    "enableEndSession" BOOLEAN,
    "subjectType" TEXT,
    "scopes" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "userId" TEXT,
    "createdAt" TIMESTAMP(3),
    "updatedAt" TIMESTAMP(3),
    "name" TEXT,
    "uri" TEXT,
    "icon" TEXT,
    "contacts" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "tos" TEXT,
    "policy" TEXT,
    "softwareId" TEXT,
    "softwareVersion" TEXT,
    "softwareStatement" TEXT,
    "redirectUris" TEXT[],
    "postLogoutRedirectUris" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "tokenEndpointAuthMethod" TEXT,
    "grantTypes" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "responseTypes" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "public" BOOLEAN,
    "type" TEXT,
    "requirePKCE" BOOLEAN,
    "referenceId" TEXT,
    "metadata" JSONB,
    "clientDiscoveryId" TEXT,
    "clientCredentialsScopes" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "backchannelLogoutUri" TEXT,
    "backchannelLogoutSessionRequired" BOOLEAN,
    "applicationType" TEXT,
    "jwks" TEXT,
    "jwksUri" TEXT,
    "dpopBoundAccessTokens" BOOLEAN DEFAULT false,

    CONSTRAINT "oauthClient_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "oauthAccessToken" (
    "id" TEXT NOT NULL,
    "token" TEXT NOT NULL,
    "clientId" TEXT NOT NULL,
    "sessionId" TEXT,
    "userId" TEXT,
    "referenceId" TEXT,
    "refreshId" TEXT,
    "expiresAt" TIMESTAMP(3) NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL,
    "scopes" TEXT[],
    "authorizationCodeId" TEXT,
    "resources" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "requestedUserInfoClaims" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "revoked" TIMESTAMP(3),
    "confirmation" JSONB,

    CONSTRAINT "oauthAccessToken_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "oauthRefreshToken" (
    "id" TEXT NOT NULL,
    "token" TEXT NOT NULL,
    "clientId" TEXT NOT NULL,
    "sessionId" TEXT,
    "userId" TEXT NOT NULL,
    "referenceId" TEXT,
    "expiresAt" TIMESTAMP(3) NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL,
    "revoked" TIMESTAMP(3),
    "authTime" TIMESTAMP(3),
    "scopes" TEXT[],
    "authorizationCodeId" TEXT,
    "resources" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "requestedUserInfoClaims" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "rotatedAt" TIMESTAMP(3),
    "rotationReplayResponse" TEXT,
    "rotationReplayExpiresAt" TIMESTAMP(3),
    "confirmation" JSONB,

    CONSTRAINT "oauthRefreshToken_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "oauthConsent" (
    "id" TEXT NOT NULL,
    "clientId" TEXT NOT NULL,
    "userId" TEXT,
    "referenceId" TEXT,
    "scopes" TEXT[],
    "createdAt" TIMESTAMP(3) NOT NULL,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "resources" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "requestedUserInfoClaims" TEXT[] DEFAULT ARRAY[]::TEXT[],

    CONSTRAINT "oauthConsent_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "oauthResource" (
    "id" TEXT NOT NULL,
    "identifier" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "accessTokenTtl" INTEGER,
    "refreshTokenTtl" INTEGER,
    "signingAlgorithm" TEXT,
    "signingKeyId" TEXT,
    "allowedScopes" TEXT[] DEFAULT ARRAY[]::TEXT[],
    "customClaims" JSONB,
    "dpopBoundAccessTokensRequired" BOOLEAN DEFAULT false,
    "disabled" BOOLEAN DEFAULT false,
    "createdAt" TIMESTAMP(3),
    "updatedAt" TIMESTAMP(3),
    "policyVersion" INTEGER DEFAULT 1,
    "metadata" JSONB,

    CONSTRAINT "oauthResource_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "oauthClientResource" (
    "id" TEXT NOT NULL,
    "clientId" TEXT NOT NULL,
    "resourceId" TEXT NOT NULL,
    "metadata" JSONB,
    "createdAt" TIMESTAMP(3),

    CONSTRAINT "oauthClientResource_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "oauthClientAssertion" (
    "id" TEXT NOT NULL,
    "expiresAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "oauthClientAssertion_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "PasswordResetToken" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "token_hash" TEXT NOT NULL,
    "expires_at" TIMESTAMP(3) NOT NULL,
    "userId" TEXT NOT NULL,

    CONSTRAINT "PasswordResetToken_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "User" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "email_verified" BOOLEAN NOT NULL DEFAULT false,
    "email_verified_at" TIMESTAMP(3),
    "twoFactorSecret" TEXT,
    "twoFactorEnabled" BOOLEAN NOT NULL DEFAULT false,
    "backupCodes" TEXT,
    "password" TEXT,
    "identityProvider" "IdentityProvider" NOT NULL DEFAULT 'email',
    "identityProviderAccountId" TEXT,
    "groupId" TEXT,
    "notificationSettings" JSONB NOT NULL DEFAULT '{}',
    "locale" TEXT NOT NULL DEFAULT 'en-US',
    "lastLoginAt" TIMESTAMP(3),
    "isActive" BOOLEAN NOT NULL DEFAULT true,

    CONSTRAINT "User_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Segment" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "title" TEXT NOT NULL,
    "description" TEXT,
    "isPrivate" BOOLEAN NOT NULL DEFAULT true,
    "filters" JSONB NOT NULL DEFAULT '[]',
    "workspaceId" TEXT NOT NULL,

    CONSTRAINT "Segment_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Language" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "code" TEXT NOT NULL,
    "alias" TEXT,
    "workspaceId" TEXT NOT NULL,

    CONSTRAINT "Language_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SurveyLanguage" (
    "languageId" TEXT NOT NULL,
    "surveyId" TEXT NOT NULL,
    "default" BOOLEAN NOT NULL DEFAULT false,
    "enabled" BOOLEAN NOT NULL DEFAULT true,

    CONSTRAINT "SurveyLanguage_pkey" PRIMARY KEY ("languageId","surveyId")
);

-- CreateTable
CREATE TABLE "EmbeddedData" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "key" TEXT,
    "name" TEXT NOT NULL,
    "description" TEXT,
    "source" "EmbeddedDataSource" NOT NULL,
    "dataType" "EmbeddedDataType" NOT NULL DEFAULT 'string',
    "defaultValue" JSONB,
    "locked" BOOLEAN NOT NULL DEFAULT false,
    "surveyId" TEXT,
    "workspaceId" TEXT NOT NULL,

    CONSTRAINT "EmbeddedData_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SurveyEmbeddedData" (
    "id" TEXT NOT NULL,
    "workspaceId" TEXT NOT NULL,
    "surveyId" TEXT NOT NULL,
    "embeddedDataId" TEXT NOT NULL,
    "storageKey" TEXT NOT NULL,
    "order" INTEGER NOT NULL,

    CONSTRAINT "SurveyEmbeddedData_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Team" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "organizationId" TEXT NOT NULL,

    CONSTRAINT "Team_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "TeamUser" (
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "teamId" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "role" "TeamUserRole" NOT NULL,

    CONSTRAINT "TeamUser_pkey" PRIMARY KEY ("teamId","userId")
);

-- CreateTable
CREATE TABLE "WorkspaceTeam" (
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "workspaceId" TEXT NOT NULL,
    "teamId" TEXT NOT NULL,
    "permission" "WorkspaceTeamPermission" NOT NULL DEFAULT 'read',

    CONSTRAINT "WorkspaceTeam_pkey" PRIMARY KEY ("workspaceId","teamId")
);

-- CreateTable
CREATE TABLE "AuthzedProjectionOutbox" (
    "id" TEXT NOT NULL,
    "targetType" TEXT NOT NULL,
    "primaryId" TEXT NOT NULL,
    "secondaryId" TEXT,
    "isRevocation" BOOLEAN NOT NULL DEFAULT false,
    "attempts" INTEGER NOT NULL DEFAULT 0,
    "permanentFailures" INTEGER NOT NULL DEFAULT 0,
    "availableAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "leasedAt" TIMESTAMP(3),
    "leaseExpiresAt" TIMESTAMP(3),
    "leaseOwner" TEXT,
    "processedAt" TIMESTAMP(3),
    "deadLetteredAt" TIMESTAMP(3),
    "lastAttemptAt" TIMESTAMP(3),
    "lastErrorCode" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "AuthzedProjectionOutbox_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Workflow" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "description" TEXT,
    "status" "WorkflowStatus" NOT NULL DEFAULT 'draft',
    "workspaceId" TEXT NOT NULL,
    "createdBy" TEXT,
    "definition" JSONB NOT NULL,

    CONSTRAINT "Workflow_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "WorkflowVersion" (
    "id" TEXT NOT NULL,
    "workflowId" TEXT NOT NULL,
    "workspaceId" TEXT NOT NULL,
    "version" INTEGER NOT NULL,
    "definition" JSONB NOT NULL,
    "publishedAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "publishedBy" TEXT,

    CONSTRAINT "WorkflowVersion_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "WorkflowRun" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "workflowId" TEXT NOT NULL,
    "workspaceId" TEXT NOT NULL,
    "workflowVersionId" TEXT,
    "responseId" TEXT,
    "status" "WorkflowRunStatus" NOT NULL DEFAULT 'queued',
    "triggerType" TEXT NOT NULL,
    "surveyId" TEXT,
    "isDryRun" BOOLEAN NOT NULL DEFAULT false,
    "idempotencyKey" TEXT,
    "attempt" INTEGER NOT NULL DEFAULT 0,
    "nextAttemptAt" TIMESTAMP(3),
    "lastErrorAt" TIMESTAMP(3),
    "triggerPayload" JSONB NOT NULL,
    "data" JSONB NOT NULL DEFAULT '{}',
    "error" TEXT,
    "dispatchedAt" TIMESTAMP(3),
    "startedAt" TIMESTAMP(3),
    "finishedAt" TIMESTAMP(3),

    CONSTRAINT "WorkflowRun_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "WorkflowRunLog" (
    "id" TEXT NOT NULL,
    "runId" TEXT NOT NULL,
    "sequence" INTEGER NOT NULL,
    "stepId" TEXT NOT NULL,
    "stepType" TEXT NOT NULL,
    "status" "WorkflowRunLogStatus" NOT NULL DEFAULT 'pending',
    "input" JSONB NOT NULL DEFAULT '{}',
    "output" JSONB NOT NULL DEFAULT '{}',
    "error" TEXT,
    "startedAt" TIMESTAMP(3),
    "finishedAt" TIMESTAMP(3),

    CONSTRAINT "WorkflowRunLog_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE INDEX "Webhook_workspaceId_idx" ON "Webhook"("workspaceId");

-- CreateIndex
CREATE INDEX "ContactAttribute_attributeKeyId_value_idx" ON "ContactAttribute"("attributeKeyId", "value");

-- CreateIndex
CREATE INDEX "ContactAttribute_attributeKeyId_valueNumber_idx" ON "ContactAttribute"("attributeKeyId", "valueNumber");

-- CreateIndex
CREATE INDEX "ContactAttribute_attributeKeyId_valueDate_idx" ON "ContactAttribute"("attributeKeyId", "valueDate");

-- CreateIndex
CREATE UNIQUE INDEX "ContactAttribute_contactId_attributeKeyId_key" ON "ContactAttribute"("contactId", "attributeKeyId");

-- CreateIndex
CREATE INDEX "ContactAttributeKey_workspaceId_created_at_idx" ON "ContactAttributeKey"("workspaceId", "created_at");

-- CreateIndex
CREATE UNIQUE INDEX "ContactAttributeKey_key_workspaceId_key" ON "ContactAttributeKey"("key", "workspaceId");

-- CreateIndex
CREATE INDEX "Contact_workspaceId_idx" ON "Contact"("workspaceId");

-- CreateIndex
CREATE UNIQUE INDEX "Response_displayId_key" ON "Response"("displayId");

-- CreateIndex
CREATE INDEX "Response_created_at_idx" ON "Response"("created_at");

-- CreateIndex
CREATE INDEX "Response_surveyId_created_at_idx" ON "Response"("surveyId", "created_at");

-- CreateIndex
CREATE INDEX "Response_contactId_created_at_idx" ON "Response"("contactId", "created_at");

-- CreateIndex
CREATE INDEX "Response_surveyId_finished_idx" ON "Response"("surveyId", "finished");

-- CreateIndex
CREATE UNIQUE INDEX "Response_surveyId_singleUseId_key" ON "Response"("surveyId", "singleUseId");

-- CreateIndex
CREATE INDEX "Tag_workspaceId_idx" ON "Tag"("workspaceId");

-- CreateIndex
CREATE UNIQUE INDEX "Tag_workspaceId_name_key" ON "Tag"("workspaceId", "name");

-- CreateIndex
CREATE INDEX "Display_surveyId_idx" ON "Display"("surveyId");

-- CreateIndex
CREATE INDEX "Display_contactId_created_at_idx" ON "Display"("contactId", "created_at");

-- CreateIndex
CREATE UNIQUE INDEX "SurveyTrigger_surveyId_actionClassId_key" ON "SurveyTrigger"("surveyId", "actionClassId");

-- CreateIndex
CREATE INDEX "SurveyAttributeFilter_attributeKeyId_idx" ON "SurveyAttributeFilter"("attributeKeyId");

-- CreateIndex
CREATE UNIQUE INDEX "SurveyAttributeFilter_surveyId_attributeKeyId_key" ON "SurveyAttributeFilter"("surveyId", "attributeKeyId");

-- CreateIndex
CREATE UNIQUE INDEX "Survey_slug_key" ON "Survey"("slug");

-- CreateIndex
CREATE INDEX "Survey_workspaceId_updated_at_idx" ON "Survey"("workspaceId", "updated_at");

-- CreateIndex
CREATE INDEX "Survey_segmentId_idx" ON "Survey"("segmentId");

-- CreateIndex
CREATE INDEX "Survey_status_publishOn_idx" ON "Survey"("status", "publishOn");

-- CreateIndex
CREATE INDEX "Survey_status_closeOn_idx" ON "Survey"("status", "closeOn");

-- CreateIndex
CREATE INDEX "Survey_archivedAt_idx" ON "Survey"("archivedAt");

-- CreateIndex
CREATE UNIQUE INDEX "Survey_id_workspaceId_key" ON "Survey"("id", "workspaceId");

-- CreateIndex
CREATE UNIQUE INDEX "SurveyQuota_surveyId_name_key" ON "SurveyQuota"("surveyId", "name");

-- CreateIndex
CREATE INDEX "ResponseQuotaLink_quotaId_status_idx" ON "ResponseQuotaLink"("quotaId", "status");

-- CreateIndex
CREATE INDEX "ActionClass_workspaceId_created_at_idx" ON "ActionClass"("workspaceId", "created_at");

-- CreateIndex
CREATE UNIQUE INDEX "ActionClass_key_workspaceId_key" ON "ActionClass"("key", "workspaceId");

-- CreateIndex
CREATE UNIQUE INDEX "ActionClass_name_workspaceId_key" ON "ActionClass"("name", "workspaceId");

-- CreateIndex
CREATE INDEX "Integration_workspaceId_idx" ON "Integration"("workspaceId");

-- CreateIndex
CREATE UNIQUE INDEX "Integration_type_workspaceId_key" ON "Integration"("type", "workspaceId");

-- CreateIndex
CREATE UNIQUE INDEX "DataMigration_name_key" ON "DataMigration"("name");

-- CreateIndex
CREATE UNIQUE INDEX "Workspace_legacyEnvironmentId_key" ON "Workspace"("legacyEnvironmentId");

-- CreateIndex
CREATE UNIQUE INDEX "Workspace_organizationId_name_key" ON "Workspace"("organizationId", "name");

-- CreateIndex
CREATE UNIQUE INDEX "OrganizationBilling_stripe_customer_id_key" ON "OrganizationBilling"("stripe_customer_id");

-- CreateIndex
CREATE INDEX "Membership_organizationId_idx" ON "Membership"("organizationId");

-- CreateIndex
CREATE INDEX "Invite_email_organizationId_idx" ON "Invite"("email", "organizationId");

-- CreateIndex
CREATE INDEX "Invite_organizationId_idx" ON "Invite"("organizationId");

-- CreateIndex
CREATE UNIQUE INDEX "ApiKey_lookupHash_key" ON "ApiKey"("lookupHash");

-- CreateIndex
CREATE INDEX "ApiKey_organizationId_idx" ON "ApiKey"("organizationId");

-- CreateIndex
CREATE INDEX "ApiKeyWorkspace_workspaceId_idx" ON "ApiKeyWorkspace"("workspaceId");

-- CreateIndex
CREATE UNIQUE INDEX "ApiKeyWorkspace_apiKeyId_workspaceId_key" ON "ApiKeyWorkspace"("apiKeyId", "workspaceId");

-- CreateIndex
CREATE INDEX "Account_userId_idx" ON "Account"("userId");

-- CreateIndex
CREATE UNIQUE INDEX "Account_provider_providerAccountId_key" ON "Account"("provider", "providerAccountId");

-- CreateIndex
CREATE UNIQUE INDEX "Account_issuer_providerAccountId_key" ON "Account"("issuer", "providerAccountId");

-- CreateIndex
CREATE UNIQUE INDEX "Session_sessionToken_key" ON "Session"("sessionToken");

-- CreateIndex
CREATE INDEX "Session_userId_idx" ON "Session"("userId");

-- CreateIndex
CREATE UNIQUE INDEX "VerificationToken_identifier_token_key" ON "VerificationToken"("identifier", "token");

-- CreateIndex
CREATE UNIQUE INDEX "TwoFactor_userId_key" ON "TwoFactor"("userId");

-- CreateIndex
CREATE UNIQUE INDEX "oauthClient_clientId_key" ON "oauthClient"("clientId");

-- CreateIndex
CREATE INDEX "oauthClient_userId_idx" ON "oauthClient"("userId");

-- CreateIndex
CREATE UNIQUE INDEX "oauthAccessToken_token_key" ON "oauthAccessToken"("token");

-- CreateIndex
CREATE INDEX "oauthAccessToken_clientId_idx" ON "oauthAccessToken"("clientId");

-- CreateIndex
CREATE INDEX "oauthAccessToken_sessionId_idx" ON "oauthAccessToken"("sessionId");

-- CreateIndex
CREATE INDEX "oauthAccessToken_userId_idx" ON "oauthAccessToken"("userId");

-- CreateIndex
CREATE INDEX "oauthAccessToken_refreshId_idx" ON "oauthAccessToken"("refreshId");

-- CreateIndex
CREATE INDEX "oauthAccessToken_authorizationCodeId_idx" ON "oauthAccessToken"("authorizationCodeId");

-- CreateIndex
CREATE UNIQUE INDEX "oauthRefreshToken_token_key" ON "oauthRefreshToken"("token");

-- CreateIndex
CREATE INDEX "oauthRefreshToken_clientId_idx" ON "oauthRefreshToken"("clientId");

-- CreateIndex
CREATE INDEX "oauthRefreshToken_sessionId_idx" ON "oauthRefreshToken"("sessionId");

-- CreateIndex
CREATE INDEX "oauthRefreshToken_userId_idx" ON "oauthRefreshToken"("userId");

-- CreateIndex
CREATE INDEX "oauthRefreshToken_authorizationCodeId_idx" ON "oauthRefreshToken"("authorizationCodeId");

-- CreateIndex
CREATE INDEX "oauthConsent_clientId_idx" ON "oauthConsent"("clientId");

-- CreateIndex
CREATE INDEX "oauthConsent_userId_idx" ON "oauthConsent"("userId");

-- CreateIndex
CREATE UNIQUE INDEX "oauthResource_identifier_key" ON "oauthResource"("identifier");

-- CreateIndex
CREATE INDEX "oauthClientResource_resourceId_idx" ON "oauthClientResource"("resourceId");

-- CreateIndex
CREATE UNIQUE INDEX "oauthClientResource_clientId_resourceId_key" ON "oauthClientResource"("clientId", "resourceId");

-- CreateIndex
CREATE INDEX "oauthClientAssertion_expiresAt_idx" ON "oauthClientAssertion"("expiresAt");

-- CreateIndex
CREATE UNIQUE INDEX "PasswordResetToken_token_hash_key" ON "PasswordResetToken"("token_hash");

-- CreateIndex
CREATE UNIQUE INDEX "PasswordResetToken_userId_key" ON "PasswordResetToken"("userId");

-- CreateIndex
CREATE INDEX "PasswordResetToken_expires_at_idx" ON "PasswordResetToken"("expires_at");

-- CreateIndex
CREATE UNIQUE INDEX "User_email_key" ON "User"("email");

-- CreateIndex
CREATE INDEX "Segment_workspaceId_idx" ON "Segment"("workspaceId");

-- CreateIndex
CREATE UNIQUE INDEX "Segment_workspaceId_title_key" ON "Segment"("workspaceId", "title");

-- CreateIndex
CREATE UNIQUE INDEX "Language_workspaceId_code_key" ON "Language"("workspaceId", "code");

-- CreateIndex
CREATE INDEX "SurveyLanguage_surveyId_idx" ON "SurveyLanguage"("surveyId");

-- CreateIndex
CREATE INDEX "EmbeddedData_surveyId_idx" ON "EmbeddedData"("surveyId");

-- CreateIndex
CREATE UNIQUE INDEX "EmbeddedData_workspaceId_key_key" ON "EmbeddedData"("workspaceId", "key");

-- CreateIndex
CREATE UNIQUE INDEX "EmbeddedData_id_workspaceId_key" ON "EmbeddedData"("id", "workspaceId");

-- CreateIndex
CREATE INDEX "SurveyEmbeddedData_embeddedDataId_idx" ON "SurveyEmbeddedData"("embeddedDataId");

-- CreateIndex
CREATE INDEX "SurveyEmbeddedData_surveyId_order_idx" ON "SurveyEmbeddedData"("surveyId", "order");

-- CreateIndex
CREATE UNIQUE INDEX "SurveyEmbeddedData_surveyId_embeddedDataId_key" ON "SurveyEmbeddedData"("surveyId", "embeddedDataId");

-- CreateIndex
CREATE UNIQUE INDEX "SurveyEmbeddedData_surveyId_storageKey_key" ON "SurveyEmbeddedData"("surveyId", "storageKey");

-- CreateIndex
CREATE UNIQUE INDEX "Team_organizationId_name_key" ON "Team"("organizationId", "name");

-- CreateIndex
CREATE INDEX "TeamUser_userId_idx" ON "TeamUser"("userId");

-- CreateIndex
CREATE INDEX "WorkspaceTeam_teamId_idx" ON "WorkspaceTeam"("teamId");

-- CreateIndex
CREATE INDEX "Workflow_workspaceId_status_idx" ON "Workflow"("workspaceId", "status");

-- CreateIndex
CREATE INDEX "Workflow_workspaceId_updated_at_idx" ON "Workflow"("workspaceId", "updated_at");

-- CreateIndex
CREATE UNIQUE INDEX "Workflow_id_workspaceId_key" ON "Workflow"("id", "workspaceId");

-- CreateIndex
CREATE INDEX "WorkflowVersion_workflowId_workspaceId_idx" ON "WorkflowVersion"("workflowId", "workspaceId");

-- CreateIndex
CREATE INDEX "WorkflowVersion_publishedBy_idx" ON "WorkflowVersion"("publishedBy");

-- CreateIndex
CREATE UNIQUE INDEX "WorkflowVersion_workflowId_version_key" ON "WorkflowVersion"("workflowId", "version");

-- CreateIndex
CREATE INDEX "WorkflowRun_workspaceId_created_at_idx" ON "WorkflowRun"("workspaceId", "created_at");

-- CreateIndex
CREATE INDEX "WorkflowRun_workflowId_workspaceId_created_at_idx" ON "WorkflowRun"("workflowId", "workspaceId", "created_at");

-- CreateIndex
CREATE INDEX "WorkflowRun_status_created_at_idx" ON "WorkflowRun"("status", "created_at");

-- CreateIndex
CREATE INDEX "WorkflowRun_responseId_idx" ON "WorkflowRun"("responseId");

-- CreateIndex
CREATE INDEX "WorkflowRun_isDryRun_idx" ON "WorkflowRun"("isDryRun");

-- CreateIndex
CREATE INDEX "WorkflowRun_nextAttemptAt_idx" ON "WorkflowRun"("nextAttemptAt");

-- CreateIndex
CREATE UNIQUE INDEX "WorkflowRun_workflowId_idempotencyKey_key" ON "WorkflowRun"("workflowId", "idempotencyKey");

-- CreateIndex
CREATE INDEX "WorkflowRunLog_runId_sequence_idx" ON "WorkflowRunLog"("runId", "sequence");

-- CreateIndex
CREATE UNIQUE INDEX "WorkflowRunLog_runId_stepId_key" ON "WorkflowRunLog"("runId", "stepId");

-- AddForeignKey
ALTER TABLE "Webhook" ADD CONSTRAINT "Webhook_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ContactAttribute" ADD CONSTRAINT "ContactAttribute_attributeKeyId_fkey" FOREIGN KEY ("attributeKeyId") REFERENCES "ContactAttributeKey"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ContactAttribute" ADD CONSTRAINT "ContactAttribute_contactId_fkey" FOREIGN KEY ("contactId") REFERENCES "Contact"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ContactAttributeKey" ADD CONSTRAINT "ContactAttributeKey_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Contact" ADD CONSTRAINT "Contact_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Response" ADD CONSTRAINT "Response_surveyId_fkey" FOREIGN KEY ("surveyId") REFERENCES "Survey"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Response" ADD CONSTRAINT "Response_contactId_fkey" FOREIGN KEY ("contactId") REFERENCES "Contact"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Response" ADD CONSTRAINT "Response_displayId_fkey" FOREIGN KEY ("displayId") REFERENCES "Display"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Tag" ADD CONSTRAINT "Tag_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "TagsOnResponses" ADD CONSTRAINT "TagsOnResponses_responseId_fkey" FOREIGN KEY ("responseId") REFERENCES "Response"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "TagsOnResponses" ADD CONSTRAINT "TagsOnResponses_tagId_fkey" FOREIGN KEY ("tagId") REFERENCES "Tag"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Display" ADD CONSTRAINT "Display_surveyId_fkey" FOREIGN KEY ("surveyId") REFERENCES "Survey"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Display" ADD CONSTRAINT "Display_contactId_fkey" FOREIGN KEY ("contactId") REFERENCES "Contact"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SurveyTrigger" ADD CONSTRAINT "SurveyTrigger_surveyId_fkey" FOREIGN KEY ("surveyId") REFERENCES "Survey"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SurveyTrigger" ADD CONSTRAINT "SurveyTrigger_actionClassId_fkey" FOREIGN KEY ("actionClassId") REFERENCES "ActionClass"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SurveyAttributeFilter" ADD CONSTRAINT "SurveyAttributeFilter_attributeKeyId_fkey" FOREIGN KEY ("attributeKeyId") REFERENCES "ContactAttributeKey"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SurveyAttributeFilter" ADD CONSTRAINT "SurveyAttributeFilter_surveyId_fkey" FOREIGN KEY ("surveyId") REFERENCES "Survey"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Survey" ADD CONSTRAINT "Survey_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Survey" ADD CONSTRAINT "Survey_createdBy_fkey" FOREIGN KEY ("createdBy") REFERENCES "User"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Survey" ADD CONSTRAINT "Survey_segmentId_fkey" FOREIGN KEY ("segmentId") REFERENCES "Segment"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SurveyQuota" ADD CONSTRAINT "SurveyQuota_surveyId_fkey" FOREIGN KEY ("surveyId") REFERENCES "Survey"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ResponseQuotaLink" ADD CONSTRAINT "ResponseQuotaLink_responseId_fkey" FOREIGN KEY ("responseId") REFERENCES "Response"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ResponseQuotaLink" ADD CONSTRAINT "ResponseQuotaLink_quotaId_fkey" FOREIGN KEY ("quotaId") REFERENCES "SurveyQuota"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SurveyFollowUp" ADD CONSTRAINT "SurveyFollowUp_surveyId_fkey" FOREIGN KEY ("surveyId") REFERENCES "Survey"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ActionClass" ADD CONSTRAINT "ActionClass_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Integration" ADD CONSTRAINT "Integration_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Workspace" ADD CONSTRAINT "Workspace_organizationId_fkey" FOREIGN KEY ("organizationId") REFERENCES "Organization"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "OrganizationBilling" ADD CONSTRAINT "OrganizationBilling_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "Organization"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Membership" ADD CONSTRAINT "Membership_organizationId_fkey" FOREIGN KEY ("organizationId") REFERENCES "Organization"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Membership" ADD CONSTRAINT "Membership_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Invite" ADD CONSTRAINT "Invite_organizationId_fkey" FOREIGN KEY ("organizationId") REFERENCES "Organization"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Invite" ADD CONSTRAINT "Invite_creatorId_fkey" FOREIGN KEY ("creatorId") REFERENCES "User"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Invite" ADD CONSTRAINT "Invite_acceptorId_fkey" FOREIGN KEY ("acceptorId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ApiKey" ADD CONSTRAINT "ApiKey_organizationId_fkey" FOREIGN KEY ("organizationId") REFERENCES "Organization"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ApiKeyWorkspace" ADD CONSTRAINT "ApiKeyWorkspace_apiKeyId_fkey" FOREIGN KEY ("apiKeyId") REFERENCES "ApiKey"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ApiKeyWorkspace" ADD CONSTRAINT "ApiKeyWorkspace_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Account" ADD CONSTRAINT "Account_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Session" ADD CONSTRAINT "Session_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "TwoFactor" ADD CONSTRAINT "TwoFactor_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthClient" ADD CONSTRAINT "oauthClient_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthAccessToken" ADD CONSTRAINT "oauthAccessToken_clientId_fkey" FOREIGN KEY ("clientId") REFERENCES "oauthClient"("clientId") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthAccessToken" ADD CONSTRAINT "oauthAccessToken_sessionId_fkey" FOREIGN KEY ("sessionId") REFERENCES "Session"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthAccessToken" ADD CONSTRAINT "oauthAccessToken_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthAccessToken" ADD CONSTRAINT "oauthAccessToken_refreshId_fkey" FOREIGN KEY ("refreshId") REFERENCES "oauthRefreshToken"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthRefreshToken" ADD CONSTRAINT "oauthRefreshToken_clientId_fkey" FOREIGN KEY ("clientId") REFERENCES "oauthClient"("clientId") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthRefreshToken" ADD CONSTRAINT "oauthRefreshToken_sessionId_fkey" FOREIGN KEY ("sessionId") REFERENCES "Session"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthRefreshToken" ADD CONSTRAINT "oauthRefreshToken_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthConsent" ADD CONSTRAINT "oauthConsent_clientId_fkey" FOREIGN KEY ("clientId") REFERENCES "oauthClient"("clientId") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthConsent" ADD CONSTRAINT "oauthConsent_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthClientResource" ADD CONSTRAINT "oauthClientResource_clientId_fkey" FOREIGN KEY ("clientId") REFERENCES "oauthClient"("clientId") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "oauthClientResource" ADD CONSTRAINT "oauthClientResource_resourceId_fkey" FOREIGN KEY ("resourceId") REFERENCES "oauthResource"("identifier") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "PasswordResetToken" ADD CONSTRAINT "PasswordResetToken_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Segment" ADD CONSTRAINT "Segment_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Language" ADD CONSTRAINT "Language_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SurveyLanguage" ADD CONSTRAINT "SurveyLanguage_languageId_fkey" FOREIGN KEY ("languageId") REFERENCES "Language"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SurveyLanguage" ADD CONSTRAINT "SurveyLanguage_surveyId_fkey" FOREIGN KEY ("surveyId") REFERENCES "Survey"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "EmbeddedData" ADD CONSTRAINT "EmbeddedData_surveyId_workspaceId_fkey" FOREIGN KEY ("surveyId", "workspaceId") REFERENCES "Survey"("id", "workspaceId") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "EmbeddedData" ADD CONSTRAINT "EmbeddedData_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SurveyEmbeddedData" ADD CONSTRAINT "SurveyEmbeddedData_surveyId_workspaceId_fkey" FOREIGN KEY ("surveyId", "workspaceId") REFERENCES "Survey"("id", "workspaceId") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SurveyEmbeddedData" ADD CONSTRAINT "SurveyEmbeddedData_embeddedDataId_workspaceId_fkey" FOREIGN KEY ("embeddedDataId", "workspaceId") REFERENCES "EmbeddedData"("id", "workspaceId") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Team" ADD CONSTRAINT "Team_organizationId_fkey" FOREIGN KEY ("organizationId") REFERENCES "Organization"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "TeamUser" ADD CONSTRAINT "TeamUser_teamId_fkey" FOREIGN KEY ("teamId") REFERENCES "Team"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "TeamUser" ADD CONSTRAINT "TeamUser_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "WorkspaceTeam" ADD CONSTRAINT "WorkspaceTeam_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "WorkspaceTeam" ADD CONSTRAINT "WorkspaceTeam_teamId_fkey" FOREIGN KEY ("teamId") REFERENCES "Team"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Workflow" ADD CONSTRAINT "Workflow_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Workflow" ADD CONSTRAINT "Workflow_createdBy_fkey" FOREIGN KEY ("createdBy") REFERENCES "User"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "WorkflowVersion" ADD CONSTRAINT "WorkflowVersion_workflowId_workspaceId_fkey" FOREIGN KEY ("workflowId", "workspaceId") REFERENCES "Workflow"("id", "workspaceId") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "WorkflowVersion" ADD CONSTRAINT "WorkflowVersion_publishedBy_fkey" FOREIGN KEY ("publishedBy") REFERENCES "User"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "WorkflowRun" ADD CONSTRAINT "WorkflowRun_workflowId_workspaceId_fkey" FOREIGN KEY ("workflowId", "workspaceId") REFERENCES "Workflow"("id", "workspaceId") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "WorkflowRun" ADD CONSTRAINT "WorkflowRun_workspaceId_fkey" FOREIGN KEY ("workspaceId") REFERENCES "Workspace"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "WorkflowRun" ADD CONSTRAINT "WorkflowRun_workflowVersionId_fkey" FOREIGN KEY ("workflowVersionId") REFERENCES "WorkflowVersion"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "WorkflowRun" ADD CONSTRAINT "WorkflowRun_responseId_fkey" FOREIGN KEY ("responseId") REFERENCES "Response"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "WorkflowRunLog" ADD CONSTRAINT "WorkflowRunLog_runId_fkey" FOREIGN KEY ("runId") REFERENCES "WorkflowRun"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- ---------------------------------------------------------------------------
-- Objects Prisma's schema cannot express, carried over from the squashed history.
--
-- The partial indexes below need a WHERE clause (`@@index` has none), and the AuthZed projection
-- outbox is fed by triggers rather than by application code, so neither survives a schema diff.
-- ---------------------------------------------------------------------------

-- `attempts` drives the retry backoff and tells an operator how many times delivery was tried.
-- `permanentFailures` is the separate, much smaller budget that gates dead-lettering, so that a
-- SpiceDB outage of any duration can never dead-letter healthy events. See outbox-repository.ts.
ALTER TABLE "AuthzedProjectionOutbox"
  ADD COLUMN IF NOT EXISTS "permanentFailures" INTEGER NOT NULL DEFAULT 0;

-- One index per access pattern, each partial to the rows that pattern can ever match. Processed rows
-- are retained for seven days, so only the prune index below is allowed to carry that history —
-- otherwise every hot-path lookup pays for a week of delivered events.
--
-- These predicates cannot be expressed in Prisma (`@@index` has no `where`), so the model in
-- packages/database/schema/main.prisma deliberately declares no indexes and points here instead.
-- `prisma db push` on a dev database therefore drops them; rerunning this migration restores them,
-- which is the same contract the triggers below already live under. The upstream migration dropped
-- each one first for idempotency; an init migration builds an empty database, so there is nothing
-- to drop.

-- The claim. Its key columns ARE the claim's ORDER BY, so the LIMIT is served by an ordered index
-- scan with no sort. Also serves the freshness guard's overdue-revocation EXISTS (equality on the
-- leading key, range on the second) and every pending counter in the status query.
CREATE INDEX IF NOT EXISTS "AuthzedProjectionOutbox_claim_idx"
  ON "AuthzedProjectionOutbox"("isRevocation" DESC, "createdAt" ASC)
  WHERE "processedAt" IS NULL AND "deadLetteredAt" IS NULL;

-- Everything still undelivered: the freshness guard's dead-letter EXISTS, `outbox replay`, and the
-- status aggregate the delivery job runs every five seconds.
--
-- The predicate is deliberately the whole undelivered set rather than the dead letters alone. The
-- status aggregate's own WHERE is a bare `processedAt IS NULL`, which does not imply a narrower
-- predicate, so PostgreSQL cannot use a dead-letters-only index for it and would fall back to
-- scanning all seven days of retained history twelve times a minute. Widened to this, the aggregate
-- is an index-only scan and the dead-letter probe still has `isRevocation` as its leading key.
--
-- A dead-lettered row always has a NULL `processedAt` — the claim skips dead letters, and replay
-- clears `deadLetteredAt` before delivery is possible — so no dead letter is lost here.
CREATE INDEX IF NOT EXISTS "AuthzedProjectionOutbox_undelivered_idx"
  ON "AuthzedProjectionOutbox"("isRevocation", "deadLetteredAt", "createdAt")
  WHERE "processedAt" IS NULL;

-- History prune. The only index that carries delivered rows.
CREATE INDEX IF NOT EXISTS "AuthzedProjectionOutbox_processed_idx"
  ON "AuthzedProjectionOutbox"("processedAt")
  WHERE "processedAt" IS NOT NULL;

/**
 * Does this UPDATE provably leave the projected relationship set a superset of what it was?
 *
 * `isRevocation` has exactly one reader: the fail-closed freshness guard. So the question it must
 * answer is "could an undelivered copy of this event leave SpiceDB granting access that PostgreSQL
 * has taken away?" — a property of how the projectors *write*, not of the permission closure in
 * authzed/schema.zed. Deriving it from that closure would put a second, untestable copy of the
 * schema here; deriving it from the write shape keeps it checkable against the reconcilers.
 *
 * Deny by default. An unmapped target type, an unmapped column, or any enum move is a revocation.
 * In particular the role ladder is deliberately NOT encoded: OrganizationRole is rankable, but a
 * rank table here has no compile-time backstop the way relationship-map.ts does, so adding a role
 * to schema.zed would silently make this wrong in the fail-OPEN direction. Role changes are
 * one-at-a-time admin actions rather than the bulk operations this classifier exists to keep off
 * the guard, so denying them costs nothing. Revisit only if a bulk re-roling path appears.
 */
CREATE OR REPLACE FUNCTION authzed_projection_is_grant(
  target_type text,
  previous_source jsonb,
  source jsonb
) RETURNS boolean AS $$
  SELECT CASE target_type
    -- reconcileUser deletes every relationship while `isActive` is false, so false -> true can only
    -- add them back. `isActive` is the only column the User trigger watches.
    WHEN 'user' THEN
      (previous_source ->> 'isActive') IS DISTINCT FROM 'true'
      AND (source ->> 'isActive') = 'true'

    -- organization-membership.ts projects every membership row regardless of `accepted` (see the
    -- comment on its readSnapshot), so accepting an invite writes byte-identical relationships.
    -- A `role` move always deletes the relation for the old role, so it stays a revocation.
    WHEN 'membership' THEN
      previous_source ->> 'role' IS NOT DISTINCT FROM source ->> 'role'

    ELSE false
  END;
$$ LANGUAGE sql IMMUTABLE;

CREATE OR REPLACE FUNCTION enqueue_authzed_projection()
RETURNS trigger AS $$
DECLARE
  source jsonb;
  previous_source jsonb;
  is_revocation boolean;
  target_type text := TG_ARGV[0];
  primary_field text := TG_ARGV[1];
  secondary_field text := NULLIF(TG_ARGV[2], '');
BEGIN
  source := CASE WHEN TG_OP = 'DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;

  -- A relationship source can move from one logical pair to another. Reconcile the old pair as a
  -- revocation before reconciling the current pair; otherwise the old edge is no longer discoverable
  -- from PostgreSQL and could survive with stale access.
  IF TG_OP = 'UPDATE' THEN
    previous_source := to_jsonb(OLD);
    IF previous_source ->> primary_field IS DISTINCT FROM source ->> primary_field
      OR (
        secondary_field IS NOT NULL
        AND previous_source ->> secondary_field IS DISTINCT FROM source ->> secondary_field
      )
    THEN
      INSERT INTO "AuthzedProjectionOutbox" (
        "id",
        "targetType",
        "primaryId",
        "secondaryId",
        "isRevocation",
        "updatedAt"
      ) VALUES (
        gen_random_uuid()::text,
        target_type,
        previous_source ->> primary_field,
        CASE WHEN secondary_field IS NULL THEN NULL ELSE previous_source ->> secondary_field END,
        true,
        NOW()
      );
    END IF;
  END IF;

  is_revocation := CASE
    WHEN TG_OP = 'INSERT' THEN false
    WHEN TG_OP = 'DELETE' THEN true
    ELSE NOT authzed_projection_is_grant(target_type, previous_source, source)
  END;

  INSERT INTO "AuthzedProjectionOutbox" (
    "id",
    "targetType",
    "primaryId",
    "secondaryId",
    "isRevocation",
    "updatedAt"
  ) VALUES (
    gen_random_uuid()::text,
    target_type,
    source ->> primary_field,
    CASE WHEN secondary_field IS NULL THEN NULL ELSE source ->> secondary_field END,
    is_revocation,
    NOW()
  );

  RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS "authzed_projection_organization" ON "Organization";
CREATE TRIGGER "authzed_projection_organization"
AFTER INSERT OR DELETE ON "Organization"
FOR EACH ROW EXECUTE FUNCTION enqueue_authzed_projection('organization', 'id', '');

DROP TRIGGER IF EXISTS "authzed_projection_membership" ON "Membership";
CREATE TRIGGER "authzed_projection_membership"
AFTER INSERT OR DELETE OR UPDATE OF "role", "accepted", "organizationId", "userId" ON "Membership"
FOR EACH ROW EXECUTE FUNCTION enqueue_authzed_projection('membership', 'organizationId', 'userId');

DROP TRIGGER IF EXISTS "authzed_projection_user" ON "User";
CREATE TRIGGER "authzed_projection_user"
AFTER INSERT OR DELETE OR UPDATE OF "isActive" ON "User"
FOR EACH ROW EXECUTE FUNCTION enqueue_authzed_projection('user', 'id', '');

DROP TRIGGER IF EXISTS "authzed_projection_team" ON "Team";
CREATE TRIGGER "authzed_projection_team"
AFTER INSERT OR DELETE OR UPDATE OF "organizationId" ON "Team"
FOR EACH ROW EXECUTE FUNCTION enqueue_authzed_projection('team', 'id', '');

DROP TRIGGER IF EXISTS "authzed_projection_team_user" ON "TeamUser";
CREATE TRIGGER "authzed_projection_team_user"
AFTER INSERT OR DELETE OR UPDATE OF "role", "teamId", "userId" ON "TeamUser"
FOR EACH ROW EXECUTE FUNCTION enqueue_authzed_projection('team_membership', 'teamId', 'userId');

DROP TRIGGER IF EXISTS "authzed_projection_workspace" ON "Workspace";
CREATE TRIGGER "authzed_projection_workspace"
AFTER INSERT OR DELETE OR UPDATE OF "organizationId" ON "Workspace"
FOR EACH ROW EXECUTE FUNCTION enqueue_authzed_projection('workspace', 'id', '');

DROP TRIGGER IF EXISTS "authzed_projection_workspace_team" ON "WorkspaceTeam";
CREATE TRIGGER "authzed_projection_workspace_team"
AFTER INSERT OR DELETE OR UPDATE OF "permission", "workspaceId", "teamId" ON "WorkspaceTeam"
FOR EACH ROW EXECUTE FUNCTION enqueue_authzed_projection('workspace_team', 'workspaceId', 'teamId');

DROP TRIGGER IF EXISTS "authzed_projection_api_key" ON "ApiKey";
CREATE TRIGGER "authzed_projection_api_key"
AFTER INSERT OR DELETE OR UPDATE OF "organizationId", "organizationAccess" ON "ApiKey"
FOR EACH ROW EXECUTE FUNCTION enqueue_authzed_projection('api_key', 'id', '');

DROP TRIGGER IF EXISTS "authzed_projection_api_key_workspace" ON "ApiKeyWorkspace";
CREATE TRIGGER "authzed_projection_api_key_workspace"
AFTER INSERT OR DELETE OR UPDATE OF "permission", "apiKeyId", "workspaceId" ON "ApiKeyWorkspace"
FOR EACH ROW EXECUTE FUNCTION enqueue_authzed_projection('api_key_workspace', 'apiKeyId', 'workspaceId');

COMMIT;
