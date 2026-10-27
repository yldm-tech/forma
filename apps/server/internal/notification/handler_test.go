package notification

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/webui"
)

// fakeSwitches is one user's settings: the switches the notifications page lists for them, by kind and id.
type fakeSwitches struct {
	mu       sync.Mutex
	user     uuid.UUID
	switches map[string]*auth.NotificationSwitch
	sets     int
}

func (f *fakeSwitches) find(userID uuid.UUID, kind string, id uuid.UUID) (*auth.NotificationSwitch, error) {
	s := f.switches[kind+" "+id.String()]
	if userID != f.user || s == nil {
		return nil, apperr.NotFound("Survey", "")
	}
	return s, nil
}

func (f *fakeSwitches) NotificationSwitchOf(_ context.Context, userID uuid.UUID, kind string, id uuid.UUID) (auth.NotificationSwitch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.find(userID, kind, id)
	if err != nil {
		return auth.NotificationSwitch{}, err
	}
	return *s, nil
}

func (f *fakeSwitches) SetNotificationSwitch(_ context.Context, userID uuid.UUID, kind string, id uuid.UUID, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.find(userID, kind, id)
	if err != nil {
		return err
	}
	f.sets++
	s.Enabled = enabled
	return nil
}

func newFakeSwitches() *fakeSwitches {
	return &fakeSwitches{user: tokenUser, switches: map[string]*auth.NotificationSwitch{
		KindAlert + " " + tokenSurvey.String(): {Type: KindAlert, ID: tokenSurvey, Name: "NPS", Enabled: true},
	}}
}

// newRouter mounts the handler ahead of an SPA whose index.html is "spa", over the given settings, with rate limiting off.
func newRouter(t *testing.T, switches Switches) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	web, err := webui.NewFromFS(fstest.MapFS{"index.html": {Data: []byte("spa")}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := tokenConfig(t, "0123456789abcdefghijklmnopqrstuv")
	cfg.RateLimitingDisabled = true
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := gin.New()
	NewHandler(cfg, switches, ratelimit.New(cfg, nil, log), log).Register(r)
	web.Register(r)
	return r
}

func serve(r *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// alertToken signs a link to tokenUser's alert for the survey id.
func alertToken(t *testing.T, id uuid.UUID) string {
	t.Helper()
	token, err := NewUnsubscribeToken(tokenConfig(t, "0123456789abcdefghijklmnopqrstuv"), tokenUser, KindAlert, id)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// Reading a link never switches anything, however often it is fetched: mail scanners and link previews fetch every link an email carries. It answers which switch the link names, by name, and whether it is on, without caching.
func TestUnsubscribeLookupHasNoSideEffect(t *testing.T) {
	t.Parallel()
	f := newFakeSwitches()
	r := newRouter(t, f)
	tok := alertToken(t, tokenSurvey)
	for range 3 {
		rec := serve(r, httptest.NewRequest(http.MethodGet, "/api/unsubscribe?token="+url.QueryEscape(tok), nil))
		if rec.Code != http.StatusOK || rec.Body.String() != `{"type":"alert","id":"`+tokenSurvey.String()+`","name":"NPS","enabled":true}` || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("lookup = %d %s %v", rec.Code, rec.Body, rec.Header())
		}
	}
	// The SPA page itself is a plain GET of the app too.
	if rec := serve(r, httptest.NewRequest(http.MethodGet, "/app/unsubscribe?token="+url.QueryEscape(tok), nil)); rec.Code != http.StatusOK || rec.Body.String() != "spa" {
		t.Fatalf("page = %d %s", rec.Code, rec.Body)
	}
	if f.sets != 0 || !f.switches[KindAlert+" "+tokenSurvey.String()].Enabled {
		t.Fatalf("a GET switched the setting (%d sets)", f.sets)
	}
}

// The SPA page posts {token, enabled}: off, then back on, answered with the switch as it now is.
func TestUnsubscribeFromThePage(t *testing.T) {
	t.Parallel()
	f := newFakeSwitches()
	r := newRouter(t, f)
	tok := alertToken(t, tokenSurvey)
	for _, enabled := range []bool{false, true} {
		body := fmt.Sprintf(`{"token":%q,"enabled":%t}`, tok, enabled)
		req := httptest.NewRequest(http.MethodPost, "/api/unsubscribe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := serve(r, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), fmt.Sprintf(`"enabled":%t`, enabled)) || f.switches[KindAlert+" "+tokenSurvey.String()].Enabled != enabled {
			t.Fatalf("enabled %t = %d %s", enabled, rec.Code, rec.Body)
		}
	}
	// enabled is required: a body without it switches nothing.
	req := httptest.NewRequest(http.MethodPost, "/api/unsubscribe", strings.NewReader(fmt.Sprintf(`{"token":%q}`, tok)))
	req.Header.Set("Content-Type", "application/json")
	if rec := serve(r, req); rec.Code != http.StatusBadRequest || f.sets != 2 {
		t.Fatalf("without enabled = %d %s, %d sets", rec.Code, rec.Body, f.sets)
	}
}

// A mail client's one-click unsubscribe (RFC 8058): List-Unsubscribe=One-Click posted as a form to the List-Unsubscribe URL turns the switch off, with no session, origin or CSRF header. A form without that field switches nothing.
func TestOneClickUnsubscribe(t *testing.T) {
	t.Parallel()
	f := newFakeSwitches()
	r := newRouter(t, f)
	target := "/api/unsubscribe?token=" + url.QueryEscape(alertToken(t, tokenSurvey))

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader("Other=One-Click"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := serve(r, req); rec.Code != http.StatusBadRequest || f.sets != 0 {
		t.Fatalf("without the one-click field = %d %s", rec.Code, rec.Body)
	}

	req = httptest.NewRequest(http.MethodPost, target, strings.NewReader("List-Unsubscribe=One-Click"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := serve(r, req)
	if rec.Code != http.StatusOK || f.switches[KindAlert+" "+tokenSurvey.String()].Enabled {
		t.Fatalf("one-click = %d %s", rec.Code, rec.Body)
	}
}

// A token that does not verify, or names a switch the user no longer has, changes nothing and says so.
func TestUnsubscribeRefusesBadLinks(t *testing.T) {
	t.Parallel()
	f := newFakeSwitches()
	r := newRouter(t, f)
	good := alertToken(t, tokenSurvey)
	parts := strings.Split(good, ".")
	tampered := parts[0] + "." + parts[1] + "." + strings.Repeat("A", len(parts[2]))
	gone := alertToken(t, uuid.MustParse("0192f5c8-0000-7000-8000-0000000000ff"))
	cases := []struct {
		name, token string
		status      int
		code        string
	}{
		{"tampered", tampered, http.StatusBadRequest, "invalid_token"},
		{"missing", "", http.StatusBadRequest, "invalid_token"},
		{"gone", gone, http.StatusNotFound, apperr.CodeNotFound},
	}
	for _, tc := range cases {
		if rec := serve(r, httptest.NewRequest(http.MethodGet, "/api/unsubscribe?token="+url.QueryEscape(tc.token), nil)); rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
			t.Errorf("%s lookup = %d %s", tc.name, rec.Code, rec.Body)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/unsubscribe?token="+url.QueryEscape(tc.token), strings.NewReader("List-Unsubscribe=One-Click"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if rec := serve(r, req); rec.Code != tc.status {
			t.Errorf("%s one-click = %d %s", tc.name, rec.Code, rec.Body)
		}
	}
	if f.sets != 0 {
		t.Fatalf("%d sets", f.sets)
	}
}

// The unsubscribe links point at /app/workspaces/{id}/settings/notifications; they land on the account notification settings with the switch they name, its elementId kept when it is a UUID.
func TestUnsubscribeRedirect(t *testing.T) {
	t.Parallel()
	r := newRouter(t, newFakeSwitches())
	survey := "0192f5c8-0000-7000-8000-000000000001"
	cases := map[string]string{
		"?type=alert&elementId=" + survey:                                   "/app/account/settings/notifications?elementId=" + survey + "&type=alert",
		"?type=unsubscribedOrganizationIds&elementId=" + survey:             "/app/account/settings/notifications?elementId=" + survey + "&type=unsubscribedOrganizationIds",
		"?type=unsubscribedOrganizationIds&elementId=clorg0000000000000001": "/app/account/settings/notifications?type=unsubscribedOrganizationIds",
		"?type=alert":                    "/app/account/settings/notifications?type=alert",
		"?type=evil&elementId=" + survey: "/app/account/settings/notifications",
		"":                               "/app/account/settings/notifications",
	}
	for query, want := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/workspaces/ws1/settings/notifications"+query, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
			t.Errorf("%q = %d %s, want %s", query, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

// The view-responses CTA links to the responses page under /app, which the SPA serves.
func TestResponsesLinkIsTheApp(t *testing.T) {
	t.Parallel()
	r := newRouter(t, newFakeSwitches())
	ws, survey := "0192f5c8-0000-7000-8000-0000000000aa", "0192f5c8-0000-7000-8000-000000000001"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/workspaces/"+ws+"/surveys/"+survey+"/responses?utm_source=email_notification", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "spa" {
		t.Errorf("the responses link = %d %q, want the SPA", rec.Code, rec.Body)
	}
}
