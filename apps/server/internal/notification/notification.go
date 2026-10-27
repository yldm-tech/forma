// Package notification sends the "response finished" email to the members who turned a survey's alerts on, forma2's getUsersWithNotifications and sendResponseFinishedEmail, and answers the unsubscribe links those emails carry.
package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"sync"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
)

// maxConcurrency is forma2's NOTIFICATION_EMAIL_MAX_CONCURRENCY: each send holds its own SMTP connection.
const maxConcurrency = 5

// Recipient is a member to notify and the locale of their email. UserID is whose switches the email's unsubscribe links sign.
type Recipient struct {
	UserID uuid.UUID
	Email  string
	Locale string
}

// Notifier finds the members to notify and sends them the email.
type Notifier struct {
	db     *gorm.DB
	cfg    config.Config
	mailer *mailer.Mailer
	log    *slog.Logger
}

func NewNotifier(cfg config.Config, db *gorm.DB, m *mailer.Mailer, log *slog.Logger) *Notifier {
	return &Notifier{db: db, cfg: cfg, mailer: m, log: log}
}

// Recipients is forma2's getUsersWithNotifications: active users with a membership in the organization that owns the workspace, that the organization does not refuse, who are owners or managers there, or are on a team with access to the workspace, and whose alert for the survey is on. A deactivated account, a suspended member and a member the organization's two-factor requirement refuses (authz.MemberNotRefusedSQL) are refused the responses in the app, so their answers are not mailed to them either. Whether the user turned off auto-subscription for the organization does not matter here, and neither does whether the membership was accepted.
func (n *Notifier) Recipients(ctx context.Context, workspaceID, surveyID uuid.UUID) ([]Recipient, error) {
	var rows []Recipient
	err := n.db.WithContext(ctx).Raw(`SELECT u.id AS user_id, u.email, u.locale FROM users u
		WHERE u.is_active
		AND EXISTS (SELECT 1 FROM memberships m JOIN workspaces w ON w.organization_id = m.organization_id WHERE m.user_id = u.id AND w.id = ? AND `+authz.MemberNotRefusedSQL("m")+`)
		AND (
			EXISTS (SELECT 1 FROM memberships m JOIN workspaces w ON w.organization_id = m.organization_id WHERE m.user_id = u.id AND w.id = ? AND m.role IN ('owner', 'manager'))
			OR EXISTS (SELECT 1 FROM team_members tm JOIN workspace_teams wt ON wt.team_id = tm.team_id WHERE tm.user_id = u.id AND wt.workspace_id = ?)
		)
		AND u.notification_settings -> 'alert' -> ? = 'true'::jsonb
		ORDER BY u.email`, workspaceID, workspaceID, workspaceID, surveyID.String()).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load notification recipients: %w", err)
	}
	return rows, nil
}

// Organization is what the email reads of the survey's organization.
type Organization struct {
	ID      uuid.UUID
	LogoURL string
}

// ResponseFinished is one response to notify about: its survey, its answers, and how many responses the survey has, finished or not.
type ResponseFinished struct {
	WorkspaceID   uuid.UUID
	Survey        *responsemail.Survey
	Answers       *responsemail.Answers
	Organization  Organization
	ResponseCount int
}

// Content is forma2's ResponseFinishedEmail for one response, the part every recipient gets alike: the elements with their file and picture URLs made absolute, the computed and hidden fields and the CTA to the responses. withUnsubscribeLinks adds each recipient's own unsubscribe links.
func (n *Notifier) Content(rf ResponseFinished) (mailer.ResponseFinished, error) {
	elements, err := rf.Answers.Elements(rf.Survey)
	if err != nil {
		return mailer.ResponseFinished{}, err
	}
	public := responsemail.PublicBase(n.cfg)
	base := n.cfg.AppPageURL("/workspaces/" + rf.WorkspaceID.String()).String()
	c := mailer.ResponseFinished{
		SurveyName:     rf.Survey.Name,
		Elements:       responsemail.WithAbsoluteFiles(elements, public),
		EmbeddedFields: rf.Answers.ComputedValues(rf.Survey),
		HiddenFields:   rf.Answers.HiddenValues(rf.Survey),
		ResponsesURL:   base + "/surveys/" + rf.Survey.ID.String() + "/responses?utm_source=email_notification&utm_medium=email&utm_content=view_responses_CTA",
		ResponseCount:  rf.ResponseCount,
	}
	if rf.Organization.LogoURL != "" {
		c.LogoURL = responsemail.ResolveStorageURL(public, rf.Organization.LogoURL)
	}
	return c, nil
}

// withUnsubscribeLinks is c with the recipient's two unsubscribe links, signed for them (NewUnsubscribeLinks): the survey's alert and the organization's auto-subscription, each opening the public unsubscribe page. The second value is the List-Unsubscribe URL, for the survey's alert. Emails sent before the links were signed carry /app/workspaces/{id}/settings/notifications, which Handler still answers.
func (n *Notifier) withUnsubscribeLinks(c mailer.ResponseFinished, rf ResponseFinished, r Recipient) (mailer.ResponseFinished, string, error) {
	survey, err := NewUnsubscribeLinks(n.cfg, r.UserID, KindAlert, rf.Survey.ID)
	if err != nil {
		return c, "", err
	}
	organization, err := NewUnsubscribeLinks(n.cfg, r.UserID, KindAutoSubscribe, rf.Organization.ID)
	if err != nil {
		return c, "", err
	}
	c.UnsubscribeSurveyURL, c.UnsubscribeOrganizationURL = survey.Page, organization.Page
	return c, survey.OneClick, nil
}

// replyTo is forma2's: the respondent's email contact attribute, otherwise MAIL_FROM. It is never in the subject.
func (n *Notifier) replyTo(rf ResponseFinished) string {
	var attrs map[string]any
	if raw := rf.Answers.Response.ContactAttributes; len(raw) > 0 && json.Unmarshal(raw, &attrs) == nil {
		if email, ok := attrs["email"].(string); ok && email != "" {
			return email
		}
	}
	return n.cfg.MailFrom
}

// Send emails every recipient, five at a time, each in their own locale and with unsubscribe links of their own. A failed send is logged and does not stop the others or fail the caller, as in forma2.
func (n *Notifier) Send(ctx context.Context, recipients []Recipient, rf ResponseFinished) error {
	shared, err := n.Content(rf)
	if err != nil {
		return err
	}
	replyTo := n.replyTo(rf)
	for start := 0; start < len(recipients); start += maxConcurrency {
		var wg sync.WaitGroup
		for _, r := range recipients[start:min(start+maxConcurrency, len(recipients))] {
			wg.Go(func() {
				content, oneClick, err := n.withUnsubscribeLinks(shared, rf, r)
				var subject, html string
				if err == nil {
					subject, html, err = n.mailer.Render(r.Locale, content)
				}
				if err == nil {
					_, err = n.mailer.Send(ctx, mailer.Message{To: r.Email, ReplyTo: replyTo, Subject: subject, HTML: html, Headers: mailer.OneClickUnsubscribe(oneClick)})
				}
				if err != nil {
					n.log.ErrorContext(ctx, "Response pipeline notification email failed", "err", err, "survey_id", rf.Survey.ID, "response_id", rf.Answers.Response.ID)
				}
			})
		}
		wg.Wait()
	}
	return nil
}

// UnsubscribeTarget is where an email's unsubscribe link (/app/workspaces/{id}/settings/notifications) lands: the account notification settings under the app's base path, which turn the named switch off. elementId, the survey or organization the switch is for, is kept when it is a UUID.
func UnsubscribeTarget(kind, elementID string) string {
	const settings = config.AppBasePath + "/account/settings/notifications"
	q := url.Values{}
	if unsubscribeTypes[kind] {
		q.Set("type", kind)
		if id, ok := rowid.Parse(elementID); ok {
			q.Set("elementId", id.String())
		}
	}
	if len(q) == 0 {
		return settings
	}
	return settings + "?" + q.Encode()
}
