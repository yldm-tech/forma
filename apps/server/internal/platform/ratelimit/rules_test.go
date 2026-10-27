package ratelimit

import (
	"testing"
	"time"
)

// The table forma2's rate-limit-configs.test.ts pins: every limit, its namespace, and that no two share a namespace (they would share counters).
func TestRules(t *testing.T) {
	cases := []struct {
		rule      Rule
		namespace string
		window    time.Duration
		limit     int
	}{
		{AuthLogin, "auth:login", 900 * time.Second, 10},
		{AuthSignup, "auth:signup", 3600 * time.Second, 30},
		{AuthForgotPassword, "auth:forgot", 3600 * time.Second, 5},
		{AuthSignInAccountIP, "auth:sign-in:account-ip", 900 * time.Second, 10},
		{AuthSignInAccountIPDaily, "auth:sign-in:account-ip:day", 86400 * time.Second, 50},
		{AuthResetPassword, "auth:reset-password", 3600 * time.Second, 5},
		{AuthVerifyEmail, "auth:verify", 3600 * time.Second, 10},
		{AuthEmailToken, "auth:email-token", 3600 * time.Second, 10},
		{APIV1, "api:v1", 60 * time.Second, 100},
		{APIV2, "api:v2", 60 * time.Second, 100},
		{APIV3, "api:v3", 60 * time.Second, 100},
		{APIMCPAuth, "api:mcp:auth", 60 * time.Second, 30},
		{APIV3SurveyGenerate, "api:v3:surveys:generate", 60 * time.Second, 10},
		{APIClient, "api:client", 60 * time.Second, 100},
		{APIClientEnvironment, "api:client:environment", 60 * time.Second, 1000},
		{ActionEmailUpdate, "action:email", 3600 * time.Second, 3},
		{ActionAccountDeletion, "action:account-delete", 3600 * time.Second, 5},
		{ActionSurveyFollowUp, "action:followup", 3600 * time.Second, 50},
		{ActionSendLinkSurvey, "action:send-link-survey-email", 3600 * time.Second, 10},
		{ActionValidateSurveyPin, "action:validate-survey-pin", 60 * time.Second, 10},
		{ActionLicenseRecheck, "action:license-recheck", 60 * time.Second, 5},
		{ActionUnsplash, "action:unsplash", 60 * time.Second, 30},
		{ActionInviteMember, "action:invite-member", 86400 * time.Second, 50},
		{ActionGenerateExampleResponses, "action:generate-example-responses", 60 * time.Second, 1},
		{ActionAITextSummary, "action:ai-text-summary", 3600 * time.Second, 60},
		{ActionAITextSummaryMember, "action:ai-text-summary:user", 3600 * time.Second, 20},
		{ActionIntegrationMutation, "action:integration-mutation", 60 * time.Second, 30},
		{ActionStateMutation, "action:state-mutation", 60 * time.Second, 120},
		{PublicResultShare, "public:result-share", 60 * time.Second, 60},
		{PublicUnsubscribe, "public:unsubscribe", 60 * time.Second, 30},
		{PublicUnsubscribeOneClick, "public:unsubscribe:one-click", 60 * time.Second, 10},
		{BASignIn, "ba:sign-in-email", 60 * time.Second, 5},
		{BAVerifyEmail, "ba:verify-email", 60 * time.Second, 30},
		{BATwoFactor, "ba:two-factor", 60 * time.Second, 5},
		{BADeleteUser, "ba:delete-user", 3600 * time.Second, 5},
		{BADeleteUserCallback, "ba:delete-user-callback", 60 * time.Second, 5},
		{BAChangePassword, "ba:change-password", 60 * time.Second, 5},
		{BASSORecovery, "ba:sso-recovery", 60 * time.Second, 5},
		{StorageUpload, "storage:upload", 60 * time.Second, 5},
		{StorageUploadPerWorkspace, "storage:upload:workspace", 60 * time.Second, 100},
		{StorageDelete, "storage:delete", 60 * time.Second, 5},
		{StorageAttachmentsExport, "storage:attachments-export", 600 * time.Second, 3},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.namespace, func(t *testing.T) {
			if tc.rule != (Rule{Namespace: tc.namespace, Window: tc.window, Limit: tc.limit}) {
				t.Fatalf("rule = %+v, want %s %d per %v", tc.rule, tc.namespace, tc.limit, tc.window)
			}
			if err := tc.rule.validate(); err != nil {
				t.Fatal(err)
			}
			if seen[tc.rule.Namespace] {
				t.Fatalf("namespace %q is used twice", tc.rule.Namespace)
			}
			seen[tc.rule.Namespace] = true
		})
	}
}
