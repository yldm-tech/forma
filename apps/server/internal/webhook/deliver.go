package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
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

// Why an attempt failed, the failure column of webhook_deliveries.
const (
	// FailureStatus is an answer outside 2xx.
	FailureStatus = "status"
	// FailureRedirect is a 3xx answer, which is never followed.
	FailureRedirect = "redirect"
	// FailureTimeout is no answer within the five seconds a request may take.
	FailureTimeout = "timeout"
	// FailureUnreachable is no connection to the endpoint, or a host name that does not resolve.
	FailureUnreachable = "unreachable"
	// FailureRefused is a URL the SSRF guard refuses: localhost, a private or internal address.
	FailureRefused = "refused"
)

// Attempt is the outcome of one request to a webhook's URL. Failure is empty when the endpoint answered 2xx; otherwise Message is the reason as the webhooks page shows it, which never carries the URL's path or query.
type Attempt struct {
	// StatusCode is the endpoint's answer, 0 when there was none.
	StatusCode int
	Latency    time.Duration
	Failure    string
	Message    string
}

// OK reports whether the endpoint accepted the delivery.
func (a Attempt) OK() bool { return a.Failure == "" }

// err is the failed attempt as the worker logs and returns it, naming the webhook and only the scheme and host of its URL.
func (a Attempt) err(w *Webhook) error {
	switch a.Failure {
	case "":
		return nil
	case FailureRefused:
		return fmt.Errorf("webhook %s URL refused: %s", w.ID, a.Message)
	case FailureRedirect:
		return fmt.Errorf("webhook %s delivery blocked: redirect status %d", w.ID, a.StatusCode)
	case FailureStatus:
		return fmt.Errorf("webhook %s delivery failed with status %d", w.ID, a.StatusCode)
	default:
		return fmt.Errorf("webhook %s delivery to %s failed: %s", w.ID, redactedURL(w.URL), a.Message)
	}
}

// Deliver is forma2's createWebhookDeliveryTask: the event's data, wrapped with the webhook's id and the event name, POSTed to the webhook's URL with the Standard Webhooks headers and, when the webhook has a secret, signed with it. jobKey identifies the pipeline job, so the message id is the same on every attempt. The URL is checked again before every delivery; a redirect, any status outside 2xx, a timeout after five seconds and a refused URL are failures.
func (d *Deliverer) Deliver(ctx context.Context, w *Webhook, event, jobKey string, data json.RawMessage) error {
	a, err := d.attempt(ctx, w, event, MessageID(jobKey, w.ID, event), data)
	if err != nil {
		return err
	}
	return a.err(w)
}

// attempt sends one delivery of the event's data to the webhook as message messageID and reports how the endpoint answered. The error is a failure to build the request (the payload, the signature), before anything is sent; a failed delivery is an Attempt with its Failure set.
func (d *Deliverer) attempt(ctx context.Context, w *Webhook, event, messageID string, data json.RawMessage) (Attempt, error) {
	body, err := encodeJS(payload{WebhookID: w.ID, Event: event, Data: data})
	if err != nil {
		return Attempt{}, fmt.Errorf("encode webhook payload: %w", err)
	}
	secret := ""
	if w.Secret != nil {
		secret = *w.Secret
	}
	header, err := headers(d.now(), messageID, body, secret)
	if err != nil {
		return Attempt{}, err
	}
	target, err := d.guard.Validate(ctx, w.URL)
	if err != nil {
		return refusedAttempt(err), nil
	}
	start := time.Now()
	status, err := d.guard.Post(ctx, target, header, body, timeout)
	a := Attempt{StatusCode: status, Latency: time.Since(start)}
	switch {
	case errors.Is(err, ssrf.ErrTimeout):
		a.Failure, a.Message = FailureTimeout, "No answer within 5 seconds"
	case err != nil:
		a.Failure, a.Message = FailureUnreachable, err.Error()
	case status >= 300 && status < 400:
		a.Failure, a.Message = FailureRedirect, fmt.Sprintf("Redirect status %d, which is not followed", status)
	case status < 200 || status >= 300:
		a.Failure, a.Message = FailureStatus, fmt.Sprintf("Status %d", status)
	}
	return a, nil
}

// refusedURL are the guard's refusals of the URL itself; its other failures are a host name that does not resolve, which may be temporary.
var refusedURL = map[string]bool{ssrf.MessageInvalidURL: true, ssrf.MessageProtocol: true, ssrf.MessageLocalHost: true, ssrf.MessagePrivateAddress: true}

// refusedAttempt is a URL the guard's check did not let through, with the guard's own message: refused for the URL itself, unreachable for a host name that does not resolve.
func refusedAttempt(err error) Attempt {
	a := Attempt{Failure: FailureUnreachable, Message: err.Error()}
	if e, ok := apperr.As(err); ok {
		a.Message = e.Message
		if refusedURL[e.Message] {
			a.Failure = FailureRefused
		}
	}
	return a
}

// redactedURL is a webhook URL as it may be logged: its scheme and host, without the path and query that often carry a token.
func redactedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(invalid URL)"
	}
	return u.Scheme + "://" + u.Host
}
