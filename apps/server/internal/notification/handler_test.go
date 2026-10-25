package notification

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

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
	NewHandler().Register(r)
	web.Register(r)
	return r
}

// The unsubscribe links point at /app/workspaces/{id}/settings/notifications; they land on the account notification settings with the switch they name, its elementId kept when it is a UUID.
func TestUnsubscribeRedirect(t *testing.T) {
	t.Parallel()
	r := newRouter(t)
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
	r := newRouter(t)
	ws, survey := "0192f5c8-0000-7000-8000-0000000000aa", "0192f5c8-0000-7000-8000-000000000001"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/workspaces/"+ws+"/surveys/"+survey+"/responses?utm_source=email_notification", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "spa" {
		t.Errorf("the responses link = %d %q, want the SPA", rec.Code, rec.Body)
	}
}
