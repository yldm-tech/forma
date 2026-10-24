package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/langcode"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/survey/scheduling"
)

// Service holds the workspace rules. Every method takes the acting user and authorizes through authz before it reads or writes anything a caller could not already see.
type Service struct {
	cfg     config.Config
	repo    *Repository
	authz   *authz.Authorizer
	limiter *ratelimit.Limiter
	audit   *audit.Emitter
	schemas *jsonschema.Validator
	files   FileStore
	log     *slog.Logger
}

// FileStore holds the files uploaded to workspaces. The storage module implements it; it cannot be imported here, because it resolves the workspace ids in its URLs through this package.
type FileStore interface {
	// DeleteWorkspaceFiles deletes every file stored under any of ids.
	DeleteWorkspaceFiles(ctx context.Context, ids ...string) error
}

func NewService(cfg config.Config, repo *Repository, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter, schemas *jsonschema.Validator, files FileStore, log *slog.Logger) *Service {
	return &Service{cfg: cfg, repo: repo, authz: authorizer, limiter: limiter, audit: emitter, schemas: schemas, files: files, log: log}
}

// record emits forma2's audit event for a workspace mutation, a failure included, as withAuditLogging did for every error its handler threw.
func (s *Service) record(ctx context.Context, action audit.Action, workspaceID, organizationID string, err error, oldObject, newObject any) {
	e := audit.Event{Action: action, TargetType: audit.TargetWorkspace, TargetID: workspaceID, OrganizationID: organizationID, ActorType: audit.ActorUser, Status: audit.StatusSuccess, OldObject: oldObject, NewObject: newObject}
	if err != nil {
		e.Status, e.OldObject, e.NewObject = audit.StatusFailure, nil, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// Create is forma2's createWorkspaceAction: owners and managers create workspaces, without a limit on their number (forma2's licence limit is infinite and the billing limit was never enforced). Teams named in the input are linked with read access.
func (s *Service) Create(ctx context.Context, userID, organizationID uuid.UUID, in NewWorkspace) (*Workspace, error) {
	ws, err := s.create(ctx, userID, organizationID, in)
	id := ""
	if ws != nil {
		id = ws.ID.String()
	}
	s.record(ctx, audit.ActionCreated, id, organizationID.String(), err, nil, ws)
	return ws, err
}

func (s *Service) create(ctx context.Context, userID, organizationID uuid.UUID, in NewWorkspace) (*Workspace, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationManage, authz.Organization(organizationID)); err != nil {
		return nil, err
	}
	var id uuid.UUID
	err := s.repo.transaction(ctx, func(tx *Repository) error {
		var err error
		id, err = tx.Create(ctx, organizationID, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	ws, err := s.repo.byID(ctx, id)
	if err == nil && ws != nil {
		analytics.WorkspaceCreated(userID.String(), organizationID.String(), ws.ID.String(), ws.Name)
	}
	return ws, err
}

// List is forma2's getUserWorkspaces: the organization's workspaces the user may read (or write, with writable), oldest first, 30 per page when a page is given. Writable lists are forma2's getWritableWorkspacesAction, gated on organization.read_access.
func (s *Service) List(ctx context.Context, userID, organizationID uuid.UUID, writable bool, pageNumber int) ([]*Workspace, error) {
	gate := authz.OrganizationRead
	if writable {
		gate = authz.OrganizationReadAccess
	}
	if err := s.authz.Require(ctx, authz.User(userID), gate, authz.Organization(organizationID)); err != nil {
		return nil, err
	}
	ids, err := s.authz.LookupWorkspaceIDs(ctx, authz.User(userID), writable)
	if err != nil {
		return nil, err
	}
	return s.repo.inOrganization(ctx, organizationID, ids, pageNumber)
}

// ReadableIDs lists the workspaces of the given organizations the user may read, in the organizations' order and oldest first within each.
func (s *Service) ReadableIDs(ctx context.Context, userID uuid.UUID, organizationIDs []uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.authz.LookupWorkspaceIDs(ctx, authz.User(userID), false)
	if err != nil {
		return nil, err
	}
	return s.repo.idsInOrganizations(ctx, organizationIDs, ids)
}

// Access is what the caller may do in a workspace, for the SPA to shape its pages. It is derived from the same decisions the endpoints enforce.
type Access struct {
	Role      string `json:"role"`
	CanWrite  bool   `json:"canWrite"`
	CanManage bool   `json:"canManage"`
	// IsReadOnly is forma2's fail-safe: a member is read-only unless a team grants them write or manage.
	IsReadOnly bool `json:"isReadOnly"`
}

// Detail is a workspace with the caller's access to it.
type Detail struct {
	*Workspace
	Access Access `json:"access"`
	// SurveyScheduling is when the workspace's scheduled surveys open and close, the zone of its organization or the instance's, for the survey editor.
	SurveyScheduling scheduling.Settings `json:"surveyScheduling"`
}

// CodeBillingRole is the error code of a billing-role member asking for workspace product data; Details.redirect is where forma2 sent them instead.
const CodeBillingRole = "billing_role"

// Get is forma2's getWorkspaceAuth for an API: workspace.read is required, so a missing workspace and a foreign one are refused alike. A billing member, whom forma2 redirected away from every product page, is refused with CodeBillingRole and the page to go to.
func (s *Service) Get(ctx context.Context, userID, workspaceID uuid.UUID) (*Detail, error) {
	actor := authz.User(userID)
	allowed, err := s.authz.Can(ctx, actor, authz.WorkspaceRead, authz.Workspace(workspaceID))
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, s.readDenied(ctx, userID, workspaceID)
	}
	ws, err := s.repo.byID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, apperr.NotFound("Workspace", "")
	}
	role, err := s.repo.membershipRole(ctx, userID, ws.OrganizationID)
	if err != nil {
		return nil, err
	}
	canWrite, err := s.authz.Can(ctx, actor, authz.WorkspaceWrite, authz.Workspace(workspaceID))
	if err != nil {
		return nil, err
	}
	canManage, err := s.authz.Can(ctx, actor, authz.WorkspaceManage, authz.Workspace(workspaceID))
	if err != nil {
		return nil, err
	}
	zone, err := s.repo.surveySchedulingTimeZone(ctx, ws.OrganizationID)
	if err != nil {
		return nil, err
	}
	return &Detail{Workspace: ws, Access: Access{Role: role, CanWrite: canWrite, CanManage: canManage, IsReadOnly: role == "member" && !canWrite && !canManage},
		SurveyScheduling: scheduling.Instance(s.cfg).ForOrganization(zone).Settings()}, nil
}

// readDenied explains a refused read to a billing member of the workspace's organization, and to anyone else is the plain refusal.
func (s *Service) readDenied(ctx context.Context, userID, workspaceID uuid.UUID) error {
	denied := apperr.Forbidden("Not authorized")
	org, err := s.repo.organizationOf(ctx, workspaceID)
	if err != nil {
		return err
	}
	if org == nil {
		return denied
	}
	member, err := s.authz.Can(ctx, authz.User(userID), authz.OrganizationRead, authz.Organization(*org))
	if err != nil {
		return err
	}
	if !member {
		return denied
	}
	role, err := s.repo.membershipRole(ctx, userID, *org)
	if err != nil {
		return err
	}
	if role != "billing" {
		return denied
	}
	return &apperr.Error{Status: denied.Status, Code: CodeBillingRole, Message: "Billing members cannot access workspaces", Details: map[string]any{"redirect": BillingPath(*org, s.cfg.IsFormaCloud)}}
}

// UpdateInput is a validated settings change; a nil field is left as it is. Config holds only the keys being changed and is merged onto the stored config.
type UpdateInput struct {
	Name                *string
	RecontactDays       *int
	InAppSurveyBranding *bool
	LinkSurveyBranding  *bool
	Placement           *string
	ClickOutsideClose   *bool
	Overlay             *string
	Config              json.RawMessage
	Styling             json.RawMessage
	Logo                json.RawMessage
	// CustomHeadScripts is set when the request names the field; a nil Value clears it.
	CustomHeadScripts *Optional[string]
}

// Optional is a field that may be absent, null or a value.
type Optional[T any] struct {
	Value *T
}

// Update is forma2's updateWorkspaceAction: workspace.manage, so teams with a manage grant edit settings as well as owners and managers, behind the state-mutation rate limit. The organization is never part of the input, so a workspace cannot be moved to another tenant.
func (s *Service) Update(ctx context.Context, userID, workspaceID uuid.UUID, in UpdateInput) (*Workspace, error) {
	old, ws, err := s.update(ctx, userID, workspaceID, in)
	org := ""
	if old != nil {
		org = old.OrganizationID.String()
	}
	s.record(ctx, audit.ActionUpdated, workspaceID.String(), org, err, old, ws)
	if err == nil {
		captureBrandingRemoved(userID, org, workspaceID, old, in)
	}
	return ws, err
}

// captureBrandingRemoved is forma2's remove_branding_enabled, once for each kind of survey whose Forma branding the update switched off.
func captureBrandingRemoved(userID uuid.UUID, org string, workspaceID uuid.UUID, old *Workspace, in UpdateInput) {
	for _, b := range []struct {
		kind      string
		was       bool
		requested *bool
	}{{"link", old.LinkSurveyBranding, in.LinkSurveyBranding}, {"in_app", old.InAppSurveyBranding, in.InAppSurveyBranding}} {
		if b.was && b.requested != nil && !*b.requested {
			analytics.Capture(userID.String(), "remove_branding_enabled", analytics.Properties{
				"organization_id": org,
				"workspace_id":    workspaceID.String(),
				"branding_type":   b.kind,
			}, analytics.Groups{OrganizationID: org, WorkspaceID: workspaceID.String()})
		}
	}
}

func (s *Service) update(ctx context.Context, userID, workspaceID uuid.UUID, in UpdateInput) (old, updated *Workspace, err error) {
	actor := authz.User(userID)
	if err := s.authz.Require(ctx, actor, authz.WorkspaceManage, authz.Workspace(workspaceID)); err != nil {
		return nil, nil, err
	}
	if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, workspaceID.String()); err != nil {
		return nil, nil, err
	}
	for name, doc := range map[string]json.RawMessage{"workspace.styling": in.Styling, "workspace.logo": in.Logo, "workspace.config": in.Config} {
		if doc != nil {
			if err := s.schemas.Validate(name, doc); err != nil {
				return nil, nil, apperr.Validation([]apperr.FieldError{{Field: strings.TrimPrefix(name, "workspace."), Rule: "schema"}})
			}
		}
	}
	if err := checkDocumentRefinements(in.Styling, in.Logo); err != nil {
		return nil, nil, err
	}
	configPatch, err := parseConfigPatch(in.Config)
	if err != nil {
		return nil, nil, err
	}
	if in.CustomHeadScripts != nil {
		// Custom head scripts run unsanitized on the public survey pages, so forma2's settings page offered them only to owners and managers of a self-hosted instance. The same rule is enforced here rather than left to the page.
		if s.cfg.IsFormaCloud {
			return nil, nil, apperr.OperationNotAllowed("Custom head scripts are not available on Forma Cloud")
		}
		org, err := s.repo.organizationOf(ctx, workspaceID)
		if err != nil {
			return nil, nil, err
		}
		if org == nil {
			return nil, nil, apperr.NotFound("Workspace", "")
		}
		if err := s.authz.Require(ctx, actor, authz.OrganizationManage, authz.Organization(*org)); err != nil {
			return nil, nil, err
		}
	}

	err = s.repo.transaction(ctx, func(tx *Repository) error {
		var err error
		if old, err = tx.lockByID(ctx, workspaceID); err != nil {
			return err
		}
		if old == nil {
			return apperr.NotFound("Workspace", "")
		}
		columns, err := s.columns(old, in, configPatch)
		if err != nil {
			return err
		}
		if err := tx.update(ctx, workspaceID, columns); err != nil {
			return err
		}
		updated, err = tx.byID(ctx, workspaceID)
		return err
	})
	if err != nil {
		return old, nil, err
	}
	return old, updated, nil
}

// checkDocumentRefinements ports the zod refinements the exported schemas cannot carry: a logo URL must be an http(s) URL or a relative /storage/ path (ZStorageUrl; rendered straight into src attributes, so a javascript: URL would be stored XSS), and an uploaded styling background needs its image (ZSurveyStylingBackground). Both documents already passed their schemas.
func checkDocumentRefinements(styling, logo json.RawMessage) error {
	type logoDoc struct {
		URL *string `json:"url"`
	}
	if logo != nil {
		var l logoDoc
		_ = json.Unmarshal(logo, &l)
		if l.URL != nil && !validStorageURL(*l.URL) {
			return apperr.Validation([]apperr.FieldError{{Field: "logo.url", Rule: "storage_url"}})
		}
	}
	if styling != nil {
		var st struct {
			Logo       *logoDoc `json:"logo"`
			Background *struct {
				Bg     *string `json:"bg"`
				BgType *string `json:"bgType"`
			} `json:"background"`
		}
		_ = json.Unmarshal(styling, &st)
		if st.Logo != nil && st.Logo.URL != nil && !validStorageURL(*st.Logo.URL) {
			return apperr.Validation([]apperr.FieldError{{Field: "styling.logo.url", Rule: "storage_url"}})
		}
		if bg := st.Background; bg != nil && bg.BgType != nil && *bg.BgType == "upload" && (bg.Bg == nil || *bg.Bg == "") {
			return apperr.Validation([]apperr.FieldError{{Field: "styling.background", Rule: "background"}})
		}
	}
	return nil
}

// validStorageURL is ZStorageUrl: a relative storage path, or an absolute URL whose scheme is http or https.
func validStorageURL(s string) bool {
	if strings.HasPrefix(s, "/storage/") {
		return true
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme == "https" || scheme == "http"
}

// configKeys are ZWorkspaceConfig's keys; anything else in a patch is dropped, as zod's object schema strips unknown keys.
var configKeys = []string{"channel", "industry", "defaultSurveyLanguage"}

// parseConfigPatch reads the config keys a request changes, already shape-checked against workspace.config. The default survey language is trimmed and must be a language the survey runtime ships strings for (forma2's ZWorkspaceUpdateActionInput).
func parseConfigPatch(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if raw == nil {
		return nil, nil
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, apperr.Validation([]apperr.FieldError{{Field: "config", Rule: "schema"}})
	}
	patch := map[string]json.RawMessage{}
	for _, k := range configKeys {
		if v, ok := all[k]; ok {
			patch[k] = v
		}
	}
	if v, ok := patch["defaultSurveyLanguage"]; ok && string(v) != "null" {
		var code string
		if err := json.Unmarshal(v, &code); err != nil {
			return nil, apperr.Validation([]apperr.FieldError{{Field: "config.defaultSurveyLanguage", Rule: "schema"}})
		}
		code = strings.TrimSpace(code)
		if code == "" {
			return nil, apperr.Validation([]apperr.FieldError{{Field: "config.defaultSurveyLanguage", Rule: "min", Param: "1"}})
		}
		if !langcode.IsSurveyRuntimeLanguage(code) {
			return nil, &apperr.Error{Status: http.StatusBadRequest, Code: apperr.CodeValidation, Message: "Unsupported default survey language",
				Details: map[string]any{"fields": []apperr.FieldError{{Field: "config.defaultSurveyLanguage", Rule: "survey_runtime_language"}}}}
		}
		patch["defaultSurveyLanguage"], _ = json.Marshal(code)
	}
	return patch, nil
}

// columns turns the input into the assignments of the update, checking the rules that depend on the stored workspace.
func (s *Service) columns(old *Workspace, in UpdateInput, configPatch map[string]json.RawMessage) ([]column, error) {
	var cols []column
	set := func(name string, value any) { cols = append(cols, column{name: name, expr: "?", value: value}) }
	setJSON := func(name string, doc []byte) {
		cols = append(cols, column{name: name, expr: "?::jsonb", value: string(doc)})
	}
	if in.Name != nil {
		set("name", *in.Name)
	}
	if in.RecontactDays != nil {
		set("recontact_days", *in.RecontactDays)
	}
	if in.InAppSurveyBranding != nil {
		set("in_app_survey_branding", *in.InAppSurveyBranding)
	}
	if in.LinkSurveyBranding != nil {
		set("link_survey_branding", *in.LinkSurveyBranding)
	}
	if in.Placement != nil {
		set("placement", *in.Placement)
	}
	if in.ClickOutsideClose != nil {
		set("click_outside_close", *in.ClickOutsideClose)
	}
	if in.Overlay != nil {
		set("overlay", *in.Overlay)
	}
	if in.Styling != nil {
		setJSON("styling", in.Styling)
	}
	if in.Logo != nil {
		setJSON("logo", in.Logo)
	}
	if in.CustomHeadScripts != nil {
		set("custom_head_scripts", in.CustomHeadScripts.Value)
	}
	if configPatch != nil {
		merged, err := mergeConfig(old, configPatch)
		if err != nil {
			return nil, err
		}
		setJSON("config", merged)
	}
	return cols, nil
}

// mergeConfig lays the changed keys over the stored config, so a settings page that has been open a while cannot revert a key another surface wrote since (forma2 merged for the same reason). A default survey language must be one of the workspace's own languages, compared on canonical codes so a legacy "de" row matches "de-DE".
func mergeConfig(old *Workspace, patch map[string]json.RawMessage) ([]byte, error) {
	stored := map[string]json.RawMessage{}
	if len(old.Config) > 0 {
		if err := json.Unmarshal(old.Config, &stored); err != nil {
			return nil, fmt.Errorf("read stored workspace config: %w", err)
		}
	}
	for k, v := range patch {
		stored[k] = v
	}
	if v, ok := patch["defaultSurveyLanguage"]; ok && string(v) != "null" {
		var code string
		_ = json.Unmarshal(v, &code)
		want := langcode.Normalize(code)
		if want == "" || !slices.ContainsFunc(old.Languages, func(l Language) bool { return langcode.Normalize(l.Code) == want }) {
			return nil, apperr.OperationNotAllowed("The default survey language must be one of the workspace's survey languages")
		}
	}
	return json.Marshal(stored)
}

// Destination is where the browser goes after a deletion: the workspace to remember as the last active one (nil when the organization has none left) and the path to open.
type Destination struct {
	WorkspaceID *uuid.UUID `json:"workspaceId"`
	Path        string     `json:"path"`
}

// Deletion is a deleted workspace and where to go next.
type Deletion struct {
	Workspace   *Workspace  `json:"workspace"`
	Destination Destination `json:"destination"`
}

// Messages of a refused workspace deletion, forma2's.
const (
	CodeDeleteConfirmationRequired = "workspace_delete_confirmation_required"
	CodeDeleteConfirmationMismatch = "workspace_delete_confirmation_mismatch"
)

// Delete is forma2's deleteWorkspaceWithConfirmation, with the order fixed: forma2 loaded the workspace and compared the typed name before it authorized, which let any signed-in user probe whether a workspace exists and guess its name. Here organization.manage on the workspace's organization comes first, and a missing workspace is refused like a foreign one. Only owners and managers delete workspaces (not teams with a manage grant), and never the organization's last one.
func (s *Service) Delete(ctx context.Context, userID, workspaceID uuid.UUID, confirmationName string) (*Deletion, error) {
	d, org, err := s.delete(ctx, userID, workspaceID, confirmationName)
	var old *Workspace
	if d != nil {
		old = d.Workspace
	}
	s.record(ctx, audit.ActionDeleted, workspaceID.String(), org, err, old, nil)
	return d, err
}

func (s *Service) delete(ctx context.Context, userID, workspaceID uuid.UUID, confirmationName string) (*Deletion, string, error) {
	if strings.TrimSpace(confirmationName) == "" {
		return nil, "", apperr.InvalidInput(CodeDeleteConfirmationRequired, "Workspace name confirmation is required to delete this workspace.")
	}
	org, err := s.repo.organizationOf(ctx, workspaceID)
	if err != nil {
		return nil, "", err
	}
	if org == nil {
		return nil, "", apperr.Forbidden("Not authorized")
	}
	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationManage, authz.Organization(*org)); err != nil {
		return nil, "", err
	}
	ws, err := s.repo.byID(ctx, workspaceID)
	if err != nil {
		return nil, org.String(), err
	}
	if ws == nil {
		return nil, org.String(), apperr.NotFound("workspace", workspaceID.String())
	}
	// Compared trimmed and lowercased, as forma2's hasMatchingWorkspaceDeleteConfirmation did.
	if !strings.EqualFold(strings.TrimSpace(confirmationName), strings.TrimSpace(ws.Name)) {
		return nil, org.String(), apperr.InvalidInput(CodeDeleteConfirmationMismatch, "Workspace name confirmation does not match")
	}
	deleted, err := s.repo.deleteIfNotLast(ctx, workspaceID, *org)
	if err != nil {
		return nil, org.String(), err
	}
	// The workspace's files go with it: those stored under its id and, for a workspace from before the workspace rename, under its environment id. A failure leaves orphaned objects behind but does not undo the deletion, as in forma2.
	fileIDs := []string{workspaceID.String()}
	if deleted.LegacyEnvironmentID != nil {
		fileIDs = append(fileIDs, *deleted.LegacyEnvironmentID)
	}
	if err := s.files.DeleteWorkspaceFiles(ctx, fileIDs...); err != nil {
		s.log.ErrorContext(ctx, "Error deleting S3 files", "err", err, "workspaceId", workspaceID.String())
	}
	// The workspace is gone; failing to pick where to go next must not report the deletion as failed, and "/" resolves a landing workspace on its own.
	dest, err := s.destinationAfterDeletion(ctx, *org)
	if err != nil {
		s.log.ErrorContext(ctx, "Post-deletion destination resolution failed", "err", err, "workspaceId", workspaceID.String(), "organizationId", org.String())
		dest = Destination{Path: "/"}
	}
	return &Deletion{Workspace: deleted, Destination: dest}, org.String(), nil
}

// destinationAfterDeletion is forma2's getPostDeletionDestination: the oldest surviving workspace of the same organization (ties broken by id), through the onboarding gate so deleting a workspace never skips onboarding; "/" when none survives. Only owners and managers delete, and they can open every workspace of the organization.
func (s *Service) destinationAfterDeletion(ctx context.Context, organizationID uuid.UUID) (Destination, error) {
	remaining, err := s.repo.ofOrganization(ctx, organizationID)
	if err != nil {
		return Destination{}, err
	}
	if len(remaining) == 0 {
		return Destination{Path: "/"}, nil
	}
	next := remaining[0]
	path, err := s.onboardingRedirect(ctx, organizationID, next.ID)
	if err != nil {
		return Destination{}, err
	}
	if path == "" {
		path = HomePath(next.ID)
	}
	return Destination{WorkspaceID: &next.ID, Path: path}, nil
}

// onboardingRedirect is forma2's getOnboardingRedirectPath: a workspace without a survey sends its owners and managers into the new-workspace flow. It returns "" when the gate does not apply.
func (s *Service) onboardingRedirect(ctx context.Context, organizationID, workspaceID uuid.UUID) (string, error) {
	n, err := s.repo.surveyCount(ctx, workspaceID)
	if err != nil || n > 0 {
		return "", err
	}
	return onboardingPath(organizationID, s.cfg.IsFormaCloud), nil
}

// onboardingWorkspace is forma2's getOnboardingWorkspace: the oldest workspace the user can read in the organization, else its oldest workspace. It returns nil when the organization has none.
func (s *Service) onboardingWorkspace(ctx context.Context, userID, organizationID uuid.UUID) (*uuid.UUID, error) {
	readable, err := s.ReadableIDs(ctx, userID, []uuid.UUID{organizationID})
	if err != nil {
		return nil, err
	}
	if len(readable) > 0 {
		return &readable[0], nil
	}
	all, err := s.repo.ofOrganization(ctx, organizationID)
	if err != nil || len(all) == 0 {
		return nil, err
	}
	return &all[0].ID, nil
}

// OnboardingRedirect applies the onboarding gate to the organization's onboarding workspace for the root page: "" when there is nothing to onboard.
func (s *Service) OnboardingRedirect(ctx context.Context, userID, organizationID uuid.UUID) (string, error) {
	ws, err := s.onboardingWorkspace(ctx, userID, organizationID)
	if err != nil || ws == nil {
		return "", err
	}
	return s.onboardingRedirect(ctx, organizationID, *ws)
}

// Onboarding is the state of an organization's new-workspace flow.
type Onboarding struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	// CompletedPath is where the flow's pages send the user once the workspace has a survey; nil while onboarding is still due.
	CompletedPath *string `json:"completedPath"`
}

// Onboarding is what forma2's workspaces/new layout and its pages read: owners and managers only, anyone else gets a not-found as the layout did.
func (s *Service) Onboarding(ctx context.Context, userID, organizationID uuid.UUID) (*Onboarding, error) {
	allowed, err := s.authz.Can(ctx, authz.User(userID), authz.OrganizationManage, authz.Organization(organizationID))
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, apperr.NotFound("Organization", "")
	}
	ws, err := s.onboardingWorkspace(ctx, userID, organizationID)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, apperr.NotFound("Onboarding workspace for organization", organizationID.String())
	}
	n, err := s.repo.surveyCount(ctx, *ws)
	if err != nil {
		return nil, err
	}
	out := &Onboarding{WorkspaceID: *ws}
	if n > 0 {
		path := HomePath(*ws)
		out.CompletedPath = &path
	}
	return out, nil
}

// ResolveEnvironment is forma2's /environments/{id} alias: an id or a legacy environment id resolves to the workspace when the user may navigate it (organization.read, and workspace.read or organization.manage_billing, which deliberately admits billing members). An unknown id and one the user may not navigate are both not found.
func (s *Service) ResolveEnvironment(ctx context.Context, userID uuid.UUID, environmentID string) (uuid.UUID, error) {
	notFound := apperr.NotFound("Environment", "")
	ws, err := s.repo.byEnvironmentID(ctx, environmentID)
	if err != nil {
		return uuid.Nil, err
	}
	if ws == nil {
		return uuid.Nil, notFound
	}
	ok, err := s.canNavigate(ctx, userID, ws.ID, ws.OrganizationID)
	if err != nil {
		return uuid.Nil, err
	}
	if !ok {
		return uuid.Nil, notFound
	}
	return ws.ID, nil
}

// canNavigate is forma2's canUserNavigateWorkspace.
func (s *Service) canNavigate(ctx context.Context, userID, workspaceID, organizationID uuid.UUID) (bool, error) {
	actor, org := authz.User(userID), authz.Organization(organizationID)
	if ok, err := s.authz.Can(ctx, actor, authz.OrganizationRead, org); err != nil || !ok {
		return false, err
	}
	if ok, err := s.authz.Can(ctx, actor, authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil || ok {
		return ok, err
	}
	return s.authz.Can(ctx, actor, authz.OrganizationManageBilling, org)
}
