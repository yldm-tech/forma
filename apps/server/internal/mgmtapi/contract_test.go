package mgmtapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The contract test replays the exchanges recorded against forma2 (testdata/forma2, see its README) in the order their scenarios need, against data equivalent to what the recorder created, and requires the same status, the recorded headers and a body equal to the recorded one with its placeholders matched by kind. An id placeholder binds to the first id it meets and must name the same id wherever it appears again; ids are the rewrite's UUIDs where forma2 had cuids. Steps whose name starts with "@" are the recorder's own requests and writes that set up a scenario without being recorded.

type recorded struct {
	Case    string `json:"case"`
	Request struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Query   map[string]string `json:"query"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	} `json:"request"`
	Response struct {
		Status     int               `json:"status"`
		Headers    map[string]string `json:"headers"`
		BodyFormat string            `json:"bodyFormat"`
		Body       json.RawMessage   `json:"body"`
	} `json:"response"`
	Notes struct {
		ContactLinkToken *struct {
			Header          map[string]string `json:"header"`
			Claims          []string          `json:"claims"`
			LifetimeSeconds *float64          `json:"lifetimeSeconds"`
		} `json:"contactLinkToken"`
	} `json:"notes"`
}

// notReplayed are the recorded exchanges the replay skips, with the reason.
var notReplayed = map[string]string{
	"mcp-unauthenticated":    mcpCrashed,
	"mcp-api-key-initialize": mcpCrashed,
	"mcp-api-key-tools-list": mcpCrashed,
	"mcp-foreign-origin":     mcpCrashed,
	"mcp-invalid-bearer":     mcpCrashed,
	"mcp-query-credential":   mcpCrashed,
}

const mcpCrashed = "forma2 answered every POST /api/mcp with a bare 500 before authenticating (notes.knownDefect), so the recording holds no MCP exchange; the intended answers the notes describe are tested in internal/mcp."

// replayOrder is the order the recorder's scenarios need: v1 first (action classes, attribute keys, the contacts the client API identified, surveys, the client API's responses and the v1 ones, storage), then v2, whose lists the v1 writes populate. The rate-limit cases come last.
var replayOrder = []string{
	"v1-management-options",
	"v1-me-no-key", "v1-me-invalid-key", "v1-me-bearer-ignored", "v1-me-organization-only-key", "v1-me", "v1-me-other-workspace-key",

	"v1-action-classes-list-no-key", "v1-action-classes-list-invalid-key", "v1-action-classes-list-organization-only-key", "v1-action-classes-list-bearer",
	"v1-action-classes-create-code", "@checkout-recorded-at",
	"v1-action-classes-create-nocode-legacy-environment-id", "v1-action-classes-create-duplicate-key", "v1-action-classes-create-invalid-body",
	"v1-action-classes-create-malformed-json", "v1-action-classes-create-missing-workspace", "v1-action-classes-create-other-workspace-key",
	"v1-action-classes-create-read-key", "v1-action-classes-create-unknown-workspace",
	"v1-action-classes-list", "v1-action-classes-list-read-key", "v1-action-classes-get", "v1-action-classes-get-other-workspace-key", "v1-action-classes-get-unknown",
	"v1-action-classes-update", "v1-action-classes-update-invalid-body", "v1-action-classes-update-malformed-json", "v1-action-classes-update-read-key",
	"v1-action-classes-update-unknown",
	"@temporary-action-class", "v1-action-classes-delete-write-key", "v1-action-classes-delete", "v1-action-classes-delete-unknown",

	"v1-contact-attribute-keys-create", "v1-contact-attribute-keys-create-number", "v1-contact-attribute-keys-create-duplicate",
	"v1-contact-attribute-keys-create-invalid-key", "v1-contact-attribute-keys-create-malformed-json", "v1-contact-attribute-keys-create-missing-type",
	"v1-contact-attribute-keys-create-read-key",
	"v1-contact-attribute-keys-get", "v1-contact-attribute-keys-get-other-workspace-key", "v1-contact-attribute-keys-get-unknown",
	"v1-contact-attribute-keys-update", "v1-contact-attribute-keys-update-invalid-body", "v1-contact-attribute-keys-update-read-key",
	"v1-contact-attribute-keys-list", "v1-contact-attribute-keys-list-other-workspace-key",
	"@temporary-attribute-key", "v1-contact-attribute-keys-delete-read-key", "v1-contact-attribute-keys-delete", "v1-contact-attribute-keys-delete-unknown",
	"v1-contact-attribute-keys-delete-default",

	"@identified-contacts",
	"v1-contacts-delete-write-key", "v1-contacts-delete", "v1-contacts-delete-unknown",
	"v1-contacts-list", "v1-contacts-list-paginated", "v1-contacts-list-invalid-pagination", "v1-contacts-list-other-workspace-key",
	"v1-contacts-get", "v1-contacts-get-other-workspace-key", "v1-contacts-get-unknown",
	"v1-contact-attributes-list", "v1-contact-attributes-list-other-workspace-key",

	"v1-surveys-create-questions", "v1-surveys-create-blocks", "v1-surveys-create-legacy-environment-id", "v1-surveys-create-other-workspace",
	"v1-surveys-create-questions-and-blocks", "v1-surveys-create-invalid-body", "v1-surveys-create-malformed-json", "v1-surveys-create-missing-workspace",
	"v1-surveys-create-other-workspace-key", "v1-surveys-create-read-key", "v1-surveys-create-recaptcha",
	"v1-surveys-list", "v1-surveys-list-paginated", "v1-surveys-list-skip-ignored", "v1-surveys-list-offset-beyond", "v1-surveys-list-organization-only-key",
	"v1-surveys-list-other-workspace-key", "v1-surveys-list-invalid-offset", "v1-surveys-list-limit-too-large", "v1-surveys-list-limit-zero",
	"v1-surveys-get", "v1-surveys-get-read-key", "v1-surveys-get-app", "v1-surveys-get-other-workspace-key", "v1-surveys-get-unknown", "v1-surveys-get-malformed-id",
	"v1-surveys-single-use-ids", "v1-surveys-single-use-ids-default-limit", "v1-surveys-single-use-ids-limit-too-large", "v1-surveys-single-use-ids-limit-zero",
	"v1-surveys-single-use-ids-not-enabled", "v1-surveys-single-use-ids-app-survey", "v1-surveys-single-use-ids-read-key", "v1-surveys-single-use-ids-unknown",
	"v1-surveys-update", "v1-surveys-update-invalid-body", "v1-surveys-update-malformed-json", "v1-surveys-update-read-key", "v1-surveys-update-unknown",
	"v1-surveys-update-questions", "v1-surveys-get-after-update",
	"v1-surveys-update-single-use-unencrypted", "v1-surveys-single-use-ids-unencrypted",
	"@temporary-survey", "v1-surveys-delete-write-key", "v1-surveys-delete", "v1-surveys-delete-unknown",

	"@client-responses",
	"v1-responses-create", "v1-responses-create-url-in-text-element", "v1-responses-create-userid",
	"v1-responses-create-invalid-body", "v1-responses-create-malformed-json", "v1-responses-create-missing-workspace", "v1-responses-create-read-key",
	"v1-responses-create-survey-other-workspace", "v1-responses-create-unknown-survey", "v1-responses-create-validation-failed",
	"v1-responses-list", "v1-responses-list-by-survey", "v1-responses-list-paginated", "v1-responses-list-invalid-pagination", "v1-responses-list-other-workspace-key",
	"v1-responses-list-survey-other-workspace-key", "v1-responses-list-unknown-survey",
	"v1-responses-get", "v1-responses-get-contact", "v1-responses-get-other-workspace-key", "v1-responses-get-unknown",
	"v1-responses-update", "v1-responses-update-invalid-body", "v1-responses-update-malformed-json", "v1-responses-update-read-key", "v1-responses-update-unknown",
	"v1-responses-update-validation-failed",
	"@temporary-response", "v1-responses-delete-write-key", "v1-responses-delete", "v1-responses-delete-unknown",

	"v1-storage-upload", "v1-storage-upload-legacy-environment-id", "v1-storage-extension-not-allowed", "v1-storage-invalid-file-name",
	"v1-storage-malformed-json", "v1-storage-missing-fields", "v1-storage-missing-workspace", "v1-storage-no-key", "v1-storage-other-workspace-key",
	"v1-storage-read-key", "v1-storage-unknown-workspace",

	"v2-management-options", "v2-health", "v2-roles-no-key", "v2-roles", "v2-openapi-not-served",
	"v2-me-no-key", "v2-me-invalid-key", "v2-me", "v2-me-bearer", "v2-me-cross-origin", "v2-me-organization-only-key", "v2-me-other-workspace-key",
	"v2-me-without-organization-access",

	"v2-contact-attribute-keys-list", "v2-contact-attribute-keys-list-paginated", "v2-contact-attribute-keys-list-other-workspace-key",
	"v2-contact-attribute-keys-list-organization-only-key", "v2-contact-attribute-keys-list-invalid-query", "v2-contact-attribute-keys-list-date-range-inverted",
	"v2-contact-attribute-keys-create", "v2-contact-attribute-keys-create-legacy-environment-id", "v2-contact-attribute-keys-create-reserved-key",
	"v2-contact-attribute-keys-create-duplicate", "v2-contact-attribute-keys-create-invalid-key", "v2-contact-attribute-keys-create-malformed-json",
	"v2-contact-attribute-keys-create-missing-description", "v2-contact-attribute-keys-create-missing-workspace", "v2-contact-attribute-keys-create-other-workspace-key",
	"v2-contact-attribute-keys-create-read-key", "v2-contact-attribute-keys-create-unknown-workspace",
	"v2-contact-attribute-keys-get", "v2-contact-attribute-keys-get-default", "v2-contact-attribute-keys-get-invalid-id", "v2-contact-attribute-keys-get-other-workspace-key",
	"v2-contact-attribute-keys-get-unknown",
	"v2-contact-attribute-keys-update", "v2-contact-attribute-keys-update-default", "v2-contact-attribute-keys-update-invalid-body",
	"v2-contact-attribute-keys-update-missing-description", "v2-contact-attribute-keys-update-read-key", "v2-contact-attribute-keys-update-unique",
	"@temporary-attribute-key-v2", "v2-contact-attribute-keys-delete-write-key", "v2-contact-attribute-keys-delete", "v2-contact-attribute-keys-delete-unknown",
	"v2-contact-attribute-keys-delete-default",

	"v2-contacts-create", "v2-contacts-create-duplicate-email", "v2-contacts-create-invalid-email", "v2-contacts-create-missing-email", "v2-contacts-create-read-key",
	"v2-contacts-create-unknown-attribute",

	"v2-responses-list", "v2-responses-list-by-survey", "v2-responses-list-by-contact", "v2-responses-list-date-filter", "v2-responses-list-invalid-query",
	"v2-responses-list-other-workspace-key",
	"v2-responses-get-client",
	"v2-responses-create", "v2-responses-create-userid", "v2-responses-create-with-contact", "v2-responses-create-invalid-body", "v2-responses-create-malformed-json",
	"v2-responses-create-other-workspace-key", "v2-responses-create-read-key", "v2-responses-create-unknown-survey", "v2-responses-create-validation-failed",
	"v2-responses-get", "v2-responses-get-invalid-id", "v2-responses-get-other-workspace-key", "v2-responses-get-unknown",
	"v2-responses-update-partial-body", "v2-responses-update-read-key", "v2-responses-update-unknown", "v2-responses-update-validation-failed", "v2-responses-update",
	"@temporary-response-v2", "v2-responses-delete-write-key", "v2-responses-delete", "v2-responses-delete-unknown",

	"v2-contact-links-contact", "v2-contact-links-contact-expiring", "v2-contact-links-contact-invalid-expiration", "v2-contact-links-contact-other-workspace-key",
	"v2-contact-links-contact-unknown-contact", "v2-contact-links-contact-unknown-survey", "v2-contact-links-contact-app-survey",
	"v2-contact-links-contact-response-created-with-contact-id", "@jane-responds", "v2-contact-links-contact-already-responded",
	"v2-contact-links-segment", "v2-contact-links-segment-paginated", "v2-contact-links-segment-too-many-keys", "v2-contact-links-segment-unknown",
	"v2-contact-links-segment-app-survey",

	"v2-contacts-bulk", "v2-contacts-bulk-new-attribute-key", "v2-contacts-bulk-update-existing", "v2-contacts-bulk-conflicting-userid", "v2-contacts-bulk-invalid",
	"v2-contacts-bulk-read-key",

	"v2-webhooks-list-empty", "v2-webhooks-create", "v2-webhooks-create-legacy-environment-id", "v2-webhooks-create-activepieces-source", "@forget-activepieces-webhook",
	"v2-webhooks-create-invalid-body", "v2-webhooks-create-private-url", "v2-webhooks-create-read-key", "v2-webhooks-create-survey-other-workspace",
	"v2-webhooks-list", "v2-webhooks-list-other-workspace-key", "v2-webhooks-list-by-survey",
	"v2-webhooks-get", "v2-webhooks-get-other-workspace-key", "v2-webhooks-get-unknown",
	"v2-webhooks-update", "v2-webhooks-update-invalid-body", "v2-webhooks-update-read-key", "v2-webhooks-update-survey-other-workspace", "v2-webhooks-update-unknown",
	"v2-webhooks-delete-write-key", "v2-webhooks-delete", "v2-webhooks-delete-unknown",

	"v2-teams-list-empty", "v2-teams-create", "v2-teams-create-duplicate", "v2-teams-create-invalid-body", "v2-teams-create-other-organization",
	"v2-teams-create-read-only-organization-access", "v2-teams-create-without-organization-access", "v2-teams-list-invalid-organization-id",
	"@temporary-team", "v2-teams-list", "v2-teams-list-paginated", "v2-teams-list-read-only-organization-access",
	"v2-teams-get", "v2-teams-get-unknown",
	"v2-teams-update", "v2-teams-update-invalid-body", "v2-teams-update-read-only-organization-access",
	"v2-teams-delete", "v2-teams-delete-unknown",

	"v2-users-list", "v2-users-list-without-organization-access", "v2-users-create", "v2-users-create-inactive-manager", "v2-users-create-billing-role",
	"v2-users-create-duplicate-email", "v2-users-create-invalid-body", "v2-users-create-read-only-organization-access",
	"v2-users-list-filtered", "v2-users-list-paginated",
	"v2-users-update", "v2-users-update-missing-email", "v2-users-update-other-organization", "v2-users-update-unknown-email",

	"v2-workspace-teams-list-empty", "v2-workspace-teams-create", "v2-workspace-teams-create-legacy-project-id", "v2-workspace-teams-create-duplicate",
	"v2-workspace-teams-create-invalid-permission", "v2-workspace-teams-create-missing-workspace", "v2-workspace-teams-create-read-only-organization-access",
	"v2-workspace-teams-create-unknown-team",
	"v2-workspace-teams-list", "v2-workspace-teams-list-by-workspace", "v2-workspace-teams-list-by-legacy-project-id",
	"v2-workspace-teams-update", "v2-workspace-teams-update-unknown",
	"v2-workspace-teams-delete-missing-workspace", "v2-workspace-teams-delete", "v2-workspace-teams-delete-unknown",

	// The MCP endpoint and the OAuth authorization server MCP clients discover.
	"mcp-get",
	"well-known-oauth-protected-resource", "well-known-oauth-protected-resource-mcp", "well-known-oauth-protected-resource-unknown",
	"well-known-oauth-authorization-server", "well-known-oauth-authorization-server-issuer-path", "well-known-openid-configuration",
	"well-known-openid-configuration-issuer-path", "auth-openid-configuration", "auth-jwks",
	"oauth-register-public-client", "oauth-register-invalid-redirect", "oauth-register-missing-redirect",

	// Each after its window has been filled to the limit.
	"v1-rate-limited", "v2-rate-limited", "v1-storage-rate-limited",
}

// deviation is a recorded answer the rewrite gives differently on purpose: adjust rewrites the expected exchange.
type deviation struct {
	reason string
	adjust func(f *recorded)
}

func replaceBody(f *recorded, old, replacement string) {
	f.Response.Body = json.RawMessage(strings.ReplaceAll(string(f.Response.Body), old, replacement))
}

// deviations are the recorded answers the rewrite gives differently on purpose, each with the reason.
var deviations = map[string]deviation{
	"v1-action-classes-create-code": {
		reason: "Recording artifact: the recorder replaced this action class's creation time with the literal it later sent back in a survey's triggers, so the create answer shows it; the replay seeds that literal after the create (step @checkout-recorded-at), and the create itself answers with the time it ran.",
		adjust: func(f *recorded) { replaceBody(f, `"2026-10-22T23:28:39.933Z"`, `"<timestamp>"`) },
	},
	"v1-surveys-single-use-ids": {
		reason: "Encrypted single-use ids are sealed by the rewrite's own cipher (base64url, bound to the survey; P3), not forma2's iv:ciphertext:tag; the survey pages open both.",
		adjust: func(f *recorded) { replaceBody(f, "<single-use-token>", "<sealed-single-use-id>") },
	},
	"v1-surveys-single-use-ids-default-limit": {
		reason: "Encrypted single-use ids are sealed by the rewrite's own cipher (base64url, bound to the survey; P3), not forma2's iv:ciphertext:tag; the survey pages open both.",
		adjust: func(f *recorded) { replaceBody(f, "<single-use-token>", "<sealed-single-use-id>") },
	},
	"v2-openapi-not-served": {
		reason: "No OpenAPI document is served, as in forma2; an unknown /api path answers the rewrite's JSON 404 instead of Next.js's HTML not-found page.",
		adjust: func(f *recorded) {
			f.Response.Headers["content-type"] = "application/json; charset=utf-8"
			delete(f.Response.Headers, "cache-control")
			delete(f.Response.Headers, "vary")
			f.Response.BodyFormat = "json"
			f.Response.Body = json.RawMessage(`{"code":"not_found","message":"Route not found"}`)
		},
	},
	"v2-webhooks-create-activepieces-source": {
		reason: "The recorded image predates 3db326f, which derives the webhook source enum from the Prisma enum and so accepts activepieces (README): the rewrite answers as 3db326f does.",
		adjust: func(f *recorded) {
			f.Response.Status = 201
			f.Response.Body = json.RawMessage(`{"data":{"id":"<id:webhook:activepieces>","name":"AP","createdAt":"<timestamp>","updatedAt":"<timestamp>","url":"https://example.com/ap","source":"activepieces","workspaceId":"<id:workspace>","triggers":["responseFinished"],"surveyIds":[],"secret":"<webhook-secret>"}}`)
		},
	},
	"v2-contact-attribute-keys-list": {
		reason: "forma2 created a workspace's default attribute keys in one statement, with one creation time, and left the order of equal times to PostgreSQL; the rewrite breaks such ties by id, so the default keys list in the order they were created (newest first here).",
		adjust: func(f *recorded) {
			reorderData(f, "seats", "plan", "language", "lastName", "firstName", "email", "userId")
		},
	},
	"v2-contact-attribute-keys-list-paginated": {
		reason: "As v2-contact-attribute-keys-list: ties of the default keys' equal creation times are broken by id (oldest first here).",
		adjust: func(f *recorded) {
			f.Response.Body = json.RawMessage(`{"data":[
				{"id":"<id:contactAttributeKey:email>","createdAt":"<timestamp>","updatedAt":"<timestamp>","isUnique":true,"key":"email","name":"Email","description":"The email of a contact","type":"default","dataType":"string","workspaceId":"<id:workspace>"},
				{"id":"<id:contactAttributeKey:firstName>","createdAt":"<timestamp>","updatedAt":"<timestamp>","isUnique":false,"key":"firstName","name":"First Name","description":"Your contact's first name","type":"default","dataType":"string","workspaceId":"<id:workspace>"}
			],"meta":{"total":7,"limit":2,"offset":1}}`)
		},
	},
	"v2-contact-attribute-keys-list-other-workspace-key": {
		reason: "As v2-contact-attribute-keys-list: ties of the default keys' equal creation times are broken by id (newest first here).",
		adjust: func(f *recorded) { reorderData(f, "language", "lastName", "firstName", "email", "userId") },
	},
	"v2-webhooks-create-invalid-body": {
		reason: "At 3db326f the source enum lists activepieces too (README), so its message does.",
		adjust: func(f *recorded) {
			replaceBody(f, `\"n8n\""`, `\"n8n\"|\"activepieces\""`)
		},
	},
	"v2-webhooks-update-invalid-body": {
		reason: "At 3db326f the source enum lists activepieces too (README), so its message does.",
		adjust: func(f *recorded) {
			replaceBody(f, `\"n8n\""`, `\"n8n\"|\"activepieces\""`)
		},
	},
	"v2-contacts-bulk": {
		reason: "forma2 defect (notes.knownDefect): every bulk upload that writes a contact failed with 500. The rewrite answers what forma2 meant to.",
		adjust: bulkSucceeded,
	},
	"v2-contacts-bulk-new-attribute-key": {
		reason: "forma2 defect (notes.knownDefect), as v2-contacts-bulk; the new key is created.",
		adjust: bulkSucceeded,
	},
	"v2-contacts-bulk-update-existing": {
		reason: "forma2 defect (notes.knownDefect), as v2-contacts-bulk.",
		adjust: bulkSucceeded,
	},
	"v2-teams-create-duplicate": {
		reason: "forma2 defect (notes.knownDefect): a duplicate team name surfaced as a 500. The rewrite answers 409.",
		adjust: func(f *recorded) {
			f.Response.Status = 409
			f.Response.Body = json.RawMessage(`{"error":{"code":409,"message":"Conflict","details":[{"field":"team","issue":"Team name already exists"}]}}`)
		},
	},
	"v2-workspace-teams-delete-unknown": {
		reason: "forma2 defect (notes.knownDefect): deleting a workspace team that does not exist surfaced as a 500. The rewrite answers 404.",
		adjust: func(f *recorded) {
			f.Response.Status = 404
			f.Response.Body = json.RawMessage(`{"error":{"code":404,"message":"Not Found","details":[{"field":"workspaceTeam","issue":"not found"}]}}`)
		},
	},
}

func init() {
	for _, name := range []string{"mcp-get", "well-known-oauth-protected-resource", "well-known-oauth-protected-resource-mcp", "well-known-oauth-protected-resource-unknown", "oauth-register-public-client", "oauth-register-invalid-redirect", "oauth-register-missing-redirect"} {
		deviations[name] = deviation{reason: goJSONHeaders, adjust: jsonHeaders}
	}
	for _, name := range []string{"well-known-oauth-authorization-server", "well-known-oauth-authorization-server-issuer-path", "well-known-openid-configuration", "well-known-openid-configuration-issuer-path", "auth-openid-configuration"} {
		deviations[name] = deviation{
			reason: goJSONHeaders + " The discovery document leaves out what this server does not implement: the client_credentials grant (every token is a user's), private_key_jwt client authentication and the JWT signing algorithms only it uses, DPoP, back-channel logout, the claims request parameter, the iss authorization response parameter, and the sid, picture, given_name and family_name claims it never issues.",
			adjust: func(f *recorded) {
				jsonHeaders(f)
				editBody(f, func(body map[string]any) {
					for _, member := range []string{
						"authorization_response_iss_parameter_supported", "dpop_signing_alg_values_supported", "backchannel_logout_supported", "backchannel_logout_session_supported",
						"claims_parameter_supported", "token_endpoint_auth_signing_alg_values_supported", "introspection_endpoint_auth_signing_alg_values_supported",
						"revocation_endpoint_auth_signing_alg_values_supported",
					} {
						delete(body, member)
					}
					without(body, "grant_types_supported", "client_credentials")
					for _, member := range []string{"token_endpoint_auth_methods_supported", "introspection_endpoint_auth_methods_supported", "revocation_endpoint_auth_methods_supported"} {
						without(body, member, "private_key_jwt")
					}
					without(body, "claims_supported", "sid", "picture", "given_name", "family_name")
				})
			},
		}
	}
	deviations["auth-jwks"] = deviation{
		reason: "The Vary header Next.js added for its React Server Components is not sent, and each key also names its use, sig (RFC 7517 §4.2), which zitadel/oidc always publishes.",
		adjust: func(f *recorded) {
			delete(f.Response.Headers, "vary")
			editBody(f, func(body map[string]any) {
				for _, k := range body["keys"].([]any) {
					k.(map[string]any)["use"] = "sig"
				}
			})
		},
	}
}

// goJSONHeaders is the deviation every OAuth and MCP exchange shares.
const goJSONHeaders = "JSON is labelled application/json; charset=utf-8, and the Vary header Next.js added for its React Server Components is not sent: neither changes what an OAuth or MCP client reads."

func jsonHeaders(f *recorded) {
	delete(f.Response.Headers, "vary")
	if f.Response.Headers["content-type"] == "application/json" {
		f.Response.Headers["content-type"] = "application/json; charset=utf-8"
	}
}

// editBody rewrites the expected JSON object.
func editBody(f *recorded, edit func(map[string]any)) {
	var body map[string]any
	if err := json.Unmarshal(f.Response.Body, &body); err != nil {
		panic(err)
	}
	edit(body)
	out, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	f.Response.Body = out
}

// without removes values from the list member of body.
func without(body map[string]any, member string, values ...string) {
	var kept []any
	for _, v := range body[member].([]any) {
		if !slices.Contains(values, v.(string)) {
			kept = append(kept, v)
		}
	}
	body[member] = kept
}

// reorderData puts the expected list's entries in the order of their keys.
func reorderData(f *recorded, keys ...string) {
	var body struct {
		Data []map[string]any `json:"data"`
		Meta any              `json:"meta"`
	}
	if err := json.Unmarshal(f.Response.Body, &body); err != nil {
		panic(err)
	}
	slices.SortStableFunc(body.Data, func(a, b map[string]any) int {
		return slices.Index(keys, a["key"].(string)) - slices.Index(keys, b["key"].(string))
	})
	out, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	f.Response.Body = out
}

func bulkSucceeded(f *recorded) {
	f.Response.Status = 200
	f.Response.Headers["access-control-allow-origin"] = "*"
	f.Response.Headers["access-control-allow-methods"] = "GET, POST, PUT, DELETE, OPTIONS"
	f.Response.Headers["access-control-allow-headers"] = "Content-Type, Authorization"
	f.Response.Body = json.RawMessage(`{"data":{"status":"success","message":"Contacts bulk upload successful"}}`)
}

func loadRecorded(t *testing.T, name string) recorded {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "forma2", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f recorded
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return f
}

var placeholder = regexp.MustCompile(`<[a-z0-9-]+(?::[A-Za-z0-9:_-]+)?>`)

// kinds are the patterns of the placeholders that stand for values differing on every run; an id placeholder not bound yet matches an id of its kind and binds to it.
var kinds = map[string]string{
	"<timestamp>":            `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z`,
	"<uuid>":                 `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`,
	"<s3:policy>":            `[A-Za-z0-9+/=]+`,
	"<s3:signature>":         `[0-9a-f]{64}`,
	"<s3:amz-date>":          `\d{8}T\d{6}Z`,
	"<yyyymmdd>":             `\d{8}`,
	"<single-use-id>":        `[a-z][a-z0-9]{23}`,
	"<single-use-signature>": `[0-9a-f]{64}`,
	"<sealed-single-use-id>": `[A-Za-z0-9_-]{40,}`,
	"<contact-link-token>":   `[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`,
	"<webhook-secret>":       `whsec_[A-Za-z0-9+/]+=*`,
	"<oauth:client-id>":      `[A-Za-z0-9_-]{20,}`,
	"<jwk:x>":                `[A-Za-z0-9_-]{43}`,
	"<jwk:kid>":              `[A-Za-z0-9_-]+`,
}

// unixTime stands for a JSON number, a time in seconds.
const unixTime = "<unix-time>"

// idKinds are the patterns an id placeholder not bound yet matches: the rewrite's UUIDs, and the block ids the server generates in a survey document.
var idKinds = map[string]string{
	"uuid":  `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`,
	"block": `[a-z][a-z0-9]{23}`,
}

// replay is the state of one replay: the bound placeholders.
type replay struct {
	t     *testing.T
	bound map[string]string
}

// substitute fills a request's placeholders, which must all be bound by then. Text that only looks like a placeholder (a name such as "<script>") is sent as it is.
func (r *replay) substitute(s string) string {
	return placeholder.ReplaceAllStringFunc(s, func(p string) string {
		v, ok := r.bound[p]
		if !ok && (strings.HasPrefix(p, "<id:") || strings.HasPrefix(p, "<api-key:")) {
			r.t.Fatalf("request placeholder %s is not bound yet", p)
		}
		if !ok {
			return p
		}
		return v
	})
}

// pattern is the regular expression an unbound placeholder matches, and whether it binds: an id is bound to the first value it matches, any other value is matched by kind every time.
func pattern(p string) (kind string, binds bool) {
	switch {
	case kinds[p] != "":
		return kinds[p], false
	case strings.HasPrefix(p, "<id:block:"):
		return idKinds["block"], true
	case strings.HasPrefix(p, "<id:"):
		return idKinds["uuid"], true
	}
	return "", false
}

// matchString matches a recorded string against an actual one, binding id placeholders it meets for the first time.
func (r *replay) matchString(want, got string) bool {
	locs := placeholder.FindAllStringIndex(want, -1)
	if len(locs) == 0 {
		return want == got
	}
	var b strings.Builder
	b.WriteString("^")
	var unbound []string
	last := 0
	for _, l := range locs {
		b.WriteString(regexp.QuoteMeta(want[last:l[0]]))
		p := want[l[0]:l[1]]
		switch kind, binds := pattern(p); {
		case r.bound[p] != "":
			b.WriteString(regexp.QuoteMeta(r.bound[p]))
		case kind != "" && !binds:
			b.WriteString("(?:" + kind + ")")
		case kind != "":
			b.WriteString("(" + kind + ")")
			unbound = append(unbound, p)
		default:
			r.t.Fatalf("unknown placeholder %s", p)
		}
		last = l[1]
	}
	b.WriteString(regexp.QuoteMeta(want[last:]) + "$")
	m := regexp.MustCompile(b.String()).FindStringSubmatch(got)
	if m == nil {
		return false
	}
	for i, p := range unbound {
		if prev, ok := r.bound[p]; ok && prev != m[i+1] {
			return false
		}
		r.bound[p] = m[i+1]
	}
	return true
}

// match compares a recorded JSON value with an actual one, reporting the first difference.
func (r *replay) match(path string, want, got any) error {
	switch w := want.(type) {
	case string:
		if w == unixTime {
			if n, ok := got.(float64); !ok || n < 1e9 || n != math.Trunc(n) {
				return fmt.Errorf("%s: got %#v, want a unix time", path, got)
			}
			return nil
		}
		g, ok := got.(string)
		if !ok || !r.matchString(w, g) {
			return fmt.Errorf("%s: got %#v, want %q", path, got, w)
		}
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: got %#v, want an object", path, got)
		}
		keys := func(m map[string]any) []string {
			var out []string
			for k := range m {
				out = append(out, k)
			}
			slices.Sort(out)
			return out
		}
		if !slices.Equal(keys(w), keys(g)) {
			return fmt.Errorf("%s: keys %v, want %v", path, keys(g), keys(w))
		}
		for _, k := range keys(w) {
			if err := r.match(path+"."+k, w[k], g[k]); err != nil {
				return err
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Errorf("%s: got %#v, want %d elements", path, got, len(w))
		}
		for i := range w {
			if err := r.match(fmt.Sprintf("%s[%d]", path, i), w[i], g[i]); err != nil {
				return err
			}
		}
	default:
		if !reflect.DeepEqual(want, got) {
			return fmt.Errorf("%s: got %#v, want %#v", path, got, want)
		}
	}
	return nil
}

// send replays one recorded request.
func (r *replay) send(e *env, f recorded) result {
	path := r.substitute(f.Request.Path)
	if len(f.Request.Query) > 0 {
		q := url.Values{}
		for k, v := range f.Request.Query {
			q.Set(k, r.substitute(v))
		}
		path += "?" + q.Encode()
	}
	var body any
	if raw := strings.TrimSpace(string(f.Request.Body)); raw != "" && raw != "null" {
		var s string
		if json.Unmarshal(f.Request.Body, &s) == nil {
			body = r.substitute(s)
		} else {
			body = r.substitute(raw)
		}
	}
	var headers []string
	for k, v := range f.Request.Headers {
		if k != "content-type" {
			headers = append(headers, k, r.substitute(v))
		}
	}
	return e.do(f.Request.Method, path, body, headers...)
}

func (r *replay) check(f recorded, res result) {
	t := r.t
	if res.Status != f.Response.Status {
		t.Fatalf("status %d, want %d: %s", res.Status, f.Response.Status, res.Body)
	}
	for name, want := range f.Response.Headers {
		if got := res.Header.Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"Access-Control-Allow-Origin", "X-Frame-Options", "Allow", "Cache-Control"} {
		if _, recorded := f.Response.Headers[strings.ToLower(name)]; !recorded && res.Header.Get(name) != "" {
			t.Errorf("header %s %q was not sent by forma2", name, res.Header.Get(name))
		}
	}
	switch f.Response.BodyFormat {
	case "json":
		var want, got any
		if err := json.Unmarshal(f.Response.Body, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(res.Body, &got); err != nil {
			t.Fatalf("body %s is not JSON: %v", res.Body, err)
		}
		if err := r.match("body", want, got); err != nil {
			t.Fatalf("%v\nbody: %s", err, res.Body)
		}
	case "empty":
		if len(res.Body) != 0 {
			t.Fatalf("body %q, want none", res.Body)
		}
	}
	if note := f.Notes.ContactLinkToken; note != nil {
		r.checkContactLinkToken(res, note.Header, note.Claims, note.LifetimeSeconds)
	}
}

// checkContactLinkToken decodes every personal link the answer carries and compares its header, claim names and lifetime with the recorded token's.
func (r *replay) checkContactLinkToken(res result, header map[string]string, claims []string, lifetime *float64) {
	tokens := regexp.MustCompile(`/c/([A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)`).FindAllStringSubmatch(string(res.Body), -1)
	if len(tokens) == 0 {
		r.t.Fatalf("no contact link token in %s", res.Body)
	}
	for _, m := range tokens {
		parts := strings.Split(m[1], ".")
		var gotHeader map[string]string
		var gotClaims map[string]any
		for i, dst := range []any{&gotHeader, &gotClaims} {
			raw, err := base64.RawURLEncoding.DecodeString(parts[i])
			if err != nil || json.Unmarshal(raw, dst) != nil {
				r.t.Fatalf("token part %d of %s does not decode", i, m[1])
			}
		}
		if !reflect.DeepEqual(gotHeader, header) {
			r.t.Errorf("token header %v, want %v", gotHeader, header)
		}
		var names []string
		for k := range gotClaims {
			names = append(names, k)
		}
		slices.Sort(names)
		if !slices.Equal(names, claims) {
			r.t.Errorf("token claims %v, want %v", names, claims)
		}
		if lifetime != nil {
			if exp, iat := gotClaims["exp"].(float64), gotClaims["iat"].(float64); exp-iat != *lifetime {
				r.t.Errorf("token lifetime %v, want %v", exp-iat, *lifetime)
			}
		}
	}
}

// The management APIs answer every request recorded against forma2 as forma2 did, apart from the deviations listed with their reasons.
func TestReplaysRecordedForma2Exchanges(t *testing.T) {
	e := newEnv(t)
	files, err := filepath.Glob(filepath.Join("testdata", "forma2", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for _, f := range files {
		if name := strings.TrimSuffix(filepath.Base(f), ".json"); notReplayed[name] == "" {
			all = append(all, name)
		}
	}
	var ordered []string
	for _, name := range replayOrder {
		if !strings.HasPrefix(name, "@") {
			ordered = append(ordered, name)
		}
	}
	slices.Sort(ordered)
	slices.Sort(all)
	if !slices.Equal(ordered, all) {
		missing, extra := difference(all, ordered), difference(ordered, all)
		t.Fatalf("the replay order lacks %v and lists %v that the recording does not have", missing, extra)
	}

	s := e.seedRecording()
	r := &replay{t: t, bound: s.bound}
	for _, name := range replayOrder {
		e.clock.nextWindow()
		if strings.HasPrefix(name, "@") {
			s.step(name, r)
			e.deliver()
			continue
		}
		f := loadRecorded(t, name)
		if d, ok := deviations[name]; ok {
			d.adjust(&f)
		}
		s.prepare(name)
		res := r.send(e, f)
		e.deliver()
		t.Run(name, func(t *testing.T) {
			sub := &replay{t: t, bound: r.bound}
			sub.check(f, res)
		})
	}
	for name, d := range deviations {
		t.Logf("%s deviates from forma2 on purpose: %s", name, d.reason)
	}
}

func difference(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

// seed is the recorder's data and the scenario steps that need it.
type seed struct {
	e     *env
	bound map[string]string
	keys  map[string]uuid.UUID
	ws    uuid.UUID
	ws2   uuid.UUID
}

// seedRecording creates what the recorder set up before the first request (README): the organization with its admin, the two workspaces (the first with a legacy environment id) and the API keys.
func (e *env) seedRecording() *seed {
	org := e.insert(`INSERT INTO organizations (name) VALUES ('Contract Organization') RETURNING id`)
	e.exec(`INSERT INTO organization_billing (organization_id, limits) VALUES (?, '{}'::jsonb)`, org)
	admin := e.user("Contract Admin", "admin@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, admin, org)
	ws := e.workspace(org, "My workspace")
	e.exec(`UPDATE workspaces SET legacy_environment_id = 'clegacyenvaaaaaaaaaaaaaa1' WHERE id = ?`, ws)
	ws2 := e.workspace(org, "Other workspace")
	s := &seed{e: e, ws: ws, ws2: ws2, keys: map[string]uuid.UUID{}, bound: map[string]string{
		"<id:organization>": org.String(), "<id:workspace>": ws.String(), "<id:workspace2>": ws2.String(), "<id:user:admin>": admin.String(),
		"<webapp-url>": webAppURL, "<s3-endpoint>": "http://localhost:9000",
	}}
	for _, k := range []struct {
		name        string
		read, write bool
		grants      []any
	}{
		{"manage", true, true, []any{ws, "manage"}},
		{"write", false, false, []any{ws, "write"}},
		{"read", true, false, []any{ws, "read"}},
		{"other", false, false, []any{ws2, "manage"}},
		{"orgonly", true, true, nil},
		{"ratelimit", false, false, []any{ws, "read"}},
	} {
		key, id := e.apiKey(org, admin, k.name, k.read, k.write, k.grants...)
		s.bound["<api-key:"+k.name+">"] = key
		s.keys[k.name] = id
	}
	e.deliver()
	return s
}

// fill sets the current window of rule to its limit for the key.
func (s *seed) fill(namespace string, keyID uuid.UUID, limit int) {
	window := s.e.clock.Now().Unix() / 60 * 60
	key := fmt.Sprintf("fb:rate_limit:%s:%s:%d", namespace, keyID, window)
	if err := s.e.cache.Set(s.e.t.Context(), key, limit, time.Minute).Err(); err != nil {
		s.e.t.Fatal(err)
	}
}

// prepare puts a recorded case's scenario in place just before it is sent.
func (s *seed) prepare(name string) {
	switch name {
	case "v1-rate-limited":
		s.fill("api:v1", s.keys["ratelimit"], 100)
	case "v2-rate-limited":
		s.fill("api:v2", s.keys["ratelimit"], 100)
	case "v1-storage-rate-limited":
		s.fill("storage:upload", s.keys["manage"], 5)
	}
}

// post sends one of the recorder's own requests with the manage key and returns its data, failing the test unless it succeeded.
func (s *seed) post(r *replay, path string, body any) map[string]any {
	res := s.e.do("POST", path, body, "x-api-key", r.bound["<api-key:manage>"])
	if res.Status != 200 && res.Status != 201 {
		s.e.t.Fatalf("POST %s: %d %s", path, res.Status, res.Body)
	}
	data, _ := res.json(s.e.t)["data"].(map[string]any)
	return data
}

// contact creates a contact of the first workspace with the given values, as identifying it through the client API would.
func (s *seed) contact(values ...string) uuid.UUID {
	e := s.e
	id := e.insert(`INSERT INTO contacts (workspace_id) VALUES (?) RETURNING id`, s.ws)
	for i := 0; i+1 < len(values); i += 2 {
		dataType := "string"
		e.db.Raw(`SELECT data_type FROM contact_attribute_keys WHERE workspace_id = ? AND key = ?`, s.ws, values[i]).Row().Scan(&dataType) //nolint:errcheck // the key exists
		if dataType == "number" {
			e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_number) SELECT ?, id, ?::float8 FROM contact_attribute_keys WHERE workspace_id = ? AND key = ?`, id, values[i+1], s.ws, values[i])
			continue
		}
		e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) SELECT ?, id, ? FROM contact_attribute_keys WHERE workspace_id = ? AND key = ?`, id, values[i+1], s.ws, values[i])
	}
	return id
}

// step runs one of the recorder's unrecorded setup steps.
func (s *seed) step(name string, r *replay) {
	e := s.e
	switch name {
	case "@checkout-recorded-at":
		// The recorder echoed this creation time in later requests, so the replay gives the action class the time it was recorded with.
		e.exec(`UPDATE action_classes SET created_at = '2026-10-22T23:28:39.933Z', updated_at = '2026-10-22T23:28:39.933Z' WHERE id = ?`, r.bound["<id:actionClass:checkout>"])
	case "@temporary-action-class":
		data := s.post(r, "/api/v1/management/action-classes", map[string]any{"workspaceId": s.ws.String(), "name": "Temporary", "type": "code", "key": "temporary"})
		r.bound["<id:actionClass:temporary>"] = data["id"].(string)
	case "@temporary-attribute-key":
		data := s.post(r, "/api/v1/management/contact-attribute-keys", map[string]any{"workspaceId": s.ws.String(), "key": "temporary", "name": "Temporary", "type": "custom"})
		r.bound["<id:contactAttributeKey:temporary>"] = data["id"].(string)
	case "@identified-contacts":
		r.bound["<id:contact:user1>"] = s.contact("userId", "contract-user-1", "email", "user1@example.com", "plan", "pro", "seats", "12").String()
		r.bound["<id:contact:user2>"] = s.contact("userId", "contract-user-2", "email", "user2@example.com", "plan", "free").String()
		r.bound["<id:contact:user3>"] = s.contact("userId", "contract-user-3", "email", "user3@example.com").String()
		e.exec(`INSERT INTO segments (workspace_id, title, is_private, filters) VALUES (?, 'Pro customers', false, ?::jsonb)`, s.ws,
			`[{"id":"clfilterplanaaaaaaaaaaaa1","connector":null,"resource":{"id":"clfilterplanrootaaaaaaaa1","root":{"type":"attribute","contactAttributeKey":"plan"},"qualifier":{"operator":"equals"},"value":"pro"}}]`)
		var segment uuid.UUID
		if err := e.db.Raw(`SELECT id FROM segments WHERE title = 'Pro customers'`).Row().Scan(&segment); err != nil {
			e.t.Fatal(err)
		}
		r.bound["<id:segment:pro>"] = segment.String()
	case "@temporary-survey":
		data := s.post(r, "/api/v1/management/surveys", map[string]any{
			"workspaceId": s.ws.String(), "name": "Temporary Survey", "type": "link", "status": "draft",
			"questions": []any{map[string]any{"id": "q1", "type": "openText", "headline": map[string]any{"default": "Hello from the other workspace"}, "required": false, "inputType": "text", "charLimit": map[string]any{"enabled": false}}},
		})
		r.bound["<id:survey:temporary>"] = data["id"].(string)
	case "@client-responses":
		link, app := r.bound["<id:survey:link>"], r.bound["<id:survey:app>"]
		display := e.insert(`INSERT INTO displays (survey_id) VALUES (?) RETURNING id`, link)
		r.bound["<id:display:client>"] = display.String()
		client := e.insert(`INSERT INTO responses (survey_id, display_id, finished, ending_id, data, variables, ttc, meta, language) VALUES (?, ?, true, 'clendscreenokaaaaaaaaaaa1', ?::jsonb, '{}', ?::jsonb, ?::jsonb, 'default') RETURNING id`,
			link, display, `{"score":5,"feedback":"Faster load times","utm_source":"newsletter"}`, `{"score":1200,"_total":8400,"feedback":3000}`,
			`{"url":"`+webAppURL+`/s/`+link+`?utm_source=newsletter","source":"link","userAgent":{"os":"macOS","device":"desktop","browser":"Chrome"}}`)
		r.bound["<id:response:client>"] = client.String()
		contact := e.insert(`INSERT INTO responses (survey_id, contact_id, finished, data, variables, ttc, meta, contact_attributes, created_at, updated_at) VALUES (?, ?, false, ?::jsonb, '{}', ?::jsonb, ?::jsonb, ?::jsonb, now() + interval '1 millisecond', now() + interval '1 millisecond') RETURNING id`,
			app, r.bound["<id:contact:user1>"], `{"checkout_feedback":"ok"}`, `{"checkout_feedback":900}`, `{"url":"https://customer.example/checkout","userAgent":{"device":"desktop"}}`,
			`{"plan":"pro","email":"user1@example.com","seats":"12","userId":"contract-user-1"}`)
		r.bound["<id:response:client-contact>"] = contact.String()
	case "@temporary-response":
		data := s.post(r, "/api/v1/management/responses", map[string]any{"workspaceId": s.ws.String(), "surveyId": r.bound["<id:survey:link>"], "finished": false, "data": map[string]any{"feedback": "to delete"}})
		r.bound["<id:response:temporary>"] = data["id"].(string)
	case "@temporary-attribute-key-v2":
		data := s.post(r, "/api/v2/management/contact-attribute-keys", map[string]any{"workspaceId": s.ws.String(), "key": "temporary_v2", "name": "Temporary", "description": nil})
		r.bound["<id:contactAttributeKey:temporary_v2>"] = data["id"].(string)
	case "@temporary-response-v2":
		data := s.post(r, "/api/v2/management/responses", map[string]any{"surveyId": r.bound["<id:survey:link>"], "finished": false, "data": map[string]any{"feedback": "to delete"}})
		r.bound["<id:response:v2-temporary>"] = data["id"].(string)
	case "@jane-responds":
		s.post(r, "/api/v2/management/responses", map[string]any{"surveyId": r.bound["<id:survey:link>"], "userId": "contract-user-4", "finished": true, "data": map[string]any{"feedback": "From Jane"}})
	case "@forget-activepieces-webhook":
		// The recorded image refused this webhook (see the deviation of v2-webhooks-create-activepieces-source); the lists recorded after it do not show it.
		e.exec(`DELETE FROM webhooks WHERE id = ?`, r.bound["<id:webhook:activepieces>"])
	case "@temporary-team":
		data := s.post(r, "/api/v2/organizations/"+r.bound["<id:organization>"]+"/teams", map[string]any{"name": "Temporary Team"})
		r.bound["<id:team:temporary>"] = data["id"].(string)
	default:
		e.t.Fatalf("unknown step %s", name)
	}
}
