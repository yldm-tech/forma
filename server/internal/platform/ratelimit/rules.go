package ratelimit

import "time"

// The limits forma2 applied in its server actions and API routes (apps/web/modules/core/rate-limit/rate-limit-configs.ts), with the same namespaces so the keys keep their shape. The comment on each says what it is keyed by at its call site.
var (
	// AuthLogin is defined by forma2 but had no call site there: sign-in was throttled by Better Auth's per-path limiter instead.
	AuthLogin = Rule{Namespace: "auth:login", Window: 15 * time.Minute, Limit: 10}
	// AuthSignup is per client IP.
	AuthSignup = Rule{Namespace: "auth:signup", Window: time.Hour, Limit: 30}
	// AuthForgotPassword is per client IP.
	AuthForgotPassword = Rule{Namespace: "auth:forgot", Window: time.Hour, Limit: 5}
	// AuthResetPassword is per client IP, separate from AuthForgotPassword so requesting reset mails cannot exhaust the budget to redeem one.
	AuthResetPassword = Rule{Namespace: "auth:reset-password", Window: time.Hour, Limit: 5}
	// AuthVerifyEmail is per client IP, shared by resending a verification mail and confirming an email change.
	AuthVerifyEmail = Rule{Namespace: "auth:verify", Window: time.Hour, Limit: 10}
	// AuthEmailToken is per client IP; the endpoint tells the caller whether an address is registered.
	AuthEmailToken = Rule{Namespace: "auth:email-token", Window: time.Hour, Limit: 10}

	APIV1 = Rule{Namespace: "api:v1", Window: time.Minute, Limit: 100}
	APIV2 = Rule{Namespace: "api:v2", Window: time.Minute, Limit: 100}
	APIV3 = Rule{Namespace: "api:v3", Window: time.Minute, Limit: 100}
	// APIMCPAuth is per client IP.
	APIMCPAuth = Rule{Namespace: "api:mcp:auth", Window: time.Minute, Limit: 30}
	// APIV3SurveyGenerate bounds AI survey generation.
	APIV3SurveyGenerate = Rule{Namespace: "api:v3:surveys:generate", Window: time.Minute, Limit: 10}
	// APIClient is per environment and client IP; APIClientEnvironment is per environment, bounding distributed-IP abuse of one environment.
	APIClient            = Rule{Namespace: "api:client", Window: time.Minute, Limit: 100}
	APIClientEnvironment = Rule{Namespace: "api:client:environment", Window: time.Minute, Limit: 1000}

	// ActionEmailUpdate is per user.
	ActionEmailUpdate = Rule{Namespace: "action:email", Window: time.Hour, Limit: 3}
	// ActionAccountDeletion is per user.
	ActionAccountDeletion   = Rule{Namespace: "action:account-delete", Window: time.Hour, Limit: 5}
	ActionSurveyFollowUp    = Rule{Namespace: "action:followup", Window: time.Hour, Limit: 50}
	ActionSendLinkSurvey    = Rule{Namespace: "action:send-link-survey-email", Window: time.Hour, Limit: 10}
	ActionValidateSurveyPin = Rule{Namespace: "action:validate-survey-pin", Window: time.Minute, Limit: 10}
	ActionLicenseRecheck    = Rule{Namespace: "action:license-recheck", Window: time.Minute, Limit: 5}
	// ActionUnsplash is per user; it keeps one account from exhausting the instance-wide Unsplash quota.
	ActionUnsplash = Rule{Namespace: "action:unsplash", Window: time.Minute, Limit: 30}
	// ActionInviteMember is per organization and weighted by the number of recipients. The invite module overrides Limit from INVITE_RATE_LIMIT_PER_24_HOURS (500 on Forma Cloud).
	ActionInviteMember = Rule{Namespace: "action:invite-member", Window: 24 * time.Hour, Limit: 50}
	// ActionGenerateExampleResponses is per user: it closes the multi-click race and bounds LLM spend.
	ActionGenerateExampleResponses = Rule{Namespace: "action:generate-example-responses", Window: time.Minute, Limit: 1}
	// ActionIntegrationMutation is per user.
	ActionIntegrationMutation = Rule{Namespace: "action:integration-mutation", Window: time.Minute, Limit: 30}
	// ActionStateMutation is per organization or workspace: the shared guard for authenticated settings writes.
	ActionStateMutation = Rule{Namespace: "action:state-mutation", Window: time.Minute, Limit: 120}

	// Better Auth's per-path limits (apps/web/modules/auth/lib/auth.ts, rateLimit.customRules), all per client IP. They guarded the endpoints the browser called directly; the flows forma2 ran through server actions were limited by the rules above instead.
	//
	// BASignIn guards the password check of a sign-in.
	BASignIn = Rule{Namespace: "ba:sign-in-email", Window: time.Minute, Limit: 5}
	// BAVerifyEmail is deliberately generous: mail scanners prefetch links and corporate NAT puts many users behind one address, and the token itself is the authorization.
	BAVerifyEmail = Rule{Namespace: "ba:verify-email", Window: time.Minute, Limit: 30}
	// BATwoFactor guards second-factor attempts.
	BATwoFactor = Rule{Namespace: "ba:two-factor", Window: time.Minute, Limit: 5}
	// BADeleteUser bounds password guesses from a stolen session against account deletion.
	BADeleteUser = Rule{Namespace: "ba:delete-user", Window: time.Hour, Limit: 5}
	// BADeleteUserCallback guards redeeming an emailed account deletion link.
	BADeleteUserCallback = Rule{Namespace: "ba:delete-user-callback", Window: time.Minute, Limit: 5}
	// BAChangePassword guards the current-password check of a password change. Better Auth left /change-password on its global flood guard only; this applies the sign-in limit to the same password check.
	BAChangePassword = Rule{Namespace: "ba:change-password", Window: time.Minute, Limit: 5}

	StorageUpload             = Rule{Namespace: "storage:upload", Window: time.Minute, Limit: 5}
	StorageUploadPerWorkspace = Rule{Namespace: "storage:upload:workspace", Window: time.Minute, Limit: 100}
	StorageDelete             = Rule{Namespace: "storage:delete", Window: time.Minute, Limit: 5}
	// StorageAttachmentsExport is charged on the download path only; one export streams thousands of objects.
	StorageAttachmentsExport = Rule{Namespace: "storage:attachments-export", Window: 10 * time.Minute, Limit: 3}
)
