package linksurvey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// cacheTTL is how long a link survey may serve a workspace's branding, or a welcome card its response count, after it changed: forma2's WORKSPACE_CONTEXT_CACHE_TTL_MS and RESPONSE_COUNT_CACHE_TTL_MS. Nothing invalidates either key, so the bound is the whole guarantee.
const cacheTTL = time.Minute

// Repository reads what the public survey pages show. Every read names the survey, workspace or contact it is scoped to.
type Repository struct {
	db      *gorm.DB
	surveys *survey.Repository
	cache   *redis.Client
	log     *slog.Logger
}

func NewRepository(db *gorm.DB, surveys *survey.Repository, cache *redis.Client, log *slog.Logger) *Repository {
	return &Repository{db: db, surveys: surveys, cache: cache, log: log}
}

// surveyByID loads a survey with its languages and its Embedded Data fields, or nil when there is none.
func (r *Repository) surveyByID(ctx context.Context, id uuid.UUID) (*survey.Survey, []embeddeddata.LinkedField, error) {
	sv, err := r.surveys.ByID(ctx, id)
	if err != nil || sv == nil {
		return nil, nil, err
	}
	fields, err := embeddeddata.SurveyFields(ctx, r.db, sv.ID)
	if err != nil {
		return nil, nil, err
	}
	return sv, fields, nil
}

// surveyIDBySlug is forma2's getSurveyBySlug: slugs are unique across the instance.
func (r *Repository) surveyIDBySlug(ctx context.Context, slug string) (uuid.UUID, bool, error) {
	var ids []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT id FROM surveys WHERE slug = ?`, slug).Scan(&ids).Error; err != nil {
		return uuid.Nil, false, fmt.Errorf("find survey by slug: %w", err)
	}
	if len(ids) == 0 {
		return uuid.Nil, false, nil
	}
	return ids[0], true, nil
}

// workspaceContext is forma2's TWorkspaceContextForLinkSurvey: the workspace's branding and the organization's whitelabel settings.
type workspaceContext struct {
	Workspace      linkWorkspace   `json:"workspace"`
	OrganizationID uuid.UUID       `json:"organizationId"`
	Whitelabel     json.RawMessage `json:"whitelabel"`
}

type linkWorkspace struct {
	ID                 uuid.UUID       `json:"id"`
	Name               string          `json:"name"`
	Styling            json.RawMessage `json:"styling"`
	Logo               json.RawMessage `json:"logo"`
	LinkSurveyBranding bool            `json:"linkSurveyBranding"`
	CustomHeadScripts  *string         `json:"customHeadScripts"`
}

// whitelabel is the organization's whitelabel document, forma2's TOrganizationWhitelabel.
type whitelabel struct {
	LogoURL    string `json:"logoUrl"`
	FaviconURL string `json:"faviconUrl"`
}

func (w workspaceContext) whitelabel() whitelabel {
	var wl whitelabel
	if len(w.Whitelabel) > 0 {
		_ = json.Unmarshal(w.Whitelabel, &wl)
	}
	return wl
}

// errWorkspaceMissing is a survey whose workspace is gone; forma2 threw ResourceNotFoundError here.
var errWorkspaceMissing = errors.New("link survey workspace not found")

// workspaceContext is forma2's getWorkspaceContextForLinkSurvey, cached for a minute under forma2's key: everything in it is workspace-scoped, so one entry serves every respondent of every survey in the workspace. A missing workspace is never cached.
func (r *Repository) workspaceContext(ctx context.Context, workspaceID uuid.UUID) (workspaceContext, error) {
	key := "fb:env:" + workspaceID.String() + ":config"
	var wc workspaceContext
	if r.cached(ctx, key, &wc) {
		return wc, nil
	}
	var rows []struct {
		ID                 uuid.UUID
		Name               string
		Styling            []byte
		Logo               []byte
		LinkSurveyBranding bool
		CustomHeadScripts  *string
		OrganizationID     uuid.UUID
		Whitelabel         []byte
	}
	err := r.db.WithContext(ctx).Raw(`SELECT w.id, w.name, w.styling, w.logo, w.link_survey_branding, w.custom_head_scripts, w.organization_id, o.whitelabel
		FROM workspaces w JOIN organizations o ON o.id = w.organization_id WHERE w.id = ?`, workspaceID).Scan(&rows).Error
	if err != nil {
		return wc, fmt.Errorf("load link survey workspace: %w", err)
	}
	if len(rows) == 0 {
		return wc, errWorkspaceMissing
	}
	row := rows[0]
	wc = workspaceContext{
		Workspace: linkWorkspace{
			ID: row.ID, Name: row.Name, Styling: jsonOrNull(row.Styling), Logo: jsonOrNull(row.Logo),
			LinkSurveyBranding: row.LinkSurveyBranding, CustomHeadScripts: row.CustomHeadScripts,
		},
		OrganizationID: row.OrganizationID,
		Whitelabel:     jsonOrNull(row.Whitelabel),
	}
	r.store(ctx, key, wc)
	return wc, nil
}

// responseCount is the welcome card's response count, forma2's getResponseCountBySurveyId served from the cache for up to a minute: it is social proof, and nothing that gates behaviour may read it.
func (r *Repository) responseCount(ctx context.Context, surveyID uuid.UUID) (int, error) {
	key := "fb:response:" + surveyID.String() + ":count"
	var n int
	if r.cached(ctx, key, &n) {
		return n, nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM responses WHERE survey_id = ?`, surveyID).Scan(&count).Error; err != nil {
		return 0, fmt.Errorf("count survey responses: %w", err)
	}
	n = int(count)
	r.store(ctx, key, n)
	return n, nil
}

// existingResponse is a response's id, whether it is finished and, for the single-use lookup, the address its email verification recorded ("" when none).
type existingResponse struct {
	ID            uuid.UUID
	Finished      bool
	VerifiedEmail string
}

// responseBySingleUseID is forma2's getResponseBySingleUseId, never cached, with the address the response's email verification recorded.
func (r *Repository) responseBySingleUseID(ctx context.Context, surveyID uuid.UUID, singleUseID string) (*existingResponse, error) {
	return r.firstResponse(ctx, `SELECT id, finished, COALESCE(data->>'verifiedEmail', '') AS verified_email FROM responses WHERE survey_id = ? AND single_use_id = ? LIMIT 1`, surveyID, singleUseID)
}

// contactResponse is forma2's getExistingContactResponse, never cached.
func (r *Repository) contactResponse(ctx context.Context, surveyID, contactID uuid.UUID) (*existingResponse, error) {
	return r.firstResponse(ctx, `SELECT id, finished FROM responses WHERE survey_id = ? AND contact_id = ? ORDER BY created_at LIMIT 1`, surveyID, contactID)
}

// verifiedEmailResponded says whether the address has finished a response to the survey, never cached. It is the rule response's claimVerifiedEmail enforces when a response is created or finished: only finished responses count, and addresses compare case-insensitively with PostgreSQL's lower(). The query is written as the expression of the partial index responses_survey_id_verified_email_idx (migration 00025), which serves it.
func (r *Repository) verifiedEmailResponded(ctx context.Context, surveyID uuid.UUID, email string) (bool, error) {
	var responded bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM responses WHERE survey_id = ? AND lower(data->>'verifiedEmail') = lower(?) AND data->>'verifiedEmail' IS NOT NULL AND finished)`, surveyID, email).Scan(&responded).Error; err != nil {
		return false, fmt.Errorf("find response by verified email: %w", err)
	}
	return responded, nil
}

func (r *Repository) firstResponse(ctx context.Context, query string, args ...any) (*existingResponse, error) {
	var rows []existingResponse
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load survey response: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// cached reads a cache entry into dst. A miss, an unreachable cache or an undecodable value all read through to the database: like forma2's cache, it never fails the request.
func (r *Repository) cached(ctx context.Context, key string, dst any) bool {
	b, err := r.cache.Get(ctx, key).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			r.log.WarnContext(ctx, "link survey cache read failed", "key", key, "err", err)
		}
		return false
	}
	return json.Unmarshal(b, dst) == nil
}

func (r *Repository) store(ctx context.Context, key string, v any) {
	b, err := json.Marshal(v)
	if err == nil {
		err = r.cache.Set(ctx, key, b, cacheTTL).Err()
	}
	if err != nil {
		r.log.WarnContext(ctx, "link survey cache write failed", "key", key, "err", err)
	}
}

func jsonOrNull(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}
