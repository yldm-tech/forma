package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/responsemail"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// CodeProviderError is a provider refusing or failing a request the integrations pages made; the message is the provider's, as forma2's pages showed it.
const CodeProviderError = "integration_provider_error"

func providerError(err error) *apperr.Error {
	return &apperr.Error{Status: http.StatusBadGateway, Code: CodeProviderError, Message: err.Error()}
}

// googleError is a Google Sheets failure as the page reads it: invalid_grant (reconnect) and insufficient_permission keep forma2's codes, anything else is the provider's message.
func googleError(err error) error {
	switch {
	case errors.Is(err, errInvalidGrant):
		return &apperr.Error{Status: http.StatusBadRequest, Code: CodeInvalidGrant, Message: CodeInvalidGrant}
	case errors.Is(err, errInsufficientPermission):
		return &apperr.Error{Status: http.StatusForbidden, Code: CodeInsufficientPermission, Message: CodeInsufficientPermission}
	}
	return providerError(err)
}

// Service holds the integration rules: the integrations pages' operations and the OAuth flows. Every operation on behalf of a caller authorizes through authz on the integration's workspace before it reads or writes.
type Service struct {
	cfg     config.Config
	repo    *Repository
	authz   *authz.Authorizer
	limiter *ratelimit.Limiter
	audit   *audit.Emitter
	states  *stateStore
	p       *providers
	tokens  *tokens
	log     *slog.Logger
	now     func() time.Time
}

func NewService(cfg config.Config, repo *Repository, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter, rdb *redis.Client, log *slog.Logger) *Service {
	p := newProviders(cfg)
	return &Service{
		cfg: cfg, repo: repo, authz: authorizer, limiter: limiter, audit: emitter, p: p, log: log, now: time.Now,
		states: &stateStore{rdb: rdb, now: time.Now},
		tokens: &tokens{repo: repo, rdb: rdb, p: p, log: log, now: time.Now, refresh: defaultAirtableRefreshTiming},
	}
}

// enabled is which integrations this instance can connect, as forma2's pages decided it.
func (s *Service) enabled() map[string]bool {
	c := s.cfg.Integrations
	return map[string]bool{
		TypeGoogleSheets: c.GoogleSheetsEnabled(), TypeAirtable: c.AirtableEnabled(), TypeNotion: c.NotionEnabled(), TypeSlack: c.SlackEnabled(),
	}
}

// Connection is who an integration is connected as, the identity the pages show: the Google or Airtable account's email, the Notion workspace's name, the Slack team's name. Connected is false when the integration's credentials are missing or no longer open, which only reconnecting fixes.
type Connection struct {
	Connected     bool    `json:"connected"`
	Email         *string `json:"email,omitempty"`
	WorkspaceName *string `json:"workspaceName,omitempty"`
	TeamName      *string `json:"teamName,omitempty"`
}

// View is an integration as the pages see it: its links and its connection, never its tokens.
type View struct {
	ID                  uuid.UUID       `json:"id"`
	Type                string          `json:"type"`
	Config              json.RawMessage `json:"config"`
	Connection          Connection      `json:"connection"`
	CreatedAt           time.Time       `json:"createdAt"`
	UpdatedAt           time.Time       `json:"updatedAt"`
	LastErrorAt         *time.Time      `json:"lastErrorAt"`
	LastErrorMessage    *string         `json:"lastErrorMessage"`
	ConsecutiveFailures int             `json:"consecutiveFailures"`
}

func (in *Integration) view() View {
	v := View{ID: in.ID, Type: in.Type, Config: in.Config, CreatedAt: in.CreatedAt, UpdatedAt: in.UpdatedAt,
		LastErrorAt: in.LastErrorAt, LastErrorMessage: in.LastErrorMessage, ConsecutiveFailures: in.ConsecutiveFailures}
	if len(v.Config) == 0 {
		v.Config = emptyConfig
	}
	switch in.Type {
	case TypeGoogleSheets, TypeAirtable:
		if in.Credentials != nil && len(in.Credentials.Key) > 0 {
			v.Connection.Connected = true
			email := in.Credentials.Email
			v.Connection.Email = &email
		}
	case TypeNotion:
		if k, ok := key[notionKey](in); ok && k.BotID != "" {
			v.Connection.Connected = true
			v.Connection.WorkspaceName = k.WorkspaceName
		}
	case TypeSlack:
		if k, ok := key[SlackKey](in); ok && k.AccessToken != "" {
			v.Connection.Connected = true
			name := k.Team.Name
			v.Connection.TeamName = &name
		}
	}
	return v
}

// Overview is the integrations pages' data: the workspace's integrations, and which integrations this instance can connect (forma2 disabled a page's Connect button otherwise).
type Overview struct {
	Integrations []View         `json:"integrations"`
	Enabled      map[string]bool `json:"enabled"`
}

// List is the integrations pages' data: workspace.read.
func (s *Service) List(ctx context.Context, userID, workspaceID uuid.UUID) (*Overview, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	found, err := s.repo.OfWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := &Overview{Integrations: make([]View, len(found)), Enabled: s.enabled()}
	for i, in := range found {
		out.Integrations[i] = in.view()
	}
	return out, nil
}

// SurveyElement is an element a link can select, with its headline in every language.
type SurveyElement struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Headline surveydoc.I18nString `json:"headline"`
}

// SurveyVariable is a computed field a Notion mapping can select: its storage key and name.
type SurveyVariable struct {
	StorageKey string `json:"storageKey"`
	Name       string `json:"name"`
}

// LinkableSurvey is a survey the link modals offer, with what a link can select of it: its elements, its hidden fields' storage keys and its computed variables.
type LinkableSurvey struct {
	ID           uuid.UUID        `json:"id"`
	Name         string           `json:"name"`
	Elements     []SurveyElement  `json:"elements"`
	HiddenFields []string         `json:"hiddenFields"`
	Variables    []SurveyVariable `json:"variables"`
}

// Surveys are the surveys the link modals offer, forma2's integrations getSurveys: workspace.read; not completed, not archived, most recently updated first.
func (s *Service) Surveys(ctx context.Context, userID, workspaceID uuid.UUID) ([]LinkableSurvey, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	ids, err := s.repo.linkableSurveys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]LinkableSurvey, 0, len(ids))
	for _, id := range ids {
		sv, err := responsemail.LoadSurvey(ctx, s.repo.db, id)
		if err != nil {
			return nil, err
		}
		if sv == nil {
			continue
		}
		ls := LinkableSurvey{ID: id, Name: sv.Name, Elements: []SurveyElement{}, HiddenFields: []string{}, Variables: []SurveyVariable{}}
		for _, el := range sv.Elements() {
			ls.Elements = append(ls.Elements, SurveyElement{ID: el.ID, Type: el.Type, Headline: el.Headline})
		}
		ls.HiddenFields = append(ls.HiddenFields, ingestedKeys(sv)...)
		for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceComputed) {
			ls.Variables = append(ls.Variables, SurveyVariable{StorageKey: f.StorageKey, Name: f.Name})
		}
		out = append(out, ls)
	}
	return out, nil
}

// integrationOf loads the workspace's integration of the type after authorizing action on the workspace; 404 when it is not connected.
func (s *Service) integrationOf(ctx context.Context, userID, workspaceID uuid.UUID, typ string, action authz.Action) (*Integration, error) {
	if err := s.authz.Require(ctx, authz.User(userID), action, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	in, err := s.repo.ByType(ctx, workspaceID, typ)
	if err != nil {
		return nil, err
	}
	if in == nil {
		return nil, apperr.NotFound("Integration", typ)
	}
	return in, nil
}

// SaveLinks is forma2's createOrUpdateIntegrationAction: the integration-mutation limit per user before anything is read, workspace.write, then the integration's links replaced by config. Credentials never come from this path: forma2 replaced a client-sent config.key with the stored one, and here the input has no place for one, so an integration that was never connected through its OAuth flow is 404. Every link must name a survey of the workspace. Only the integration's id is returned.
func (s *Service) SaveLinks(ctx context.Context, userID, workspaceID uuid.UUID, typ string, config json.RawMessage, surveyIDs []string) (uuid.UUID, error) {
	var org *uuid.UUID
	var saved *Integration
	err := func() error {
		if err := s.limiter.Apply(ctx, ratelimit.ActionIntegrationMutation, userID.String()); err != nil {
			return err
		}
		var err error
		if org, err = s.repo.organizationOf(ctx, workspaceID); err != nil {
			return err
		}
		in, err := s.integrationOf(ctx, userID, workspaceID, typ, authz.WorkspaceWrite)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for _, raw := range surveyIDs {
			id, ok := legacyid.Resolve(raw)
			if !ok {
				return apperr.BadRequest("Invalid survey id")
			}
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		foreign, err := s.repo.foreignSurveys(ctx, workspaceID, ids)
		if err != nil {
			return err
		}
		if len(foreign) > 0 {
			return apperr.InvalidInput("invalid_integration_surveys", "Every survey of an integration must belong to its workspace")
		}
		if err := s.repo.SaveConfig(ctx, in.ID, config); err != nil {
			return err
		}
		saved, err = s.repo.ByID(ctx, in.ID)
		return err
	}()
	e := audit.Event{Action: audit.ActionCreatedUpdated, TargetType: audit.TargetIntegration, TargetID: audit.Unknown, ActorType: audit.ActorUser, Status: audit.StatusSuccess}
	if org != nil {
		e.OrganizationID = org.String()
	}
	if saved != nil {
		e.TargetID, e.NewObject = saved.ID.String(), saved.view()
	}
	if err != nil {
		e.Status, e.NewObject = audit.StatusFailure, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
	if err != nil {
		return uuid.Nil, err
	}
	return saved.ID, nil
}

// Delete is forma2's deleteIntegrationAction: the integration-mutation limit per user, workspace.write on the integration's workspace, then the integration removed with its links and credentials.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	var before *Integration
	var org *uuid.UUID
	err := func() error {
		if err := s.limiter.Apply(ctx, ratelimit.ActionIntegrationMutation, userID.String()); err != nil {
			return err
		}
		var err error
		if before, err = s.repo.ByID(ctx, id); err != nil {
			return err
		}
		if before == nil {
			return apperr.NotFound("Integration", id.String())
		}
		if org, err = s.repo.organizationOf(ctx, before.WorkspaceID); err != nil {
			return err
		}
		if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(before.WorkspaceID)); err != nil {
			return err
		}
		deleted, err := s.repo.Delete(ctx, id)
		if err == nil && !deleted {
			err = apperr.NotFound("Integration", id.String())
		}
		return err
	}()
	e := audit.Event{Action: audit.ActionDeleted, TargetType: audit.TargetIntegration, TargetID: id.String(), ActorType: audit.ActorUser, Status: audit.StatusSuccess}
	if org != nil {
		e.OrganizationID = org.String()
	}
	if before != nil {
		e.OldObject = before.view()
	}
	if err != nil {
		e.Status, e.OldObject = audit.StatusFailure, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
	return err
}

// ValidateGoogleSheets is forma2's validateGoogleSheetsConnectionAction: workspace.write; false without an integration, otherwise the credential is authorized (and refreshed when it needs to be), and a credential that cannot be is invalid_grant, which the page answers with its reconnect banner.
func (s *Service) ValidateGoogleSheets(ctx context.Context, userID, workspaceID uuid.UUID) (bool, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(workspaceID)); err != nil {
		return false, err
	}
	in, err := s.repo.ByType(ctx, workspaceID, TypeGoogleSheets)
	if err != nil || in == nil {
		return false, err
	}
	if _, err := s.tokens.google(ctx, in); err != nil {
		return false, googleError(err)
	}
	return true, nil
}

// SpreadsheetName is forma2's getSpreadsheetNameByIdAction: workspace.write, the workspace's own Google Sheets integration (404 without one), then the spreadsheet's title.
func (s *Service) SpreadsheetName(ctx context.Context, userID, workspaceID uuid.UUID, spreadsheetID string) (string, error) {
	in, err := s.integrationOf(ctx, userID, workspaceID, TypeGoogleSheets, authz.WorkspaceWrite)
	if err != nil {
		return "", err
	}
	token, err := s.tokens.google(ctx, in)
	if err != nil {
		return "", googleError(err)
	}
	name, err := s.p.spreadsheetName(ctx, token, spreadsheetID)
	if err != nil {
		return "", googleError(err)
	}
	return name, nil
}

// AirtableBases is what forma2's Airtable page loaded, getAirtableTables: workspace.write and the bases the connected account can see, with the token refreshed when it needs to be. A failure is the page's cue to offer reconnecting.
func (s *Service) AirtableBases(ctx context.Context, userID, workspaceID uuid.UUID) ([]Item, error) {
	in, err := s.integrationOf(ctx, userID, workspaceID, TypeAirtable, authz.WorkspaceWrite)
	if err != nil {
		return nil, err
	}
	k, err := s.tokens.airtable(ctx, in)
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to load Airtable bases — token may be expired or revoked", "err", err, "workspace_id", workspaceID)
		return nil, providerError(err)
	}
	bases, err := s.p.airtableBases(ctx, k.AccessToken)
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to load Airtable bases — token may be expired or revoked", "err", err, "workspace_id", workspaceID)
		return nil, providerError(err)
	}
	return bases, nil
}

// AirtableTables is forma2's GET /api/v1/integrations/airtable/tables: workspace.read, the workspace's Airtable integration (404 without one), and the base's tables with the token refreshed when it needs to be.
func (s *Service) AirtableTables(ctx context.Context, userID, workspaceID uuid.UUID, baseID string) ([]Item, error) {
	in, err := s.integrationOf(ctx, userID, workspaceID, TypeAirtable, authz.WorkspaceRead)
	if err != nil {
		return nil, err
	}
	k, err := s.tokens.airtable(ctx, in)
	if err != nil {
		return nil, providerError(err)
	}
	tables, err := s.p.airtableTables(ctx, k.AccessToken, baseID)
	if err != nil {
		return nil, providerError(err)
	}
	out := make([]Item, len(tables))
	for i, t := range tables {
		out[i] = Item{ID: t.ID, Name: t.Name}
	}
	return out, nil
}

// NotionDatabases is what forma2's Notion page loaded, getNotionDatabases: workspace.write and the databases the connection was given.
func (s *Service) NotionDatabases(ctx context.Context, userID, workspaceID uuid.UUID) ([]NotionDatabase, error) {
	in, err := s.integrationOf(ctx, userID, workspaceID, TypeNotion, authz.WorkspaceWrite)
	if err != nil {
		return nil, err
	}
	k, ok := key[notionKey](in)
	if !ok || k.BotID == "" {
		return []NotionDatabase{}, nil
	}
	dbs, err := s.p.notionDatabases(ctx, k.AccessToken)
	if err != nil {
		return nil, providerError(err)
	}
	return dbs, nil
}

// SlackChannels is forma2's getSlackChannelsAction: workspace.write and the channels the bot is in. A token Slack reports as expired (a Slack app with token rotation switched on, which forma2 never refreshed) deletes the integration, so the page offers to connect again, as forma2 did.
func (s *Service) SlackChannels(ctx context.Context, userID, workspaceID uuid.UUID) ([]Item, error) {
	in, err := s.integrationOf(ctx, userID, workspaceID, TypeSlack, authz.WorkspaceWrite)
	if err != nil {
		return nil, err
	}
	k, ok := key[SlackKey](in)
	if !ok {
		return []Item{}, nil
	}
	channels, err := s.p.slackChannels(ctx, k.AccessToken)
	if err != nil {
		var refused *slackRefusal
		if errors.As(err, &refused) && refused.code == "token_expired" {
			if _, delErr := s.repo.Delete(ctx, in.ID); delErr != nil {
				return nil, fmt.Errorf("delete expired slack integration: %w", delErr)
			}
		}
		return nil, providerError(err)
	}
	return channels, nil
}
