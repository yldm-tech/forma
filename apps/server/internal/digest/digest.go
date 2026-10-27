// Package digest sends the weekly summary email: every Monday at 09:00 in an organization's display time zone, each member who can see surveys there gets, for those surveys, the past week's new responses, completion rate, NPS and CSAT against the week before, and top tags, with links to the summaries filtered to the week. A member turns it off per organization on the notifications page (users.notification_settings, unsubscribedWeeklySummaryOrganizationIds).
package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/analysis"
	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// When the summary goes out: Monday from 09:00 in the organization's zone. A worker that was down at 09:00 still sends it later that Monday; after Monday the week is skipped rather than summarized late.
const (
	sendWeekday = time.Monday
	sendHour    = 9
)

// maxSurveys is how many surveys the email details, the busiest first; the others are counted. maxTags is how many tags a survey lists.
const (
	maxSurveys = 10
	maxTags    = 3
)

// messageIDDomain is the Message-ID domain when MAIL_FROM has none.
const messageIDDomain = "forma.yldm.ai"

// Digest finds the organizations whose summary is due, their recipients, and builds and sends each recipient's email.
type Digest struct {
	db     *gorm.DB
	cfg    config.Config
	mailer *mailer.Mailer
	log    *slog.Logger
}

func New(cfg config.Config, db *gorm.DB, m *mailer.Mailer, log *slog.Logger) *Digest {
	return &Digest{db: db, cfg: cfg, mailer: m, log: log}
}

// Zone is an organization's display time zone, UTC when it has none or it does not load: the zone the summary page and the trends show its days in.
func Zone(name *string) *time.Location {
	if name == nil || *name == "" || *name == "Local" {
		return time.UTC
	}
	loc, err := time.LoadLocation(*name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// DueWeek is the week whose summary is due at now in loc, as the date of its Monday: the Monday-to-Sunday week that ended at the last local midnight, from 09:00 on the Monday after it until that Monday ends. False at any other time.
func DueWeek(now time.Time, loc *time.Location) (string, bool) {
	local := now.In(loc)
	if local.Weekday() != sendWeekday || local.Hour() < sendHour {
		return "", false
	}
	y, m, d := local.Date()
	return time.Date(y, m, d-7, 0, 0, 0, 0, time.UTC).Format(time.DateOnly), true
}

// Period is a summarized week in its organization's zone: [Start, End) and the week before it, [PreviousStart, Start), with the week's first and last day.
type Period struct {
	PreviousStart, Start, End time.Time
	FirstDay, LastDay         string
}

// PeriodOf is the week starting on the Monday week (YYYY-MM-DD) in loc. Its bounds are local midnights, so a week that crosses a daylight saving change is 167 or 169 hours long.
func PeriodOf(week string, loc *time.Location) (Period, error) {
	day, err := time.Parse(time.DateOnly, week)
	if err != nil {
		return Period{}, fmt.Errorf("parse summary week %q: %w", week, err)
	}
	y, m, d := day.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, loc)
	return Period{
		PreviousStart: time.Date(y, m, d-7, 0, 0, 0, 0, loc),
		Start:         start,
		End:           time.Date(y, m, d+7, 0, 0, 0, 0, loc),
		FirstDay:      week,
		LastDay:       time.Date(y, m, d+6, 0, 0, 0, 0, time.UTC).Format(time.DateOnly),
	}, nil
}

// DueOrganizations are the organizations whose summary is due at now, with the week each summarizes. The zones are read first, so the time is worked out once per zone in use rather than once per organization.
func (d *Digest) DueOrganizations(ctx context.Context, now time.Time) ([]OrganizationArgs, error) {
	var zones []string
	if err := d.db.WithContext(ctx).Raw(`SELECT DISTINCT COALESCE(display_time_zone, '') FROM organizations`).Scan(&zones).Error; err != nil {
		return nil, fmt.Errorf("list organization time zones: %w", err)
	}
	weeks := map[string]string{}
	for _, name := range zones {
		if week, ok := DueWeek(now, Zone(&name)); ok {
			weeks[name] = week
		}
	}
	if len(weeks) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(weeks))
	for name := range weeks {
		names = append(names, name)
	}
	var rows []struct {
		ID   uuid.UUID
		Zone string
	}
	if err := d.db.WithContext(ctx).Raw(`SELECT id, COALESCE(display_time_zone, '') AS zone FROM organizations WHERE COALESCE(display_time_zone, '') IN ? ORDER BY id`, names).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list organizations due a weekly summary: %w", err)
	}
	out := make([]OrganizationArgs, len(rows))
	for i, r := range rows {
		out[i] = OrganizationArgs{OrganizationID: r.ID, Week: weeks[r.Zone]}
	}
	return out, nil
}

// Recipient is a member who gets the organization's summary. Manager is whether they see every workspace (an owner or manager); the others see the workspaces granted to their teams.
type Recipient struct {
	UserID  uuid.UUID
	Email   string
	Locale  string
	Manager bool
}

// Recipients are the organization's members who get its summary: active accounts with a membership the organization does not refuse (authz.MemberNotRefusedSQL: not suspended, and not refused by its two-factor requirement, which once its grace period is over refuses a member who signs in with a password and has not enrolled), in a role that reads responses (not billing), who can see a workspace there, and who have not turned the summary off. With only set, just that member, if they still qualify. The rules are the notifications page's, which lists the organizations the switch is for.
func (d *Digest) Recipients(ctx context.Context, orgID uuid.UUID, only *uuid.UUID) ([]Recipient, error) {
	query := `SELECT u.id AS user_id, u.email, u.locale, m.role IN ('owner', 'manager') AS manager
		FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = ? AND u.is_active AND ` + authz.MemberNotRefusedSQL("m") + ` AND m.role <> 'billing'
		AND NOT COALESCE(u.notification_settings -> '` + auth.NotificationWeeklySummary + `', '[]'::jsonb) @> jsonb_build_array(?::text)
		AND (m.role IN ('owner', 'manager') OR EXISTS (
			SELECT 1 FROM team_members tm JOIN workspace_teams wt ON wt.team_id = tm.team_id JOIN workspaces w ON w.id = wt.workspace_id
			WHERE tm.user_id = u.id AND w.organization_id = m.organization_id))`
	args := []any{orgID, orgID.String()}
	if only != nil {
		query += ` AND u.id = ?`
		args = append(args, *only)
	}
	var rows []Recipient
	if err := d.db.WithContext(ctx).Raw(query+` ORDER BY u.email`, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list weekly summary recipients: %w", err)
	}
	return rows, nil
}

// organization is what the email reads of the organization.
type organization struct {
	Name            string
	DisplayTimeZone *string
	LogoURL         *string
}

func (d *Digest) organization(ctx context.Context, id uuid.UUID) (*organization, error) {
	var rows []organization
	if err := d.db.WithContext(ctx).Raw(`SELECT name, display_time_zone, whitelabel ->> 'logoUrl' AS logo_url FROM organizations WHERE id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load organization: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// surveyActivity is a survey the recipient can see with responses in the week or the week before.
type surveyActivity struct {
	ID                uuid.UUID
	Name              string
	WorkspaceID       uuid.UUID
	WorkspaceName     string
	Responses         int
	PreviousResponses int
}

// activity lists the surveys of the organization the recipient can see that are not archived and had responses in the period or the one before, the busiest first.
func (d *Digest) activity(ctx context.Context, orgID uuid.UUID, r Recipient, p Period) ([]surveyActivity, error) {
	var rows []surveyActivity
	err := d.db.WithContext(ctx).Raw(`SELECT s.id, s.name, s.workspace_id, w.name AS workspace_name,
			count(*) FILTER (WHERE r.created_at >= ?) AS responses, count(*) FILTER (WHERE r.created_at < ?) AS previous_responses
		FROM surveys s
		JOIN workspaces w ON w.id = s.workspace_id
		JOIN responses r ON r.survey_id = s.id AND r.created_at >= ? AND r.created_at < ?
		WHERE w.organization_id = ? AND s.archived_at IS NULL
		AND (? OR EXISTS (SELECT 1 FROM workspace_teams wt JOIN team_members tm ON tm.team_id = wt.team_id WHERE wt.workspace_id = w.id AND tm.user_id = ?))
		GROUP BY s.id, s.name, s.workspace_id, w.name
		ORDER BY responses DESC, previous_responses DESC, s.name, s.id`,
		p.Start, p.Start, p.PreviousStart, p.End, orgID, r.Manager, r.UserID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list weekly summary surveys: %w", err)
	}
	return rows, nil
}

// topTags are the tags most put on the survey's responses created in the period, most used first.
func (d *Digest) topTags(ctx context.Context, surveyID uuid.UUID, p Period) ([]mailer.WeeklySummaryTag, error) {
	var rows []mailer.WeeklySummaryTag
	if err := d.db.WithContext(ctx).Raw(`SELECT t.name, count(*) AS count
		FROM response_tags rt JOIN tags t ON t.id = rt.tag_id JOIN responses r ON r.id = rt.response_id
		WHERE r.survey_id = ? AND r.created_at >= ? AND r.created_at < ?
		GROUP BY t.name ORDER BY count(*) DESC, t.name LIMIT ?`, surveyID, p.Start, p.End, maxTags).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list weekly summary tags: %w", err)
	}
	return rows, nil
}

// Content is the recipient's summary of the organization for the week: nil when they can see no survey with responses in it or the week before, so nothing is sent.
func (d *Digest) Content(ctx context.Context, orgID uuid.UUID, r Recipient, week string) (*mailer.WeeklySummary, error) {
	org, err := d.organization(ctx, orgID)
	if err != nil || org == nil {
		return nil, err
	}
	loc := Zone(org.DisplayTimeZone)
	p, err := PeriodOf(week, loc)
	if err != nil {
		return nil, err
	}
	surveys, err := d.activity(ctx, orgID, r, p)
	if err != nil || len(surveys) == 0 {
		return nil, err
	}
	c := &mailer.WeeklySummary{
		OrganizationName: org.Name, PeriodStart: p.FirstDay, PeriodEnd: p.LastDay, TimeZone: loc.String(),
		SettingsURL: d.cfg.AppPageURL("/account/settings/notifications").String() + "?" + url.Values{"type": {auth.NotificationWeeklySummary}, "elementId": {orgID.String()}}.Encode(),
	}
	if org.LogoURL != nil && *org.LogoURL != "" {
		c.LogoURL = responsemail.ResolveStorageURL(responsemail.PublicBase(d.cfg), *org.LogoURL)
	}
	for i, s := range surveys {
		c.Responses += s.Responses
		c.PreviousResponses += s.PreviousResponses
		if i >= maxSurveys {
			c.MoreSurveys++
			continue
		}
		entry, err := d.surveyEntry(ctx, s, loc, p)
		if err != nil {
			return nil, err
		}
		c.Surveys = append(c.Surveys, entry)
	}
	return c, nil
}

// surveyEntry is one survey's block of the email.
func (d *Digest) surveyEntry(ctx context.Context, s surveyActivity, loc *time.Location, p Period) (mailer.WeeklySummarySurvey, error) {
	entry := mailer.WeeklySummarySurvey{
		Name: s.Name, WorkspaceName: s.WorkspaceName, SummaryURL: d.summaryURL(s, p),
		Responses: s.Responses, PreviousResponses: s.PreviousResponses,
	}
	current, previous, err := analysis.ComparePeriods(ctx, d.db, s.ID, loc, p.PreviousStart, p.Start, p.End)
	if err != nil {
		return entry, err
	}
	if current != nil {
		entry.Responses, entry.PreviousResponses, entry.Completed = current.Responses, previous.Responses, current.Completed
		entry.NPS = score(current, previous, surveydoc.ElementNPS)
		entry.CSAT = score(current, previous, surveydoc.ElementCSAT)
	}
	if entry.Tags, err = d.topTags(ctx, s.ID, p); err != nil {
		return entry, err
	}
	return entry, nil
}

// score is the survey's first element of the type in both weeks, nil without one or when nobody answered it in either week.
func score(current, previous *analysis.PeriodStats, elementType string) *mailer.WeeklySummaryScore {
	cur, prev := current.Score(elementType), previous.Score(elementType)
	if cur == nil || prev == nil || (cur.Value == nil && prev.Value == nil) {
		return nil
	}
	return &mailer.WeeklySummaryScore{Current: cur.Value, Previous: prev.Value}
}

// summaryURL opens the survey's summary filtered to the week, in the ?filter= form the analysis views read (createdAt between the week's first and last millisecond, both inclusive).
func (d *Digest) summaryURL(s surveyActivity, p Period) string {
	const iso = "2006-01-02T15:04:05.000Z07:00"
	filter, _ := json.Marshal(map[string]any{"d": map[string]string{
		"f": p.Start.UTC().Format(iso),
		"t": p.End.Add(-time.Millisecond).UTC().Format(iso),
	}})
	q := url.Values{"filter": {string(filter)}, "utm_source": {"email_weekly_summary"}, "utm_medium": {"email"}}
	return d.cfg.AppPageURL("/workspaces/"+s.WorkspaceID.String()+"/surveys/"+s.ID.String()+"/summary").String() + "?" + q.Encode()
}

// Send builds the recipient's summary and mails it in their locale. It sends nothing, and reports false, when they no longer qualify, there is nothing to summarize, or mail is not configured. A message for the same organization, member and week always carries the same Message-ID.
func (d *Digest) Send(ctx context.Context, orgID, userID uuid.UUID, week string) (bool, error) {
	recipients, err := d.Recipients(ctx, orgID, &userID)
	if err != nil || len(recipients) == 0 {
		return false, err
	}
	r := recipients[0]
	content, err := d.Content(ctx, orgID, r, week)
	if err != nil || content == nil {
		return false, err
	}
	subject, html, err := d.mailer.Render(r.Locale, *content)
	if err != nil {
		return false, err
	}
	return d.mailer.Send(ctx, mailer.Message{To: r.Email, Subject: subject, HTML: html, MessageID: d.messageID(orgID, userID, week)})
}

// messageID is the email's stable Message-ID, on MAIL_FROM's domain.
func (d *Digest) messageID(orgID, userID uuid.UUID, week string) string {
	sum := sha256.Sum256([]byte("weekly-summary:" + orgID.String() + ":" + userID.String() + ":" + week))
	domain := messageIDDomain
	if _, after, ok := strings.Cut(d.cfg.MailFrom, "@"); ok && strings.TrimSpace(after) != "" {
		domain = strings.TrimSpace(after)
	}
	return "<" + hex.EncodeToString(sum[:])[:32] + "@" + domain + ">"
}
