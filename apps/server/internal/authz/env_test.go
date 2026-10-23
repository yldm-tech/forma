package authz

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/yldm-tech/forma/apps/server/internal/authz/spicedbtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
)

// testEnv is a fresh migrated database and an emptied SpiceDB with the canonical schema.
type testEnv struct {
	t          *testing.T
	ctx        context.Context
	dsn        string
	db         *gorm.DB
	cfg        config.AuthzConfig
	client     *Client
	authorizer *Authorizer
	delivery   *Delivery
}

var allDefinitions = []string{"api_key", "organization", "team", "workspace", "survey", "dashboard", "response"}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dsn := pgtest.DSN(t)
	db := openGORM(t, dsn)
	cfg := spicedbtest.Config(t, canonicalSchema, allDefinitions...)
	client, err := NewClient(cfg, RequestProfile, discardLog())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &testEnv{
		t: t, ctx: context.Background(), dsn: dsn, db: db, cfg: cfg, client: client,
		authorizer: NewAuthorizer(config.Config{UserManagementMinimumRole: "manager"}, db, client),
		delivery:   NewDelivery(db, client, discardLog()),
	}
}

func openGORM(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// newDBEnv is a database without SpiceDB, for the outbox repository.
func newDBEnv(t *testing.T) *testEnv {
	t.Helper()
	return &testEnv{t: t, ctx: context.Background(), db: pgtest.GORM(t)}
}

func (e *testEnv) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *testEnv) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id string
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return uuid.MustParse(id)
}

func (e *testEnv) org() uuid.UUID {
	return e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
}

func (e *testEnv) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

func (e *testEnv) member(org, user uuid.UUID, role string) {
	e.exec(`INSERT INTO memberships (organization_id, user_id, role, accepted) VALUES (?, ?, ?, true)`, org, user, role)
}

func (e *testEnv) team(org uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, uuid.NewString())
}

func (e *testEnv) teamMember(team, user uuid.UUID, role string) {
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)`, team, user, role)
}

func (e *testEnv) workspace(org uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, uuid.NewString())
}

func (e *testEnv) workspaceTeam(workspace, team uuid.UUID, permission string) {
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, workspace, team, permission)
}

func (e *testEnv) apiKey(org uuid.UUID, read, write bool) uuid.UUID {
	access := fmt.Sprintf(`{"accessControl":{"read":%v,"write":%v}}`, read, write)
	return e.insert(`INSERT INTO api_keys (organization_id, label, hashed_key, lookup_hash, organization_access) VALUES (?, 'key', 'h', ?, ?::jsonb) RETURNING id`, org, uuid.NewString(), access)
}

func (e *testEnv) apiKeyWorkspace(key, workspace uuid.UUID, permission string) {
	e.exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, ?)`, key, workspace, permission)
}

func (e *testEnv) survey(workspace uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Survey') RETURNING id`, workspace)
}

func (e *testEnv) response(survey uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO responses (survey_id) VALUES (?) RETURNING id`, survey)
}

// drain delivers the outbox and fails the test unless everything was delivered.
func (e *testEnv) drain() {
	e.t.Helper()
	res, err := e.delivery.Drain(e.ctx, DefaultDrainBatches)
	if err != nil {
		e.t.Fatalf("Drain: %v", err)
	}
	if res.Failed != 0 || res.Status != "drained" {
		e.t.Fatalf("Drain = %+v, want everything delivered", res)
	}
}

func (e *testEnv) can(actor Actor, action Action, resource Resource) bool {
	e.t.Helper()
	ok, err := e.authorizer.Can(e.ctx, actor, action, resource)
	if err != nil {
		e.t.Fatalf("Can(%s %s %s): %v", actor.Type, action, resource.Type, err)
	}
	return ok
}

// expect asserts a set of decisions for one actor.
func (e *testEnv) expect(actor Actor, resource Resource, want map[Action]bool) {
	e.t.Helper()
	for action, allowed := range want {
		if got := e.can(actor, action, resource); got != allowed {
			e.t.Errorf("%s %s on %s %s = %v, want %v", actor.Type, action, resource.Type, resource.ID, got, allowed)
		}
	}
}

// spicedbCheck asks SpiceDB directly, bypassing the PostgreSQL scope.
func (e *testEnv) spicedbCheck(resourceType string, resourceID uuid.UUID, permission, subjectType string, subjectID uuid.UUID) bool {
	e.t.Helper()
	ok, err := e.client.CheckPermission(e.ctx, ObjectRef{resourceType, resourceID.String()}, permission, ObjectRef{subjectType, subjectID.String()})
	if err != nil {
		e.t.Fatalf("CheckPermission: %v", err)
	}
	return ok
}

func (e *testEnv) relationships(resourceType string, resourceID uuid.UUID) []Relationship {
	e.t.Helper()
	page, err := e.client.ReadRelationships(e.ctx, RelationshipFilter{ResourceType: resourceType, ResourceID: resourceID.String()}, maxRelationshipReads, "")
	if err != nil {
		e.t.Fatalf("ReadRelationships: %v", err)
	}
	return page.Relationships
}

func (e *testEnv) countOutbox(where string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Raw(`SELECT count(*) FROM authz_projection_outbox WHERE `+where, args...).Scan(&n).Error; err != nil {
		e.t.Fatalf("count outbox: %v", err)
	}
	return n
}

func spicedbTestEmpty(t *testing.T) config.AuthzConfig { return spicedbtest.Empty(t) }
