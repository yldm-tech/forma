package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

// Repository reads and writes workspaces and the rows a new workspace starts with. Every query names the organization or workspace it is scoped to.
type Repository struct {
	db  *gorm.DB
	log *slog.Logger
}

func NewRepository(db *gorm.DB, log *slog.Logger) *Repository {
	return &Repository{db: db, log: log}
}

// WithTx binds the repository to a transaction the caller runs, so a workspace can be created in the same transaction as its organization.
func (r *Repository) WithTx(tx *gorm.DB) *Repository {
	return &Repository{db: tx, log: r.log}
}

func (r *Repository) transaction(ctx context.Context, fn func(tx *Repository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(r.WithTx(tx))
	})
}

const workspaceColumns = `w.id, w.created_at, w.updated_at, w.legacy_environment_id, w.name, w.organization_id, w.styling, w.recontact_days,
	w.link_survey_branding, w.in_app_survey_branding, w.config, w.placement, w.click_outside_close, w.overlay, w.logo,
	w.app_setup_completed, w.custom_head_scripts`

type workspaceRow struct {
	ID                  uuid.UUID
	CreatedAt           time.Time
	UpdatedAt           time.Time
	LegacyEnvironmentID sql.NullString
	Name                string
	OrganizationID      uuid.UUID
	Styling             []byte
	RecontactDays       int
	LinkSurveyBranding  bool
	InAppSurveyBranding bool
	Config              []byte
	Placement           string
	ClickOutsideClose   bool
	Overlay             string
	Logo                []byte
	AppSetupCompleted   bool
	CustomHeadScripts   sql.NullString
}

func (w workspaceRow) workspace(languages []Language) *Workspace {
	out := &Workspace{
		ID: w.ID, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt, Name: w.Name, OrganizationID: w.OrganizationID,
		Styling: w.Styling, RecontactDays: w.RecontactDays, InAppSurveyBranding: w.InAppSurveyBranding, LinkSurveyBranding: w.LinkSurveyBranding,
		Config: w.Config, Placement: w.Placement, ClickOutsideClose: w.ClickOutsideClose, Overlay: w.Overlay, Languages: languages,
		AppSetupCompleted: w.AppSetupCompleted, Logo: w.Logo,
	}
	if w.LegacyEnvironmentID.Valid {
		out.LegacyEnvironmentID = &w.LegacyEnvironmentID.String
	}
	if w.CustomHeadScripts.Valid {
		out.CustomHeadScripts = &w.CustomHeadScripts.String
	}
	if out.Languages == nil {
		out.Languages = []Language{}
	}
	return out
}

// find loads the workspaces a query selects, with their languages, in the query's order.
func (r *Repository) find(ctx context.Context, query string, args ...any) ([]*Workspace, error) {
	var rows []workspaceRow
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load workspaces: %w", err)
	}
	if len(rows) == 0 {
		return []*Workspace{}, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, w := range rows {
		ids[i] = w.ID
	}
	var languages []struct {
		ID          uuid.UUID
		CreatedAt   time.Time
		UpdatedAt   time.Time
		Code        string
		Alias       sql.NullString
		WorkspaceID uuid.UUID
	}
	if err := r.db.WithContext(ctx).Raw(
		`SELECT id, created_at, updated_at, code, alias, workspace_id FROM languages WHERE workspace_id IN ? ORDER BY created_at, id`, ids,
	).Scan(&languages).Error; err != nil {
		return nil, fmt.Errorf("load workspace languages: %w", err)
	}
	byWorkspace := map[uuid.UUID][]Language{}
	for _, l := range languages {
		lang := Language{ID: l.ID, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt, Code: l.Code, WorkspaceID: l.WorkspaceID}
		if l.Alias.Valid {
			lang.Alias = &l.Alias.String
		}
		byWorkspace[l.WorkspaceID] = append(byWorkspace[l.WorkspaceID], lang)
	}
	out := make([]*Workspace, len(rows))
	for i, w := range rows {
		out[i] = w.workspace(byWorkspace[w.ID])
	}
	return out, nil
}

func (r *Repository) findOne(ctx context.Context, query string, args ...any) (*Workspace, error) {
	list, err := r.find(ctx, query, args...)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list[0], nil
}

// byID returns nil when the workspace does not exist.
func (r *Repository) byID(ctx context.Context, id uuid.UUID) (*Workspace, error) {
	return r.findOne(ctx, `SELECT `+workspaceColumns+` FROM workspaces w WHERE w.id = ?`, id)
}

// lockByID reads the workspace for update inside a transaction, so concurrent settings writes serialize and a config merge never loses a key.
func (r *Repository) lockByID(ctx context.Context, id uuid.UUID) (*Workspace, error) {
	return r.findOne(ctx, `SELECT `+workspaceColumns+` FROM workspaces w WHERE w.id = ? FOR NO KEY UPDATE`, id)
}

// page appends forma2's take/skip pagination; page 0 means every row.
func page(query string, n int) string {
	if n < 1 {
		return query
	}
	return query + fmt.Sprintf(` LIMIT %d OFFSET %d`, ItemsPerPage, ItemsPerPage*(n-1))
}

// inOrganization lists the organization's workspaces among ids, oldest first with the id as tie-break: callers treat the first as "the" workspace and paginate, which needs a total order.
func (r *Repository) inOrganization(ctx context.Context, organizationID uuid.UUID, ids []uuid.UUID, pageNumber int) ([]*Workspace, error) {
	if len(ids) == 0 {
		return []*Workspace{}, nil
	}
	return r.find(ctx, page(`SELECT `+workspaceColumns+` FROM workspaces w WHERE w.organization_id = ? AND w.id IN ? ORDER BY w.created_at, w.id`, pageNumber), organizationID, ids)
}

// ofOrganization lists every workspace of the organization, oldest first.
func (r *Repository) ofOrganization(ctx context.Context, organizationID uuid.UUID) ([]*Workspace, error) {
	return r.find(ctx, `SELECT `+workspaceColumns+` FROM workspaces w WHERE w.organization_id = ? ORDER BY w.created_at, w.id`, organizationID)
}

// idsInOrganizations keeps the ids that belong to one of the organizations, ordered by the organizations' order and then oldest first.
func (r *Repository) idsInOrganizations(ctx context.Context, organizationIDs, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(organizationIDs) == 0 || len(ids) == 0 {
		return []uuid.UUID{}, nil
	}
	var rows []struct {
		ID             uuid.UUID
		OrganizationID uuid.UUID
	}
	if err := r.db.WithContext(ctx).Raw(
		`SELECT id, organization_id FROM workspaces WHERE organization_id IN ? AND id IN ? ORDER BY created_at, id`, organizationIDs, ids,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("filter workspaces: %w", err)
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, org := range organizationIDs {
		for _, row := range rows {
			if row.OrganizationID == org {
				out = append(out, row.ID)
			}
		}
	}
	return out, nil
}

// organizationOf returns the organization a workspace belongs to, or nil when it does not exist.
func (r *Repository) organizationOf(ctx context.Context, id uuid.UUID) (*uuid.UUID, error) {
	var orgs []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT organization_id FROM workspaces WHERE id = ?`, id).Scan(&orgs).Error; err != nil {
		return nil, fmt.Errorf("resolve workspace organization: %w", err)
	}
	if len(orgs) == 0 {
		return nil, nil
	}
	return &orgs[0], nil
}

// byEnvironmentID resolves an id the way forma2's resolver did: a workspace id, or the environment id an SDK snippet installed before the workspace rename still sends. It returns nil when neither matches.
func (r *Repository) byEnvironmentID(ctx context.Context, environmentID string) (*struct{ ID, OrganizationID uuid.UUID }, error) {
	var rows []struct{ ID, OrganizationID uuid.UUID }
	db := r.db.WithContext(ctx)
	var err error
	if id, parseErr := uuid.Parse(environmentID); parseErr == nil {
		err = db.Raw(`SELECT id, organization_id FROM workspaces WHERE id = ? OR legacy_environment_id = ? ORDER BY id = ? DESC LIMIT 1`, id, environmentID, id).Scan(&rows).Error
	} else {
		err = db.Raw(`SELECT id, organization_id FROM workspaces WHERE legacy_environment_id = ?`, environmentID).Scan(&rows).Error
	}
	if err != nil {
		return nil, fmt.Errorf("resolve environment id: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// ClientWorkspace is the workspace an id from outside resolved to.
type ClientWorkspace struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
}

// ResolveClientID is forma2's findWorkspaceByIdOrLegacyEnvId, for ids that arrive from outside (an SDK snippet, a stored /storage URL): the workspace with that id, where a forma2 id resolves to the workspace migrated from it, or else the one whose legacy environment id it is. It returns nil when nothing matches. The id is not authenticated: the caller authorizes whatever it reads next.
func (r *Repository) ResolveClientID(ctx context.Context, raw string) (*ClientWorkspace, error) {
	var rows []ClientWorkspace
	id, _ := legacyid.Resolve(raw)
	if err := r.db.WithContext(ctx).Raw(`SELECT id, organization_id FROM workspaces WHERE id = ? OR legacy_environment_id = ? ORDER BY id = ? DESC LIMIT 1`, id, raw, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve client id: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// membershipRole is the user's role in the organization, or "" when they are not a member.
func (r *Repository) membershipRole(ctx context.Context, userID, organizationID uuid.UUID) (string, error) {
	var roles []string
	if err := r.db.WithContext(ctx).Raw(`SELECT role FROM memberships WHERE user_id = ? AND organization_id = ?`, userID, organizationID).Scan(&roles).Error; err != nil {
		return "", fmt.Errorf("load membership: %w", err)
	}
	if len(roles) == 0 {
		return "", nil
	}
	return roles[0], nil
}

// surveyCount is the number of surveys in the workspace, the onboarding gate's measure.
func (r *Repository) surveyCount(ctx context.Context, workspaceID uuid.UUID) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM surveys WHERE workspace_id = ?`, workspaceID).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count surveys: %w", err)
	}
	return n, nil
}

// NewWorkspace is what a workspace is created from. Channel and Industry are nil for "not chosen".
type NewWorkspace struct {
	Name     string
	Channel  *string
	Industry *string
	TeamIDs  []uuid.UUID
}

// Create inserts a workspace with forma2's seeds (the default contact attribute keys and English as its survey language) and links the teams, which must be distinct and belong to the organization. The repository must be bound to a transaction (WithTx), so a rejected team or a duplicate name leaves nothing behind.
func (r *Repository) Create(ctx context.Context, organizationID uuid.UUID, in NewWorkspace) (uuid.UUID, error) {
	db := r.db.WithContext(ctx)
	if len(in.TeamIDs) > 0 {
		if err := r.checkTeams(ctx, organizationID, in.TeamIDs); err != nil {
			return uuid.Nil, err
		}
	}
	var id uuid.UUID
	err := db.Raw(
		`INSERT INTO workspaces (organization_id, name, config) VALUES (?, ?, jsonb_build_object('channel', ?::text, 'industry', ?::text)) RETURNING id`,
		organizationID, in.Name, in.Channel, in.Industry,
	).Row().Scan(&id)
	if err != nil {
		if isUniqueViolation(err, "workspaces_organization_id_name_key") {
			return uuid.Nil, errNameTaken()
		}
		return uuid.Nil, fmt.Errorf("create workspace: %w", err)
	}
	for _, k := range defaultContactAttributeKeys {
		if err := db.Exec(
			`INSERT INTO contact_attribute_keys (workspace_id, key, name, description, type, is_unique) VALUES (?, ?, ?, ?, 'default', ?)`,
			id, k.key, k.name, k.description, k.unique,
		).Error; err != nil {
			return uuid.Nil, fmt.Errorf("create contact attribute key %s: %w", k.key, err)
		}
	}
	if err := db.Exec(`INSERT INTO languages (workspace_id, code, alias) VALUES (?, ?, NULL)`, id, defaultLanguage).Error; err != nil {
		return uuid.Nil, fmt.Errorf("create workspace language: %w", err)
	}
	for _, team := range in.TeamIDs {
		if err := db.Exec(`INSERT INTO workspace_teams (workspace_id, team_id) VALUES (?, ?)`, id, team).Error; err != nil {
			return uuid.Nil, fmt.Errorf("link workspace team: %w", err)
		}
	}
	return id, nil
}

// checkTeams is forma2's ENG-1922 guard: a caller-supplied team must belong to the organization, or the workspace would be linked across tenants. A foreign team and a missing one fail alike, so the answer reveals nothing about other organizations; the rejected ids are logged for security observability, ids only. The teams are share-locked so none leaves the organization before the links commit.
func (r *Repository) checkTeams(ctx context.Context, organizationID uuid.UUID, teamIDs []uuid.UUID) error {
	unique := slices.Clone(teamIDs)
	slices.SortFunc(unique, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	if len(slices.Compact(unique)) != len(teamIDs) {
		return apperr.InvalidInput(apperr.CodeValidation, "teamIds must be unique")
	}
	var found []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT id FROM teams WHERE id IN ? AND organization_id = ? FOR SHARE`, teamIDs, organizationID).Scan(&found).Error; err != nil {
		return fmt.Errorf("check workspace teams: %w", err)
	}
	if len(found) == len(teamIDs) {
		return nil
	}
	foreign := make([]string, 0, len(teamIDs)-len(found))
	for _, id := range teamIDs {
		if !slices.Contains(found, id) {
			foreign = append(foreign, id.String())
		}
	}
	r.log.WarnContext(ctx, "Rejected cross-organization team assignment on workspace creation", "organizationId", organizationID.String(), "foreignTeamIds", foreign)
	return apperr.InvalidInput(apperr.CodeValidation, "teamIds must belong to the organization")
}

// column is one assignment of an update; expr is the placeholder with any cast the column needs.
type column struct {
	name  string
	expr  string
	value any
}

// update writes the given columns of one workspace. Column names are constants chosen by the service.
func (r *Repository) update(ctx context.Context, id uuid.UUID, columns []column) error {
	if len(columns) == 0 {
		return nil
	}
	sets := make([]string, 0, len(columns)+1)
	args := make([]any, 0, len(columns)+1)
	for _, c := range columns {
		sets = append(sets, c.name+" = "+c.expr)
		args = append(args, c.value)
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	err := r.db.WithContext(ctx).Exec(`UPDATE workspaces SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...).Error
	if isUniqueViolation(err, "workspaces_organization_id_name_key") {
		return errNameTaken()
	}
	if err != nil {
		return fmt.Errorf("update workspace: %w", err)
	}
	return nil
}

// deleteIfNotLast is forma2's deleteWorkspaceIfNotLast: every workspace of the organization is locked in a stable order, so concurrent deletions serialize and the second sees the first before it evaluates the guard. It returns the deleted workspace.
func (r *Repository) deleteIfNotLast(ctx context.Context, id, organizationID uuid.UUID) (*Workspace, error) {
	var deleted *Workspace
	err := r.transaction(ctx, func(tx *Repository) error {
		var ids []uuid.UUID
		if err := tx.db.WithContext(ctx).Raw(`SELECT id FROM workspaces WHERE organization_id = ? ORDER BY id FOR UPDATE`, organizationID).Scan(&ids).Error; err != nil {
			return fmt.Errorf("lock organization workspaces: %w", err)
		}
		if !slices.Contains(ids, id) {
			return apperr.NotFound("workspace", id.String())
		}
		if len(ids) <= 1 {
			return apperr.OperationNotAllowed("You can't delete the last workspace.")
		}
		var err error
		if deleted, err = tx.byID(ctx, id); err != nil {
			return err
		}
		if err := tx.db.WithContext(ctx).Exec(`DELETE FROM workspaces WHERE id = ? AND organization_id = ?`, id, organizationID).Error; err != nil {
			return fmt.Errorf("delete workspace: %w", err)
		}
		return nil
	})
	return deleted, err
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

// CodeNameTaken is the error code of a workspace name already used in the organization.
const CodeNameTaken = "workspace_name_taken"

func errNameTaken() *apperr.Error {
	return apperr.InvalidInput(CodeNameTaken, "A workspace with this name already exists in your organization")
}
