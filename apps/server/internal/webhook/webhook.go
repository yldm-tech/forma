// Package webhook is forma2's webhooks: the subscriptions a workspace keeps (apps/web/modules/integrations/webhooks), the API Zapier, Make, n8n and Activepieces subscribe through (/api/v1/webhooks), and the delivery of response events to them, signed as Standard Webhooks. Every request to a webhook's URL goes through platform/ssrf.
package webhook

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
)

// Sources of a webhook, forma2's WebhookSource: who created it. Delivery ignores it.
const (
	SourceUser         = "user"
	SourceZapier       = "zapier"
	SourceMake         = "make"
	SourceN8N          = "n8n"
	SourceActivepieces = "activepieces"
)

// Sources lists every source in forma2's order.
var Sources = []string{SourceUser, SourceZapier, SourceMake, SourceN8N, SourceActivepieces}

// Triggers are the response pipeline events a webhook may subscribe to, forma2's PipelineTriggers in its zod order.
var Triggers = []string{"responseFinished", "responseCreated", "responseUpdated"}

// Webhook is a stored webhook. AllSurveys (the all_surveys column) scopes it to every survey of the workspace, current and future, and SurveyIDs is then empty; otherwise it fires only for SurveyIDs, which keeps the ids of surveys deleted since, as forma2's surveyIds did.
type Webhook struct {
	ID          uuid.UUID
	Name        *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	URL         string
	Source      string
	WorkspaceID uuid.UUID
	Triggers    []string
	SurveyIDs   []uuid.UUID
	AllSurveys  bool
	Secret      *string
}

// View is a webhook as forma2's APIs serialized the Prisma row, in its field order, with JavaScript's toISOString timestamps. Secret is present only where forma2 returned it: the response to a create. Health is present only in the settings page's list. EnvironmentID is the v1 API's legacy alias of the workspace. AllSurveys is the settings page's explicit scope, which the v1 API never had: there an empty surveyIds is every survey.
type View struct {
	ID            uuid.UUID   `json:"id"`
	Name          *string     `json:"name"`
	CreatedAt     string      `json:"createdAt"`
	UpdatedAt     string      `json:"updatedAt"`
	URL           string      `json:"url"`
	Source        string      `json:"source"`
	WorkspaceID   uuid.UUID   `json:"workspaceId"`
	Triggers      []string    `json:"triggers"`
	SurveyIDs     []uuid.UUID `json:"surveyIds"`
	AllSurveys    *bool       `json:"allSurveys,omitempty"`
	Secret        *string     `json:"secret,omitempty"`
	EnvironmentID *string     `json:"environmentId,omitempty"`
	// Health is the settings page's delivery health, in its list only, and absent for a webhook nothing was sent to yet.
	Health *Health `json:"health,omitempty"`
}

// view is w without its secret.
func (w *Webhook) view() View {
	return View{
		ID: w.ID, Name: w.Name, CreatedAt: jsvalue.ISOString(w.CreatedAt), UpdatedAt: jsvalue.ISOString(w.UpdatedAt),
		URL: w.URL, Source: w.Source, WorkspaceID: w.WorkspaceID, Triggers: w.Triggers, SurveyIDs: w.SurveyIDs,
	}
}

// appView is w as the settings page reads it: without its secret, with its explicit scope.
func (w *Webhook) appView() View {
	v := w.view()
	v.AllSurveys = &w.AllSurveys
	return v
}

// viewWithSecret is w with its secret, the create response.
func (w *Webhook) viewWithSecret() View {
	v := w.view()
	secret := ""
	if w.Secret != nil {
		secret = *w.Secret
	}
	v.Secret = &secret
	return v
}

// auditObject is the webhook as the audit event records it; the secret is masked by the emitter.
func (w *Webhook) auditObject() map[string]any {
	return map[string]any{
		"id": w.ID.String(), "name": w.Name, "createdAt": jsvalue.ISOString(w.CreatedAt), "updatedAt": jsvalue.ISOString(w.UpdatedAt),
		"url": w.URL, "source": w.Source, "workspaceId": w.WorkspaceID.String(), "triggers": w.Triggers, "surveyIds": w.SurveyIDs, "secret": w.Secret,
	}
}

// secretPrefix is the Standard Webhooks secret prefix.
const secretPrefix = "whsec_"

// GenerateSecret is forma2's generateWebhookSecret: whsec_ and the standard base64 of 32 random bytes.
func GenerateSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return secretPrefix + base64.StdEncoding.EncodeToString(b)
}

// validSecret reports whether s is a secret Standard Webhooks receivers can verify with: whsec_ and the standard base64 of at least 24 bytes.
func validSecret(s string) bool {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, secretPrefix))
	return strings.HasPrefix(s, secretPrefix) && err == nil && len(key) >= 24
}

// Sign is forma2's generateStandardWebhookSignature: "v1," and the base64 HMAC-SHA256 of "<id>.<timestamp>.<body>" under the secret's key. The key is the secret's base64 after an optional whsec_ prefix; a secret stored before secrets were checked is decoded as leniently as Node's Buffer.from(s, "base64") did, so its receivers keep verifying.
func Sign(secret, messageID string, timestamp time.Time, body []byte) (string, error) {
	wh, err := standardwebhooks.NewWebhook(secret)
	if err != nil {
		if wh, err = standardwebhooks.NewWebhookRaw(lenientBase64(strings.TrimPrefix(secret, secretPrefix))); err != nil {
			return "", fmt.Errorf("webhook signing key: %w", err)
		}
	}
	return wh.Sign(messageID, timestamp, body)
}

// notBase64 is every character Node's base64 decoder skips.
var notBase64 = regexp.MustCompile(`[^A-Za-z0-9+/\-_]`)

// lenientBase64 is Buffer.from(s, "base64"): both alphabets, characters outside them ignored, decoding stopping at the first padding character, and a trailing partial group dropped.
func lenientBase64(s string) []byte {
	if i := strings.IndexByte(s, '='); i >= 0 {
		s = s[:i]
	}
	s = strings.NewReplacer("-", "+", "_", "/").Replace(notBase64.ReplaceAllString(s, ""))
	if len(s)%4 == 1 {
		s = s[:len(s)-1]
	}
	b, _ := base64.RawStdEncoding.DecodeString(s)
	return b
}

// MessageID is forma2's createWebhookMessageId: the SHA-256 hex of "<job key>:<webhook id>:<event>", the same on every attempt of one pipeline job.
func MessageID(jobKey string, webhookID uuid.UUID, event string) string {
	sum := sha256.Sum256([]byte(jobKey + ":" + webhookID.String() + ":" + event))
	return hex.EncodeToString(sum[:])
}

// discordWebhook is forma2's isDiscordWebhook.
var discordWebhook = regexp.MustCompile(`^https://discord\.com/api/webhooks/\d+/.+$`)

// IsDiscord reports whether url is a Discord webhook, which cannot read forma2's payloads.
func IsDiscord(url string) bool { return discordWebhook.MatchString(url) }
