package response

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

// The client API's answers keep forma2's shapes (the recorded v1/v2 responses and displays fixtures): a quota is reported only when one is full, its ending only when it has one.
func TestClientAnswersKeepForma2Shapes(t *testing.T) {
	id := uuid.MustParse("01a16296-1cdb-7bad-910b-fb405354957b")
	quotaID := uuid.MustParse("01a16296-1cc9-74ac-a078-d29e8d432647")
	cases := []struct {
		value any
		want  string
	}{
		{Result{ID: id}, `{"id":"01a16296-1cdb-7bad-910b-fb405354957b","quotaFull":false}`},
		{Result{ID: id, QuotaFull: (&quota{ID: quotaID, Action: actionEndSurvey, EndingCardID: ptr("clendscreenquotaaaaaaaaa2")}).full()},
			`{"id":"01a16296-1cdb-7bad-910b-fb405354957b","quotaFull":true,"quota":{"id":"01a16296-1cc9-74ac-a078-d29e8d432647","action":"endSurvey","endingCardId":"clendscreenquotaaaaaaaaa2"}}`},
		{Result{ID: id, QuotaFull: (&quota{ID: quotaID, Action: "continueSurvey"}).full()},
			`{"id":"01a16296-1cdb-7bad-910b-fb405354957b","quotaFull":true,"quota":{"id":"01a16296-1cc9-74ac-a078-d29e8d432647","action":"continueSurvey"}}`},
		{Display{ID: id, SurveyID: quotaID}, `{"id":"01a16296-1cdb-7bad-910b-fb405354957b","contactId":null,"surveyId":"01a16296-1cc9-74ac-a078-d29e8d432647"}`},
	}
	for _, c := range cases {
		got, err := json.Marshal(c.value)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("%T = %s\nwant %s", c.value, got, c.want)
		}
	}
}

func TestResolveID(t *testing.T) {
	native := uuid.New()
	if got, err := ResolveID(native.String()); err != nil || got != native {
		t.Errorf("ResolveID(uuid) = %v, %v", got, err)
	}
	if got, err := ResolveID("clsurveyaaaaaaaaaaaaaaaa"); err != nil || got != legacyid.ID("clsurveyaaaaaaaaaaaaaaaa") {
		t.Errorf("ResolveID(cuid) = %v, %v", got, err)
	}
	for _, bad := range []string{"", "not-a-cuid", "UPPER", "a b"} {
		_, err := ResolveID(bad)
		appErr(t, err, http.StatusBadRequest, "Validation failed: Invalid cuid2", map[string]any{})
	}
}

func TestNotFoundNamesWhatTheCallerSent(t *testing.T) {
	appErr(t, NotFound("Organization", nil), http.StatusNotFound, "Organization not found", map[string]any{"resource_id": nil, "resource_type": "Organization"})
}
