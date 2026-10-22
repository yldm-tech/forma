package response

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/survey"
)

// The cases of forma2's V1D/lib/display.test.ts, V2D/lib/display.test.ts and V1D/[displayId]/response/lib/response.test.ts, against a real database.

func TestCreateDisplayV1CreatesTheContactOnce(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{typ: survey.TypeApp})
	ctx := context.Background()

	first, err := e.svc.CreateDisplay(ctx, DisplayInput{WorkspaceID: ws, SurveyID: sv.String(), UserID: ptr("contract-user-1")})
	if err != nil {
		t.Fatal(err)
	}
	if first.ContactID == nil || first.SurveyID != sv {
		t.Fatalf("display %+v", first)
	}
	if n := e.count(`SELECT count(*) FROM contact_attributes a JOIN contact_attribute_keys k ON k.id = a.attribute_key_id
		WHERE a.contact_id = ? AND k.key = 'userId' AND a.value_text = 'contract-user-1'`, *first.ContactID); n != 1 {
		t.Fatalf("the created contact has %d userId attributes", n)
	}
	second, err := e.svc.CreateDisplay(ctx, DisplayInput{WorkspaceID: ws, SurveyID: sv.String(), UserID: ptr("contract-user-1")})
	if err != nil {
		t.Fatal(err)
	}
	if *second.ContactID != *first.ContactID || second.ID == first.ID || e.count(`SELECT count(*) FROM contacts`) != 1 {
		t.Fatalf("second display %+v reused no contact", second)
	}
	anonymous, err := e.svc.CreateDisplay(ctx, DisplayInput{WorkspaceID: ws, SurveyID: sv.String()})
	if err != nil {
		t.Fatal(err)
	}
	if anonymous.ContactID != nil {
		t.Fatalf("anonymous display has contact %v", anonymous.ContactID)
	}
}

func TestCreateDisplayV2OnlyAttachesAnExistingContact(t *testing.T) {
	e := newEnv(t)
	org, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	own := e.contact(ws, "u1")
	foreign := e.contact(e.workspace(org), "u2")
	ctx := context.Background()

	d, err := e.svc.CreateDisplay(ctx, DisplayInput{WorkspaceID: ws, SurveyID: sv.String(), ContactID: &own})
	if err != nil {
		t.Fatal(err)
	}
	if d.ContactID == nil || *d.ContactID != own {
		t.Fatalf("contact %v, want %s", d.ContactID, own)
	}
	missing := uuid.New()
	for _, c := range []uuid.UUID{foreign, missing} {
		d, err := e.svc.CreateDisplay(ctx, DisplayInput{WorkspaceID: ws, SurveyID: sv.String(), ContactID: &c})
		if err != nil {
			t.Fatal(err)
		}
		if d.ContactID != nil {
			t.Fatalf("contact %v attached", d.ContactID)
		}
	}
	if n := e.count(`SELECT count(*) FROM contacts`); n != 2 {
		t.Fatalf("%d contacts; v2 never creates one", n)
	}
}

func TestCreateDisplayRefusals(t *testing.T) {
	e := newEnv(t)
	org, ws := e.organization(e.user())
	paused := e.survey(ws, surveyOpts{status: survey.StatusPaused})
	foreign := e.survey(e.workspace(org), surveyOpts{})
	ctx := context.Background()

	_, err := e.svc.CreateDisplay(ctx, DisplayInput{WorkspaceID: ws, SurveyID: "clunknownsurveyaaaaaaaaa1"})
	appErr(t, err, http.StatusNotFound, "Survey not found", map[string]any{"resource_id": "clunknownsurveyaaaaaaaaa1", "resource_type": "Survey"})
	_, err = e.svc.CreateDisplay(ctx, DisplayInput{WorkspaceID: ws, SurveyID: foreign.String()})
	appErr(t, err, http.StatusNotFound, "Survey not found", map[string]any{"resource_id": foreign.String(), "resource_type": "Survey"})
	// Checked before any contact is written.
	_, err = e.svc.CreateDisplay(ctx, DisplayInput{WorkspaceID: ws, SurveyID: paused.String(), UserID: ptr("u1")})
	appErr(t, err, http.StatusForbidden, "Survey is not accepting submissions", map[string]any{"surveyId": paused.String()})
	if n := e.count(`SELECT count(*) FROM contacts`) + e.count(`SELECT count(*) FROM displays`); n != 0 {
		t.Fatalf("%d rows written by refused displays", n)
	}
}

func TestDisplayResponseID(t *testing.T) {
	e := newEnv(t)
	org, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	ctx := context.Background()
	d := e.display(sv, nil)

	id, err := e.svc.DisplayResponseID(ctx, ws, d.String())
	if err != nil || id != nil {
		t.Fatalf("unanswered display: %v, %v", id, err)
	}
	created, err := e.submit(ctx, ws, sv, `{}`, CreateInput{DisplayID: ptr(d.String())})
	if err != nil {
		t.Fatal(err)
	}
	id, err = e.svc.DisplayResponseID(ctx, ws, d.String())
	if err != nil || id == nil || *id != created.ID {
		t.Fatalf("answered display: %v, %v", id, err)
	}
	_, err = e.svc.DisplayResponseID(ctx, ws, "clunknowndisplayaaaaaaaa1")
	appErr(t, err, http.StatusNotFound, "Display not found", map[string]any{"resource_id": "clunknowndisplayaaaaaaaa1", "resource_type": "Display"})
	_, err = e.svc.DisplayResponseID(ctx, e.workspace(org), d.String())
	appErr(t, err, http.StatusNotFound, "Display not found", map[string]any{"resource_id": d.String(), "resource_type": "Display"})
}
