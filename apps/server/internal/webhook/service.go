package webhook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ssrf"
)

// timeout bounds one request to a webhook, forma2's WEBHOOK_TIMEOUT_MS and testEndpoint's abort.
const timeout = 5 * time.Second

// Stable codes of the endpoint test's failures. The status codes are forma2's translation keys under workspace.integrations.webhooks, which the SPA translates; the messages are their en-US text.
const (
	CodeDiscordNotSupported = "discord_webhook_not_supported"
	CodeEndpointRedirect    = "endpoint_redirect_error"
	CodeEndpointTimeout     = "endpoint_timeout_error"
	CodeEndpointUnreachable = "endpoint_unreachable_error"
	CodeInvalidSecret       = "invalid_webhook_secret"
	CodeForeignSurveys      = "invalid_webhook_surveys"
)

// endpointStatusErrors are the answers forma2's testEndpoint refused; every other status, 4xx included, counts as reachable.
var endpointStatusErrors = map[int]*apperr.Error{
	http.StatusInternalServerError: apperr.InvalidInput("endpoint_internal_server_error", "Internal Server Error (500): The service encountered an unexpected error"),
	http.StatusNotFound:            apperr.InvalidInput("endpoint_not_found_error", "Not Found (404): The endpoint doesn't exist"),
	http.StatusMethodNotAllowed:    apperr.InvalidInput("endpoint_method_not_allowed_error", "Method Not Allowed (405): The endpoint exists, but doesn't accept POST requests"),
	http.StatusBadGateway:          apperr.InvalidInput("endpoint_bad_gateway_error", "Bad Gateway (502): Proxy/gateway error, service not reachable"),
	http.StatusServiceUnavailable:  apperr.InvalidInput("endpoint_service_unavailable_error", "Service Unavailable (503): Service is temporarily down"),
	http.StatusGatewayTimeout:      apperr.InvalidInput("endpoint_gateway_timeout_error", "Gateway Timeout (504): Gateway timeout, service not reachable"),
}

// Service holds the webhook rules: the settings page's operations, the v1 API's, and delivery. Every operation on behalf of a caller authorizes through authz on the webhook's workspace before it reads or writes.
type Service struct {
	repo    *Repository
	authz   *authz.Authorizer
	limiter *ratelimit.Limiter
	audit   *audit.Emitter
	guard   *ssrf.Guard
	log     *slog.Logger
	now     func() time.Time
}

func NewService(repo *Repository, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter, guard *ssrf.Guard, log *slog.Logger) *Service {
	return &Service{repo: repo, authz: authorizer, limiter: limiter, audit: emitter, guard: guard, log: log, now: time.Now}
}

// Input is a webhook as the settings page writes it, forma2's ZWebhookInput. AllSurveys, which forma2 did not have, states the scope; without it (the v1 API, a page loaded before it existed) an empty SurveyIDs scopes the webhook to every survey, as in forma2.
type Input struct {
	Name       *string  `json:"name"`
	URL        string   `json:"url"`
	Source     string   `json:"source"`
	Triggers   []string `json:"triggers"`
	SurveyIDs  []string `json:"surveyIds"`
	AllSurveys *bool    `json:"allSurveys"`
}

// fields validates the input: forma2's zod rules (a URL, at least one known trigger, a known source), a scope that is either every survey or at least one, and surveys of the webhook's own workspace. kept are the surveys the webhook already lists, which stay valid after their survey is deleted.
func (s *Service) fields(ctx context.Context, workspaceID uuid.UUID, in Input, kept []uuid.UUID) (fields, error) {
	f := fields{Name: in.Name, URL: in.URL, Source: in.Source}
	if f.Source == "" {
		f.Source = SourceUser
	}
	if !slices.Contains(Sources, f.Source) {
		return f, apperr.BadRequest("Invalid webhook source")
	}
	if len(in.Triggers) == 0 {
		return f, apperr.BadRequest("At least one trigger is required")
	}
	for _, t := range in.Triggers {
		if !slices.Contains(Triggers, t) {
			return f, apperr.BadRequest("Invalid webhook trigger")
		}
		if !slices.Contains(f.Triggers, t) {
			f.Triggers = append(f.Triggers, t)
		}
	}
	ids, err := resolveSurveyIDs(in.SurveyIDs)
	if err != nil {
		return f, err
	}
	f.AllSurveys = len(ids) == 0
	if in.AllSurveys != nil {
		if *in.AllSurveys != (len(ids) == 0) {
			return f, apperr.BadRequest("A webhook is scoped to every survey or to at least one listed survey")
		}
		f.AllSurveys = *in.AllSurveys
	}
	foreign, err := s.repo.foreignSurveys(ctx, workspaceID, ids)
	if err != nil {
		return f, err
	}
	if slices.ContainsFunc(foreign, func(id uuid.UUID) bool { return !slices.Contains(kept, id) }) {
		return f, apperr.InvalidInput(CodeForeignSurveys, "Every survey of a webhook must belong to its workspace")
	}
	f.SurveyIDs = ids
	return f, nil
}

// resolveSurveyIDs reads survey ids from outside, which are UUIDs.
func resolveSurveyIDs(raw []string) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for _, r := range raw {
		id, ok := rowid.Parse(r)
		if !ok {
			return nil, apperr.BadRequest("Invalid survey id")
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// List is the settings page's webhooks, forma2's getWebhooks: workspace.read, newest first, never with their secrets.
func (s *Service) List(ctx context.Context, userID, workspaceID uuid.UUID) ([]View, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	found, err := s.repo.OfWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]View, len(found))
	for i, w := range found {
		out[i] = w.appView()
	}
	return out, nil
}

// CreateInput is the settings page's create, forma2's createWebhookAction: the webhook and the secret the endpoint test just signed with, so the stored secret is the one the receiver was shown.
type CreateInput struct {
	Input
	Secret *string `json:"webhookSecret"`
}

// Create is forma2's createWebhookAction: workspace.write, the state-mutation limit per workspace, the URL's SSRF check, no Discord, and the given secret or a new one. The answer is the only one that carries the secret.
func (s *Service) Create(ctx context.Context, userID, workspaceID uuid.UUID, in CreateInput) (*View, error) {
	var org *uuid.UUID
	created, err := func() (*Webhook, error) {
		if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(workspaceID)); err != nil {
			return nil, err
		}
		var err error
		if org, err = s.repo.organizationOf(ctx, workspaceID); err != nil {
			return nil, err
		}
		if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, workspaceID.String()); err != nil {
			return nil, err
		}
		f, err := s.fields(ctx, workspaceID, in.Input, nil)
		if err != nil {
			return nil, err
		}
		if _, err := s.guard.Validate(ctx, f.URL); err != nil {
			return nil, err
		}
		if IsDiscord(f.URL) {
			return nil, apperr.InvalidInput(CodeDiscordNotSupported, "Discord webhooks are currently not supported.")
		}
		secret := GenerateSecret()
		if in.Secret != nil {
			// forma2 stored whatever the browser sent; a secret receivers cannot verify with is refused here.
			if !validSecret(*in.Secret) {
				return nil, apperr.InvalidInput(CodeInvalidSecret, "Webhook secret must be a whsec_ Standard Webhooks secret")
			}
			secret = *in.Secret
		}
		id, err := s.repo.create(ctx, workspaceID, f, secret)
		if err != nil {
			return nil, err
		}
		return s.repo.ByID(ctx, id)
	}()
	e := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetWebhook, ActorType: audit.ActorUser, Status: audit.StatusSuccess, NewObject: in.Input}
	if org != nil {
		e.OrganizationID = org.String()
	}
	if created != nil {
		e.TargetID = created.ID.String()
	}
	if err != nil {
		e.Status, e.NewObject = audit.StatusFailure, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
	if err != nil {
		return nil, err
	}
	if org != nil {
		analytics.Capture(userID.String(), "integration_connected", analytics.Properties{
			"integration_type": "webhook",
			"organization_id":  org.String(),
			"workspace_id":     workspaceID.String(),
		}, analytics.Groups{OrganizationID: org.String(), WorkspaceID: workspaceID.String()})
	}
	v := created.viewWithSecret()
	v.AllSurveys = &created.AllSurveys
	return &v, nil
}

// owned loads a webhook and authorizes action on its workspace. A webhook that does not exist is 404, as forma2's getOrganizationIdFromWebhookId answered first.
func (s *Service) owned(ctx context.Context, userID, id uuid.UUID, action authz.Action) (*Webhook, error) {
	w, err := s.repo.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, apperr.NotFound("webhook", id.String())
	}
	if err := s.authz.Require(ctx, authz.User(userID), action, authz.Workspace(w.WorkspaceID)); err != nil {
		return nil, err
	}
	return w, nil
}

// Update is forma2's updateWebhookAction: workspace.write on the webhook's workspace, the state-mutation limit, and the URL's SSRF check. The source is never changed. forma2 stored a cleared name as "", which this keeps.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, in Input) (*View, error) {
	var before, after *Webhook
	err := func() error {
		var err error
		if before, err = s.owned(ctx, userID, id, authz.WorkspaceWrite); err != nil {
			return err
		}
		if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, before.WorkspaceID.String()); err != nil {
			return err
		}
		in.Source = before.Source
		f, err := s.fields(ctx, before.WorkspaceID, in, before.SurveyIDs)
		if err != nil {
			return err
		}
		if _, err := s.guard.Validate(ctx, f.URL); err != nil {
			return err
		}
		if err := s.repo.update(ctx, id, f); err != nil {
			return err
		}
		after, err = s.repo.ByID(ctx, id)
		return err
	}()
	s.auditChange(ctx, audit.ActionUpdated, id, before, after, err)
	if err != nil {
		return nil, err
	}
	v := after.appView()
	return &v, nil
}

// Delete is forma2's deleteWebhookAction: workspace.write on the webhook's workspace (the v1 API asks for manage), then the state-mutation limit.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	var before *Webhook
	err := func() error {
		var err error
		if before, err = s.owned(ctx, userID, id, authz.WorkspaceWrite); err != nil {
			return err
		}
		if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, before.WorkspaceID.String()); err != nil {
			return err
		}
		deleted, err := s.repo.delete(ctx, id)
		if err == nil && !deleted {
			err = apperr.NotFound("Webhook", id.String())
		}
		return err
	}()
	s.auditChange(ctx, audit.ActionDeleted, id, before, nil, err)
	return err
}

func (s *Service) auditChange(ctx context.Context, action audit.Action, id uuid.UUID, before, after *Webhook, err error) {
	e := audit.Event{Action: action, TargetType: audit.TargetWebhook, TargetID: id.String(), ActorType: audit.ActorUser, Status: audit.StatusSuccess}
	if before != nil {
		if org, orgErr := s.repo.organizationOf(ctx, before.WorkspaceID); orgErr == nil && org != nil {
			e.OrganizationID = org.String()
		}
		e.OldObject = before.view()
	}
	if after != nil {
		e.NewObject = after.view()
	}
	if err != nil {
		e.Status, e.OldObject, e.NewObject = audit.StatusFailure, nil, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// TestInput is forma2's testEndpointAction input: the URL as the form holds it, and either the webhook being edited, whose stored secret signs the test, or the secret of the webhook being created.
type TestInput struct {
	URL       string  `json:"url"`
	WebhookID *string `json:"webhookId"`
	Secret    *string `json:"secret"`
}

// TestResult is the endpoint test's answer. Secret is the secret the test was signed with when the webhook is being created, for the create that follows; for an existing webhook it is left out, where forma2 sent the stored secret back to the browser.
type TestResult struct {
	Success bool    `json:"success"`
	Secret  *string `json:"secret,omitempty"`
}

// TestEndpoint is forma2's testEndpointAction: workspace.write on the webhook's workspace (an existing webhook) or on the workspace it is being created in, then one signed {"event":"testEndpoint"} POST to the URL.
func (s *Service) TestEndpoint(ctx context.Context, userID, workspaceID uuid.UUID, in TestInput) (*TestResult, error) {
	var secret string
	result := &TestResult{Success: true}
	if in.WebhookID != nil {
		id, ok := rowid.Parse(*in.WebhookID)
		if !ok {
			return nil, apperr.BadRequest("Invalid webhook id")
		}
		w, err := s.owned(ctx, userID, id, authz.WorkspaceWrite)
		if err != nil {
			return nil, err
		}
		if w.Secret != nil {
			secret = *w.Secret
		}
	} else {
		if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(workspaceID)); err != nil {
			return nil, err
		}
		secret = GenerateSecret()
		if in.Secret != nil {
			if !validSecret(*in.Secret) {
				return nil, apperr.InvalidInput(CodeInvalidSecret, "Webhook secret must be a whsec_ Standard Webhooks secret")
			}
			secret = *in.Secret
		}
		result.Secret = &secret
	}
	if err := s.testEndpoint(ctx, in.URL, secret); err != nil {
		return nil, err
	}
	return result, nil
}

// testEndpoint is forma2's testEndpoint: the SSRF check, no Discord, then the test POST, whose answer counts as reachable unless it redirects or is one of the six statuses forma2 refused.
func (s *Service) testEndpoint(ctx context.Context, rawURL, secret string) error {
	target, err := s.guard.Validate(ctx, rawURL)
	if err != nil {
		return err
	}
	if IsDiscord(rawURL) {
		return apperr.InvalidInput(CodeDiscordNotSupported, "Discord webhooks are currently not supported.")
	}
	messageID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("webhook test message id: %w", err)
	}
	body := []byte(`{"event":"testEndpoint"}`)
	header, err := headers(s.now(), messageID.String(), body, secret)
	if err != nil {
		return err
	}
	status, err := s.guard.Post(ctx, target, header, body, timeout)
	switch {
	case errors.Is(err, ssrf.ErrTimeout):
		return apperr.InvalidInput(CodeEndpointTimeout, "Request timed out after 5 seconds")
	case err != nil:
		return apperr.InvalidInput(CodeEndpointUnreachable, "Error while fetching the URL: "+err.Error())
	case status >= 300 && status < 400:
		return apperr.InvalidInput(CodeEndpointRedirect, "Webhook endpoint returned a redirect, which is not allowed")
	}
	if e, ok := endpointStatusErrors[status]; ok {
		return e
	}
	return nil
}

// headers are the Standard Webhooks headers of one request sent at now: content type, message id and timestamp, and the signature when there is a secret. The names are written in lower case, as forma2 sent them.
func headers(now time.Time, messageID string, body []byte, secret string) (http.Header, error) {
	h := http.Header{
		"content-type":      {"application/json"},
		"webhook-id":        {messageID},
		"webhook-timestamp": {strconv.FormatInt(now.Unix(), 10)},
	}
	if secret != "" {
		sig, err := Sign(secret, messageID, now, body)
		if err != nil {
			return nil, err
		}
		h["webhook-signature"] = []string{sig}
	}
	return h, nil
}
