package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/contact"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mrkdwn"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ssrf"
	"github.com/yldm-tech/forma/apps/server/internal/webhook"
)

// requestTimeout bounds one request of an http_request or chat step, the webhooks' five seconds.
const requestTimeout = 5 * time.Second

// stepRetryWaits are the waits before the in-step retries of a request that failed for a reason worth retrying (no answer, 408, 429 or a 5xx): two quick ones, well inside the run's timeout. The run's own retries come after them.
var stepRetryWaits = []time.Duration{2 * time.Second, 10 * time.Second}

// discordMaxContent is the length Discord accepts for a message's content.
const discordMaxContent = 2000

// maxResponseExcerpt is how much of a refusing receiver's answer a failed step records: enough for an error message, never a whole page.
const maxResponseExcerpt = 512

// responseExcerpt is the start of an answer's body as text: invalid UTF-8, a character cut at the limit among it, dropped, and surrounding space trimmed.
func responseExcerpt(body []byte) string {
	return strings.TrimSpace(strings.ToValidUTF8(string(body), ""))
}

// actions executes the steps after send_email, for the runner and for the builder's test send.
type actions struct {
	db         *gorm.DB
	guard      *ssrf.Guard
	cipher     *secret.Cipher
	retryWaits []time.Duration
	now        func() time.Time
}

func newActions(db *gorm.DB, guard *ssrf.Guard, cipher *secret.Cipher) *actions {
	return &actions{db: db, guard: guard, cipher: cipher, retryWaits: stepRetryWaits, now: time.Now}
}

// actionEnv is what one step acts with.
type actionEnv struct {
	workspaceID uuid.UUID
	// responseID and contactID are the run's response and its contact; a test send has neither.
	responseID uuid.UUID
	contactID  *uuid.UUID
	template   *Template
	// messageID is the Standard Webhooks id of the step's requests, the same on every delivery of the step, so a receiver can drop a repeat.
	messageID string
	// retry retries a transient request failure within the step; a test send answers after one try.
	retry bool
}

// outcome is how a step ended. permanent marks a failure no retry can change (a refused URL, a 4xx, a tag that does not exist), which fails the run at once. reason classifies a failed request or chat step for the builder's test send, which shows it in the member's language.
type outcome struct {
	status    string
	output    map[string]any
	err       string
	permanent bool
	reason    string
}

// The reasons a request or chat step failed, TestSendResult.Reason.
const (
	reasonURLRefused  = "url_refused"
	reasonTimeout     = "timeout"
	reasonUnreachable = "unreachable"
	reasonRedirect    = "redirect"
	reasonStatus      = "status"
	reasonSecret      = "secret_unreadable"
	reasonConfig      = "invalid_config"
)

// because is o classified as reason.
func (o outcome) because(reason string) outcome {
	o.reason = reason
	return o
}

func succeeded(output map[string]any) outcome { return outcome{status: StepSucceeded, output: output} }

func failed(msg string, permanent bool, output map[string]any) outcome {
	return outcome{status: StepFailed, err: msg, permanent: permanent, output: output}
}

// stepMessageID is the message id of a run's step, the hex sha256 of "<run id>:<step id>", like a webhook delivery's.
func stepMessageID(runID uuid.UUID, stepID string) string {
	sum := sha256.Sum256([]byte(runID.String() + ":" + stepID))
	return hex.EncodeToString(sum[:])
}

// stepInput is what a step's run log records it was asked to do, without its secrets or the path and query of a URL, which often carry a token, and the label its author gave the step, which the run detail names it by.
func stepInput(step Step) map[string]any {
	n := step.Node
	var input map[string]any
	switch {
	case step.Type == ActionSendEmail:
		input = map[string]any{"to": step.Config.To, "subject": step.Config.Subject}
	case n.HTTP != nil:
		input = map[string]any{"method": n.HTTP.Method, "url": redactedURL(n.HTTP.URL)}
	case n.Chat != nil:
		input = map[string]any{"channel": n.Chat.WebhookURL.Hint}
	case n.Tag != nil:
		input = map[string]any{"tagId": n.Tag.TagID}
	case n.Attribute != nil:
		input = map[string]any{"attributeKey": n.Attribute.AttributeKey}
	default:
		input = map[string]any{}
	}
	if n.Label != nil && strings.TrimSpace(*n.Label) != "" {
		input["label"] = *n.Label
	}
	return input
}

// redactedURL is a URL as a run log may show it: its scheme and host.
func redactedURL(raw string) string {
	if o := origin(raw); o != "" {
		return o
	}
	return "(invalid URL)"
}

// perform runs one action that is not send_email.
func (a *actions) perform(ctx context.Context, env actionEnv, n Node) outcome {
	switch {
	case n.HTTP != nil:
		return a.httpRequest(ctx, env, n.HTTP)
	case n.Chat != nil:
		return a.chatMessage(ctx, env, n.ActionType, n.Chat)
	case n.Tag != nil:
		return a.tagResponse(ctx, env, n.Tag)
	case n.Attribute != nil:
		return a.setContactAttribute(ctx, env, n.Attribute)
	}
	return failed(fmt.Sprintf("Workflow node %s is not an executable action", n.ID), true, nil)
}

func (a *actions) httpRequest(ctx context.Context, env actionEnv, cfg *HTTPRequestConfig) outcome {
	header := http.Header{}
	for _, h := range cfg.Headers {
		value := ""
		switch {
		case h.Secret != nil:
			plain, err := openSecret(a.cipher, *h.Secret, headerSecretContext(env.workspaceID, cfg.URL))
			if err != nil {
				return failed(fmt.Sprintf("The secret of header %s cannot be read; enter it again", h.Name), true, nil).because(reasonSecret)
			}
			value = plain
		case h.Value != nil:
			value = *h.Value
		}
		// The transport refuses such a value on every attempt, so it fails the step for good instead of being retried as a network error.
		if strings.ContainsFunc(value, isControl) {
			return failed(fmt.Sprintf("The value of header %s contains a line break or another control character", h.Name), true, nil).because(reasonConfig)
		}
		header.Add(h.Name, value)
	}
	var body []byte
	if strings.TrimSpace(cfg.Body) != "" {
		rendered, err := env.template.RenderJSON(cfg.Body)
		if err != nil {
			return failed("The body template is not valid JSON", true, nil).because(reasonConfig)
		}
		body = rendered
		header.Set("Content-Type", "application/json")
	}
	now := a.now()
	header.Set("Webhook-Id", env.messageID)
	header.Set("Webhook-Timestamp", strconv.FormatInt(now.Unix(), 10))
	if cfg.SigningSecret != nil {
		key, err := openSecret(a.cipher, *cfg.SigningSecret, signingSecretContext(env.workspaceID, cfg.URL))
		if err != nil {
			return failed("The signing secret cannot be read; enter it again", true, nil).because(reasonSecret)
		}
		signature, err := webhook.Sign(key, env.messageID, now, body)
		if err != nil {
			return failed("The signing secret cannot sign requests; enter it again", true, nil).because(reasonSecret)
		}
		header.Set("Webhook-Signature", signature)
	}
	return a.deliver(ctx, env, cfg.Method, cfg.URL, header, body)
}

func (a *actions) chatMessage(ctx context.Context, env actionEnv, actionType string, cfg *ChatMessageConfig) outcome {
	target, err := openSecret(a.cipher, cfg.WebhookURL, chatSecretContext(env.workspaceID, actionType))
	if err != nil {
		return failed("The webhook URL cannot be read; enter it again", true, nil).because(reasonSecret)
	}
	if !ValidChatWebhookURL(actionType, target) {
		return failed(chatURLInvalid(actionType), true, nil).because(reasonConfig)
	}
	payload, err := encodeJSON(chatPayload(actionType, env.template, cfg.Message))
	if err != nil {
		return failed(err.Error(), true, nil).because(reasonConfig)
	}
	return a.deliver(ctx, env, http.MethodPost, target, http.Header{"Content-Type": {"application/json"}}, payload)
}

// markdownEscape backslash-escapes the characters Discord's markdown and a Teams Adaptive Card's read as markup in a value: emphasis, strikethrough, code, spoilers, headings, quotes, list markers, [text](url) links, and Discord's <...> mentions, channels, emoji and timestamps. An answer then shows as typed instead of as formatting or a disguised link; the template's own markup is left alone.
var markdownEscape = strings.NewReplacer(
	`\`, `\\`, "*", `\*`, "_", `\_`, "~", `\~`, "`", "\\`", "|", `\|`,
	"[", `\[`, "]", `\]`, "(", `\(`, ")", `\)`, "#", `\#`, ">", `\>`, "<", `\<`, "-", `\-`,
).Replace

// chatPayload is the message in the shape each chat's incoming webhooks take: Slack's text, Discord's content with every mention disabled so an answer cannot ping a channel, and a Teams Adaptive Card, which both connectors and Power Automate workflows accept.
func chatPayload(actionType string, t *Template, message string) any {
	switch actionType {
	case ActionSlackMessage:
		// Each value is escaped with mrkdwn.Escape, so an answer cannot notify the channel, mention anyone, forge a link or take on formatting; the template's own markup is left alone.
		return map[string]any{"text": t.RenderText(message, mrkdwn.Escape)}
	case ActionDiscordMessage:
		text := t.RenderText(message, markdownEscape)
		if utf8.RuneCountInString(text) > discordMaxContent {
			text = string([]rune(text)[:discordMaxContent-1]) + "…"
		}
		return map[string]any{"content": text, "allowed_mentions": map[string]any{"parse": []string{}}}
	}
	text := t.RenderText(message, markdownEscape)
	return map[string]any{
		"type": "message",
		"attachments": []any{map[string]any{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content": map[string]any{
				"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
				"type":    "AdaptiveCard",
				"version": "1.4",
				"body":    []any{map[string]any{"type": "TextBlock", "text": text, "wrap": true}},
			},
		}},
	}
}

// refusedURL are the guard's refusals of a URL itself, which no retry changes; its other refusal, a host name that does not resolve, may be temporary.
var refusedURL = map[string]bool{ssrf.MessageInvalidURL: true, ssrf.MessageProtocol: true, ssrf.MessageLocalHost: true, ssrf.MessagePrivateAddress: true}

// deliver sends one request through the SSRF guard and reads its status: a 2xx succeeds; a refused URL, a redirect (never followed) and any other 4xx are permanent failures; no answer, a timeout, 408, 429 and a 5xx are retried within the step when env.retry is set, after each of retryWaits.
func (a *actions) deliver(ctx context.Context, env actionEnv, method, rawURL string, header http.Header, body []byte) outcome {
	target, err := a.guard.Validate(ctx, rawURL)
	if err != nil {
		msg := err.Error()
		if e, ok := apperr.As(err); ok {
			msg = e.Message
		}
		return failed("URL refused: "+msg, refusedURL[msg], nil).because(reasonURLRefused)
	}
	attempts := 1
	if env.retry {
		attempts += len(a.retryWaits)
	}
	var last outcome
	for attempt := 1; attempt <= attempts; attempt++ {
		status, excerpt, err := a.guard.Exchange(ctx, method, target, header, body, requestTimeout, maxResponseExcerpt)
		output := map[string]any{"attempts": attempt}
		if status != 0 {
			output["status"] = status
		}
		// What a receiver said when it refused the request is the first thing to read when a step fails.
		if text := responseExcerpt(excerpt); text != "" && (status < 200 || status >= 300) {
			output["responseBody"] = text
		}
		switch {
		case errors.Is(err, ssrf.ErrTimeout):
			last = failed(fmt.Sprintf("No answer within %d seconds", int(requestTimeout.Seconds())), false, output).because(reasonTimeout)
		case err != nil:
			last = failed("Request failed: "+err.Error(), false, output).because(reasonUnreachable)
		case status >= 200 && status < 300:
			return succeeded(output)
		case status >= 300 && status < 400:
			return failed(fmt.Sprintf("Redirect status %d, which is not followed", status), true, output).because(reasonRedirect)
		case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
			last = failed(fmt.Sprintf("Status %d", status), false, output).because(reasonStatus)
		default:
			return failed(fmt.Sprintf("Status %d", status), true, output).because(reasonStatus)
		}
		if attempt == attempts {
			break
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(a.retryWaits[attempt-1]):
		}
	}
	return last
}

func (a *actions) tagResponse(ctx context.Context, env actionEnv, cfg *TagResponseConfig) outcome {
	tagID, err := uuid.Parse(cfg.TagID)
	if err != nil {
		return failed("The tag id is not valid", true, nil)
	}
	var found []uuid.UUID
	if err := a.db.WithContext(ctx).Raw(`SELECT id FROM tags WHERE id = ? AND workspace_id = ?`, tagID, env.workspaceID).Scan(&found).Error; err != nil {
		return failed(fmt.Sprintf("Load tag: %v", err), false, nil)
	}
	if len(found) == 0 {
		return failed(fmt.Sprintf("Tag %s no longer exists in this workspace", tagID), true, nil)
	}
	// Tagging twice is tagging once: a step retried after its tag landed changes nothing.
	res := a.db.WithContext(ctx).Exec(`INSERT INTO response_tags (response_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, env.responseID, tagID)
	if res.Error != nil {
		return failed(fmt.Sprintf("Tag response: %v", res.Error), false, nil)
	}
	return succeeded(map[string]any{"tagId": tagID.String(), "added": res.RowsAffected == 1})
}

func (a *actions) setContactAttribute(ctx context.Context, env actionEnv, cfg *SetContactAttributeConfig) outcome {
	if env.contactID == nil {
		return outcome{status: StepSkipped, output: map[string]any{"reason": "The response has no contact"}}
	}
	value := env.template.RenderText(cfg.Value, func(s string) string { return s })
	// An answer left blank must not wipe what the contact holds.
	if strings.TrimSpace(value) == "" {
		return outcome{status: StepSkipped, output: map[string]any{"reason": "The value is empty", "contactId": env.contactID.String()}}
	}
	err := contact.SetAttribute(ctx, a.db, env.workspaceID, *env.contactID, cfg.AttributeKey, value)
	var refused *contact.AttributeError
	switch {
	case errors.As(err, &refused):
		return failed(refused.Message, true, nil)
	case err != nil:
		return failed(err.Error(), false, nil)
	}
	return succeeded(map[string]any{"contactId": env.contactID.String(), "attributeKey": cfg.AttributeKey})
}
