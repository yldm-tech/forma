package seed

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/response/answers"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// seededTables are the tables a seed writes to, counted to prove a second seed adds nothing.
var seededTables = []string{
	"organizations", "organization_billing", "users", "user_identities", "memberships", "workspaces", "languages", "contact_attribute_keys",
	"surveys", "survey_languages", "displays", "responses", "workflows", "workflow_versions", "workflow_runs", "workflow_run_logs",
	"action_classes", "api_keys", "api_key_workspaces",
}

func counts(t *testing.T, db *gorm.DB) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, table := range seededTables {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		out[table] = n
	}
	return out
}

func count(t *testing.T, db *gorm.DB, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(sql, args...).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestContractSeedIsIdempotentAndUsable(t *testing.T) {
	ctx := context.Background()
	db := pgtest.GORM(t)
	const keySecret = "0123456789abcdef0123456789abcdef"
	opts := Options{APIKeySecret: keySecret, Contract: true}

	fixtures, err := Run(ctx, db, opts, discard)
	if err != nil {
		t.Fatal(err)
	}
	first := counts(t, db)
	for _, table := range seededTables {
		if first[table] == 0 {
			t.Errorf("the seed wrote nothing to %s", table)
		}
	}
	if first["responses"] != 150 || first["displays"] != 195 {
		t.Errorf("responses %d, displays %d; want forma2's 3 x 50 responses and 30%% more displays", first["responses"], first["displays"])
	}

	if _, err := Run(ctx, db, opts, discard); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if second := counts(t, db); !maps.Equal(first, second) {
		t.Fatalf("a second seed changed the row counts:\nfirst  %v\nsecond %v", first, second)
	}

	// The authorization triggers saw the writes, so the worker projects the seeded tenancy into SpiceDB.
	for _, target := range []string{"organization", "membership", "workspace", "api_key", "api_key_workspace"} {
		if count(t, db, `SELECT count(*) FROM authz_projection_outbox WHERE target_type = ?`, target) == 0 {
			t.Errorf("no %s projection was enqueued", target)
		}
	}

	// Every seeded user signs in with the documented password. They share one hash, so one bcrypt check covers all three.
	var hashes []string
	if err := db.Raw(`SELECT DISTINCT password_hash FROM user_identities WHERE user_id IN ? AND provider = 'credential'`, []uuid.UUID{Users[0].ID, Users[1].ID, Users[2].ID}).Scan(&hashes).Error; err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM user_identities WHERE provider = 'credential' AND user_id IN ?`, []uuid.UUID{Users[0].ID, Users[1].ID, Users[2].ID}); n != 3 || len(hashes) != 1 {
		t.Fatalf("%d credential identities with %d distinct hashes, want 3 sharing one", n, len(hashes))
	}
	if ok, err := secret.CheckPassword(hashes[0], Password); err != nil || !ok {
		t.Errorf("the seeded users cannot sign in with the seed password: %v", err)
	}

	// The key built from SEED_API_KEY authenticates with manage access to the seeded workspace and nothing at the organization level.
	h := http.Header{}
	h.Set(apikey.HeaderAPIKey, apikey.Prefix+keySecret)
	p, err := apikey.NewAuthenticator(db, discard).Authenticate(ctx, h, apikey.Options{})
	if err != nil {
		t.Fatalf("the seeded API key does not authenticate: %v", err)
	}
	if p.APIKeyID != APIKeyID || p.OrganizationID != OrganizationID || len(p.WorkspacePermissions) != 1 ||
		p.WorkspacePermissions[0].WorkspaceID != WorkspaceID || p.WorkspacePermissions[0].Permission != "manage" || p.OrganizationAccess != (apikey.OrganizationAccess{}) {
		t.Errorf("seeded key principal = %+v", p)
	}

	// Every fixture names a row of the seeded workspace.
	ids := []uuid.UUID{fixtures.Read["surveyId"], fixtures.Read["workflowId"], fixtures.Read["runId"]}
	for _, op := range fixtures.Operations {
		for _, id := range op.Path {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		n := count(t, db, `SELECT (SELECT count(*) FROM surveys WHERE id = ? AND workspace_id = ?) + (SELECT count(*) FROM workflows WHERE id = ? AND workspace_id = ?) + (SELECT count(*) FROM workflow_runs WHERE id = ? AND workspace_id = ?)`,
			id, WorkspaceID, id, WorkspaceID, id, WorkspaceID)
		if n != 1 {
			t.Errorf("fixture %s names %d rows of the seeded workspace, want 1", id, n)
		}
	}
	if fixtures.WorkspaceID != WorkspaceID || len(fixtures.Operations) != 12 {
		t.Errorf("fixtures = %+v", fixtures)
	}
	if n := count(t, db, `SELECT count(*) FROM survey_languages WHERE survey_id = ?`, fixtures.Read["surveyId"]); n != int64(len(readSurveyLanguages)) {
		t.Errorf("the read fixture configures %d languages, want %d", n, len(readSurveyLanguages))
	}
	if n := count(t, db, `SELECT count(*) FROM surveys WHERE id = ? AND archived_at IS NOT NULL`, fixtures.Operations["restoreSurveyV3"].Path["surveyId"]); n != 1 {
		t.Error("the restore fixture is not archived")
	}
}

func TestClearRemovesOnlySeededRows(t *testing.T) {
	ctx := context.Background()
	db := pgtest.GORM(t)

	var foreignOrg, foreignUser uuid.UUID
	if err := db.Raw(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`).Row().Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`INSERT INTO users (name, email) VALUES ('Ada', 'ada@example.com') RETURNING id`).Row().Scan(&foreignUser); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Acme workspace')`, foreignOrg).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := Run(ctx, db, Options{}, discard); err != nil {
		t.Fatal(err)
	}
	seeded := counts(t, db)
	// A survey someone added to the seeded workspace belongs to the seed's organization and goes with it.
	if err := db.Exec(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Added by hand')`, WorkspaceID).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := Run(ctx, db, Options{Clear: true}, discard); err != nil {
		t.Fatal(err)
	}
	if reseeded := counts(t, db); !maps.Equal(seeded, reseeded) {
		t.Errorf("clear and seed did not converge on a fresh seed:\nseeded   %v\nreseeded %v", seeded, reseeded)
	}

	if err := Clear(ctx, db); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM organizations WHERE id = ?`, OrganizationID); n != 0 {
		t.Error("the seeded organization survived Clear")
	}
	if n := count(t, db, `SELECT count(*) FROM users WHERE email LIKE '%@forma.yldm.ai'`); n != 0 {
		t.Errorf("%d seeded users survived Clear", n)
	}
	if n := count(t, db, `SELECT count(*) FROM surveys`); n != 0 {
		t.Errorf("%d surveys survived Clear", n)
	}
	if n := count(t, db, `SELECT (SELECT count(*) FROM organizations WHERE id = ?) + (SELECT count(*) FROM users WHERE id = ?) + (SELECT count(*) FROM workspaces WHERE organization_id = ?)`, foreignOrg, foreignUser, foreignOrg); n != 3 {
		t.Errorf("Clear removed rows it did not seed: %d of 3 left", n)
	}
}

func TestRunRefusesUnusableOptions(t *testing.T) {
	for name, opts := range map[string]Options{
		"contract without a key": {Contract: true},
		"key outside base64url":  {APIKeySecret: "not a key"},
	} {
		t.Run(name, func(t *testing.T) {
			// Refused before the database is touched.
			if _, err := Run(context.Background(), nil, opts, discard); err == nil {
				t.Fatal("Run accepted the options")
			}
		})
	}
}

// The generated answers are answers the survey runtime itself would submit.
func TestSeededAnswersPassResponseValidation(t *testing.T) {
	for _, sv := range seedSurveys {
		for i := range 12 {
			// Through JSON, as the database hands the data back.
			var data map[string]any
			if err := json.Unmarshal([]byte(jsonOf(responseData(sv.elements, i))), &data); err != nil {
				t.Fatal(err)
			}
			errs, err := answers.Validate(blocksOf(sv.id, sv.elements), data, "default", "en-US", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(errs) > 0 {
				t.Fatalf("%s response %d: %+v", sv.name, i, errs)
			}
		}
	}
}
