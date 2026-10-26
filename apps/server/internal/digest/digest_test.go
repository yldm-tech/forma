package digest

import (
	"io"
	"log/slog"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer/mailertest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestDueWeekIsMondayFromNineInTheZone(t *testing.T) {
	t.Parallel()
	tokyo := mustZone(t, "Asia/Tokyo")
	for _, c := range []struct {
		at   time.Time
		loc  *time.Location
		want string
	}{
		{time.Date(2026, 10, 26, 8, 59, 0, 0, tokyo), tokyo, ""},
		{time.Date(2026, 10, 26, 9, 0, 0, 0, tokyo), tokyo, "2026-10-19"},
		{time.Date(2026, 10, 26, 23, 59, 0, 0, tokyo), tokyo, "2026-10-19"},
		{time.Date(2026, 10, 27, 0, 0, 0, 0, tokyo), tokyo, ""},
		{time.Date(2026, 10, 25, 12, 0, 0, 0, tokyo), tokyo, ""},
		// Monday 09:00 in Tokyo is Monday 00:00 UTC, which is not yet due in UTC.
		{time.Date(2026, 10, 26, 9, 0, 0, 0, tokyo), time.UTC, ""},
		// A Monday that starts a month: the week summarized began in the month before.
		{time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC), time.UTC, "2026-05-25"},
	} {
		got, ok := DueWeek(c.at, c.loc)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s in %s = %q %v, want %q", c.at, c.loc, got, ok, c.want)
		}
	}
}

// A week's bounds are local midnights, so the week a clock change falls in is an hour longer or shorter.
func TestPeriodOfFollowsLocalMidnights(t *testing.T) {
	t.Parallel()
	ny := mustZone(t, "America/New_York")
	p, err := PeriodOf("2026-10-26", ny)
	if err != nil {
		t.Fatal(err)
	}
	if p.End.Sub(p.Start) != 169*time.Hour || p.Start.Sub(p.PreviousStart) != 168*time.Hour {
		t.Errorf("period %+v", p)
	}
	if p.FirstDay != "2026-10-26" || p.LastDay != "2026-11-01" || !p.Start.Equal(time.Date(2026, 10, 26, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("period %+v", p)
	}
	if _, err := PeriodOf("2026-13-01", ny); err == nil {
		t.Error("a malformed week must be refused")
	}
	if Zone(nil) != time.UTC || Zone(ptr("Mars/Olympus")) != time.UTC || Zone(ptr("Asia/Tokyo")).String() != "Asia/Tokyo" {
		t.Error("zone fallback")
	}
}

func ptr[T any](v T) *T { return &v }

const blocks = `[{"id":"b1","name":"B1","elements":[
	{"id":"nps","type":"nps","headline":{"default":"Recommend?"},"required":false},
	{"id":"csat","type":"csat","headline":{"default":"Satisfied?"},"required":false,"scale":"smiley","range":5}
]}]`

type env struct {
	t         *testing.T
	db        *gorm.DB
	transport *mailertest.Transport
	digest    *Digest
	enqueuer  *jobs.Enqueuer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Parallel()
	app, _ := url.Parse("https://app.example.com")
	cfg := config.Config{WebAppURL: app, MailFrom: "noreply@mail.example.com", MailFromName: "Forma"}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	transport := &mailertest.Transport{}
	m, err := mailer.NewWithTransport(cfg, transport, log)
	if err != nil {
		t.Fatal(err)
	}
	enqueuer, err := jobs.NewEnqueuer(log)
	if err != nil {
		t.Fatal(err)
	}
	db := pgtest.GORM(t)
	return &env{t: t, db: db, transport: transport, digest: New(cfg, db, m, log), enqueuer: enqueuer}
}

func (e *env) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *env) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Raw(query, args...).Row().Scan(&n); err != nil {
		e.t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// member adds a user with a membership in org in role; settings is their notification_settings.
func (e *env) member(org uuid.UUID, email, role, settings string) uuid.UUID {
	e.t.Helper()
	u := e.insert(`INSERT INTO users (name, email, locale, notification_settings) VALUES ('U', ?, 'en-US', ?::jsonb) RETURNING id`, email, settings)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, u, org, role)
	return u
}

// grant gives user's new team access to ws.
func (e *env) grant(org, ws, user uuid.UUID) {
	e.t.Helper()
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, 'read')`, ws, team)
}

func (e *env) survey(ws uuid.UUID, name string, archived bool) uuid.UUID {
	e.t.Helper()
	var archivedAt *time.Time
	if archived {
		now := time.Now()
		archivedAt = &now
	}
	return e.insert(`INSERT INTO surveys (workspace_id, name, type, status, blocks, welcome_card, endings, archived_at)
		VALUES (?, ?, 'link', 'inProgress', ?::jsonb, '{"enabled": false}'::jsonb, '[]'::jsonb, ?) RETURNING id`, ws, name, blocks, archivedAt)
}

func (e *env) response(survey uuid.UUID, at time.Time, finished bool, data string, tags ...uuid.UUID) {
	e.t.Helper()
	id := e.insert(`INSERT INTO responses (survey_id, created_at, updated_at, finished, data) VALUES (?, ?, ?, ?, ?::jsonb) RETURNING id`, survey, at, at, finished, data)
	for _, tag := range tags {
		e.exec(`INSERT INTO response_tags (response_id, tag_id) VALUES (?, ?)`, id, tag)
	}
}

// fixture is an organization in Tokyo with two workspaces, members in every situation the recipient rules tell apart, and surveys with responses in the week of 2026-10-19 and the week before.
type fixture struct {
	*env
	org, ws1, ws2                        uuid.UUID
	owner, manager, teamMember, optedOut uuid.UUID
	feedback, exitPoll                   uuid.UUID
}

const week = "2026-10-19"

func newFixture(t *testing.T) *fixture {
	e := newEnv(t)
	f := &fixture{env: e}
	f.org = e.insert(`INSERT INTO organizations (name, display_time_zone) VALUES ('Acme', 'Asia/Tokyo') RETURNING id`)
	f.ws1 = e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Production') RETURNING id`, f.org)
	f.ws2 = e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Staging') RETURNING id`, f.org)
	f.owner = e.member(f.org, "owner@example.com", "owner", `{"alert": {}}`)
	f.manager = e.member(f.org, "manager@example.com", "manager", `{}`)
	f.teamMember = e.member(f.org, "team@example.com", "member", `{}`)
	e.grant(f.org, f.ws1, f.teamMember)
	// Not recipients: a member no team gives a workspace, a billing member, a suspended member, a deactivated account, and an owner who turned the summary off.
	e.member(f.org, "no-team@example.com", "member", `{}`)
	e.member(f.org, "billing@example.com", "billing", `{}`)
	suspended := e.member(f.org, "suspended@example.com", "manager", `{}`)
	e.exec(`UPDATE memberships SET suspended_at = now() WHERE user_id = ?`, suspended)
	inactive := e.member(f.org, "inactive@example.com", "owner", `{}`)
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, inactive)
	f.optedOut = e.member(f.org, "opted-out@example.com", "owner", `{"unsubscribedWeeklySummaryOrganizationIds": ["`+f.org.String()+`"]}`)

	tokyo := mustZone(t, "Asia/Tokyo")
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, tokyo) }
	billing := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'billing') RETURNING id`, f.ws1)
	ui := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'ui') RETURNING id`, f.ws1)
	f.feedback = e.survey(f.ws1, "Customer Feedback", false)
	// The week before: one promoter, satisfied.
	e.response(f.feedback, at(12, 10), true, `{"nps": 10, "csat": 5}`)
	// The week: Monday 00:30 local is in it, Sunday 23:30 local too.
	e.response(f.feedback, at(19, 0).Add(30*time.Minute), true, `{"nps": 3, "csat": 2}`, billing)
	e.response(f.feedback, at(21, 15), false, `{"nps": 9}`, billing, ui)
	e.response(f.feedback, at(25, 23).Add(30*time.Minute), true, `{"csat": 4}`, ui, billing)
	// After the week: not counted.
	e.response(f.feedback, at(26, 1), true, `{"nps": 0}`)
	f.exitPoll = e.survey(f.ws2, "Exit poll", false)
	e.response(f.exitPoll, at(13, 12), true, `{}`)
	archived := e.survey(f.ws1, "Archived", true)
	e.response(archived, at(20, 12), true, `{}`)
	quiet := e.survey(f.ws1, "Quiet", false)
	e.response(quiet, at(1, 12), true, `{}`)
	return f
}

func TestRecipientsAreTheMembersWhoCanSeeSurveysAndKeptItOn(t *testing.T) {
	f := newFixture(t)
	got, err := f.digest.Recipients(t.Context(), f.org, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []Recipient{
		{UserID: f.manager, Email: "manager@example.com", Locale: "en-US", Manager: true},
		{UserID: f.owner, Email: "owner@example.com", Locale: "en-US", Manager: true},
		{UserID: f.teamMember, Email: "team@example.com", Locale: "en-US", Manager: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("recipients %+v, want %+v", got, want)
	}
	if got, err := f.digest.Recipients(t.Context(), f.org, &f.optedOut); err != nil || len(got) != 0 {
		t.Errorf("opted out: %+v, %v", got, err)
	}
}

func TestContentComparesTheWeekWithTheOneBefore(t *testing.T) {
	f := newFixture(t)
	owner := Recipient{UserID: f.owner, Manager: true}
	c, err := f.digest.Content(t.Context(), f.org, owner, week)
	if err != nil {
		t.Fatal(err)
	}
	if c.OrganizationName != "Acme" || c.PeriodStart != "2026-10-19" || c.PeriodEnd != "2026-10-25" || c.TimeZone != "Asia/Tokyo" || c.Responses != 3 || c.PreviousResponses != 2 || c.MoreSurveys != 0 {
		t.Errorf("content %+v", c)
	}
	if want := "https://app.example.com/app/account/settings/notifications?elementId=" + f.org.String() + "&type=unsubscribedWeeklySummaryOrganizationIds"; c.SettingsURL != want {
		t.Errorf("settings URL %s, want %s", c.SettingsURL, want)
	}
	if len(c.Surveys) != 2 || c.Surveys[0].Name != "Customer Feedback" || c.Surveys[1].Name != "Exit poll" {
		t.Fatalf("surveys %+v", c.Surveys)
	}
	fb := c.Surveys[0]
	// The week: a detractor and a promoter (NPS 0), one of two CSAT answers satisfied (50%); the week before: a promoter, satisfied.
	if fb.Responses != 3 || fb.PreviousResponses != 1 || fb.Completed != 2 || fb.WorkspaceName != "Production" {
		t.Errorf("feedback %+v", fb)
	}
	if fb.NPS == nil || *fb.NPS.Current != 0 || *fb.NPS.Previous != 100 || fb.CSAT == nil || *fb.CSAT.Current != 50 || *fb.CSAT.Previous != 100 {
		t.Errorf("scores %+v %+v", fb.NPS, fb.CSAT)
	}
	if want := []mailer.WeeklySummaryTag{{Name: "billing", Count: 3}, {Name: "ui", Count: 2}}; !reflect.DeepEqual(fb.Tags, want) {
		t.Errorf("tags %+v", fb.Tags)
	}
	summary, err := url.Parse(fb.SummaryURL)
	if err != nil || summary.Path != "/app/workspaces/"+f.ws1.String()+"/surveys/"+f.feedback.String()+"/summary" {
		t.Fatalf("summary URL %s", fb.SummaryURL)
	}
	if got := summary.Query().Get("filter"); got != `{"d":{"f":"2026-10-18T15:00:00.000Z","t":"2026-10-25T14:59:59.999Z"}}` {
		t.Errorf("filter %s", got)
	}
	// The exit poll had a response the week before only, which answered no score element, so it shows no scores.
	ep := c.Surveys[1]
	if ep.Responses != 0 || ep.PreviousResponses != 1 || ep.NPS != nil || ep.CSAT != nil || len(ep.Tags) != 0 {
		t.Errorf("exit poll %+v", ep)
	}

	// A team member sees the workspace their team was given, not the other.
	c, err = f.digest.Content(t.Context(), f.org, Recipient{UserID: f.teamMember}, week)
	if err != nil || len(c.Surveys) != 1 || c.Surveys[0].Name != "Customer Feedback" || c.Responses != 3 || c.PreviousResponses != 1 {
		t.Errorf("team member content %+v, %v", c, err)
	}
	// A week without activity has nothing to send.
	if c, err := f.digest.Content(t.Context(), f.org, owner, "2026-06-01"); err != nil || c != nil {
		t.Errorf("quiet week %+v, %v", c, err)
	}
}

func TestSendMailsTheRecipientOnce(t *testing.T) {
	f := newFixture(t)
	sent, err := f.digest.Send(t.Context(), f.org, f.owner, week)
	if err != nil || !sent {
		t.Fatalf("send = %v, %v", sent, err)
	}
	msgs := f.transport.Messages()
	if len(msgs) != 1 || msgs[0].To != "owner@example.com" || msgs[0].Subject != "Weekly summary for Acme" || !strings.Contains(msgs[0].HTML, "Customer Feedback") || !strings.Contains(msgs[0].HTML, "Exit poll") {
		t.Fatalf("messages %+v", msgs)
	}
	if id := msgs[0].MessageID; !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@mail.example.com>") || id != f.digest.messageID(f.org, f.owner, week) {
		t.Errorf("message id %s", id)
	}
	// Someone who turned the summary off, or is not a member, gets nothing.
	for _, user := range []uuid.UUID{f.optedOut, uuid.New()} {
		if sent, err := f.digest.Send(t.Context(), f.org, user, week); err != nil || sent {
			t.Errorf("send to %s = %v, %v", user, sent, err)
		}
	}
	if len(f.transport.Messages()) != 1 {
		t.Errorf("messages %d, want 1", len(f.transport.Messages()))
	}
}

func TestDueOrganizationsAreThoseWhoseMondayMorningCame(t *testing.T) {
	f := newFixture(t)
	utc := f.insert(`INSERT INTO organizations (name) VALUES ('Nowhere') RETURNING id`)
	honolulu := f.insert(`INSERT INTO organizations (name, display_time_zone) VALUES ('Aloha', 'Pacific/Honolulu') RETURNING id`)
	// Monday 09:00 in Tokyo: due there only.
	got, err := f.digest.DueOrganizations(t.Context(), time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC))
	if err != nil || !reflect.DeepEqual(got, []OrganizationArgs{{OrganizationID: f.org, Week: week}}) {
		t.Errorf("at 00:00 UTC: %+v, %v", got, err)
	}
	// Monday 10:00 UTC is still Monday in Tokyo (19:00) and Monday 09:00 in UTC, but Monday 00:00 in Honolulu.
	got, err = f.digest.DueOrganizations(t.Context(), time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC))
	if err != nil || len(got) != 2 || !contains(got, OrganizationArgs{OrganizationID: f.org, Week: week}) || !contains(got, OrganizationArgs{OrganizationID: utc, Week: week}) || contains(got, OrganizationArgs{OrganizationID: honolulu, Week: week}) {
		t.Errorf("at 10:00 UTC: %+v, %v", got, err)
	}
}

func contains(list []OrganizationArgs, want OrganizationArgs) bool {
	for _, a := range list {
		if a == want {
			return true
		}
	}
	return false
}

// The organization's job enqueues one recipient job per member, and running it again for the same week enqueues none.
func TestOrganizationJobFansOutOncePerWeek(t *testing.T) {
	f := newFixture(t)
	w := &organizationWorker{digest: f.digest, enqueuer: f.enqueuer}
	job := &river.Job[OrganizationArgs]{Args: OrganizationArgs{OrganizationID: f.org, Week: week}}
	for range 2 {
		if err := w.Work(t.Context(), job); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = ? AND args ->> 'week' = ? AND max_attempts = ?`, RecipientArgs{}.Kind(), week, recipientAttempts); n != 3 {
		t.Errorf("recipient jobs %d, want 3", n)
	}
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = ? AND args ->> 'userId' = ?`, RecipientArgs{}.Kind(), f.optedOut.String()); n != 0 {
		t.Errorf("a job for the member who opted out")
	}
}
