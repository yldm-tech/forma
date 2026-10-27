package integration

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// CodeNotConfigured is an integration whose OAuth app this instance has no credentials for; the message names the missing one, as forma2's did.
const CodeNotConfigured = "integration_not_configured"

func notConfigured(message string) *apperr.Error {
	return &apperr.Error{Status: http.StatusInternalServerError, Code: CodeNotConfigured, Message: message}
}

// internalError is a failure forma2 answered with its message and a 500.
func internalError(message string) *apperr.Error {
	return &apperr.Error{Status: http.StatusInternalServerError, Code: apperr.CodeInternal, Message: message}
}

// errStateRefused is the answer to a callback whose state is refused.
var errStateRefused = apperr.BadRequest("Invalid OAuth state")

// requireConfigured is the configuration checks forma2's authorize and callback routes made, in their order and with their messages.
func (s *Service) requireConfigured(typ string) error {
	c := s.cfg.Integrations
	checks := map[string][]struct {
		missing bool
		message string
	}{
		TypeGoogleSheets: {{c.GoogleSheetsClientID == "", "Google client id is missing"}, {c.GoogleSheetsClientSecret == "", "Google client secret is missing"}, {c.GoogleSheetsRedirectURL == "", "Google redirect url is missing"}},
		TypeAirtable:     {{c.AirtableClientID == "", "Airtable client id is missing"}},
		TypeNotion:       {{c.NotionClientID == "", "Notion client id is missing"}, {c.NotionClientSecret == "", "Notion client secret is missing"}},
		TypeSlack:        {{c.SlackClientID == "", "Slack client id is missing"}, {c.SlackClientSecret == "", "Slack client secret is missing"}},
	}
	for _, check := range checks[typ] {
		if check.missing {
			return notConfigured(check.message)
		}
	}
	return nil
}

// Authorize starts a provider's OAuth flow for the workspace, forma2's GET /api/google-sheet and /api/v1/integrations/{airtable,notion,slack}: workspace.manage (forma2 asked for workspace.write; sending the workspace's responses to an outside account is a manager's decision, as a webhook's URL is), the provider configured, then a state bound to the provider, the caller's session and the workspace (with a PKCE verifier for Airtable), and the provider's authorize URL carrying it.
func (s *Service) Authorize(ctx context.Context, actor httpapi.Actor, workspaceID uuid.UUID, typ string) (string, error) {
	if err := s.authz.Require(ctx, authz.User(actor.UserID), authz.WorkspaceManage, authz.Workspace(workspaceID)); err != nil {
		return "", err
	}
	if err := s.requireConfigured(typ); err != nil {
		return "", err
	}
	st := oauthState{Provider: typ, UserID: actor.UserID, SessionID: actor.SessionID, WorkspaceID: workspaceID}
	if typ == TypeAirtable {
		st.PKCEVerifier = oauth2.GenerateVerifier()
	}
	state, err := s.states.create(ctx, st)
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to create integration OAuth state", "err", err, "provider", typ, "workspace_id", workspaceID)
		return "", internalError("Unable to start OAuth flow")
	}
	switch typ {
	case TypeGoogleSheets:
		return s.p.googleAuthorizeURL(state), nil
	case TypeAirtable:
		return s.p.airtableAuthorizeURL(state, st.PKCEVerifier), nil
	case TypeNotion:
		return s.p.notionAuthorizeURL(state), nil
	}
	return s.p.slackAuthorizeURL(state), nil
}

// callbackPaths are where each callback sends the browser back to, under the app's base path on WEBAPP_URL: forma2's paths, the Airtable, Notion and Slack ones through the old settings location the SPA forwards.
var callbackPaths = map[string]string{
	TypeGoogleSheets: "/integrations/google-sheets",
	TypeAirtable:     "/settings/workspace/integrations/airtable",
	TypeNotion:       "/settings/workspace/integrations/notion",
	TypeSlack:        "/settings/workspace/integrations/slack",
}

func (s *Service) pageURL(typ string, workspaceID uuid.UUID, errorCode string) string {
	u := s.p.webApp + config.AppBasePath + "/workspaces/" + workspaceID.String() + callbackPaths[typ]
	if errorCode != "" {
		u += "?error=" + url.QueryEscape(errorCode)
	}
	return u
}

// Callback completes a provider's OAuth flow, forma2's callback routes: the state consumed (it must have been issued to this provider, user and session), workspace.manage on the workspace it names, as Authorize needs, then the provider's code exchanged and the credentials stored, keeping the integration's links. It returns where to send the browser; a provider error the user can act on is passed to the page as ?error=.
func (s *Service) Callback(ctx context.Context, actor httpapi.Actor, typ string, query url.Values) (string, error) {
	st, err := s.states.consume(ctx, typ, actor.UserID, actor.SessionID, query.Get("state"))
	if errors.Is(err, errInvalidState) {
		return "", errStateRefused
	}
	if err != nil {
		return "", err
	}
	if typ == TypeAirtable && st.PKCEVerifier == "" {
		return "", errStateRefused
	}
	if err := s.authz.Require(ctx, authz.User(actor.UserID), authz.WorkspaceManage, authz.Workspace(st.WorkspaceID)); err != nil {
		return "", err
	}
	code, safeError := query.Get("code"), safeCallbackError(query.Get("error"))
	switch typ {
	case TypeGoogleSheets:
		return s.googleCallback(ctx, actor.UserID, st.WorkspaceID, code, safeError)
	case TypeAirtable:
		return s.airtableCallback(ctx, actor.UserID, st, code, safeError)
	case TypeNotion:
		return s.notionCallback(ctx, actor.UserID, st.WorkspaceID, code, safeError)
	}
	return s.slackCallback(ctx, actor.UserID, st.WorkspaceID, code, safeError)
}

// googleCallback checks the provider's error before the code, as forma2 did, and sends the browser back without one when there is no code.
func (s *Service) googleCallback(ctx context.Context, userID, workspaceID uuid.UUID, code, safeError string) (string, error) {
	if safeError != "" {
		return s.pageURL(TypeGoogleSheets, workspaceID, safeError), nil
	}
	if err := s.requireConfigured(TypeGoogleSheets); err != nil {
		return "", err
	}
	if code == "" {
		return s.pageURL(TypeGoogleSheets, workspaceID, ""), nil
	}
	k, err := s.p.googleExchange(ctx, code)
	if err != nil {
		return "", err
	}
	email, err := s.p.googleEmail(ctx, k.AccessToken)
	if err != nil {
		return "", err
	}
	if email == "" {
		return "", internalError("Failed to get user email")
	}
	if err := s.connect(ctx, userID, workspaceID, TypeGoogleSheets, k, email); err != nil {
		return "", err
	}
	return s.pageURL(TypeGoogleSheets, workspaceID, ""), nil
}

func (s *Service) airtableCallback(ctx context.Context, userID uuid.UUID, st *oauthState, code, safeError string) (string, error) {
	if code == "" && safeError != "" {
		return s.pageURL(TypeAirtable, st.WorkspaceID, safeError), nil
	}
	if code == "" {
		return "", apperr.BadRequest("`code` is missing")
	}
	if err := s.requireConfigured(TypeAirtable); err != nil {
		return "", err
	}
	err := func() error {
		k, err := s.p.airtableToken(ctx, url.Values{
			"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {s.p.airtableRedirectURI()},
			"client_id": {s.cfg.Integrations.AirtableClientID}, "code_verifier": {st.PKCEVerifier},
		}, s.now())
		if err != nil {
			return err
		}
		email, err := s.p.airtableEmail(ctx, k.AccessToken)
		if err != nil {
			return err
		}
		return s.connect(ctx, userID, st.WorkspaceID, TypeAirtable, k, email)
	}()
	if err != nil {
		s.log.ErrorContext(ctx, "Error in GET /api/v1/integrations/airtable/callback", "err", err.Error())
		return "", internalError("Unable to complete Airtable OAuth flow")
	}
	return s.pageURL(TypeAirtable, st.WorkspaceID, ""), nil
}

// notionCallback reports a refused exchange, or an answer without a usable credential, to the page as oauth_error and keeps the integration's existing credential.
func (s *Service) notionCallback(ctx context.Context, userID, workspaceID uuid.UUID, code, safeError string) (string, error) {
	if code == "" && safeError != "" {
		return s.pageURL(TypeNotion, workspaceID, safeError), nil
	}
	if err := s.requireConfigured(TypeNotion); err != nil {
		return "", err
	}
	if code == "" {
		return "", apperr.BadRequest("Missing code or error parameter")
	}
	k, err := s.p.notionExchange(ctx, code)
	if errors.Is(err, errNotionExchange) {
		s.log.ErrorContext(ctx, "Notion OAuth token exchange failed", "workspace_id", workspaceID, "err", err)
		return s.pageURL(TypeNotion, workspaceID, "oauth_error"), nil
	}
	if err != nil {
		return "", err
	}
	if err := s.connect(ctx, userID, workspaceID, TypeNotion, k, ""); err != nil {
		return "", err
	}
	return s.pageURL(TypeNotion, workspaceID, ""), nil
}

// slackCallback answers Slack refusing the code with Slack's error code, as forma2 did.
func (s *Service) slackCallback(ctx context.Context, userID, workspaceID uuid.UUID, code, safeError string) (string, error) {
	if code == "" && safeError != "" {
		return s.pageURL(TypeSlack, workspaceID, safeError), nil
	}
	if err := s.requireConfigured(TypeSlack); err != nil {
		return "", err
	}
	if code == "" {
		return "", apperr.BadRequest("Missing code or error parameter")
	}
	k, err := s.p.slackExchange(ctx, code)
	var refused *slackRefusal
	if errors.As(err, &refused) {
		return "", apperr.BadRequest(refused.code)
	}
	if err != nil {
		return "", err
	}
	if err := s.connect(ctx, userID, workspaceID, TypeSlack, k, ""); err != nil {
		return "", err
	}
	return s.pageURL(TypeSlack, workspaceID, ""), nil
}

// connect stores what a callback obtained.
func (s *Service) connect(ctx context.Context, userID, workspaceID uuid.UUID, typ string, k any, email string) error {
	raw, err := encodeJS(k)
	if err != nil {
		return err
	}
	if _, err = s.repo.Connect(ctx, workspaceID, typ, Credentials{Key: raw, Email: email}); err != nil {
		return err
	}
	s.captureConnected(ctx, userID, workspaceID, typ)
	return nil
}

// captureConnected is forma2's integration_connected.
func (s *Service) captureConnected(ctx context.Context, userID, workspaceID uuid.UUID, typ string) {
	if !analytics.Enabled() {
		return
	}
	org, err := s.repo.organizationOf(ctx, workspaceID)
	if err != nil || org == nil {
		s.log.ErrorContext(ctx, "Failed to capture PostHog integration_connected event", "integrationType", typ, "err", err)
		return
	}
	analytics.Capture(userID.String(), "integration_connected", analytics.Properties{
		"integration_type": typ,
		"organization_id":  org.String(),
		"workspace_id":     workspaceID.String(),
	}, analytics.Groups{OrganizationID: org.String(), WorkspaceID: workspaceID.String()})
}
