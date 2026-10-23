package forma2import

import (
	"fmt"
	"strings"
)

// Expression helpers for the source queries. forma2 stored timestamps as TIMESTAMP(3) without a zone, holding UTC; AT TIME ZONE 'UTC' reads them as such whatever the session's TimeZone. JSON is read as its text; a JSON null in a nullable column becomes SQL NULL.
func ts(col string) string  { return col + " AT TIME ZONE 'UTC'" }
func js(col string) string  { return col + "::text" }
func jsn(col string) string { return "NULLIF(" + col + ", 'null'::jsonb)::text" }

func c(name, expr string, k kind) column { return column{name: name, expr: expr, kind: k} }

// literal quotes s as an SQL string literal.
func literal(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// stamps are the created_at and updated_at columns of a forma2 table, aliased t, that has both under those names.
func stamps() []column {
	return []column{c("created_at", ts("t.created_at"), plain), c("updated_at", ts("t.updated_at"), plain)}
}

func cols(groups ...[]column) []column {
	var out []column
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// unfinishedRun is the error a workflow run still queued or running in forma2 is closed with: forma2's BullMQ jobs are not carried over, so nothing would ever finish it.
const unfinishedRun = "Canceled by the migration from forma2: the run had not finished, and forma2's queued jobs were not carried over."

// tables are the tables of this schema in the order their foreign keys need, each with the forma2 rows it is filled from (docs: the forma2 to rewrite mapping).
func (im *importer) tables() []*tableSpec {
	s := im.schema
	return []*tableSpec{
		// ---- tenancy ----
		{
			target: "organizations", from: "Organization", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("name", "t.name", plain),
				c("whitelabel", js("t.whitelabel"), jsonDoc),
				c("is_ai_smart_tools_enabled", `t."isAISmartToolsEnabled"`, plain),
				c("display_time_zone", `t."displayTimeZone"`, plain),
				c("workflow_run_retention_days", s.opt("Organization", "workflowRunRetentionDays", `t."workflowRunRetentionDays"`, "NULL::integer"), plain),
				c("display_retention_days", s.opt("Organization", "displayRetentionDays", `t."displayRetentionDays"`, "NULL::integer"), plain),
			}),
			query: `FROM "Organization" t ORDER BY t.id`,
		},
		{
			target: "organization_billing", from: "OrganizationBilling", oneToOne: true, key: []string{"organization_id"},
			cols: cols([]column{c("organization_id", "t.organization_id", id)}, stamps(), []column{
				c("stripe_customer_id", "t.stripe_customer_id", plain),
				c("limits", js("t.limits"), jsonDoc),
				c("usage_cycle_anchor", ts("t.usage_cycle_anchor"), plain),
				c("stripe", jsn("t.stripe"), jsonDoc),
			}),
			query: `FROM "OrganizationBilling" t ORDER BY t.organization_id`,
		},
		{
			target: "users", from: "User", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("name", "t.name", plain),
				c("email", "t.email", plain),
				c("email_verified", "t.email_verified", plain),
				c("locale", "t.locale", plain),
				c("notification_settings", js(`t."notificationSettings"`), derived),
				c("last_login_at", ts(`t."lastLoginAt"`), plain),
				c("is_active", `t."isActive"`, plain),
			}),
			query: `FROM "User" t ORDER BY t.id`,
			hook: func(r *row) (bool, error) {
				out, err := rewriteNotificationSettings(r.str("notification_settings"))
				if err != nil {
					return false, fmt.Errorf("user %s: %w", r.str("id"), err)
				}
				r.set("notification_settings", out)
				return true, nil
			},
		},
		{
			target: "user_identities", from: "Account", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("user_id", `t."userId"`, id),
				c("provider", "t.provider", derived),
				c("issuer", "''", derived),
				c("subject", `t."providerAccountId"`, derived),
				c("password_hash", "t.password", derived),
			}),
			query: `FROM "Account" t ORDER BY t.id`,
			hook:  identityHook,
		},
		{
			// Users forma2 had not yet moved to an Account: a password on the user row, or an identity provider named on it (forma2's sign-in fallback).
			target: "user_identities", from: "User", derivedFrom: `User.password, User.identityProvider`, key: []string{"id"},
			cols: cols([]column{c("id", "t.seed", id)}, stamps(), []column{
				c("user_id", "t.user_id", id),
				c("provider", "t.provider", derived),
				c("issuer", "''", derived),
				c("subject", "t.subject", derived),
				c("password_hash", "t.password_hash", derived),
			}),
			query: `FROM (
				SELECT 'forma2-user-password:' || u.id AS seed, u.id AS user_id, 'credential' AS provider, u.id AS subject, u.password AS password_hash, u.created_at, u.updated_at
				FROM "User" u
				WHERE u.password IS NOT NULL AND NOT EXISTS (SELECT 1 FROM "Account" a WHERE a."userId" = u.id AND a.provider = 'credential')
				UNION ALL
				SELECT 'forma2-user-identity:' || u.id, u.id, u."identityProvider"::text, u."identityProviderAccountId", NULL, u.created_at, u.updated_at
				FROM "User" u
				WHERE u."identityProvider" <> 'email' AND u."identityProviderAccountId" IS NOT NULL
					AND NOT EXISTS (SELECT 1 FROM "Account" a WHERE a."userId" = u.id AND a."providerAccountId" = u."identityProviderAccountId")
			) t ORDER BY t.seed`,
			hook: identityHook,
		},
		{
			target: "user_totp", from: "TwoFactor", oneToOne: true, key: []string{"user_id"},
			cols: []column{
				c("user_id", `t."userId"`, id),
				c("secret_ciphertext", "NULL::bytea", derived),
				c("backup_codes_ciphertext", "NULL::bytea", derived),
				// Only a row Better Auth marked verified, of a user with two-factor sign-in on, was an enabled second factor.
				c("verified", `t.verified IS NOT FALSE AND u."twoFactorEnabled"`, derived),
				c("failed_attempts", `t."failedVerificationCount"`, plain),
				c("locked_until", ts(`t."lockedUntil"`), plain),
				c("x_secret", "t.secret", extra),
				c("x_backup_codes", `t."backupCodes"`, extra),
			},
			query:   `FROM "TwoFactor" t JOIN "User" u ON u.id = t."userId" ORDER BY t."userId"`,
			prepare: im.loadTOTP,
			hook:    im.betterAuthTOTPHook,
		},
		{
			// forma2's legacy columns, for a user with two-factor sign-in on and no TwoFactor row (forma2 healed these at sign-in).
			target: "user_totp", from: "User", derivedFrom: "User.twoFactorSecret, User.backupCodes", key: []string{"user_id"},
			cols: []column{
				c("user_id", "t.id", id),
				c("secret_ciphertext", "NULL::bytea", derived),
				c("backup_codes_ciphertext", "NULL::bytea", derived),
				c("verified", "true", derived),
				c("failed_attempts", "0", derived),
				c("locked_until", "NULL::timestamptz", derived),
				c("x_secret", `t."twoFactorSecret"`, extra),
				c("x_backup_codes", `t."backupCodes"`, extra),
			},
			query: `FROM "User" t WHERE t."twoFactorEnabled" AND t."twoFactorSecret" IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM "TwoFactor" f WHERE f."userId" = t.id) ORDER BY t.id`,
			prepare: im.loadTOTP,
			hook:    im.legacyTOTPHook,
		},
		{
			target: "workspaces", from: "Workspace", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("organization_id", `t."organizationId"`, id),
				c("name", "t.name", plain),
				c("legacy_environment_id", `t."legacyEnvironmentId"`, plain),
				c("styling", js("t.styling"), jsonDoc),
				c("config", js("t.config"), jsonDoc),
				c("recontact_days", `t."recontactDays"`, plain),
				c("link_survey_branding", `t."linkSurveyBranding"`, plain),
				c("in_app_survey_branding", `t."inAppSurveyBranding"`, plain),
				c("placement", "t.placement::text", plain),
				c("click_outside_close", `t."clickOutsideClose"`, plain),
				c("overlay", "t.overlay::text", plain),
				c("logo", jsn("t.logo"), jsonDoc),
				c("app_setup_completed", `t."appSetupCompleted"`, plain),
				c("custom_head_scripts", `t."customHeadScripts"`, plain),
			}),
			query: `FROM "Workspace" t ORDER BY t.id`,
		},
		{
			// forma2 kept no timestamps on a membership; the import's time stands in for them.
			target: "memberships", from: "Membership", oneToOne: true, key: []string{"user_id", "organization_id"},
			cols: []column{
				c("user_id", `t."userId"`, id),
				c("organization_id", `t."organizationId"`, id),
				c("role", "t.role::text", plain),
				c("accepted", "t.accepted", plain),
			},
			query: `FROM "Membership" t ORDER BY t."userId", t."organizationId"`,
		},
		{
			target: "teams", from: "Team", oneToOne: true, key: []string{"id"},
			cols:  cols([]column{c("id", "t.id", id)}, stamps(), []column{c("organization_id", `t."organizationId"`, id), c("name", "t.name", plain)}),
			query: `FROM "Team" t ORDER BY t.id`,
		},
		{
			target: "team_members", from: "TeamUser", oneToOne: true, key: []string{"team_id", "user_id"},
			cols:  cols([]column{c("team_id", `t."teamId"`, id), c("user_id", `t."userId"`, id)}, stamps(), []column{c("role", "t.role::text", plain)}),
			query: `FROM "TeamUser" t ORDER BY t."teamId", t."userId"`,
		},
		{
			target: "workspace_teams", from: "WorkspaceTeam", oneToOne: true, key: []string{"workspace_id", "team_id"},
			cols:  cols([]column{c("workspace_id", `t."workspaceId"`, id), c("team_id", `t."teamId"`, id)}, stamps(), []column{c("permission", "t.permission::text", plain)}),
			query: `FROM "WorkspaceTeam" t ORDER BY t."workspaceId", t."teamId"`,
		},
		{
			// Addresses are stored lowercased, at most one invite per address and organization: the newest one is kept. forma2's invite links were JWTs this server does not read, so pending invites have to be sent again.
			target: "invites", from: "Invite", oneToOne: true, key: []string{"id"},
			cols: []column{
				c("id", "t.id", id),
				c("created_at", ts(`t."createdAt"`), plain),
				c("organization_id", `t."organizationId"`, id),
				c("email", "lower(t.email)", derived),
				c("name", "t.name", plain),
				c("creator_id", `t."creatorId"`, id),
				c("acceptor_id", `t."acceptorId"`, id),
				c("expires_at", ts(`t."expiresAt"`), plain),
				c("role", "t.role::text", plain),
				c("x_address", `t."organizationId" || ':' || lower(t.email)`, extra),
			},
			query:      `FROM "Invite" t ORDER BY t."organizationId", lower(t.email), t."createdAt" DESC, t.id DESC`,
			hook:       newestInviteHook(),
			skipReason: "an older invite to the same address in the same organization",
		},
		{
			target: "invite_teams", from: "Invite", derivedFrom: "Invite.teamIds", key: []string{"invite_id", "team_id"},
			cols: []column{c("invite_id", "i.id", id), c("team_id", "tm.id", id)},
			query: `FROM (
				SELECT DISTINCT ON (i."organizationId", lower(i.email)) i.id, i."organizationId", i."teamIds"
				FROM "Invite" i ORDER BY i."organizationId", lower(i.email), i."createdAt" DESC, i.id DESC
			) i
			CROSS JOIN LATERAL (SELECT DISTINCT unnest(i."teamIds") AS team_id) u
			JOIN "Team" tm ON tm.id = u.team_id AND tm."organizationId" = i."organizationId"
			ORDER BY i.id, tm.id`,
		},
		{
			// Keys minted before forma2 added bcrypt have no lookup hash; their hashed key is the lookup hash.
			target: "api_keys", from: "ApiKey", oneToOne: true, key: []string{"id"},
			cols: []column{
				c("id", "t.id", id),
				c("created_at", ts(`t."createdAt"`), plain),
				c("organization_id", `t."organizationId"`, id),
				c("created_by", "u.id", id),
				c("label", "t.label", plain),
				c("hashed_key", `t."hashedKey"`, plain),
				c("lookup_hash", `COALESCE(t."lookupHash", t."hashedKey")`, derived),
				c("organization_access", js(`t."organizationAccess"`), jsonDoc),
				c("last_used_at", ts(`t."lastUsedAt"`), plain),
			},
			query: `FROM "ApiKey" t LEFT JOIN "User" u ON u.id = t."createdBy" ORDER BY t.id`,
		},
		{
			target: "api_key_workspaces", from: "ApiKeyWorkspace", oneToOne: true, key: []string{"api_key_id", "workspace_id"},
			cols: []column{
				c("api_key_id", `t."apiKeyId"`, id),
				c("workspace_id", `t."workspaceId"`, id),
				c("created_at", ts(`t."createdAt"`), plain),
				c("updated_at", ts(`t."updatedAt"`), plain),
				c("permission", "t.permission::text", plain),
			},
			query: `FROM "ApiKeyWorkspace" t ORDER BY t."apiKeyId", t."workspaceId"`,
		},

		// ---- survey definition ----
		{
			target: "languages", from: "Language", oneToOne: true, key: []string{"id"},
			cols:  cols([]column{c("id", "t.id", id)}, stamps(), []column{c("workspace_id", `t."workspaceId"`, id), c("code", "t.code", plain), c("alias", "t.alias", plain)}),
			query: `FROM "Language" t ORDER BY t.id`,
		},
		{
			target: "action_classes", from: "ActionClass", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("workspace_id", `t."workspaceId"`, id),
				c("name", "t.name", plain),
				c("description", "t.description", plain),
				c("type", "t.type::text", plain),
				c("key", "t.key", plain),
				c("no_code_config", jsn(`t."noCodeConfig"`), jsonDoc),
			}),
			query: `FROM "ActionClass" t ORDER BY t.id`,
		},
		{
			target: "contact_attribute_keys", from: "ContactAttributeKey", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("workspace_id", `t."workspaceId"`, id),
				c("key", "t.key", plain),
				c("name", "t.name", plain),
				c("description", "t.description", plain),
				c("is_unique", `t."isUnique"`, plain),
				c("type", "t.type::text", plain),
				c("data_type", `t."dataType"::text`, plain),
			}),
			query: `FROM "ContactAttributeKey" t ORDER BY t.id`,
		},
		{
			target: "contacts", from: "Contact", oneToOne: true, key: []string{"id"},
			cols:  cols([]column{c("id", "t.id", id)}, stamps(), []column{c("workspace_id", `t."workspaceId"`, id)}),
			query: `FROM "Contact" t ORDER BY t.id`,
		},
		{
			// One typed value per attribute, by the key's data type. A typed copy forma2 never backfilled leaves the text, which this server falls back to.
			target: "contact_attributes", from: "ContactAttribute", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("contact_id", `t."contactId"`, id),
				c("attribute_key_id", `t."attributeKeyId"`, id),
				c("value_text", `CASE WHEN (k."dataType" = 'number' AND t."valueNumber" IS NOT NULL) OR (k."dataType" = 'date' AND t."valueDate" IS NOT NULL) THEN NULL ELSE t.value END`, derived),
				c("value_number", `CASE WHEN k."dataType" = 'number' THEN t."valueNumber" END`, derived),
				c("value_date", `CASE WHEN k."dataType" = 'date' THEN `+ts(`t."valueDate"`)+` END`, derived),
			}),
			query: `FROM "ContactAttribute" t JOIN "ContactAttributeKey" k ON k.id = t."attributeKeyId" ORDER BY t.id`,
		},
		{
			// A survey's private segment is titled with the survey's id, which this server looks it up by; the title follows the survey to its new id.
			target: "segments", from: "Segment", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("workspace_id", `t."workspaceId"`, id),
				c("title", "t.title", derived),
				c("description", "t.description", plain),
				c("is_private", `t."isPrivate"`, plain),
				c("filters", js("t.filters"), derived),
				c("x_private_survey", `t."isPrivate" AND EXISTS (SELECT 1 FROM "Survey" s WHERE s.id = t.title AND s."workspaceId" = t."workspaceId")`, extra),
			}),
			query: `FROM "Segment" t ORDER BY t.id`,
			hook:  segmentHook,
		},
		{
			target: "surveys", from: "Survey", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("workspace_id", `t."workspaceId"`, id),
				c("created_by", `t."createdBy"`, id),
				c("segment_id", `t."segmentId"`, id),
				c("name", "t.name", plain),
				c("type", "t.type::text", plain),
				c("status", "t.status::text", plain),
				c("redirect_url", `t."redirectUrl"`, plain),
				c("welcome_card", js(`t."welcomeCard"`), jsonDoc),
				c("blocks", `COALESCE(to_jsonb(t.blocks), '[]'::jsonb)::text`, derived),
				c("endings", `COALESCE(to_jsonb(t.endings), '[]'::jsonb)::text`, jsonDoc),
				c("hidden_fields", js(`t."hiddenFields"`), jsonDoc),
				c("variables", js("t.variables"), jsonDoc),
				c("inline_triggers", jsn(`t."inlineTriggers"`), jsonDoc),
				c("survey_closed_message", jsn(`t."surveyClosedMessage"`), jsonDoc),
				c("workspace_overwrites", jsn(`t."workspaceOverwrites"`), jsonDoc),
				c("styling", jsn("t.styling"), jsonDoc),
				c("single_use", jsn(`t."singleUse"`), jsonDoc),
				c("recaptcha", jsn("t.recaptcha"), jsonDoc),
				c("metadata", js("t.metadata"), jsonDoc),
				c("display_option", `t."displayOption"::text`, plain),
				c("recontact_days", `t."recontactDays"`, plain),
				c("display_limit", `t."displayLimit"`, plain),
				c("display_percentage", `round(t."displayPercentage", 2)`, derived),
				c("auto_close", `t."autoClose"`, plain),
				c("auto_complete", `t."autoComplete"`, plain),
				c("delay", "t.delay", plain),
				c("publish_on", ts(`t."publishOn"`), plain),
				c("close_on", ts(`t."closeOn"`), plain),
				c("archived_at", ts(`t."archivedAt"`), plain),
				c("is_verify_email_enabled", `t."isVerifyEmailEnabled"`, plain),
				c("is_back_button_hidden", `t."isBackButtonHidden"`, plain),
				c("is_auto_progressing_enabled", `t."isAutoProgressingEnabled"`, plain),
				c("is_capture_ip_enabled", `t."isCaptureIpEnabled"`, plain),
				c("is_anonymize_responses_enabled", "t.is_anonymize_responses_enabled", plain),
				c("show_language_switch", `t."showLanguageSwitch"`, plain),
				c("pin", "t.pin", plain),
				c("slug", "t.slug", plain),
				c("custom_head_scripts", `t."customHeadScripts"`, plain),
				c("custom_head_scripts_mode", `t."customHeadScriptsMode"::text`, plain),
				c("x_questions", js("t.questions"), extra),
			}),
			query: `FROM "Survey" t ORDER BY t.id`,
			hook:  surveyHook,
		},
		{
			target: "survey_triggers", from: "SurveyTrigger", oneToOne: true, key: []string{"survey_id", "action_class_id"},
			cols:  []column{c("survey_id", `t."surveyId"`, id), c("action_class_id", `t."actionClassId"`, id), c("created_at", ts("t.created_at"), plain)},
			query: `FROM "SurveyTrigger" t ORDER BY t."surveyId", t."actionClassId"`,
		},
		{
			target: "survey_attribute_filters", from: "SurveyAttributeFilter", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("survey_id", `t."surveyId"`, id),
				c("attribute_key_id", `t."attributeKeyId"`, id),
				c("condition", "t.condition::text", plain),
				c("value", "t.value", plain),
			}),
			query: `FROM "SurveyAttributeFilter" t ORDER BY t.id`,
		},
		{
			target: "survey_quotas", from: "SurveyQuota", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("survey_id", `t."surveyId"`, id),
				c("name", "t.name", plain),
				c("limit", `t."limit"`, plain),
				c("logic", js("t.logic"), jsonDoc),
				c("action", "t.action::text", plain),
				c("ending_card_id", `t."endingCardId"`, plain),
				c("count_partial_submissions", `t."countPartialSubmissions"`, plain),
			}),
			query: `FROM "SurveyQuota" t ORDER BY t.id`,
		},
		{
			target: "survey_follow_ups", from: "SurveyFollowUp", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("survey_id", `t."surveyId"`, id),
				c("name", "t.name", plain),
				c("trigger", js("t.trigger"), jsonDoc),
				c("action", js("t.action"), jsonDoc),
			}),
			query: `FROM "SurveyFollowUp" t ORDER BY t.id`,
		},
		{
			// At most one default language per survey: of several, the lowest language id stays the default.
			target: "survey_languages", from: "SurveyLanguage", oneToOne: true, key: []string{"survey_id", "language_id"},
			cols: []column{
				c("survey_id", `t."surveyId"`, id),
				c("language_id", `t."languageId"`, id),
				c("is_default", `t."default" AND row_number() OVER (PARTITION BY t."surveyId", t."default" ORDER BY t."languageId") = 1`, derived),
				c("enabled", "t.enabled", plain),
				c("x_demoted", `t."default" AND row_number() OVER (PARTITION BY t."surveyId", t."default" ORDER BY t."languageId") > 1`, extra),
			},
			query: `FROM "SurveyLanguage" t ORDER BY t."surveyId", t."languageId"`,
			hook: func(r *row) (bool, error) {
				if r.flag("x_demoted") {
					im.warn("survey %s had more than one default language; language %s is no longer its default", r.str("survey_id"), r.str("language_id"))
				}
				return true, nil
			},
		},
		{
			// Reserved fields are a catalog in this server's code, never stored.
			target: "embedded_data", from: "EmbeddedData", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("workspace_id", `t."workspaceId"`, id),
				c("survey_id", `t."surveyId"`, id),
				c("key", "t.key", plain),
				c("name", "t.name", plain),
				c("description", "t.description", plain),
				c("source", "t.source::text", plain),
				c("data_type", `t."dataType"::text`, plain),
				c("default_value", jsn(`t."defaultValue"`), jsonDoc),
				c("locked", "t.locked", plain),
			}),
			query:      `FROM "EmbeddedData" t ORDER BY t.id`,
			hook:       func(r *row) (bool, error) { return r.str("source") != "reserved", nil },
			skipReason: "a reserved field, which this server does not store",
		},
		{
			target: "survey_embedded_data", from: "SurveyEmbeddedData", oneToOne: true, key: []string{"id"},
			cols: []column{
				c("id", "t.id", id),
				c("workspace_id", `t."workspaceId"`, id),
				c("survey_id", `t."surveyId"`, id),
				c("embedded_data_id", `t."embeddedDataId"`, id),
				c("storage_key", `t."storageKey"`, plain),
				c("position", `t."order"`, plain),
				c("x_reserved", "e.source = 'reserved'", extra),
			},
			query:      `FROM "SurveyEmbeddedData" t JOIN "EmbeddedData" e ON e.id = t."embeddedDataId" ORDER BY t.id`,
			hook:       func(r *row) (bool, error) { return !r.flag("x_reserved"), nil },
			skipReason: "a link to a reserved field, which every survey has",
		},

		// ---- response collection ----
		{
			target: "displays", from: "Display", oneToOne: true, key: []string{"id"},
			cols:  cols([]column{c("id", "t.id", id)}, stamps(), []column{c("survey_id", `t."surveyId"`, id), c("contact_id", `t."contactId"`, id)}),
			query: `FROM "Display" t ORDER BY t.id`,
		},
		{
			target: "responses", from: "Response", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("survey_id", `t."surveyId"`, id),
				c("contact_id", `t."contactId"`, id),
				c("display_id", `t."displayId"`, id),
				c("finished", "t.finished", plain),
				c("ending_id", `t."endingId"`, plain),
				c("data", js("t.data"), jsonDoc),
				c("variables", js("t.variables"), jsonDoc),
				c("ttc", js("t.ttc"), jsonDoc),
				c("meta", js("t.meta"), jsonDoc),
				c("ingest_flags", jsn("t.ingest_flags"), jsonDoc),
				c("contact_attributes", jsn(`t."contactAttributes"`), jsonDoc),
				c("single_use_id", `t."singleUseId"`, plain),
				c("language", "t.language", plain),
			}),
			query: `FROM "Response" t ORDER BY t.id`,
		},
		{
			target: "tags", from: "Tag", oneToOne: true, key: []string{"id"},
			cols:  cols([]column{c("id", "t.id", id)}, stamps(), []column{c("workspace_id", `t."workspaceId"`, id), c("name", "t.name", plain)}),
			query: `FROM "Tag" t ORDER BY t.id`,
		},
		{
			target: "response_tags", from: "TagsOnResponses", oneToOne: true, key: []string{"response_id", "tag_id"},
			cols:  []column{c("response_id", `t."responseId"`, id), c("tag_id", `t."tagId"`, id)},
			query: `FROM "TagsOnResponses" t ORDER BY t."responseId", t."tagId"`,
		},
		{
			target: "response_quota_links", from: "ResponseQuotaLink", oneToOne: true, key: []string{"response_id", "quota_id"},
			cols:  []column{c("response_id", `t."responseId"`, id), c("quota_id", `t."quotaId"`, id), c("status", "t.status::text", plain)},
			query: `FROM "ResponseQuotaLink" t ORDER BY t."responseId", t."quotaId"`,
		},

		// ---- integrations ----
		{
			// An empty survey list meant every survey; the scope is now explicit.
			target: "webhooks", from: "Webhook", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("workspace_id", `t."workspaceId"`, id),
				c("name", "t.name", plain),
				c("url", "t.url", plain),
				c("source", "t.source::text", plain),
				c("triggers", `COALESCE(t.triggers::text[], '{}')`, derived),
				c("secret", "t.secret", plain),
				c("all_surveys", `COALESCE(cardinality(t."surveyIds"), 0) = 0`, derived),
			}),
			query: `FROM "Webhook" t ORDER BY t.id`,
			hook:  webhookHook,
		},
		{
			// Survey ids of deleted surveys are kept, as forma2 kept them.
			target: "webhook_surveys", from: "Webhook", derivedFrom: "Webhook.surveyIds", key: []string{"webhook_id", "survey_id"},
			cols: []column{c("webhook_id", "w.id", id), c("survey_id", "u.survey_id", id)},
			query: `FROM "Webhook" w CROSS JOIN LATERAL (SELECT DISTINCT unnest(w."surveyIds") AS survey_id) u
				WHERE u.survey_id IS NOT NULL AND u.survey_id <> '' ORDER BY w.id, u.survey_id`,
		},
		{
			// forma2 kept no timestamps on an integration; the import's time stands in for them. The links keep forma2's survey ids, which delivery resolves.
			target: "integrations", from: "Integration", oneToOne: true, key: []string{"id"},
			cols: []column{
				c("id", "t.id", id),
				c("workspace_id", `t."workspaceId"`, id),
				c("type", "t.type::text", plain),
				c("config", js("t.config"), derived),
				c("last_error_at", s.opt("Integration", "lastErrorAt", `CASE WHEN t."lastErrorMessage" IS NOT NULL THEN `+ts(`t."lastErrorAt"`)+` END`, "NULL::timestamptz"), derived),
				c("last_error_message", s.opt("Integration", "lastErrorMessage", `CASE WHEN t."lastErrorAt" IS NOT NULL THEN t."lastErrorMessage" END`, "NULL::text"), derived),
				c("consecutive_failures", s.opt("Integration", "consecutiveFailures", `GREATEST(t."consecutiveFailures", 0)`, "0"), derived),
			},
			query: `FROM "Integration" t ORDER BY t.id`,
			hook:  integrationConfigHook,
		},
		{
			target: "integration_credentials", from: "Integration", derivedFrom: "Integration.config.key", key: []string{"integration_id"},
			cols: []column{
				c("integration_id", "t.id", id),
				c("ciphertext", "NULL::bytea", derived),
				c("x_key", "(t.config -> 'key')::text", extra),
				c("x_email", "t.config ->> 'email'", extra),
			},
			query:   `FROM "Integration" t WHERE jsonb_typeof(t.config -> 'key') = 'object' ORDER BY t.id`,
			prepare: im.loadCredentials,
			hook:    im.credentialsHook,
		},

		// ---- workflows ----
		{
			// trigger_survey_id is the survey the newest version's trigger targets; null matches every survey. The definition keeps forma2's survey id, which this server resolves and stores as the new id on the next save.
			target: "workflows", from: "Workflow", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("workspace_id", `t."workspaceId"`, id),
				c("created_by", `t."createdBy"`, id),
				c("name", "t.name", plain),
				c("description", "t.description", plain),
				c("status", "t.status::text", plain),
				c("definition", js("t.definition"), jsonDoc),
				c("trigger_survey_id", `(SELECT CASE WHEN v.definition -> 'trigger' ->> 'triggerType' = 'response.completed' AND jsonb_typeof(v.definition -> 'trigger' -> 'config' -> 'surveyId') = 'string'
					THEN NULLIF(v.definition -> 'trigger' -> 'config' ->> 'surveyId', '') END
					FROM "WorkflowVersion" v WHERE v."workflowId" = t.id ORDER BY v.version DESC LIMIT 1)`, id),
			}),
			query: `FROM "Workflow" t ORDER BY t.id`,
		},
		{
			target: "workflow_versions", from: "WorkflowVersion", oneToOne: true, key: []string{"id"},
			cols: []column{
				c("id", "t.id", id),
				c("workflow_id", `t."workflowId"`, id),
				c("workspace_id", `t."workspaceId"`, id),
				c("version", "t.version", plain),
				c("definition", js("t.definition"), jsonDoc),
				c("published_at", ts(`t."publishedAt"`), plain),
				c("published_by", `t."publishedBy"`, id),
			},
			query: `FROM "WorkflowVersion" t ORDER BY t.id`,
		},
		{
			// A run still queued or running in forma2 would wait for a BullMQ job that is not carried over; it is closed as canceled.
			target: "workflow_runs", from: "WorkflowRun", oneToOne: true, key: []string{"id"},
			cols: cols([]column{c("id", "t.id", id)}, stamps(), []column{
				c("workflow_id", `t."workflowId"`, id),
				c("workspace_id", `t."workspaceId"`, id),
				c("workflow_version_id", `t."workflowVersionId"`, id),
				c("response_id", `t."responseId"`, id),
				c("survey_id", `t."surveyId"`, id),
				c("status", `CASE WHEN t.status IN ('queued', 'running') THEN 'canceled' ELSE t.status::text END`, derived),
				c("trigger_type", `t."triggerType"`, plain),
				c("is_dry_run", `t."isDryRun"`, plain),
				c("idempotency_key", `t."idempotencyKey"`, plain),
				c("attempt", "t.attempt", plain),
				c("next_attempt_at", `CASE WHEN t.status IN ('queued', 'running') THEN NULL ELSE `+ts(`t."nextAttemptAt"`)+` END`, derived),
				c("last_error_at", ts(`t."lastErrorAt"`), plain),
				c("trigger_payload", js(`t."triggerPayload"`), jsonDoc),
				c("data", js("t.data"), jsonDoc),
				c("error", `CASE WHEN t.status IN ('queued', 'running') THEN COALESCE(t.error, `+literal(unfinishedRun)+`) ELSE t.error END`, derived),
				c("dispatched_at", ts(`t."dispatchedAt"`), plain),
				c("started_at", ts(`t."startedAt"`), plain),
				c("finished_at", `CASE WHEN t.status IN ('queued', 'running') THEN `+ts(`COALESCE(t."finishedAt", t.updated_at)`)+` ELSE `+ts(`t."finishedAt"`)+` END`, derived),
				c("x_unfinished", `t.status IN ('queued', 'running')`, extra),
			}),
			query: `FROM "WorkflowRun" t ORDER BY t.id`,
			hook: func(r *row) (bool, error) {
				if r.flag("x_unfinished") {
					im.warn("workflow run %s was still queued or running in forma2 and is imported as canceled", r.str("id"))
				}
				return true, nil
			},
		},
		{
			target: "workflow_run_logs", from: "WorkflowRunLog", oneToOne: true, key: []string{"id"},
			cols: []column{
				c("id", "t.id", id),
				c("run_id", `t."runId"`, id),
				c("sequence", "t.sequence", plain),
				c("step_id", `t."stepId"`, plain),
				c("step_type", `t."stepType"`, plain),
				c("status", `CASE WHEN r.status IN ('queued', 'running') AND t.status IN ('pending', 'running') THEN 'skipped' ELSE t.status::text END`, derived),
				c("input", js("t.input"), jsonDoc),
				c("output", js("t.output"), jsonDoc),
				c("error", "t.error", plain),
				c("started_at", ts(`t."startedAt"`), plain),
				c("finished_at", ts(`t."finishedAt"`), plain),
			},
			query: `FROM "WorkflowRunLog" t JOIN "WorkflowRun" r ON r.id = t."runId" ORDER BY t.id`,
		},
	}
}

// notMigratedTables are forma2's tables the import leaves behind, with why.
var notMigratedTables = []NotMigrated{
	{Table: "Session", Reason: "sessions are not carried over; everyone signs in again once"},
	{Table: "VerificationToken", Reason: "short-lived tokens; a link that carried one asks again"},
	{Table: "PasswordResetToken", Reason: "short-lived tokens; a reset link asks again"},
	{Table: "jwks", Reason: "this server signs OAuth tokens with keys of its own; forma2's tokens lived at most 15 minutes"},
	{Table: "oauthClient", Reason: "MCP clients register again through dynamic client registration"},
	{Table: "oauthAccessToken", Reason: "MCP clients authorize again"},
	{Table: "oauthRefreshToken", Reason: "MCP clients authorize again"},
	{Table: "oauthConsent", Reason: "depends on the OAuth clients, which are not carried over"},
	{Table: "oauthResource", Reason: "this server derives the MCP resource from its configuration"},
	{Table: "oauthClientResource", Reason: "allowed resources are recorded per client at registration"},
	{Table: "oauthClientAssertion", Reason: "this server has no private_key_jwt client authentication"},
	{Table: "DataMigration", Reason: "forma2's own bookkeeping"},
	{Table: "AuthzedProjectionOutbox", Reason: "this schema's triggers queue the SpiceDB projection of every imported row"},
	{Table: "ResponsePipelineOutbox", Reason: "forma2's BullMQ redelivery; this server's jobs run on River"},
	{Table: "_prisma_migrations", Reason: "left for forma2, so it can be started again on its tables"},
}
