package forma2import_test

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/authz/spicedbtest"
	"github.com/yldm-tech/forma/server/internal/billing"
	"github.com/yldm-tech/forma/server/internal/brevo"
	"github.com/yldm-tech/forma/server/internal/forma2import"
	"github.com/yldm-tech/forma/server/internal/integration"
	"github.com/yldm-tech/forma/server/internal/invite"
	"github.com/yldm-tech/forma/server/internal/linksurvey"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/mailer/mailertest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/webui"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

var (
	orgID       = legacyid.ID("clorg00000000000000000001")
	workspaceID = legacyid.ID("clws000000000000000000001")
	ownerID     = legacyid.ID("cluserowner0000000000000001")
	survey1     = "clsurvey000000000000000001"
	survey2     = "clsurvey000000000000000002"
)

// testCipher is the cipher the server runs with: ENCRYPTION_KEY as forma2 had it, key version 1.
func testCipher(t *testing.T) *secret.Cipher {
	t.Helper()
	c, err := secret.NewCipher(config.Config{EncryptionKey: []byte(encryptionKey), EncryptionKeyVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func options(t *testing.T, dsn string) forma2import.Options {
	return forma2import.Options{TargetDSN: dsn, Cipher: testCipher(t), Forma2Key: []byte(encryptionKey), BetterAuth: secret.BetterAuthSecrets{Secret: nextAuthSecret}}
}

// importEnv is the environment `forma import-forma2` reads, as a deployment sets it.
func importEnv(t *testing.T, dsn string, authzCfg *config.AuthzConfig) {
	t.Setenv("DATABASE_URL", dsn+"&schema=public")
	t.Setenv("MIGRATE_DATABASE_URL", "")
	t.Setenv("ENCRYPTION_KEY", encryptionKey)
	t.Setenv("ENCRYPTION_KEY_VERSION", "1")
	t.Setenv("ENCRYPTION_PREVIOUS_KEYS", "")
	t.Setenv("NEXTAUTH_SECRET", nextAuthSecret)
	t.Setenv("BETTER_AUTH_SECRET", "")
	t.Setenv("LOG_FORMAT", "text")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("AUTHZED_ENABLED", "0")
	t.Setenv("AUTHZED_CONSISTENCY", "fully_consistent")
	if authzCfg != nil {
		t.Setenv("AUTHZED_ENABLED", "1")
		t.Setenv("AUTHZED_ENDPOINT", authzCfg.Endpoint)
		t.Setenv("AUTHZED_TOKEN", authzCfg.Token)
		t.Setenv("AUTHZED_INSECURE", "1")
	}
}

func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := forma2import.RunCLI(context.Background(), args, &stdout, &stderr)
	return code, stdout.String() + stderr.String()
}

func openGORM(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func count(t *testing.T, db *gorm.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func table(r *forma2import.Report, name, from string) forma2import.TableReport {
	for _, t := range r.Tables {
		if t.Table == name && t.From == from {
			return t
		}
	}
	return forma2import.TableReport{Table: "missing " + name}
}

// The whole cutover on forma2's latest schema: a dry run that leaves nothing behind, the import through the CLI with the SpiceDB backfill, a re-run that changes nothing, forma2's tables untouched throughout, and this server's own code reading the result.
func TestImportForma2(t *testing.T) {
	ctx := context.Background()
	dsn := forma2DB(t, 7)
	before := forma2Checksums(t, dsn)
	for name, sum := range before {
		if strings.HasPrefix(sum, "0:") {
			t.Errorf("the seed leaves forma2's %s empty", name)
		}
	}
	db := openGORM(t, dsn)

	// The server refuses to start on forma2's data before it is imported.
	if err := forma2import.GuardDSN(ctx, dsn); !errors.Is(err, forma2import.ErrForma2NotImported) {
		t.Fatalf("guard before the import: %v", err)
	}

	dry, err := forma2import.Run(ctx, withDryRun(options(t, dsn)))
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, reportText(dry))
	}
	if dry.Committed || len(dry.Failures) > 0 || table(dry, "surveys", "Survey").Written != 2 {
		t.Fatalf("dry run report:\n%s", reportText(dry))
	}
	if n := count(t, db, `SELECT (SELECT count(*) FROM users) + (SELECT count(*) FROM surveys) + (SELECT count(*) FROM forma2_imports) + (SELECT count(*) FROM authz_projection_outbox)`); n != 0 {
		t.Fatalf("the dry run left %d rows", n)
	}

	authzCfg := spicedbtest.Config(t, readSchema(t))
	importEnv(t, dsn, &authzCfg)
	code, out := runCLI(t)
	if code != 0 {
		t.Fatalf("import exited %d:\n%s", code, out)
	}
	for _, want := range []string{"forma2 import: committed", "SpiceDB backfill: reconciled", "Session (1 rows)", "0 failures"} {
		if !strings.Contains(out, want) {
			t.Errorf("the CLI output lacks %q:\n%s", want, out)
		}
	}
	if err := forma2import.GuardDSN(ctx, dsn); err != nil {
		t.Fatalf("guard after the import: %v", err)
	}

	// A second import is refused unless asked for, and then changes nothing.
	if code, out := runCLI(t); code != 1 || !strings.Contains(out, "--refresh") {
		t.Fatalf("second import without --refresh exited %d:\n%s", code, out)
	}
	again, err := forma2import.Run(ctx, withRefresh(options(t, dsn)))
	if err != nil {
		t.Fatalf("re-run: %v\n%s", err, reportText(again))
	}
	for _, tr := range again.Tables {
		if tr.Inserted != 0 || tr.Updated != 0 || tr.Unchanged != tr.Written || tr.Present != tr.Written {
			t.Errorf("re-run changed %s from %s: %+v", tr.Table, tr.From, tr)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM forma2_imports WHERE outcome = 'imported' AND legacy_namespace = ?`, legacyid.Namespace); n != 2 {
		t.Errorf("forma2_imports holds %d imports, want 2", n)
	}
	if after := forma2Checksums(t, dsn); !maps.Equal(before, after) {
		for name, sum := range before {
			if after[name] != sum {
				t.Errorf("the import changed forma2's %s", name)
			}
		}
	}

	t.Run("report", func(t *testing.T) { checkReport(t, again) })
	t.Run("rows", func(t *testing.T) { checkRows(t, db) })
	t.Run("authorization", func(t *testing.T) { checkAuthorization(t, db, authzCfg) })
	t.Run("sign in", func(t *testing.T) { checkSignIn(t, db) })
	t.Run("surveys", func(t *testing.T) { checkSurveys(t, db) })
	t.Run("api keys", func(t *testing.T) { checkAPIKeys(t, db) })
	t.Run("integrations", func(t *testing.T) { checkIntegrations(t, db) })
}

func withDryRun(o forma2import.Options) forma2import.Options  { o.DryRun = true; return o }
func withRefresh(o forma2import.Options) forma2import.Options { o.Refresh = true; return o }

func reportText(r *forma2import.Report) string {
	if r == nil {
		return "(no report)"
	}
	var b bytes.Buffer
	forma2import.PrintReport(&b, r)
	return b.String()
}

func readSchema(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "authzed", "schema.zed"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func checkReport(t *testing.T, r *forma2import.Report) {
	if len(r.Forma2Migrations) != 7 || len(r.AbsentColumns) != 0 {
		t.Errorf("source: %v, absent %v", r.Forma2Migrations, r.AbsentColumns)
	}
	want := map[[2]string][3]int{ // forma2 rows, skipped, written
		{"organizations", "Organization"}:                           {1, 0, 1},
		{"users", "User"}:                                           {5, 0, 5},
		{"user_identities", "Account"}:                              {4, 0, 4},
		{"user_identities", "User.password, User.identityProvider"}: {-1, 0, 2},
		{"user_totp", "TwoFactor"}:                                  {1, 0, 1},
		{"user_totp", "User.twoFactorSecret, User.backupCodes"}:     {-1, 0, 1},
		{"memberships", "Membership"}:                               {5, 0, 5},
		{"invites", "Invite"}:                                       {2, 1, 1},
		{"invite_teams", "Invite.teamIds"}:                          {-1, 0, 1},
		{"api_keys", "ApiKey"}:                                      {2, 0, 2},
		{"contact_attribute_keys", "ContactAttributeKey"}:           {8, 0, 8},
		{"contact_attributes", "ContactAttribute"}:                  {4, 0, 4},
		{"surveys", "Survey"}:                                       {2, 0, 2},
		{"survey_languages", "SurveyLanguage"}:                      {2, 0, 2},
		{"embedded_data", "EmbeddedData"}:                           {3, 1, 2},
		{"survey_embedded_data", "SurveyEmbeddedData"}:              {2, 1, 1},
		{"displays", "Display"}:                                     {2, 0, 2},
		{"responses", "Response"}:                                   {2, 0, 2},
		{"webhooks", "Webhook"}:                                     {2, 0, 2},
		{"webhook_surveys", "Webhook.surveyIds"}:                    {-1, 0, 2},
		{"integrations", "Integration"}:                             {3, 0, 3},
		{"integration_credentials", "Integration.config.key"}:       {-1, 0, 3},
		{"workflow_runs", "WorkflowRun"}:                            {2, 0, 2},
		{"workflow_run_logs", "WorkflowRunLog"}:                     {2, 0, 2},
	}
	for k, w := range want {
		got := table(r, k[0], k[1])
		if got.Forma2Rows != w[0] || got.Skipped != w[1] || got.Written != w[2] {
			t.Errorf("%s from %s: forma2 %d, skipped %d, written %d; want %v", k[0], k[1], got.Forma2Rows, got.Skipped, got.Written, w)
		}
	}
	if len(r.Tables) != 43 {
		t.Errorf("%d table reports, want 43", len(r.Tables))
	}
	var notMigrated []string
	for _, n := range r.NotMigrated {
		notMigrated = append(notMigrated, n.Table)
	}
	if len(notMigrated) != 15 || !slices.Contains(notMigrated, "ResponsePipelineOutbox") {
		t.Errorf("not migrated: %v", notMigrated)
	}
	if r.SurveysValidated != 2 || r.SamplesCompared < 60 {
		t.Errorf("validated %d surveys, compared %d samples", r.SurveysValidated, r.SamplesCompared)
	}
	warnings := strings.Join(r.Warnings, "\n")
	for _, w := range []string{"more than one default language", "imported as canceled", "access_token did not decrypt"} {
		if !strings.Contains(warnings, w) {
			t.Errorf("no warning about %q:\n%s", w, warnings)
		}
	}
}

// checkRows looks at the rows where the mapping does more than copy.
func checkRows(t *testing.T, db *gorm.DB) {
	var settings string
	db.Raw(`SELECT notification_settings::text FROM users WHERE id = ?`, ownerID).Scan(&settings)
	for _, want := range []string{`"` + legacyid.ID(survey1).String() + `": true`, `["` + orgID.String() + `"]`, `"weeklySummary"`} {
		if !strings.Contains(settings, want) {
			t.Errorf("notification settings %s lack %s", settings, want)
		}
	}
	var identities []struct {
		UserID   uuid.UUID
		Provider string
		Issuer   string
		Subject  string
		HasHash  bool
	}
	db.Raw(`SELECT user_id, provider, issuer, subject, password_hash IS NOT NULL AS has_hash FROM user_identities ORDER BY provider, subject`).Scan(&identities)
	got := map[string]string{}
	for _, i := range identities {
		if i.Issuer != "" {
			t.Errorf("identity %s has issuer %q", i.Provider, i.Issuer)
		}
		if (i.Provider == "credential") != i.HasHash || (i.Provider == "credential" && i.Subject != i.UserID.String()) {
			t.Errorf("identity %+v", i)
		}
		got[i.Provider+":"+i.Subject] = i.UserID.String()
	}
	wantIdentities := map[string]string{
		"azuread:azure-oid-1":            legacyid.ID("clusermember000000000000001").String(),
		"github:gh-4242":                 legacyid.ID("clusersso000000000000000001").String(),
		"google:google-sub-1":            legacyid.ID("clusersso000000000000000001").String(),
		"credential:" + ownerID.String(): ownerID.String(),
	}
	for k, v := range wantIdentities {
		if got[k] != v {
			t.Errorf("identity %s belongs to %q, want %s (all: %v)", k, got[k], v, got)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM user_identities WHERE provider = 'credential'`); n != 3 {
		t.Errorf("%d password identities, want 3", n)
	}

	var invite struct {
		Email string
		Role  string
	}
	db.Raw(`SELECT email, role FROM invites`).Scan(&invite)
	if invite.Email != "invitee@example.com" || invite.Role != "manager" {
		t.Errorf("invite %+v, want the newer one, lowercased", invite)
	}
	if n := count(t, db, `SELECT count(*) FROM invite_teams WHERE team_id = ?`, legacyid.ID("clteam00000000000000000001")); n != 1 {
		t.Errorf("invite teams: %d", n)
	}
	var key struct {
		LookupHash string
		HashedKey  string
		CreatedBy  *uuid.UUID
	}
	db.Raw(`SELECT lookup_hash, hashed_key, created_by FROM api_keys WHERE id = ?`, legacyid.ID("clapikeylegacy000000000001")).Scan(&key)
	if key.LookupHash != sha256Hex(legacyKey) || key.HashedKey != key.LookupHash || key.CreatedBy != nil {
		t.Errorf("legacy api key %+v", key)
	}

	var attrs []struct {
		Key       string
		ValueText *string
		ValueNum  *float64
		ValueDate *time.Time
	}
	db.Raw(`SELECT k.key, a.value_text, a.value_number AS value_num, a.value_date FROM contact_attributes a JOIN contact_attribute_keys k ON k.id = a.attribute_key_id ORDER BY k.key`).Scan(&attrs)
	if len(attrs) != 4 || attrs[0].Key != "age" || attrs[0].ValueNum == nil || *attrs[0].ValueNum != 42 || attrs[0].ValueText != nil ||
		attrs[1].ValueDate == nil || !attrs[1].ValueDate.Equal(time.Date(1990, 1, 2, 0, 0, 0, 0, time.UTC)) ||
		attrs[3].Key != "score" || attrs[3].ValueText == nil || *attrs[3].ValueText != "7.5" {
		t.Errorf("contact attributes %+v", attrs)
	}

	var segment struct {
		Filters string
	}
	db.Raw(`SELECT filters::text FROM segments WHERE id = ?`, legacyid.ID("clsegment0000000000000000001")).Scan(&segment)
	for _, want := range []string{`"segmentId": "` + legacyid.ID("clsegment0000000000000000002").String(), `"surveyIds": ["` + legacyid.ID(survey1).String(), `"contactAttributeKey": "age"`} {
		if !strings.Contains(segment.Filters, want) {
			t.Errorf("segment filters %s lack %s", segment.Filters, want)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM segments WHERE is_private AND title = ?`, legacyid.ID(survey2).String()); n != 1 {
		t.Error("the private segment is not titled with its survey's new id")
	}

	var sv struct {
		Styling           *string
		DisplayPercentage *float64
	}
	db.Raw(`SELECT styling::text, display_percentage FROM surveys WHERE id = ?`, legacyid.ID(survey2)).Scan(&sv)
	if sv.DisplayPercentage == nil || *sv.DisplayPercentage != 12.35 {
		t.Errorf("display percentage %v, want 12.35", sv.DisplayPercentage)
	}
	if n := count(t, db, `SELECT count(*) FROM surveys WHERE id = ? AND styling IS NULL`, legacyid.ID(survey1)); n != 1 {
		t.Error("JSON null styling was not stored as SQL NULL")
	}
	if n := count(t, db, `SELECT count(*) FROM survey_languages WHERE survey_id = ? AND is_default`, legacyid.ID(survey1)); n != 1 {
		t.Errorf("%d default languages", n)
	}

	var hooks []struct {
		Triggers   string
		AllSurveys bool
	}
	db.Raw(`SELECT triggers::text, all_surveys FROM webhooks ORDER BY name NULLS LAST`).Scan(&hooks)
	if len(hooks) != 2 || hooks[0].Triggers != "{responseFinished}" || !hooks[0].AllSurveys || hooks[1].AllSurveys {
		t.Errorf("webhooks %+v", hooks)
	}
	if n := count(t, db, `SELECT count(*) FROM webhook_surveys WHERE survey_id IN (?, ?)`, legacyid.ID(survey1), legacyid.ID("clsurveydeleted0000000001")); n != 2 {
		t.Errorf("webhook surveys: %d", n)
	}

	var integ struct {
		Config              string
		LastErrorMessage    *string
		ConsecutiveFailures int
	}
	db.Raw(`SELECT config::text, last_error_message, consecutive_failures FROM integrations WHERE id = ?`, legacyid.ID("clintsheets0000000000000001")).Scan(&integ)
	if integ.Config != `{"data": []}` || integ.LastErrorMessage == nil || integ.ConsecutiveFailures != 3 {
		t.Errorf("sheets integration %+v", integ)
	}

	var wf struct{ TriggerSurveyID *uuid.UUID }
	db.Raw(`SELECT trigger_survey_id FROM workflows`).Scan(&wf)
	if wf.TriggerSurveyID == nil || *wf.TriggerSurveyID != legacyid.ID(survey1) {
		t.Errorf("workflow trigger survey %v", wf.TriggerSurveyID)
	}
	var run struct {
		Status        string
		NextAttemptAt *time.Time
		Error         *string
	}
	db.Raw(`SELECT status, next_attempt_at, error FROM workflow_runs WHERE id = ?`, legacyid.ID("clworkflowrun0000000000002")).Scan(&run)
	if run.Status != "canceled" || run.NextAttemptAt != nil || run.Error == nil {
		t.Errorf("unfinished run %+v", run)
	}
	if n := count(t, db, `SELECT count(*) FROM workflow_run_logs WHERE status = 'skipped'`); n != 1 {
		t.Errorf("%d skipped run logs", n)
	}
	if n := count(t, db, `SELECT count(*) FROM organizations WHERE workflow_run_retention_days = 90 AND display_retention_days = 30 AND created_at = '2026-09-20 10:00:00.123+00'`); n != 1 {
		t.Error("organization retention or creation time not carried over")
	}
	// Every imported organization, membership, user, team, workspace, grant and key queued its SpiceDB projection.
	if n := count(t, db, `SELECT count(DISTINCT target_type) FROM authz_projection_outbox`); n != 9 {
		t.Errorf("the outbox holds %d target types, want 9", n)
	}
}

func checkAuthorization(t *testing.T, db *gorm.DB, acfg config.AuthzConfig) {
	log := slog.New(slog.DiscardHandler)
	client, err := authz.NewClient(acfg, authz.RequestProfile, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	a := authz.NewAuthorizer(config.Config{UserManagementMinimumRole: "manager"}, db, client)
	ctx := context.Background()
	for _, tc := range []struct {
		user   string
		action authz.Action
		want   bool
	}{
		{"cluserowner0000000000000001", authz.WorkspaceManage, true},
		{"cluserlegacy2fa000000000001", authz.WorkspaceWrite, true}, // through the team's readWrite grant
		{"clusersso000000000000000001", authz.WorkspaceWrite, false},
	} {
		ok, err := a.Can(ctx, authz.User(legacyid.ID(tc.user)), tc.action, authz.Workspace(workspaceID))
		if err != nil || ok != tc.want {
			t.Errorf("%s %s: %v, %v; want %v", tc.user, tc.action, ok, err, tc.want)
		}
	}
}

// checkSignIn signs in through the auth module's routes: with forma2's bcrypt password, and with a second factor whose secret was re-encrypted, from a TwoFactor row and from forma2's legacy columns.
func checkSignIn(t *testing.T, db *gorm.DB) {
	webApp, _ := url.Parse("http://app.example.com")
	cfg := config.Config{
		WebAppURL: webApp, RateLimitingDisabled: true, TrustedProxyHopCount: 1, MailFrom: "noreply@example.com", MailFromName: "Forma",
		SMTPHost: "smtp.example.com", SMTPPort: "587", PasswordResetTokenLifetimeMinutes: 30, SessionMaxAge: 86400,
		EncryptionKey: []byte(encryptionKey), EncryptionKeyVersion: 1, UserManagementMinimumRole: "manager",
	}
	log := slog.New(slog.DiscardHandler)
	rdb := valkeytest.Client(t)
	m, err := mailer.NewWithTransport(cfg, &mailertest.Transport{}, log)
	if err != nil {
		t.Fatal(err)
	}
	limiter, emitter := ratelimit.New(cfg, rdb, log), audit.New(cfg, log)
	sessions := auth.NewSessions(cfg, db, log)
	invites := invite.NewService(cfg, invite.NewRepository(db), authz.NewAuthorizer(cfg, db, nil), limiter, m, emitter, log)
	enqueuer, err := jobs.NewEnqueuer(log)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := billing.NewLifecycle(cfg, billing.NewStripe(cfg, log), db, enqueuer, log)
	svc := auth.NewService(cfg, auth.NewRepository(db), sessions, invites, limiter, m, emitter, rdb, lifecycle, enqueuer, brevo.NewContacts(cfg), log)
	r := gin.New()
	group := r.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg))
	auth.NewHandler(cfg, svc, sessions, auth.NewTwoFactor(cfg, svc, testCipher(t))).Register(group)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	signIn := func(email, password string, second func(post func(string, any) map[string]any)) {
		t.Helper()
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar}
		post := func(path string, body any) map[string]any {
			t.Helper()
			raw, _ := json.Marshal(body)
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/app"+path, bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://app.example.com")
			req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			out := map[string]any{}
			b, _ := io.ReadAll(res.Body)
			_ = json.Unmarshal(b, &out)
			out["httpStatus"] = res.StatusCode
			return out
		}
		res := post("/auth/sign-in", map[string]string{"email": email, "password": password})
		if second == nil {
			if res["status"] != "signed_in" {
				t.Errorf("%s: sign-in answered %v", email, res)
			}
			return
		}
		if res["status"] != "two_factor_required" {
			t.Errorf("%s: sign-in answered %v, want a second factor", email, res)
			return
		}
		second(post)
	}

	signIn("Owner@Example.com", ownerPassword, nil)
	withTOTP := func(key []byte, what string) func(post func(string, any) map[string]any) {
		return func(post func(string, any) map[string]any) {
			code, err := totp.GenerateCodeCustom(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key), time.Now(),
				totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
			if err != nil {
				t.Fatal(err)
			}
			if res := post("/auth/two-factor/verify-totp", map[string]string{"code": code}); res["httpStatus"] != http.StatusOK {
				t.Errorf("TOTP from %s: %v", what, res)
			}
		}
	}
	signIn("twofactor@example.com", twoFactorPassword, withTOTP([]byte(betterAuthTOTPKey), "the Better Auth secret"))
	signIn("legacy@example.com", legacyPassword, withTOTP([]byte("12345678901234567890"), "the legacy secret"))
	signIn("legacy@example.com", legacyPassword, func(post func(string, any) map[string]any) {
		if res := post("/auth/two-factor/verify-backup-code", map[string]string{"code": "abcde-f0123"}); res["httpStatus"] != http.StatusOK {
			t.Errorf("backup code from the legacy columns: %v", res)
		}
	})
	// A wrong password is still refused.
	jar2, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar2}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/app/auth/sign-in", strings.NewReader(`{"email":"owner@example.com","password":"wrong-password-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://app.example.com")
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("a wrong password answered %d", res.StatusCode)
	}
}

// checkSurveys lists the workspace's surveys through the survey module and opens a survey's public page by its forma2 id.
func checkSurveys(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	webApp, _ := url.Parse("http://localhost:3999")
	cfg := config.Config{WebAppURL: webApp, RateLimitingDisabled: true, TrustedProxyHopCount: 1, MailFrom: "noreply@example.com", MailFromName: "Forma",
		EncryptionKey: []byte(encryptionKey), EncryptionKeyText: config.Secret(encryptionKey), EncryptionKeyVersion: 1, NextAuthSecret: nextAuthSecret, SurveySchedulingTimeZone: "UTC"}
	log := slog.New(slog.DiscardHandler)
	cache := valkeytest.Client(t)
	limiter := ratelimit.New(cfg, cache, log)
	repo := survey.NewRepository(db)
	surveys := survey.NewService(cfg, repo, nil, limiter, audit.New(cfg, log), nil, testCipher(t), nil, nil, log)

	list, err := surveys.ListV1(ctx, []uuid.UUID{workspaceID}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]*survey.Survey{}
	for _, s := range list {
		names[s.Name] = s
	}
	if len(list) != 2 || names["Customer feedback"] == nil || names["In-app check"] == nil {
		t.Fatalf("listed %d surveys: %v", len(list), slices.Collect(maps.Keys(names)))
	}
	var blocks []map[string]any
	_ = json.Unmarshal(names["In-app check"].Blocks, &blocks)
	if len(blocks) != 2 || blocks[0]["name"] != "Block 1" || blocks[0]["buttonLabel"] == nil {
		t.Errorf("the legacy questions became %s", names["In-app check"].Blocks)
	}

	m, err := mailer.NewWithTransport(cfg, &mailertest.Transport{}, log)
	if err != nil {
		t.Fatal(err)
	}
	svc := linksurvey.NewService(cfg, linksurvey.NewRepository(db, repo, cache, log), surveys, m, log)
	web, err := webui.NewFromFS(fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}, "survey.html": {Data: []byte(`<!doctype html><html><head><!--forma:head--></head><body><!--forma:state--></body></html>`)}})
	if err != nil {
		t.Fatal(err)
	}
	engine := httpserver.NewEngine(cfg, log)
	linksurvey.NewHandler(cfg, svc, limiter, web, log).Register(engine)
	for _, path := range []string{"/s/" + survey1, "/s/" + legacyid.ID(survey1).String()} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "What did you like?") {
			t.Errorf("GET %s: %d\n%s", path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/p/feedback", nil))
	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("GET /p/feedback: %d", rec.Code)
	}
}

// checkAPIKeys authenticates forma2's keys: a current one (bcrypt, found by its lookup hash) and one from before forma2 added bcrypt.
func checkAPIKeys(t *testing.T, db *gorm.DB) {
	a := apikey.NewAuthenticator(db, slog.New(slog.DiscardHandler))
	for _, raw := range []string{apikey.Prefix + modernKeySecret, legacyKey} {
		h := http.Header{}
		h.Set(apikey.HeaderAPIKey, raw)
		p, err := a.Authenticate(context.Background(), h, apikey.Options{})
		if err != nil || p == nil || p.OrganizationID != orgID || len(p.WorkspacePermissions) != 1 || p.WorkspacePermissions[0].WorkspaceID != workspaceID {
			t.Errorf("key %s…: %+v, %v", raw[:6], p, err)
		}
	}
}

// checkIntegrations opens the sealed credentials the way delivery does.
func checkIntegrations(t *testing.T, db *gorm.DB) {
	repo := integration.NewRepository(db, testCipher(t), slog.New(slog.DiscardHandler))
	ctx := context.Background()
	key := func(typ string) (map[string]any, string) {
		in, err := repo.ByType(ctx, workspaceID, typ)
		if err != nil || in == nil || in.Credentials == nil {
			t.Fatalf("%s: %+v, %v", typ, in, err)
		}
		k := map[string]any{}
		_ = json.Unmarshal(in.Credentials.Key, &k)
		return k, in.Credentials.Email
	}
	if k, _ := key("notion"); k["access_token"] != notionToken || k["bot_id"] != "bot-1" {
		t.Errorf("notion key %v", k)
	}
	if k, email := key("googleSheets"); k["access_token"] != sheetsAccess || k["refresh_token"] != sheetsRefresh || email != "sheets@example.com" || k["expiry_date"] != float64(1760000000000) {
		t.Errorf("google sheets key %v, %s", k, email)
	}
	if k, _ := key("slack"); !strings.Contains(k["access_token"].(string), ":") {
		t.Errorf("slack key %v: the undecryptable token should be kept as stored", k)
	}
	in, err := repo.ByType(ctx, workspaceID, "notion")
	if err != nil {
		t.Fatal(err)
	}
	var links struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal(in.Config, &links)
	if len(links.Data) != 1 || links.Data[0]["surveyId"] != survey1 {
		t.Errorf("notion links %s", in.Config)
	}
}

// Production ran forma2's init migration only: the import reads that shape and defaults what later migrations added.
func TestImportForma2InitSchema(t *testing.T) {
	dsn := forma2DB(t, 1)
	r, err := forma2import.Run(context.Background(), options(t, dsn))
	if err != nil {
		t.Fatalf("%v\n%s", err, reportText(r))
	}
	if !r.Committed || len(r.Forma2Migrations) != 1 || len(r.AbsentColumns) != 7 {
		t.Fatalf("report:\n%s", reportText(r))
	}
	db := openGORM(t, dsn)
	if n := count(t, db, `SELECT count(*) FROM organizations WHERE workflow_run_retention_days IS NULL AND display_retention_days IS NULL`); n != 1 {
		t.Error("retention windows not defaulted")
	}
	if n := count(t, db, `SELECT count(*) FROM integrations WHERE consecutive_failures = 0 AND last_error_at IS NULL`); n != 3 {
		t.Error("integration health not defaulted")
	}
	if n := count(t, db, `SELECT count(*) FROM workflows WHERE trigger_survey_id = ?`, legacyid.ID(survey1)); n != 1 {
		t.Error("trigger survey not derived from the published version")
	}
}

// A run that does not verify writes nothing: here a survey forma2 saved with an invalid document.
func TestImportForma2RollsBackWhatDoesNotVerify(t *testing.T) {
	ctx := context.Background()
	dsn := forma2DB(t, 1)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, conn, `UPDATE "Survey" SET "welcomeCard" = '{"enabled": true}' WHERE id = $1`, survey1)
	_ = conn.Close(ctx)

	r, err := forma2import.Run(ctx, options(t, dsn))
	if !errors.Is(err, forma2import.ErrVerification) || r == nil || r.Committed || !strings.Contains(strings.Join(r.Failures, "\n"), legacyid.ID(survey1).String()) {
		t.Fatalf("%v\n%s", err, reportText(r))
	}
	db := openGORM(t, dsn)
	if n := count(t, db, `SELECT (SELECT count(*) FROM surveys) + (SELECT count(*) FROM forma2_imports)`); n != 0 {
		t.Fatalf("a run that failed verification left %d rows", n)
	}
	if err := forma2import.GuardDSN(ctx, dsn); !errors.Is(err, forma2import.ErrForma2NotImported) {
		t.Fatalf("guard: %v", err)
	}
}

// Discarding forma2's data is a recorded decision that lets the server start; an import over it then needs --refresh.
func TestDiscardForma2Data(t *testing.T) {
	ctx := context.Background()
	dsn := forma2DB(t, 1)
	if err := forma2import.Discard(ctx, dsn, " "); err == nil {
		t.Fatal("discarded without a reason")
	}
	if err := forma2import.Discard(ctx, dsn, "fresh start on the old database"); err != nil {
		t.Fatal(err)
	}
	if err := forma2import.GuardDSN(ctx, dsn); err != nil {
		t.Fatalf("guard after the discard: %v", err)
	}
	if err := forma2import.Discard(ctx, dsn, "again"); !errors.Is(err, forma2import.ErrAlreadyRecorded) {
		t.Fatalf("second discard: %v", err)
	}
	if _, err := forma2import.Run(ctx, options(t, dsn)); !errors.Is(err, forma2import.ErrAlreadyRecorded) {
		t.Fatalf("import after the discard: %v", err)
	}
}

// The guard leaves a database without forma2's data alone.
func TestGuardIgnoresFreshDatabases(t *testing.T) {
	ctx := context.Background()
	dsn := forma2DB(t, 1)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	exec(t, conn, `TRUNCATE "User" CASCADE`)
	if err := forma2import.Guard(ctx, conn); err != nil {
		t.Fatalf("empty forma2 tables: %v", err)
	}
	exec(t, conn, `DROP TABLE "User" CASCADE`)
	if err := forma2import.Guard(ctx, conn); err != nil {
		t.Fatalf("no forma2 tables: %v", err)
	}
}
