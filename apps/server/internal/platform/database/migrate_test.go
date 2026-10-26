package database

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/yldm-tech/forma/apps/server/migrations"
)

// startPostgres runs a throwaway PostgreSQL 18, the minimum version the schema supports (uuidv7()).
func startPostgres(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("forma"),
		tcpostgres.WithUsername("forma"),
		tcpostgres.WithPassword("forma"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return dsn
}

func migratedDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := startPostgres(t)
	if err := MigrateUp(context.Background(), dsn, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func mustScanID(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	var id string
	if err := db.QueryRow(query, args...).Scan(&id); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return id
}

// wantViolation asserts the statement is rejected with the given SQLSTATE (23514 check, 23505 unique, 23503 foreign key).
func wantViolation(t *testing.T, db *sql.DB, code string, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("exec %q: got %v, want SQLSTATE %s", query, err, code)
	}
}

func TestMigrationsRoundTrip(t *testing.T) {
	dsn := startPostgres(t)
	ctx := context.Background()
	provider, cleanup, err := NewMigrator(dsn, migrations.FS)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	defer cleanup()

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up: %v", err)
	}
	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatalf("down to 0: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up again after down: %v", err)
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if version < 1 {
		t.Fatalf("version = %d after up, want >= 1", version)
	}
}

// seed creates an organization, a user and a workspace, the parents almost every other table needs.
func seed(t *testing.T, db *sql.DB) (orgID, userID, workspaceID string) {
	t.Helper()
	orgID = mustScanID(t, db, `INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	userID = mustScanID(t, db, `INSERT INTO users (name, email) VALUES ('Ada', 'ada@example.com') RETURNING id`)
	workspaceID = mustScanID(t, db, `INSERT INTO workspaces (organization_id, name) VALUES ($1, 'Main') RETURNING id`, orgID)
	return orgID, userID, workspaceID
}

func TestSchemaConstraints(t *testing.T) {
	db := migratedDB(t)
	orgID, userID, workspaceID := seed(t, db)

	t.Run("emails are unique regardless of case", func(t *testing.T) {
		wantViolation(t, db, "23505", `INSERT INTO users (name, email) VALUES ('Ada 2', 'ADA@example.com')`)
	})

	t.Run("only a credential identity may have a password hash, and it may outlive its password", func(t *testing.T) {
		wantViolation(t, db, "23514", `INSERT INTO user_identities (user_id, provider, subject, password_hash) VALUES ($1, 'google', '123', 'x')`, userID)
		mustExec(t, db, `INSERT INTO user_identities (user_id, provider, subject, password_hash) VALUES ($1, 'credential', 'ada@example.com', '$2a$12$hash')`, userID)
		mustExec(t, db, `UPDATE user_identities SET password_hash = NULL WHERE user_id = $1 AND provider = 'credential'`, userID)
	})

	t.Run("an authorization request in flight is a verification", func(t *testing.T) {
		mustExec(t, db, `INSERT INTO verifications (purpose, email, token_hash, expires_at) VALUES ('oauth_state', '', '\x01', now())`)
		wantViolation(t, db, "23514", `INSERT INTO verifications (purpose, email, token_hash, expires_at) VALUES ('oauth_other', '', '\x02', now())`)
	})

	t.Run("a contact attribute holds exactly one typed value", func(t *testing.T) {
		contactID := mustScanID(t, db, `INSERT INTO contacts (workspace_id) VALUES ($1) RETURNING id`, workspaceID)
		keyID := mustScanID(t, db, `INSERT INTO contact_attribute_keys (workspace_id, key, data_type) VALUES ($1, 'plan', 'string') RETURNING id`, workspaceID)
		wantViolation(t, db, "23514", `INSERT INTO contact_attributes (contact_id, attribute_key_id) VALUES ($1, $2)`, contactID, keyID)
		wantViolation(t, db, "23514", `INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text, value_number) VALUES ($1, $2, 'pro', 1)`, contactID, keyID)
		mustExec(t, db, `INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) VALUES ($1, $2, 'pro')`, contactID, keyID)
	})

	surveyID := mustScanID(t, db, `INSERT INTO surveys (workspace_id, name) VALUES ($1, 'NPS') RETURNING id`, workspaceID)

	t.Run("survey values outside their ranges are rejected", func(t *testing.T) {
		wantViolation(t, db, "23514", `UPDATE surveys SET display_percentage = 100.5 WHERE id = $1`, surveyID)
		wantViolation(t, db, "23514", `UPDATE surveys SET status = 'live' WHERE id = $1`, surveyID)
		wantViolation(t, db, "23514", `UPDATE surveys SET blocks = '{}' WHERE id = $1`, surveyID)
	})

	t.Run("embedded data is either local to a survey or in the library, never both or neither", func(t *testing.T) {
		wantViolation(t, db, "23514", `INSERT INTO embedded_data (workspace_id, name, source) VALUES ($1, 'x', 'computed')`, workspaceID)
		wantViolation(t, db, "23514", `INSERT INTO embedded_data (workspace_id, survey_id, key, name, source) VALUES ($1, $2, 'k', 'x', 'computed')`, workspaceID, surveyID)
		mustExec(t, db, `INSERT INTO embedded_data (workspace_id, survey_id, name, source) VALUES ($1, $2, 'local', 'computed')`, workspaceID, surveyID)
		mustExec(t, db, `INSERT INTO embedded_data (workspace_id, key, name, source) VALUES ($1, 'brand', 'Brand', 'ingested')`, workspaceID)
	})

	t.Run("embedded data cannot point at a survey in another workspace", func(t *testing.T) {
		otherWorkspaceID := mustScanID(t, db, `INSERT INTO workspaces (organization_id, name) VALUES ($1, 'Other') RETURNING id`, orgID)
		wantViolation(t, db, "23503", `INSERT INTO embedded_data (workspace_id, survey_id, name, source) VALUES ($1, $2, 'x', 'computed')`, otherWorkspaceID, surveyID)
	})

	t.Run("a survey has at most one default language", func(t *testing.T) {
		en := mustScanID(t, db, `INSERT INTO languages (workspace_id, code) VALUES ($1, 'en') RETURNING id`, workspaceID)
		de := mustScanID(t, db, `INSERT INTO languages (workspace_id, code) VALUES ($1, 'de') RETURNING id`, workspaceID)
		mustExec(t, db, `INSERT INTO survey_languages (survey_id, language_id, is_default) VALUES ($1, $2, true)`, surveyID, en)
		wantViolation(t, db, "23505", `INSERT INTO survey_languages (survey_id, language_id, is_default) VALUES ($1, $2, true)`, surveyID, de)
	})

	t.Run("responses keep their answers as a JSON object", func(t *testing.T) {
		wantViolation(t, db, "23514", `INSERT INTO responses (survey_id, data) VALUES ($1, '[]')`, surveyID)
		mustExec(t, db, `INSERT INTO responses (survey_id, data) VALUES ($1, '{"q1": "yes"}')`, surveyID)
	})

	t.Run("webhook triggers are limited to the pipeline events", func(t *testing.T) {
		wantViolation(t, db, "23514", `INSERT INTO webhooks (workspace_id, url, triggers) VALUES ($1, 'https://example.com', '{responseDeleted}')`, workspaceID)
		mustExec(t, db, `INSERT INTO webhooks (workspace_id, url, triggers) VALUES ($1, 'https://example.com', '{responseCreated,responseFinished}')`, workspaceID)
	})

	t.Run("a webhook delivery's state matches its latest attempt, and it goes with its response", func(t *testing.T) {
		webhookID := mustScanID(t, db, `INSERT INTO webhooks (workspace_id, url, triggers) VALUES ($1, 'https://example.com/d', '{responseFinished}') RETURNING id`, workspaceID)
		responseID := mustScanID(t, db, `INSERT INTO responses (survey_id) VALUES ($1) RETURNING id`, surveyID)
		insert := `INSERT INTO webhook_deliveries (webhook_id, response_id, survey_id, event, message_id, payload, status, attempts, last_attempted_at, next_attempt_at, delivered_at, status_code, failure, failure_message, latency_ms)
			VALUES ($1, $2, $3, 'responseFinished', $4, '{}', $5, 1, now(), $6::timestamptz, $7::timestamptz, $8::integer, $9, $10, 10)`
		at := "2026-01-01T00:00:00Z"
		wantViolation(t, db, "23514", insert, webhookID, responseID, surveyID, "m1", "retrying", nil, nil, 500, "status", "Status 500")
		wantViolation(t, db, "23514", insert, webhookID, responseID, surveyID, "m1", "succeeded", nil, nil, 200, nil, nil)
		wantViolation(t, db, "23514", insert, webhookID, responseID, surveyID, "m1", "failed", nil, at, 500, "status", "Status 500")
		wantViolation(t, db, "23514", insert, webhookID, responseID, surveyID, "m1", "failed", nil, nil, 200, nil, nil)
		wantViolation(t, db, "23514", insert, webhookID, responseID, surveyID, "m1", "failed", nil, nil, nil, "timeout", nil)
		wantViolation(t, db, "23514", insert, webhookID, responseID, surveyID, "m1", "succeeded", nil, at, 200, "carrier-pigeon", "lost")
		// A delivered message whose latest attempt, a resend, failed.
		mustExec(t, db, insert, webhookID, responseID, surveyID, "m0", "succeeded", nil, at, 503, "status", "Status 503")
		mustExec(t, db, insert, webhookID, responseID, surveyID, "m1", "retrying", at, nil, 500, "status", "Status 500")
		wantViolation(t, db, "23505", insert, webhookID, responseID, surveyID, "m1", "succeeded", nil, at, 200, nil, nil)
		mustExec(t, db, `DELETE FROM responses WHERE id = $1`, responseID)
		var left int
		if err := db.QueryRow(`SELECT count(*) FROM webhook_deliveries`).Scan(&left); err != nil || left != 0 {
			t.Fatalf("webhook deliveries left after their response was deleted: %d, %v", left, err)
		}
	})

	t.Run("a survey template is named, keeps a document, and goes with its organization", func(t *testing.T) {
		templateOrg := mustScanID(t, db, `INSERT INTO organizations (name) VALUES ('Templates') RETURNING id`)
		insert := `INSERT INTO survey_templates (organization_id, created_by, name, welcome_card, blocks, endings, hidden_fields, variables) VALUES ($1, $2, $3, '{"enabled": false}', $4::jsonb, '[]', '{"enabled": false}', '[]')`
		wantViolation(t, db, "23514", insert, templateOrg, userID, "", "[]")
		wantViolation(t, db, "23514", insert, templateOrg, userID, "NPS", "{}")
		mustExec(t, db, insert, templateOrg, userID, "NPS", "[]")
		mustExec(t, db, `DELETE FROM organizations WHERE id = $1`, templateOrg)
		var left int
		if err := db.QueryRow(`SELECT count(*) FROM survey_templates`).Scan(&left); err != nil || left != 0 {
			t.Fatalf("survey templates left after their organization was deleted: %d, %v", left, err)
		}
	})

	t.Run("a results link has a well-formed key of its own and goes with its survey", func(t *testing.T) {
		shareSurvey := mustScanID(t, db, `INSERT INTO surveys (workspace_id, name) VALUES ($1, 'Shared') RETURNING id`, workspaceID)
		insert := `INSERT INTO survey_result_shares (survey_id, created_by, share_key, filter_criteria) VALUES ($1, $2, $3, $4::jsonb)`
		key := strings.Repeat("a", 42) + "-"
		wantViolation(t, db, "23514", insert, shareSurvey, userID, "short", "{}")
		wantViolation(t, db, "23514", insert, shareSurvey, userID, strings.Repeat("a", 42)+"=", "{}")
		wantViolation(t, db, "23514", insert, shareSurvey, userID, key, "[]")
		withRevealed := `INSERT INTO survey_result_shares (survey_id, share_key, revealed) VALUES ($1, $2, $3::jsonb)`
		wantViolation(t, db, "23514", withRevealed, shareSurvey, strings.Repeat("b", 43), `{"id":"q1"}`)
		wantViolation(t, db, "23514", withRevealed, shareSurvey, strings.Repeat("b", 43), `[`+strings.TrimSuffix(strings.Repeat(`{"id":"q1"},`, 501), ",")+`]`)
		mustExec(t, db, insert, shareSurvey, userID, key, "{}")
		wantViolation(t, db, "23505", insert, shareSurvey, userID, key, "{}")
		mustExec(t, db, `DELETE FROM surveys WHERE id = $1`, shareSurvey)
		var left int
		if err := db.QueryRow(`SELECT count(*) FROM survey_result_shares`).Scan(&left); err != nil || left != 0 {
			t.Fatalf("results links left after their survey was deleted: %d, %v", left, err)
		}
	})

	t.Run("an organization's last retention sweep has positive windows and counts and goes with its organization", func(t *testing.T) {
		sweptOrg := mustScanID(t, db, `INSERT INTO organizations (name) VALUES ('Swept') RETURNING id`)
		insert := `INSERT INTO organization_retention_sweeps (organization_id, swept_at, dry_run, workflow_run_retention_days, workflow_runs_matched) VALUES ($1, now(), true, $2, $3)`
		wantViolation(t, db, "23514", insert, sweptOrg, 0, 1)
		wantViolation(t, db, "23514", insert, sweptOrg, 30, -1)
		mustExec(t, db, insert, sweptOrg, 30, 1)
		wantViolation(t, db, "23505", insert, sweptOrg, 30, 1)
		mustExec(t, db, `DELETE FROM organizations WHERE id = $1`, sweptOrg)
		var left int
		if err := db.QueryRow(`SELECT count(*) FROM organization_retention_sweeps`).Scan(&left); err != nil || left != 0 {
			t.Fatalf("retention sweeps left after their organization was deleted: %d, %v", left, err)
		}
	})

	t.Run("an integration error has both a time and a message or neither", func(t *testing.T) {
		wantViolation(t, db, "23514", `INSERT INTO integrations (workspace_id, type, last_error_at) VALUES ($1, 'slack', now())`, workspaceID)
	})

	t.Run("an API key's hint is its last four characters or nothing", func(t *testing.T) {
		insert := `INSERT INTO api_keys (organization_id, label, hashed_key, lookup_hash, key_hint, expires_at) VALUES ($1, 'k', 'h', $2, $3, $4::timestamptz)`
		wantViolation(t, db, "23514", insert, orgID, "l1", "abc", nil)
		wantViolation(t, db, "23514", insert, orgID, "l2", "abcde", nil)
		mustExec(t, db, insert, orgID, "l3", "ab-_", "2026-01-01T00:00:00Z")
		mustExec(t, db, insert, orgID, "l4", nil, nil)
	})

	t.Run("an organization holds one invite per address regardless of case, and its links go with it", func(t *testing.T) {
		inviteID := mustScanID(t, db, `INSERT INTO invites (organization_id, email, creator_id, expires_at) VALUES ($1, 'grace@example.com', $2, now() + interval '7 days') RETURNING id`, orgID, userID)
		wantViolation(t, db, "23505", `INSERT INTO invites (organization_id, email, creator_id, expires_at) VALUES ($1, 'Grace@Example.com', $2, now() + interval '7 days')`, orgID, userID)
		mustExec(t, db, `INSERT INTO invite_tokens (token_hash, invite_id, expires_at) VALUES ('\x01', $1, now() + interval '7 days')`, inviteID)
		mustExec(t, db, `DELETE FROM invites WHERE id = $1`, inviteID)
		var left int
		if err := db.QueryRow(`SELECT count(*) FROM invite_tokens`).Scan(&left); err != nil || left != 0 {
			t.Fatalf("invite tokens left after the invite was deleted: %d, %v", left, err)
		}
	})
}

type outboxRow struct {
	targetType   string
	primaryID    string
	secondaryID  sql.NullString
	isRevocation bool
}

// drainOutbox returns and deletes what the triggers enqueued, oldest first.
func drainOutbox(t *testing.T, db *sql.DB) []outboxRow {
	t.Helper()
	rows, err := db.Query(`DELETE FROM authz_projection_outbox RETURNING target_type, primary_id, secondary_id, is_revocation, id`)
	if err != nil {
		t.Fatalf("drain outbox: %v", err)
	}
	defer func() { _ = rows.Close() }()
	type ordered struct {
		outboxRow
		id string
	}
	var got []ordered
	for rows.Next() {
		var r ordered
		if err := rows.Scan(&r.targetType, &r.primaryID, &r.secondaryID, &r.isRevocation, &r.id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	// uuidv7 ids sort in insertion order.
	sort.Slice(got, func(i, j int) bool { return got[i].id < got[j].id })
	out := make([]outboxRow, len(got))
	for i := range got {
		out[i] = got[i].outboxRow
	}
	return out
}

func TestAuthzProjectionTriggers(t *testing.T) {
	db := migratedDB(t)
	orgID, userID, workspaceID := seed(t, db)
	otherOrgID := mustScanID(t, db, `INSERT INTO organizations (name) VALUES ('Globex') RETURNING id`)
	_ = drainOutbox(t, db)

	cases := []struct {
		name  string
		apply func()
		want  []outboxRow
	}{
		{
			name: "a new membership is a grant",
			apply: func() {
				mustExec(t, db, `INSERT INTO memberships (user_id, organization_id, role) VALUES ($1, $2, 'member')`, userID, orgID)
			},
			want: []outboxRow{{"membership", orgID, sql.NullString{String: userID, Valid: true}, false}},
		},
		{
			name:  "accepting an invite writes the same relationships, so it is a grant",
			apply: func() { mustExec(t, db, `UPDATE memberships SET accepted = true WHERE user_id = $1`, userID) },
			want:  []outboxRow{{"membership", orgID, sql.NullString{String: userID, Valid: true}, false}},
		},
		{
			name:  "a role change is a revocation",
			apply: func() { mustExec(t, db, `UPDATE memberships SET role = 'owner' WHERE user_id = $1`, userID) },
			want:  []outboxRow{{"membership", orgID, sql.NullString{String: userID, Valid: true}, true}},
		},
		{
			name: "moving a membership revokes the old pair before reconciling the new one",
			apply: func() {
				mustExec(t, db, `UPDATE memberships SET organization_id = $1 WHERE user_id = $2`, otherOrgID, userID)
			},
			want: []outboxRow{
				{"membership", orgID, sql.NullString{String: userID, Valid: true}, true},
				// The role is unchanged, so the new pair only gains relationships.
				{"membership", otherOrgID, sql.NullString{String: userID, Valid: true}, false},
			},
		},
		{
			name:  "deactivating a user is a revocation",
			apply: func() { mustExec(t, db, `UPDATE users SET is_active = false WHERE id = $1`, userID) },
			want:  []outboxRow{{"user", userID, sql.NullString{}, true}},
		},
		{
			name:  "reactivating a user is a grant",
			apply: func() { mustExec(t, db, `UPDATE users SET is_active = true WHERE id = $1`, userID) },
			want:  []outboxRow{{"user", userID, sql.NullString{}, false}},
		},
		{
			name:  "an unwatched column change enqueues nothing",
			apply: func() { mustExec(t, db, `UPDATE users SET name = 'Ada L.' WHERE id = $1`, userID) },
			want:  nil,
		},
		{
			name:  "deleting a workspace is a revocation",
			apply: func() { mustExec(t, db, `DELETE FROM workspaces WHERE id = $1`, workspaceID) },
			want:  []outboxRow{{"workspace", workspaceID, sql.NullString{}, true}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.apply()
			got := drainOutbox(t, db)
			if len(got) != len(tc.want) {
				t.Fatalf("enqueued %d rows %+v, want %d %+v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("row %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestWithMigrationLockSerializesMigrators(t *testing.T) {
	dsn := startPostgres(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	release := make(chan struct{})
	firstHolds := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- WithMigrationLock(ctx, dsn, log, func(context.Context) error {
			close(firstHolds)
			<-release
			return nil
		})
	}()
	<-firstHolds

	var secondRan bool
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- WithMigrationLock(ctx, dsn, log, func(context.Context) error {
			secondRan = true
			return nil
		})
	}()

	// The second migrator is observably blocked on the advisory lock before the first lets go.
	deadline := time.Now().Add(30 * time.Second)
	for {
		var waiting int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND wait_event = 'advisory'`).Scan(&waiting); err != nil {
			t.Fatalf("pg_stat_activity: %v", err)
		}
		if waiting == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second migrator never waited for the lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case err := <-secondDone:
		t.Fatalf("the second migrator finished (%v) while the first held the lock", err)
	default:
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := <-secondDone; err != nil || !secondRan {
		t.Fatalf("second: ran %v, err %v", secondRan, err)
	}

	var free bool
	if err := db.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, migrationLockKey).Scan(&free); err != nil || !free {
		t.Fatalf("lock still held after both migrators returned (free %v, err %v)", free, err)
	}
}

// 00022 suspends every membership of a deactivated account and leaves every account as it is: nothing tells an organization's deactivation from the operator's, so none is undone. Its Down deactivates every account with a suspended membership again, so a release without suspensions does not give their access back.
func TestMembershipSuspensionMigration(t *testing.T) {
	const version = 22
	dsn := startPostgres(t)
	ctx := context.Background()
	provider, cleanup, err := NewMigrator(dsn, migrations.FS)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	defer cleanup()
	if _, err := provider.UpTo(ctx, version-1); err != nil {
		t.Fatalf("up to %d: %v", version-1, err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	orgA := mustScanID(t, db, `INSERT INTO organizations (name) VALUES ('A') RETURNING id`)
	orgB := mustScanID(t, db, `INSERT INTO organizations (name) VALUES ('B') RETURNING id`)
	user := func(email string, active bool) string {
		return mustScanID(t, db, `INSERT INTO users (name, email, is_active) VALUES ('U', $1, $2) RETURNING id`, email, active)
	}
	deactivated, orphan, active := user("deactivated@example.com", false), user("orphan@example.com", false), user("active@example.com", true)
	for _, m := range [][2]string{{deactivated, orgA}, {deactivated, orgB}, {active, orgA}} {
		mustExec(t, db, `INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES ($1, $2, 'member', true)`, m[0], m[1])
	}

	if _, err := provider.UpTo(ctx, version); err != nil {
		t.Fatalf("up to %d: %v", version, err)
	}
	isActive := func(id string) bool {
		t.Helper()
		var v bool
		if err := db.QueryRow(`SELECT is_active FROM users WHERE id = $1`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	suspended := func(id string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM memberships WHERE user_id = $1 AND suspended_at IS NOT NULL`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if isActive(deactivated) || suspended(deactivated) != 2 {
		t.Fatalf("deactivated member: active %v, %d suspended memberships; want it still inactive with both suspended", isActive(deactivated), suspended(deactivated))
	}
	if isActive(orphan) || !isActive(active) || suspended(active) != 0 {
		t.Fatal("an account without memberships was reactivated, or an active member was suspended")
	}

	// The operator turns the account back on, and one organization restores it.
	mustExec(t, db, `UPDATE users SET is_active = true WHERE id = $1`, deactivated)
	mustExec(t, db, `UPDATE memberships SET suspended_at = NULL WHERE user_id = $1 AND organization_id = $2`, deactivated, orgB)
	if _, err := provider.DownTo(ctx, version-1); err != nil {
		t.Fatalf("down to %d: %v", version-1, err)
	}
	if isActive(deactivated) || !isActive(active) {
		t.Fatalf("after Down: suspended member active %v, active member active %v", isActive(deactivated), isActive(active))
	}
}
