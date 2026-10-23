package ratelimit

import "testing"

// forma2's envoy-rate-limit-coverage.test.ts, case for case.
func TestCoveredByGateway(t *testing.T) {
	covered := []struct {
		method, path string
		auth         GatewayAuth
	}{
		{"POST", "/api/auth/callback/credentials", GatewayAuthNone},
		{"POST", "/api/auth/callback/token", GatewayAuthNone},
		{"GET", "/api/v1/management/surveys", GatewayAuthAPIKey},
		{"POST", "/api/v1/management/storage", GatewayAuthAPIKey},
		{"GET", "/api/v1/management/me", GatewayAuthAPIKey},
		{"DELETE", "/api/v1/webhooks/webhook-id", GatewayAuthAPIKey},
		{"GET", "/api/v1/client/env_123/environment", GatewayAuthNone},
		{"POST", "/api/v1/client/env_123/responses", GatewayAuthNone},
		{"PUT", "/api/v1/client/env_123/responses/response_123", GatewayAuthNone},
		{"POST", "/api/v1/client/env_123/displays", GatewayAuthNone},
		{"POST", "/api/v1/client/env_123/user", GatewayAuthNone},
		{"POST", "/api/v1/client/env_123/storage", GatewayAuthNone},
		{"POST", "/api/v2/client/env_123/responses", GatewayAuthNone},
		{"PUT", "/api/v2/client/env_123/responses/response_123", GatewayAuthNone},
		{"POST", "/api/v2/client/env_123/displays", GatewayAuthNone},
		{"POST", "/api/v2/client/env_123/storage", GatewayAuthNone},
		{"DELETE", "/storage/env_123/private/file.pdf", GatewayAuthAPIKey},
		{"get", "/api/v1/client/env_123/environment", GatewayAuthNone},
	}
	for _, tc := range covered {
		if !CoveredByGateway(tc.method, tc.path, tc.auth) {
			t.Errorf("%s %s (auth %d) not covered, want covered", tc.method, tc.path, tc.auth)
		}
	}
	uncovered := []struct {
		method, path string
		auth         GatewayAuth
	}{
		{"GET", "/api/v1/client/og", GatewayAuthNone},
		{"GET", "/api/v2/health", GatewayAuthNone},
		{"PATCH", "/api/v1/client/env_123/environment", GatewayAuthNone},
		{"GET", "/api/v1/client/env_123/displays", GatewayAuthNone},
		{"GET", "/api/v1/client/env_123/displays/display_123/response", GatewayAuthNone},
		{"PUT", "/api/v2/client/env_123/responses", GatewayAuthNone},
		{"POST", "/api/v2/client/env_123/responses/response_123", GatewayAuthNone},
		{"GET", "/api/v2/client/env_123/environment", GatewayAuthNone},
		{"POST", "/api/v2/client/env_123/user", GatewayAuthNone},
		{"GET", "/api/v1/management/me", GatewayAuthSession},
		{"POST", "/api/v1/management/storage", GatewayAuthSession},
		{"DELETE", "/storage/env_123/private/file.pdf", GatewayAuthSession},
		{"GET", "/api/v1/webhooks", GatewayAuthAPIKey},
		{"OPTIONS", "/api/v1/client/env_123/environment", GatewayAuthNone},
		{"GET", "/api/v1/client/env_123/environment", GatewayAuthAPIKey},
	}
	for _, tc := range uncovered {
		if CoveredByGateway(tc.method, tc.path, tc.auth) {
			t.Errorf("%s %s (auth %d) covered, want not covered", tc.method, tc.path, tc.auth)
		}
	}
}
