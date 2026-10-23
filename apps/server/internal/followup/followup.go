// Package followup sends a survey's follow-up emails when a response is finished, forma2's sendFollowUpsForResponse (apps/web/modules/survey/follow-ups/lib): each follow-up whose trigger the response meets, to the address its `to` resolves to, with the author's subject and a body sanitized at send time.
package followup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/responsemail"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// Codes of a run that sent nothing, forma2's FollowUpSendError.
const (
	CodeResponseNotFound  = "response_not_found"
	CodeSurveyNotFound    = "survey_not_found"
	CodeOrgNotFound       = "organization_not_found"
	CodeRateLimitExceeded = "rate_limit_exceeded"
)

// Error is a run that sent nothing, with forma2's code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Statuses of one follow-up's outcome.
const (
	StatusSuccess = "success"
	StatusError   = "error"
	StatusSkipped = "skipped"
)

// Result is one follow-up's outcome, forma2's FollowUpResult.
type Result struct {
	FollowUpID string
	Status     string
	Error      string
}

// smtpNotConfigured is forma2's SMTP_NOT_CONFIGURED_ERROR: the mailer reports an unconfigured instance as not sent rather than failed.
const smtpNotConfigured = "SMTP is not configured; follow-up email was not sent"

// Sender sends follow-ups. It runs in `forma worker`, on behalf of no caller: forma2's permission check always allowed follow-ups.
type Sender struct {
	db      *gorm.DB
	cfg     config.Config
	mailer  *mailer.Mailer
	limiter *ratelimit.Limiter
	log     *slog.Logger
}

func NewSender(cfg config.Config, db *gorm.DB, m *mailer.Mailer, limiter *ratelimit.Limiter, log *slog.Logger) *Sender {
	return &Sender{db: db, cfg: cfg, mailer: m, limiter: limiter, log: log}
}

// stored is a follow-up as survey_follow_ups holds it.
type stored struct {
	ID      uuid.UUID
	Trigger []byte
	Action  []byte
}

type followUp struct {
	ID      string
	Trigger surveydoc.FollowUpTrigger
	Action  surveydoc.FollowUpAction
}

func (s *Sender) followUps(ctx context.Context, surveyID uuid.UUID) ([]followUp, error) {
	var rows []stored
	if err := s.db.WithContext(ctx).Raw(`SELECT id, trigger, action FROM survey_follow_ups WHERE survey_id = ? ORDER BY created_at, id`, surveyID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load follow-ups: %w", err)
	}
	out := make([]followUp, len(rows))
	for i, r := range rows {
		out[i].ID = r.ID.String()
		if err := json.Unmarshal(r.Trigger, &out[i].Trigger); err != nil {
			return nil, fmt.Errorf("decode follow-up %s trigger: %w", r.ID, err)
		}
		if err := json.Unmarshal(r.Action, &out[i].Action); err != nil {
			return nil, fmt.Errorf("decode follow-up %s action: %w", r.ID, err)
		}
	}
	return out, nil
}

// organization is the survey's organization: its id for the rate limit and its email logo.
type organization struct {
	ID      uuid.UUID
	LogoURL *string
}

func (s *Sender) organization(ctx context.Context, workspaceID uuid.UUID) (*organization, error) {
	var rows []organization
	if err := s.db.WithContext(ctx).Raw(`SELECT o.id, o.whitelabel ->> 'logoUrl' AS logo_url FROM organizations o JOIN workspaces w ON w.organization_id = o.id WHERE w.id = ?`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load follow-up organization: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// Send is forma2's sendFollowUpsForResponse. The response is read again as it is stored now, with its survey and organization; one unit of the organization's follow-up limit (50 an hour) is spent per response, and over the limit none of the response's follow-ups is sent. Every follow-up whose trigger the response meets runs concurrently; an `endings` trigger needs the response to have ended on one of its endings. locale is the respondent's, for the email's chrome and recalled dates.
func (s *Sender) Send(ctx context.Context, responseID uuid.UUID, locale string) ([]Result, error) {
	r, err := response.Load(ctx, s.db, responseID)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, &Error{Code: CodeResponseNotFound, Message: "Response not found"}
	}
	sv, err := responsemail.LoadSurvey(ctx, s.db, r.SurveyID)
	if err != nil {
		return nil, err
	}
	if sv == nil {
		return nil, &Error{Code: CodeSurveyNotFound, Message: "Survey not found"}
	}
	org, err := s.organization(ctx, sv.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, &Error{Code: CodeOrgNotFound, Message: "Organization not found"}
	}
	if err := s.limiter.Apply(ctx, ratelimit.ActionSurveyFollowUp, org.ID.String()); err != nil {
		if e, ok := apperr.As(err); ok && e.Status == http.StatusTooManyRequests {
			return nil, &Error{Code: CodeRateLimitExceeded, Message: "Too many follow‐up requests; please wait a bit and try again."}
		}
		return nil, err
	}
	followUps, err := s.followUps(ctx, sv.ID)
	if err != nil || len(followUps) == 0 {
		return []Result{}, err
	}
	answers, err := responsemail.Decode(r)
	if err != nil {
		return nil, err
	}
	logo := ""
	if org.LogoURL != nil {
		logo = *org.LogoURL
	}
	results := make([]Result, len(followUps))
	var wg sync.WaitGroup
	for i, f := range followUps {
		if props := f.Trigger.Properties; props != nil && (r.EndingID == nil || !slices.Contains(props.EndingIDs, *r.EndingID)) {
			results[i] = Result{FollowUpID: f.ID, Status: StatusSkipped}
			continue
		}
		wg.Go(func() {
			results[i] = s.evaluate(ctx, f, sv, answers, logo, locale)
		})
	}
	wg.Wait()
	var failures []string
	for _, res := range results {
		if res.Status == StatusError {
			failures = append(failures, fmt.Sprintf("FollowUp %s failed: %s", res.FollowUpID, res.Error))
		}
	}
	if len(failures) > 0 {
		s.log.ErrorContext(ctx, "Follow-up processing errors", "errors", failures, "response_id", r.ID, "survey_id", sv.ID, "organization_id", org.ID)
	}
	return results, nil
}

// evaluate is forma2's evaluateFollowUp: the recipient resolved from `to`, the email built and sent. The From is always the instance's MAIL_FROM: forma2 ignored the follow-up's own from, and so does this.
func (s *Sender) evaluate(ctx context.Context, f followUp, sv *responsemail.Survey, a *responsemail.Answers, logo, locale string) Result {
	fail := func(message string) Result { return Result{FollowUpID: f.ID, Status: StatusError, Error: message} }
	p := f.Action.Properties
	to, reason := a.ResolveRecipient(p.To)
	switch reason {
	case "":
	case responsemail.ReasonValueNotFound:
		return fail("To value not found in response data for followup: " + f.ID)
	case responsemail.ReasonNoEmail:
		return fail("Email address not found in response data for followup: " + f.ID)
	default:
		return fail("Email address is not valid for followup: " + f.ID)
	}
	content, err := a.FollowUpContent(sv, responsemail.EmailOptions{
		Subject: p.Subject, Body: p.Body, AttachResponseData: p.AttachResponseData,
		IncludeVariables: p.IncludeVariables != nil && *p.IncludeVariables, IncludeHiddenFields: p.IncludeHiddenFields != nil && *p.IncludeHiddenFields,
		LogoURL: logo, Locale: locale, PublicBase: responsemail.PublicBase(s.cfg),
	})
	if err != nil {
		return fail(err.Error())
	}
	chrome := locale
	if chrome == "" {
		chrome = mailer.DefaultLocale
	}
	subject, html, err := s.mailer.Render(chrome, content)
	if err != nil {
		return fail(err.Error())
	}
	sent, err := s.mailer.Send(ctx, mailer.Message{To: to, ReplyTo: strings.Join(p.ReplyTo, ", "), Subject: subject, HTML: html})
	if err != nil {
		return fail(err.Error())
	}
	if !sent {
		return fail(smtpNotConfigured)
	}
	return Result{FollowUpID: f.ID, Status: StatusSuccess}
}

// IsRateLimited reports whether err is a run the follow-up limit refused.
func IsRateLimited(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == CodeRateLimitExceeded
}
