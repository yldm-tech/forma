package notification

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/webui"
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

// The unsubscribe links of emails already sent point at /workspaces/{id}/settings/notifications, which forma2 never served; they land on the account notification settings with the switch they name, a forma2 id resolved to the id migrated from it.
func TestUnsubscribeRedirect(t *testing.T) {
	t.Parallel()
	r := newRouter(t)
	survey := "0192f5c8-0000-7000-8000-000000000001"
	cases := map[string]string{
		"?type=alert&elementId=" + survey:                                   "/account/settings/notifications?elementId=" + survey + "&type=alert",
		"?type=unsubscribedOrganizationIds&elementId=clorg0000000000000001": "/account/settings/notifications?elementId=" + legacyid.ID("clorg0000000000000001").String() + "&type=unsubscribedOrganizationIds",
		"?type=alert":                    "/account/settings/notifications?type=alert",
		"?type=evil&elementId=" + survey: "/account/settings/notifications",
		"":                               "/account/settings/notifications",
	}
	for query, want := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/workspaces/ws1/settings/notifications"+query, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
			t.Errorf("%q = %d %s, want %s", query, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

// The view-responses CTA of emails sent before the migration names the workspace and survey by their forma2 ids; it redirects to the ids migrated from them with its query kept, and the SPA serves the canonical path.
func TestResponsesLinkRedirect(t *testing.T) {
	t.Parallel()
	r := newRouter(t)
	ws, survey := "0192f5c8-0000-7000-8000-0000000000aa", "0192f5c8-0000-7000-8000-000000000001"
	query := "?utm_source=email_notification&utm_medium=email&utm_content=view_responses_CTA"
	redirects := map[string]string{
		"/workspaces/clws00000000000000000001/surveys/clsv00000000000000000001/responses" + query: "/workspaces/" + legacyid.ID("clws00000000000000000001").String() + "/surveys/" + legacyid.ID("clsv00000000000000000001").String() + "/responses" + query,
		"/workspaces/" + ws + "/surveys/clsv00000000000000000001/responses":                       "/workspaces/" + ws + "/surveys/" + legacyid.ID("clsv00000000000000000001").String() + "/responses",
	}
	for path, want := range redirects {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
			t.Errorf("%s = %d %q, want %s", path, rec.Code, rec.Header().Get("Location"), want)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/workspaces/"+ws+"/surveys/"+survey+"/responses"+query, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "spa" {
		t.Errorf("the canonical path = %d %q, want the SPA", rec.Code, rec.Body)
	}
}
