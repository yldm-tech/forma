package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/langcode"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// Workspace survey languages, forma2's apps/web/lib/language/service.ts behind the multi-language actions: owners, managers and teams with manage access maintain them, each change behind the state-mutation limit.

type languageRow struct {
	ID          uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Code        string
	Alias       sql.NullString
	WorkspaceID uuid.UUID
}

func (l languageRow) language() *Language {
	out := &Language{ID: l.ID, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt, Code: l.Code, WorkspaceID: l.WorkspaceID}
	if l.Alias.Valid {
		out.Alias = &l.Alias.String
	}
	return out
}

const languageColumns = `id, created_at, updated_at, code, alias, workspace_id`

func (r *Repository) scanLanguage(ctx context.Context, query string, args ...any) (*Language, error) {
	var rows []languageRow
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].language(), nil
}

// languages lists the workspace's languages, oldest first.
func (r *Repository) languages(ctx context.Context, workspaceID uuid.UUID) ([]*Language, error) {
	var rows []languageRow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+languageColumns+` FROM languages WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load languages: %w", err)
	}
	out := make([]*Language, len(rows))
	for i, row := range rows {
		out[i] = row.language()
	}
	return out, nil
}

// language returns a language of the workspace, or nil when the workspace has none with that id.
func (r *Repository) language(ctx context.Context, workspaceID, id uuid.UUID) (*Language, error) {
	l, err := r.scanLanguage(ctx, `SELECT `+languageColumns+` FROM languages WHERE id = ? AND workspace_id = ?`, id, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("load language: %w", err)
	}
	return l, nil
}

func (r *Repository) createLanguage(ctx context.Context, workspaceID uuid.UUID, code string, alias *string) (*Language, error) {
	l, err := r.scanLanguage(ctx, `INSERT INTO languages (workspace_id, code, alias) VALUES (?, ?, ?) RETURNING `+languageColumns, workspaceID, code, alias)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "languages_workspace_id_code_key" {
		return nil, apperr.Conflict("Language " + code + " already exists in this workspace")
	}
	if err != nil {
		return nil, fmt.Errorf("create language: %w", err)
	}
	return l, nil
}

// updateLanguageAlias writes the alias only: a language's code is immutable, so it stays canonical whatever the caller sends.
func (r *Repository) updateLanguageAlias(ctx context.Context, workspaceID, id uuid.UUID, alias *string) (*Language, error) {
	l, err := r.scanLanguage(ctx, `UPDATE languages SET alias = ?, updated_at = now() WHERE id = ? AND workspace_id = ? RETURNING `+languageColumns, alias, id, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("update language: %w", err)
	}
	return l, nil
}

func (r *Repository) deleteLanguage(ctx context.Context, workspaceID, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM languages WHERE id = ? AND workspace_id = ?`, id, workspaceID).Error; err != nil {
		return fmt.Errorf("delete language: %w", err)
	}
	return nil
}

// surveysUsingLanguage lists the names of the workspace's surveys that have the language, oldest first.
func (r *Repository) surveysUsingLanguage(ctx context.Context, workspaceID, id uuid.UUID) ([]string, error) {
	names := []string{}
	err := r.db.WithContext(ctx).Raw(
		`SELECT s.name FROM survey_languages sl JOIN surveys s ON s.id = sl.survey_id WHERE sl.language_id = ? AND s.workspace_id = ? ORDER BY s.created_at, s.id`, id, workspaceID,
	).Scan(&names).Error
	if err != nil {
		return nil, fmt.Errorf("list surveys using language: %w", err)
	}
	return names, nil
}

// recordLanguage emits forma2's audit event for a language change, a failure included.
func (s *Service) recordLanguage(ctx context.Context, action audit.Action, id uuid.UUID, ws *Workspace, err error, oldObject, newObject any) {
	e := audit.Event{Action: action, TargetType: audit.TargetLanguage, ActorType: audit.ActorUser, Status: audit.StatusSuccess, OldObject: oldObject, NewObject: newObject}
	if id != uuid.Nil {
		e.TargetID = id.String()
	}
	if ws != nil {
		e.OrganizationID = ws.OrganizationID.String()
	}
	if err != nil {
		e.Status, e.OldObject, e.NewObject = audit.StatusFailure, nil, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// manageLanguages authorizes a language change (workspace.manage), applies the state-mutation limit and loads the workspace.
func (s *Service) manageLanguages(ctx context.Context, userID, workspaceID uuid.UUID) (*Workspace, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceManage, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, workspaceID.String()); err != nil {
		return nil, err
	}
	ws, err := s.repo.byID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, apperr.NotFound("Workspace", "")
	}
	return ws, nil
}

func languageValidation(message string) *apperr.Error {
	return apperr.InvalidInput(apperr.CodeValidation, message)
}

// comparableLanguage is a code or alias as the language rules compare it: its canonical tag when it names a language, else itself, lowercased either way.
func comparableLanguage(value string) string {
	trimmed := strings.TrimSpace(value)
	if canonical := langcode.Normalize(trimmed); canonical != "" {
		return strings.ToLower(canonical)
	}
	return strings.ToLower(trimmed)
}

// baseLanguage is the base language (primary subtag) a code or alias names, or "" when it names none.
func baseLanguage(value string) string {
	canonical := langcode.Normalize(value)
	if canonical == "" {
		return ""
	}
	base, _, _ := strings.Cut(canonical, "-")
	return strings.ToLower(base)
}

// trimmedAlias is a language's alias without surrounding spaces, "" when it has none.
func trimmedAlias(alias *string) string {
	if alias == nil {
		return ""
	}
	return strings.TrimSpace(*alias)
}

// aliasNames tells whether alias, on the language with code owner, names the workspace's language code instead: it is that code, the same canonical language, or a code of its base language while the owner's base language differs (ja on de-DE names ja-JP).
func aliasNames(alias, owner, code string) bool {
	base := baseLanguage(alias)
	return strings.EqualFold(alias, code) || comparableLanguage(alias) == comparableLanguage(code) || (base != "" && base != baseLanguage(owner) && base == baseLanguage(code))
}

// aliasProblem is the workspace languages form's rule for an alias (apps/web/src/lib/workspace-languages.ts), checked for the language with code and alias against the workspace's other languages: an alias is used once, and it may name its own language (ja for ja-JP, zh for zh-Hans-CN) or no language at all, but not another language of the workspace, which `?lang=<alias>` would then not open, nor a language the workspace does not have.
func aliasProblem(code, alias string, others []*Language) *apperr.Error {
	if alias == "" {
		return nil
	}
	for _, other := range others {
		if strings.EqualFold(trimmedAlias(other.Alias), alias) {
			return languageValidation(fmt.Sprintf("The alias '%s' of %s is already the alias of %s. Each alias can be used once.", alias, code, other.Code))
		}
		if aliasNames(alias, code, other.Code) {
			return languageValidation(fmt.Sprintf("The alias '%s' of %s names %s, another language of this workspace. Choose an alias that is not another language's code.", alias, code, other.Code))
		}
	}
	if base := baseLanguage(alias); base != "" && base != baseLanguage(code) {
		return languageValidation(fmt.Sprintf("The alias '%s' of %s is the code of %s, which this workspace does not have. Add that language, or choose an alias that is not a language code.", alias, code, langcode.Normalize(alias)))
	}
	return nil
}

// newLanguageProblem checks a language about to be added against the workspace's languages: it must not be one of them by canonical code, its alias must keep the alias rule, and no alias already stored may name it.
func newLanguageProblem(code, alias string, existing []*Language) *apperr.Error {
	for _, other := range existing {
		if comparableLanguage(other.Code) == comparableLanguage(code) {
			return apperr.Conflict("Language " + code + " already exists in this workspace")
		}
	}
	if err := aliasProblem(code, alias, existing); err != nil {
		return err
	}
	for _, other := range existing {
		if stored := trimmedAlias(other.Alias); stored != "" && aliasNames(stored, other.Code, code) {
			return languageValidation(fmt.Sprintf("The alias '%s' of %s names %s. Change that alias before adding %s.", stored, other.Code, code, code))
		}
	}
	return nil
}

// CreateLanguage is forma2's createLanguageAction: the code is stored in its canonical form and must be one of the curated languages; an alias is kept as given.
func (s *Service) CreateLanguage(ctx context.Context, userID, workspaceID uuid.UUID, code string, alias *string) (*Language, error) {
	ws, err := s.manageLanguages(ctx, userID, workspaceID)
	var l *Language
	if err == nil {
		l, err = s.createLanguage(ctx, workspaceID, code, alias)
	}
	id := uuid.Nil
	if l != nil {
		id = l.ID
	}
	s.recordLanguage(ctx, audit.ActionCreated, id, ws, err, nil, l)
	if err == nil && ws != nil {
		org := ws.OrganizationID.String()
		analytics.Capture(userID.String(), "workspace_language_created", analytics.Properties{
			"organization_id": org,
			"workspace_id":    workspaceID.String(),
			"language_code":   l.Code,
		}, analytics.Groups{OrganizationID: org, WorkspaceID: workspaceID.String()})
	}
	return l, err
}

func (s *Service) createLanguage(ctx context.Context, workspaceID uuid.UUID, code string, alias *string) (*Language, error) {
	if code == "" {
		return nil, languageValidation("Language code is required")
	}
	canonical := langcode.Normalize(code)
	if canonical == "" {
		return nil, languageValidation(fmt.Sprintf("Invalid language code: '%s'", code))
	}
	if !langcode.IsCanonical(canonical) {
		return nil, languageValidation(fmt.Sprintf("Unsupported language code: '%s'", code))
	}
	var l *Language
	err := s.repo.transaction(ctx, func(tx *Repository) error {
		existing, err := tx.lockedLanguages(ctx, workspaceID)
		if err != nil {
			return err
		}
		if problem := newLanguageProblem(canonical, trimmedAlias(alias), existing); problem != nil {
			return problem
		}
		l, err = tx.createLanguage(ctx, workspaceID, canonical, alias)
		return err
	})
	return l, err
}

// lockedLanguages locks the workspace row and lists its languages, so concurrent changes to them (the languages form saves its rows in parallel) are checked one after another.
func (r *Repository) lockedLanguages(ctx context.Context, workspaceID uuid.UUID) ([]*Language, error) {
	ws, err := r.lockByID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, apperr.NotFound("Workspace", "")
	}
	return r.languages(ctx, workspaceID)
}

// UpdateLanguage is forma2's updateLanguageAction: only the alias changes. A language of another workspace is not found. A changed alias must keep the alias rule (aliasProblem); an unchanged one is written back as it is, so a stored alias an older rule let in never blocks a save.
func (s *Service) UpdateLanguage(ctx context.Context, userID, workspaceID, languageID uuid.UUID, alias *string) (*Language, error) {
	ws, err := s.manageLanguages(ctx, userID, workspaceID)
	var old, updated *Language
	if err == nil {
		err = s.repo.transaction(ctx, func(tx *Repository) error {
			existing, err := tx.lockedLanguages(ctx, workspaceID)
			if err != nil {
				return err
			}
			var others []*Language
			for _, l := range existing {
				if l.ID == languageID {
					old = l
				} else {
					others = append(others, l)
				}
			}
			if old == nil {
				return apperr.NotFound("Language", languageID.String())
			}
			if trimmedAlias(alias) != trimmedAlias(old.Alias) {
				if problem := aliasProblem(old.Code, trimmedAlias(alias), others); problem != nil {
					return problem
				}
			}
			updated, err = tx.updateLanguageAlias(ctx, workspaceID, languageID, alias)
			if err == nil && updated == nil {
				err = apperr.NotFound("Language", languageID.String())
			}
			return err
		})
	}
	s.recordLanguage(ctx, audit.ActionUpdated, languageID, ws, err, old, updated)
	return updated, err
}

// DeleteLanguage is forma2's deleteLanguageAction: the language must be the workspace's own and must not be its default survey language. The workspace row is locked first, so a concurrent settings update cannot make the language the default between the check and the delete. The surveys' use of the language goes with it.
func (s *Service) DeleteLanguage(ctx context.Context, userID, workspaceID, languageID uuid.UUID) (*Language, error) {
	ws, err := s.manageLanguages(ctx, userID, workspaceID)
	var deleted *Language
	if err == nil {
		err = s.repo.transaction(ctx, func(tx *Repository) error {
			locked, err := tx.lockByID(ctx, workspaceID)
			if err != nil {
				return err
			}
			if locked == nil {
				return apperr.NotFound("Workspace", "")
			}
			l, err := tx.language(ctx, workspaceID, languageID)
			if err != nil {
				return err
			}
			if l == nil {
				return apperr.NotFound("Language", languageID.String())
			}
			if isDefaultSurveyLanguage(l.Code, locked.Config) {
				return apperr.OperationNotAllowed("This language is the workspace's default survey language and cannot be removed")
			}
			if err := tx.deleteLanguage(ctx, workspaceID, languageID); err != nil {
				return err
			}
			deleted = l
			return nil
		})
	}
	s.recordLanguage(ctx, audit.ActionDeleted, languageID, ws, err, deleted, nil)
	if err != nil {
		return nil, err
	}
	return deleted, nil
}

// isDefaultSurveyLanguage is forma2's isWorkspaceDefaultSurveyLanguage: compared on canonical codes, so a legacy "de" row is the "de-DE" default while es-MX and es-ES stay different languages.
func isDefaultSurveyLanguage(code string, config json.RawMessage) bool {
	var cfg struct {
		DefaultSurveyLanguage *string `json:"defaultSurveyLanguage"`
	}
	if json.Unmarshal(config, &cfg) != nil || cfg.DefaultSurveyLanguage == nil {
		return false
	}
	want := langcode.Normalize(*cfg.DefaultSurveyLanguage)
	return want != "" && langcode.Normalize(code) == want
}

// SurveysUsingLanguage is forma2's getSurveysUsingGivenLanguageAction (workspace.manage): the names of the surveys that would lose the language, which the settings page shows before refusing to remove it.
func (s *Service) SurveysUsingLanguage(ctx context.Context, userID, workspaceID, languageID uuid.UUID) ([]string, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceManage, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	l, err := s.repo.language(ctx, workspaceID, languageID)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, apperr.NotFound("Language", languageID.String())
	}
	return s.repo.surveysUsingLanguage(ctx, workspaceID, languageID)
}

// AppConnection is what the app-connection settings page shows: the workspace id an SDK snippet is set up with, the server the snippet points at, and whether an SDK has fetched its environment yet.
type AppConnection struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	AppURL      string    `json:"appUrl"`
	// AppSetupCompleted flips once, when an SDK first fetches the workspace's environment state.
	AppSetupCompleted bool `json:"appSetupCompleted"`
}

// AppConnection is forma2's app-connection page data, behind its getWorkspaceAuth (workspace.read).
func (s *Service) AppConnection(ctx context.Context, userID, workspaceID uuid.UUID) (*AppConnection, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	ws, err := s.repo.byID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, apperr.NotFound("Workspace", "")
	}
	return &AppConnection{WorkspaceID: ws.ID, AppURL: appURL(s.cfg.WebAppURL.String()), AppSetupCompleted: ws.AppSetupCompleted}, nil
}

// appURL is WEBAPP_URL as the snippets embed it, without a trailing slash.
func appURL(webAppURL string) string {
	return strings.TrimRight(webAppURL, "/")
}
