package notification

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/webui"
)

// newRouter mounts the handler ahead of an SPA whose index.html is "spa".
func newRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	web, err := webui.NewFromFS(fstest.MapFS{"index.html": {Data: []byte("spa")}})
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	NewHandler(web).Register(r)
	web.Register(r)
	return r
}

// The unsubscribe links point at /app/workspaces/{id}/settings/notifications (emails sent before the SPA moved under /app at the same path without /app, which the SPA handler redirects there), a page forma2 never served; they land on the account notification settings with the switch they name, a forma2 id resolved to the id migrated from it.
func TestUnsubscribeRedirect(t *testing.T) {
	t.Parallel()
	r := newRouter(t)
	survey := "0192f5c8-0000-7000-8000-000000000001"
	cases := map[string]string{
		"?type=alert&elementId=" + survey:                                   "/app/account/settings/notifications?elementId=" + survey + "&type=alert",
		"?type=unsubscribedOrganizationIds&elementId=clorg0000000000000001": "/app/account/settings/notifications?elementId=" + legacyid.ID("clorg0000000000000001").String() + "&type=unsubscribedOrganizationIds",
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
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/workspaces/ws1/settings/notifications?type=alert&elementId="+survey, nil))
	if want := "/app/workspaces/ws1/settings/notifications?type=alert&elementId=" + survey; rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != want {
		t.Errorf("the root link = %d %s, want 308 %s", rec.Code, rec.Header().Get("Location"), want)
	}
}

// The view-responses CTA of emails sent before the migration names the workspace and survey by their forma2 ids; it redirects to the ids migrated from them with its query kept, and the SPA serves the canonical path. Links without /app, from emails sent before the SPA moved under it, reach the same handler through the SPA handler's redirect.
func TestResponsesLinkRedirect(t *testing.T) {
	t.Parallel()
	r := newRouter(t)
	ws, survey := "0192f5c8-0000-7000-8000-0000000000aa", "0192f5c8-0000-7000-8000-000000000001"
	query := "?utm_source=email_notification&utm_medium=email&utm_content=view_responses_CTA"
	redirects := map[string]string{
		"/app/workspaces/clws00000000000000000001/surveys/clsv00000000000000000001/responses" + query: "/app/workspaces/" + legacyid.ID("clws00000000000000000001").String() + "/surveys/" + legacyid.ID("clsv00000000000000000001").String() + "/responses" + query,
		"/app/workspaces/" + ws + "/surveys/clsv00000000000000000001/responses":                       "/app/workspaces/" + ws + "/surveys/" + legacyid.ID("clsv00000000000000000001").String() + "/responses",
	}
	for path, want := range redirects {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
			t.Errorf("%s = %d %q, want %s", path, rec.Code, rec.Header().Get("Location"), want)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/workspaces/"+ws+"/surveys/"+survey+"/responses"+query, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "spa" {
		t.Errorf("the canonical path = %d %q, want the SPA", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/workspaces/clws00000000000000000001/surveys/clsv00000000000000000001/responses"+query, nil))
	if want := "/app/workspaces/clws00000000000000000001/surveys/clsv00000000000000000001/responses" + query; rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != want {
		t.Errorf("the root link = %d %s, want 308 %s", rec.Code, rec.Header().Get("Location"), want)
	}
}
