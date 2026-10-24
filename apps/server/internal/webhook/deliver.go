package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/ssrf"
)

// payload is the body of a delivery, forma2's JSON.stringify({webhookId, event, data}).
type payload struct {
	WebhookID uuid.UUID       `json:"webhookId"`
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
}

// encodeJS encodes v as JSON.stringify did: without escaping <, > and & and without a trailing newline.
func encodeJS(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Deliverer sends response pipeline events to the webhooks subscribed to them. It runs in `forma worker`, on behalf of no caller.
type Deliverer struct {
	repo  *Repository
	guard *ssrf.Guard
	now   func() time.Time
}

func NewDeliverer(repo *Repository, guard *ssrf.Guard) *Deliverer {
	return &Deliverer{repo: repo, guard: guard, now: time.Now}
}

// Matching is the workspace's webhooks subscribed to event that cover the survey.
func (d *Deliverer) Matching(ctx context.Context, workspaceID uuid.UUID, event string, surveyID uuid.UUID) ([]*Webhook, error) {
	return d.repo.Matching(ctx, workspaceID, event, surveyID)
}

// Deliver is forma2's createWebhookDeliveryTask: the event's data, wrapped with the webhook's id and the event name, POSTed to the webhook's URL with the Standard Webhooks headers and, when the webhook has a secret, signed with it. jobKey identifies the pipeline job, so the message id is the same on every attempt. The URL is checked again before every delivery; a redirect, any status outside 2xx, a timeout after five seconds and a refused URL are failures.
func (d *Deliverer) Deliver(ctx context.Context, w *Webhook, event, jobKey string, data json.RawMessage) error {
	body, err := encodeJS(payload{WebhookID: w.ID, Event: event, Data: data})
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}
	secret := ""
	if w.Secret != nil {
		secret = *w.Secret
	}
	header, err := headers(d.now(), MessageID(jobKey, w.ID, event), body, secret)
	if err != nil {
		return err
	}
	target, err := d.guard.Validate(ctx, w.URL)
	if err != nil {
		return fmt.Errorf("webhook %s URL refused: %w", w.ID, err)
	}
	status, err := d.guard.Post(ctx, target, header, body, timeout)
	switch {
	case err != nil:
		return fmt.Errorf("webhook %s delivery to %s failed: %w", w.ID, redactedURL(w.URL), err)
	case status >= 300 && status < 400:
		return fmt.Errorf("webhook %s delivery blocked: redirect status %d", w.ID, status)
	case status < 200 || status >= 300:
		return fmt.Errorf("webhook %s delivery failed with status %d", w.ID, status)
	}
	return nil
}

// redactedURL is a webhook URL as it may be logged: its scheme and host, without the path and query that often carry a token.
func redactedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(invalid URL)"
	}
	return u.Scheme + "://" + u.Host
}
