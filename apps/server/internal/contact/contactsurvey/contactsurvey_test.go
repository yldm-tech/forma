package contactsurvey

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/analytics/analyticstest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/survey"
)

func contactPath(id uuid.UUID, rest string) string { return "/api/app/contacts/" + id.String() + rest }
func segmentLinksPath(id uuid.UUID) string {
	return "/api/app/surveys/" + id.String() + "/personal-links"
}

// The contact page's activity, forma2's getResponsesByContactId and getDisplaysByContactId: newest first, the responses with their tags and quotas, the surveys they name, and the counts AttributesSection shows.
func TestActivity(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	e.organization(outsider)
	c := e.contact(ws, e.now, map[string]string{"email": "a@example.com", "userId": "U1"})
	other := e.contact(ws, e.now, nil)
	first, second := e.linkSurvey(ws, "First", ""), e.linkSurvey(ws, "Second", "")
	base := e.now.Add(-time.Hour)
	d1 := e.insert(`INSERT INTO displays (survey_id, contact_id, created_at) VALUES (?, ?, ?) RETURNING id`, first, c, base)
	d2 := e.insert(`INSERT INTO displays (survey_id, contact_id, created_at) VALUES (?, ?, ?) RETURNING id`, second, c, base.Add(2*time.Minute))
	e.exec(`INSERT INTO displays (survey_id, contact_id, created_at) VALUES (?, ?, ?)`, first, other, base)
	r1 := e.insert(`INSERT INTO responses (survey_id, contact_id, display_id, finished, data, created_at) VALUES (?, ?, ?, true, '{"q1":"hi"}', ?) RETURNING id`, first, c, d1, base.Add(time.Minute))
	r2 := e.insert(`INSERT INTO responses (survey_id, contact_id, finished, created_at) VALUES (?, ?, false, ?) RETURNING id`, second, c, base.Add(3*time.Minute))
	tag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'vip') RETURNING id`, ws)
	e.exec(`INSERT INTO response_tags (response_id, tag_id) VALUES (?, ?)`, r1, tag)

	for _, user := range []uuid.UUID{owner, reader} {
		var a Activity
		e.ok(e.do(user, http.MethodGet, contactPath(c, "/activity"), nil), &a)
		if a.ResponseCount != 2 || a.DisplayCount != 2 || len(a.Responses) != 2 || len(a.Displays) != 2 {
			t.Fatalf("activity %+v", a)
		}
		if a.Responses[0].ID != r2 || a.Responses[1].ID != r1 || a.Displays[0].ID != d2 || a.Displays[1].ID != d1 {
			t.Fatalf("not newest first: %+v", a)
		}
		if len(a.Responses[1].Tags) != 1 || a.Responses[1].Tags[0].Name != "vip" || a.Responses[1].Contact == nil || *a.Responses[1].Contact.UserID != "U1" || a.Responses[1].Quotas == nil {
			t.Fatalf("response %+v", a.Responses[1])
		}
		names := map[uuid.UUID]string{}
		for _, s := range a.Surveys {
			names[s.ID] = s.Name
		}
		if len(a.Surveys) != 2 || names[first] != "First" || names[second] != "Second" {
			t.Fatalf("surveys %+v", a.Surveys)
		}
	}

	var none Activity
	e.ok(e.do(owner, http.MethodGet, contactPath(e.contact(ws, e.now, nil), "/activity"), nil), &none)
	if none.Responses == nil || none.Displays == nil || none.Surveys == nil || none.ResponseCount != 0 {
		t.Fatalf("empty activity %+v", none)
	}
	e.fails(e.do(outsider, http.MethodGet, contactPath(c, "/activity"), nil), http.StatusForbidden, "forbidden")
	e.fails(e.do(owner, http.MethodGet, contactPath(uuid.New(), "/activity"), nil), http.StatusNotFound, "not_found")
}

// forma2's generatePersonalSurveyLinkAction: workspace.write, the stateMutation limit, the survey in the contact's workspace, the created/contact audit event and personal_link_created; the link is the one the v2 API issues.
func TestPersonalLink(t *testing.T) {
	events := analyticstest.Start(t)
	e := newEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	_, foreignWS := e.organization(e.user())
	c := e.contact(ws, e.now, nil)
	sv := e.linkSurvey(ws, "Survey", "")
	link := func(user uuid.UUID, body map[string]any) map[string]string {
		var out map[string]string
		e.ok(e.do(user, http.MethodPost, contactPath(c, "/personal-link"), body), &out)
		return out
	}

	// The link the v2 API issues for the same contact and survey reads the same.
	got := e.decodeLink(link(owner, map[string]any{"surveyId": sv})["surveyUrl"])
	loaded, err := e.surveys.Load(t.Context(), sv)
	if err != nil {
		t.Fatal(err)
	}
	v2Link, err := Link(e.cfg, e.surveys, loaded, c, e.now, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := e.decodeLink(v2Link); got.contactID != c.String() || got.surveyID != sv.String() || !got.issuedAt.Equal(e.now) || !got.expiry.IsZero() || len(got.query) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("link %+v, want %+v", got, want)
	}
	if expiring := e.decodeLink(link(owner, map[string]any{"surveyId": sv, "expirationDays": 7})["surveyUrl"]); !expiring.expiry.Equal(e.now.Add(7 * 24 * time.Hour)) {
		t.Fatalf("expiring link %+v", expiring)
	}

	audits := e.auditEvents()
	last := audits[len(audits)-1]
	target, _ := last["target"].(map[string]any)
	changes, _ := last["changes"].(map[string]any)
	if last["action"] != "created" || last["status"] != "success" || target["type"] != "contact" || target["id"] != c.String() || last["organizationId"] != org.String() || changes["surveyId"] != sv.String() {
		t.Fatalf("audit event %v", last)
	}

	e.fails(e.do(reader, http.MethodPost, contactPath(c, "/personal-link"), map[string]any{"surveyId": sv}), http.StatusForbidden, "forbidden")
	if audits := e.auditEvents(); audits[len(audits)-1]["status"] != "failure" {
		t.Fatalf("refusal not audited: %v", audits[len(audits)-1])
	}
	foreign := e.linkSurvey(foreignWS, "Foreign", "")
	e.fails(e.do(owner, http.MethodPost, contactPath(c, "/personal-link"), map[string]any{"surveyId": foreign}), http.StatusBadRequest, CodeContactWorkspaceMismatch)
	e.fails(e.do(owner, http.MethodPost, contactPath(c, "/personal-link"), map[string]any{"surveyId": uuid.New()}), http.StatusNotFound, "not_found")
	e.fails(e.do(owner, http.MethodPost, contactPath(uuid.New(), "/personal-link"), map[string]any{"surveyId": sv}), http.StatusNotFound, "not_found")
	e.fails(e.do(owner, http.MethodPost, contactPath(c, "/personal-link"), map[string]any{}), http.StatusBadRequest, "validation_error")

	events.Flush()
	n := 0
	for _, m := range events.All() {
		if m.Event == "personal_link_created" && m.DistinctID == owner.String() {
			n++
			if m.Properties["survey_id"] != sv.String() || m.Properties["workspace_id"] != ws.String() || m.Properties["organization_id"] != org.String() {
				t.Errorf("personal_link_created %v", m.Properties)
			}
		}
	}
	if n != 2 {
		t.Fatalf("personal_link_created sent %d times, want 2", n)
	}

	// With a workspace's stateMutation budget spent, its next link is refused.
	_, limited := e.organization(owner)
	if err := e.limiter.ApplyN(t.Context(), ratelimit.ActionStateMutation, limited.String(), ratelimit.ActionStateMutation.Limit); err != nil {
		t.Fatal(err)
	}
	e.fails(e.do(owner, http.MethodPost, contactPath(e.contact(limited, e.now, nil), "/personal-link"), map[string]any{"surveyId": e.linkSurvey(limited, "Limited", "")}), http.StatusTooManyRequests, "too_many_requests")
}

// The surveys the contact page offers personal links to: the workspace's link surveys in progress, not archived.
func TestPublishedLinkSurveys(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	e.organization(outsider)
	published := e.linkSurvey(ws, "Published", "")
	archived := e.linkSurvey(ws, "Archived", "")
	e.exec(`UPDATE surveys SET archived_at = now() WHERE id = ?`, archived)
	e.exec(`INSERT INTO surveys (workspace_id, name, type, status) VALUES (?, 'Draft', 'link', 'draft'), (?, 'App', 'app', 'inProgress')`, ws, ws)
	path := "/api/app/workspaces/" + ws.String() + "/published-link-surveys"
	var got []SurveyRef
	e.ok(e.do(reader, http.MethodGet, path, nil), &got)
	if len(got) != 1 || got[0].ID != published || got[0].Name != "Published" {
		t.Fatalf("surveys %+v", got)
	}
	e.fails(e.do(outsider, http.MethodGet, path, nil), http.StatusForbidden, "forbidden")
}

// A survey that takes single-use links gets fresh single-use parameters on every personal link, signed when they are not encrypted.
func TestPersonalLinkSingleUse(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	c := e.contact(ws, e.now, nil)
	plain := e.linkSurvey(ws, "Plain", `{"enabled": true, "isEncrypted": false}`)
	sealed := e.linkSurvey(ws, "Sealed", `{"enabled": true, "isEncrypted": true}`)

	issue := func(sv uuid.UUID) url.Values {
		var out map[string]string
		e.ok(e.do(owner, http.MethodPost, contactPath(c, "/personal-link"), map[string]any{"surveyId": sv}), &out)
		return e.decodeLink(out["surveyUrl"]).query
	}
	q := issue(plain)
	if q.Get("suId") == "" || q.Get("suToken") != survey.SingleUseSignature(string(e.cfg.EncryptionKeyText), plain.String(), q.Get("suId")) {
		t.Fatalf("plain single-use parameters %v", q)
	}
	if again := issue(plain); again.Get("suId") == q.Get("suId") {
		t.Fatal("single-use id reused")
	}
	if q := issue(sealed); q.Get("suId") == "" || q.Has("suToken") {
		t.Fatalf("encrypted single-use parameters %v", q)
	}
}

// forma2's generatePersonalLinksAction: a CSV with the contacts of a segment of the survey's workspace, newest first.
func TestSegmentLinks(t *testing.T) {
	e := newEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	_, foreignWS := e.organization(e.user())
	sv := e.linkSurvey(ws, "Survey", "")
	older := e.contact(ws, e.now.Add(-2*time.Hour), map[string]string{"email": "old@example.com", "userId": "U-old", "firstName": "=cmd()", "lastName": `Say "hi"`, "plan": "pro"})
	newer := e.contact(ws, e.now.Add(-time.Hour), map[string]string{"email": "new@example.com", "plan": "pro"})
	e.contact(ws, e.now, map[string]string{"email": "free@example.com", "plan": "free"})
	pro := e.segment(ws, `[{"id":"n1","connector":null,"resource":{"id":"l1","root":{"type":"attribute","contactAttributeKey":"plan"},"value":"pro","qualifier":{"operator":"equals"}}}]`)
	nobody := e.segment(ws, `[{"id":"n1","connector":null,"resource":{"id":"l1","root":{"type":"attribute","contactAttributeKey":"plan"},"value":"enterprise","qualifier":{"operator":"equals"}}}]`)

	var file LinksFile
	e.ok(e.do(owner, http.MethodPost, segmentLinksPath(sv), map[string]any{"segmentId": pro, "expirationDays": 30}), &file)
	if file.FileName != "personal-links-"+sv.String()+"-1792747800000.csv" {
		t.Fatalf("file name %q", file.FileName)
	}
	lines := strings.Split(file.CSVContent, "\n")
	if len(lines) != 3 || lines[0] != `"Forma Contact ID","User ID","First Name","Last Name","Email","Personal Link"` {
		t.Fatalf("csv %q", file.CSVContent)
	}
	for i, want := range []struct {
		contact uuid.UUID
		cells   string
	}{
		{newer, `"` + newer.String() + `","","","","new@example.com","`},
		{older, `"` + older.String() + `","U-old","'=cmd()","Say ""hi""","old@example.com","`},
	} {
		link, ok := strings.CutPrefix(lines[i+1], want.cells)
		if !ok || !strings.HasSuffix(link, `"`) {
			t.Fatalf("row %d %q, want it to start %q", i+1, lines[i+1], want.cells)
		}
		if l := e.decodeLink(strings.TrimSuffix(link, `"`)); l.contactID != want.contact.String() || l.surveyID != sv.String() || !l.expiry.Equal(e.now.Add(30*24*time.Hour)) {
			t.Fatalf("row %d link %+v", i+1, l)
		}
	}

	e.fails(e.do(reader, http.MethodPost, segmentLinksPath(sv), map[string]any{"segmentId": pro}), http.StatusForbidden, "forbidden")
	e.fails(e.do(owner, http.MethodPost, segmentLinksPath(sv), map[string]any{"segmentId": nobody}), http.StatusBadRequest, CodeNoContactsInSegment)
	e.fails(e.do(owner, http.MethodPost, segmentLinksPath(sv), map[string]any{"segmentId": uuid.New()}), http.StatusBadRequest, CodeNoContactsInSegment)
	e.fails(e.do(owner, http.MethodPost, segmentLinksPath(sv), map[string]any{"segmentId": e.segment(foreignWS, `[]`)}), http.StatusBadRequest, CodeSegmentWorkspaceMismatch)
	e.fails(e.do(owner, http.MethodPost, segmentLinksPath(uuid.New()), map[string]any{"segmentId": pro}), http.StatusNotFound, "not_found")
}
