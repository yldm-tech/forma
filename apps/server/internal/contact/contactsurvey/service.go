package contactsurvey

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/contact"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// The stable codes forma2's personal-link errors carried as their message (personal-link-errors.ts); the client maps them to translated text.
const (
	CodeSegmentWorkspaceMismatch = "segment_survey_workspace_mismatch"
	CodeContactWorkspaceMismatch = "contact_survey_workspace_mismatch"
	CodeNoContactsInSegment      = "no_contacts_in_segment"
)

// Service holds the rules. Every method authorizes the acting user on the workspace the contact or survey belongs to.
type Service struct {
	cfg       config.Config
	db        *gorm.DB
	surveys   *survey.Service
	evaluator *contact.Evaluator
	authz     *authz.Authorizer
	limiter   *ratelimit.Limiter
	audit     *audit.Emitter
	log       *slog.Logger
	now       func() time.Time
}

func NewService(cfg config.Config, db *gorm.DB, surveys *survey.Service, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter, log *slog.Logger) *Service {
	return &Service{cfg: cfg, db: db, surveys: surveys, evaluator: contact.NewEvaluator(db, log), authz: authorizer, limiter: limiter, audit: emitter, log: log, now: time.Now}
}

// owner is the workspace and organization a contact or survey belongs to.
type owner struct {
	WorkspaceID    uuid.UUID
	OrganizationID uuid.UUID
}

// contactOwner is forma2's getWorkspaceIdFromContactId and getOrganizationIdFromContactId; nil when the contact does not exist.
func (s *Service) contactOwner(ctx context.Context, contactID uuid.UUID) (*owner, error) {
	var rows []owner
	if err := s.db.WithContext(ctx).Raw(`SELECT c.workspace_id, w.organization_id FROM contacts c JOIN workspaces w ON w.id = c.workspace_id WHERE c.id = ?`, contactID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("find contact workspace: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// surveyOwner is forma2's getWorkspaceIdFromSurveyId and getOrganizationIdFromSurveyId; nil when the survey does not exist.
func (s *Service) surveyOwner(ctx context.Context, surveyID uuid.UUID) (*owner, error) {
	var rows []owner
	if err := s.db.WithContext(ctx).Raw(`SELECT s.workspace_id, w.organization_id FROM surveys s JOIN workspaces w ON w.id = s.workspace_id WHERE s.id = ?`, surveyID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("find survey workspace: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// SurveyRef is a survey the activity mentions, what forma2's display and response cards show of it.
type SurveyRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Activity is forma2's contact activity: the contact's responses and displays, newest first, the surveys they belong to, and how many of each there are (the counts AttributesSection shows).
type Activity struct {
	Responses     []response.WithQuotas     `json:"responses"`
	Displays      []response.ContactDisplay `json:"displays"`
	Surveys       []SurveyRef               `json:"surveys"`
	ResponseCount int                       `json:"responseCount"`
	DisplayCount  int                       `json:"displayCount"`
}

// Activity is what forma2's contact page loaded through getResponsesByContactId, getDisplaysByContactId and getSurveys: a reader of the contact's workspace sees it; an unknown contact is not found.
func (s *Service) Activity(ctx context.Context, userID, contactID uuid.UUID) (*Activity, error) {
	o, err := s.contactOwner(ctx, contactID)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, apperr.NotFound("Contact", contactID.String())
	}
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(o.WorkspaceID)); err != nil {
		return nil, err
	}
	responses, err := response.ByContact(ctx, s.db, contactID, o.WorkspaceID)
	if err != nil {
		return nil, err
	}
	displays, err := response.DisplaysByContact(ctx, s.db, contactID, o.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ids := map[uuid.UUID]bool{}
	for _, r := range responses {
		ids[r.SurveyID] = true
	}
	for _, d := range displays {
		ids[d.SurveyID] = true
	}
	surveys := []SurveyRef{}
	if len(ids) > 0 {
		list := make([]uuid.UUID, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		if err := s.db.WithContext(ctx).Raw(`SELECT id, name FROM surveys WHERE workspace_id = ? AND id IN ? ORDER BY created_at DESC, id DESC`, o.WorkspaceID, list).Scan(&surveys).Error; err != nil {
			return nil, fmt.Errorf("load activity surveys: %w", err)
		}
	}
	return &Activity{Responses: responses, Displays: displays, Surveys: surveys, ResponseCount: len(responses), DisplayCount: len(displays)}, nil
}

// PublishedLinkSurveys is forma2's getPublishedLinkSurveys, what the contact page offers personal links to: the workspace's link surveys in progress and not archived, newest first, for a reader of the workspace.
func (s *Service) PublishedLinkSurveys(ctx context.Context, userID, workspaceID uuid.UUID) ([]SurveyRef, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	out := []SurveyRef{}
	if err := s.db.WithContext(ctx).Raw(`SELECT id, name FROM surveys WHERE workspace_id = ? AND type = ? AND status = ? AND archived_at IS NULL ORDER BY created_at DESC, id DESC`,
		workspaceID, survey.TypeLink, survey.StatusInProgress).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("list published link surveys: %w", err)
	}
	return out, nil
}

// expiry is a token lifetime of days, forma2's `${expirationDays}d`; none without a positive number of days.
func expiry(days *float64) time.Duration {
	if days == nil || *days <= 0 {
		return 0
	}
	return time.Duration(*days * float64(24*time.Hour))
}

// PersonalLink is forma2's generatePersonalSurveyLinkAction: a writer of the contact's workspace, within the workspace's stateMutation limit, gets the contact's personal link to a survey of the same workspace. The event is recorded as created/contact, its new object the survey, and PostHog hears personal_link_created.
func (s *Service) PersonalLink(ctx context.Context, userID, contactID, surveyID uuid.UUID, expirationDays *float64) (link string, err error) {
	ev := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetContact, ActorType: audit.ActorUser, ActorID: userID.String()}
	defer func() {
		ev.Status = audit.StatusSuccess
		if err != nil {
			ev.Status, ev.NewObject = audit.StatusFailure, nil
		}
		s.audit.Log(ctx, audit.FromRequest(ctx, ev))
	}()

	o, err := s.contactOwner(ctx, contactID)
	if err != nil {
		return "", err
	}
	if o == nil {
		return "", apperr.NotFound("contact", contactID.String())
	}
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(o.WorkspaceID)); err != nil {
		return "", err
	}
	if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, o.WorkspaceID.String()); err != nil {
		return "", err
	}
	so, err := s.surveyOwner(ctx, surveyID)
	if err != nil {
		return "", err
	}
	if so == nil {
		return "", apperr.NotFound("survey", surveyID.String())
	}
	if so.WorkspaceID != o.WorkspaceID {
		return "", apperr.InvalidInput(CodeContactWorkspaceMismatch, CodeContactWorkspaceMismatch)
	}
	// forma2 attributed the event to the contact and its organization only once the survey passed the workspace check.
	ev.OrganizationID, ev.TargetID = o.OrganizationID.String(), contactID.String()

	sv, err := s.surveys.Load(ctx, surveyID)
	if err != nil {
		return "", err
	}
	if sv == nil {
		return "", apperr.NotFound("Survey", surveyID.String())
	}
	link, err = Link(s.cfg, s.surveys, sv, contactID, s.now(), expiry(expirationDays))
	if err != nil {
		return "", err
	}
	analytics.Capture(userID.String(), "personal_link_created", analytics.Properties{
		"organization_id": o.OrganizationID.String(),
		"workspace_id":    o.WorkspaceID.String(),
		"survey_id":       surveyID.String(),
	}, analytics.Groups{OrganizationID: o.OrganizationID.String(), WorkspaceID: o.WorkspaceID.String()})
	ev.NewObject = map[string]any{"surveyId": surveyID.String()}
	return link, nil
}

// LinksFile is a CSV of personal links and the name forma2 downloaded it under.
type LinksFile struct {
	FileName   string `json:"fileName"`
	CSVContent string `json:"csvContent"`
}

// linksHeader is the CSV's header row, in forma2's order.
var linksHeader = []string{"Forma Contact ID", "User ID", "First Name", "Last Name", "Email", "Personal Link"}

// SegmentLinks is forma2's generatePersonalLinksAction: a writer of the survey's workspace gets a personal link for every contact of a segment of the same workspace, newest first, as a CSV with the contact's userId, name and email. A segment of another workspace is refused; a missing segment or one without contacts is no_contacts_in_segment. PostHog hears personal_link_created with the number of links.
func (s *Service) SegmentLinks(ctx context.Context, userID, surveyID, segmentID uuid.UUID, expirationDays *float64) (*LinksFile, error) {
	o, err := s.surveyOwner(ctx, surveyID)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, apperr.NotFound("survey", surveyID.String())
	}
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(o.WorkspaceID)); err != nil {
		return nil, err
	}
	noContacts := apperr.InvalidInput(CodeNoContactsInSegment, CodeNoContactsInSegment)
	sv, err := s.surveys.Load(ctx, surveyID)
	if err != nil {
		return nil, err
	}
	var segmentWorkspace []uuid.UUID
	if err := s.db.WithContext(ctx).Raw(`SELECT workspace_id FROM segments WHERE id = ?`, segmentID).Scan(&segmentWorkspace).Error; err != nil {
		return nil, fmt.Errorf("find segment workspace: %w", err)
	}
	if sv == nil || len(segmentWorkspace) == 0 {
		return nil, noContacts
	}
	if segmentWorkspace[0] != sv.WorkspaceID {
		return nil, apperr.InvalidInput(CodeSegmentWorkspaceMismatch, CodeSegmentWorkspaceMismatch)
	}
	ids, err := s.evaluator.AllSegmentContacts(ctx, segmentID)
	if err != nil {
		// forma2's getContactsInSegment answered null when the segment's filters could not be resolved, a nested segment gone missing included.
		if e, ok := apperr.As(err); ok && e.Code == apperr.CodeNotFound {
			return nil, noContacts
		}
		return nil, err
	}
	values, err := s.contactValues(ctx, ids)
	if err != nil {
		return nil, err
	}
	rows := [][]string{linksHeader}
	lifetime := expiry(expirationDays)
	for _, id := range ids {
		link, err := Link(s.cfg, s.surveys, sv, id, s.now(), lifetime)
		if err != nil {
			s.log.ErrorContext(ctx, "Failed to generate survey URL for contact", "err", err, "contactId", id.String(), "surveyId", surveyID.String())
			continue
		}
		v := values[id]
		rows = append(rows, []string{id.String(), v["userId"], v["firstName"], v["lastName"], v["email"], link})
	}
	if len(rows) == 1 {
		return nil, noContacts
	}
	analytics.Capture(userID.String(), "personal_link_created", analytics.Properties{
		"organization_id": o.OrganizationID.String(),
		"workspace_id":    o.WorkspaceID.String(),
		"survey_id":       surveyID.String(),
		"link_count":      len(rows) - 1,
	}, analytics.Groups{OrganizationID: o.OrganizationID.String(), WorkspaceID: o.WorkspaceID.String()})
	return &LinksFile{
		FileName:   "personal-links-" + surveyID.String() + "-" + strconv.FormatInt(s.now().UnixMilli(), 10) + ".csv",
		CSVContent: csvText(rows),
	}, nil
}

// contactValues are the userId, firstName, lastName and email of the contacts, as text.
func (s *Service) contactValues(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]map[string]string, error) {
	out := map[uuid.UUID]map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ContactID   uuid.UUID
		Key         string
		ValueText   *string
		ValueNumber *float64
		ValueDate   *time.Time
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT a.contact_id, k.key, a.value_text, a.value_number, a.value_date FROM contact_attributes a JOIN contact_attribute_keys k ON k.id = a.attribute_key_id
		WHERE a.contact_id IN ? AND k.key IN ?`, ids, []string{"userId", "firstName", "lastName", "email"}).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load contact attributes: %w", err)
	}
	for _, row := range rows {
		if out[row.ContactID] == nil {
			out[row.ContactID] = map[string]string{}
		}
		out[row.ContactID][row.Key] = contact.Columns{Text: row.ValueText, Number: row.ValueNumber, Date: row.ValueDate}.String()
	}
	return out, nil
}

// csvText is forma2's convertToCsv of text cells: json2csv's defaults (every header and cell quoted, rows joined by "\n") with a cell a spreadsheet would read as a formula given a leading apostrophe.
func csvText(rows [][]string) string {
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		for j, cell := range row {
			if j > 0 {
				b.WriteByte(',')
			}
			if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
				cell = "'" + cell
			}
			b.WriteString(`"` + strings.ReplaceAll(cell, `"`, `""`) + `"`)
		}
	}
	return b.String()
}
