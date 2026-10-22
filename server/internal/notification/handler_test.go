package notification

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

// The unsubscribe links of emails already sent point at /workspaces/{id}/settings/notifications, which forma2 never served; they land on the account notification settings with the switch they name, a forma2 id resolved to the id migrated from it.
func TestUnsubscribeRedirect(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler().Register(r)
	survey := "0192f5c8-0000-7000-8000-000000000001"
	cases := map[string]string{
		"?type=alert&elementId=" + survey:                                  "/account/settings/notifications?elementId=" + survey + "&type=alert",
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
