package authz

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// seedTenant creates one organization with every kind of authorization record, without delivering the outbox.
func (e *testEnv) seedTenant() (org, owner, member, ws uuid.UUID) {
	org, owner, member = e.org(), e.user(), e.user()
	e.member(org, owner, "owner")
	e.member(org, member, "member")
	team := e.team(org)
	ws = e.workspace(org)
	e.teamMember(team, member, "admin")
	e.workspaceTeam(ws, team, "manage")
	key := e.apiKey(org, true, true)
	e.apiKeyWorkspace(key, ws, "read")
	return org, owner, member, ws
}

func (e *testEnv) backfill(req BackfillRequest) BackfillResult {
	e.t.Helper()
	if req.MaxPrune == 0 {
		req.MaxPrune = MaxPrune
	}
	if req.Scope.Kind == "" {
		req.Scope.Kind = ScopeAll
	}
	return NewBackfill(e.db, e.client, discardLog()).Run(e.ctx, req)
}

func (e *testEnv) write(updates ...RelationshipUpdate) {
	e.t.Helper()
	if err := e.client.WriteRelationships(e.ctx, updates); err != nil {
		e.t.Fatalf("WriteRelationships: %v", err)
	}
}

func TestBackfillRebuildsAnEmptySpiceDBFromPostgres(t *testing.T) {
	e := newTestEnv(t)
	org, owner, member, ws := e.seedTenant()
	e.exec(`DELETE FROM authz_projection_outbox`)

	dry := e.backfill(BackfillRequest{})
	if dry.Status != StatusDrifted || dry.Mode != "dry_run" || dry.Counters.Missing != 8 || dry.Counters.Scanned != 1 || dry.CompletedAtSnapshot != nil {
		t.Fatalf("dry run = %+v", dry.Counters)
	}
	if e.can(User(owner), OrganizationRead, Organization(org)) {
		t.Fatal("a dry run wrote")
	}

	applied := e.backfill(BackfillRequest{Apply: true})
	if applied.Status != StatusReconciled || applied.Counters.Reconciled != 1 || applied.CompletedAtSnapshot == nil {
		t.Fatalf("apply = %+v", applied)
	}
	e.expect(User(owner), Organization(org), map[Action]bool{OrganizationWrite: true})
	e.expect(User(member), Workspace(ws), map[Action]bool{WorkspaceManage: true})

	confirm := e.backfill(BackfillRequest{})
	if confirm.Status != StatusReconciled || confirm.Counters != (BackfillCounters{Scanned: 1}) {
		t.Fatalf("confirm = %+v", confirm.Counters)
	}
}

func TestBackfillReportsAndRepairsMismatchedPermissions(t *testing.T) {
	e := newTestEnv(t)
	org, owner, _, _ := e.seedTenant()
	e.drain()
	// SpiceDB says manager and owner; PostgreSQL says owner.
	e.write(touch(Relationship{Resource: ObjectRef{typeOrganization, org.String()}, Relation: "manager", Subject: SubjectRef{Type: typeUser, ID: owner.String()}}))

	dry := e.backfill(BackfillRequest{Scope: BackfillScope{Kind: ScopeOrganization, OrganizationID: org.String()}})
	if dry.Status != StatusDrifted || dry.Counters.MismatchedPermissions != 1 || len(dry.MismatchedPermissions) != 1 || dry.OrphanScope != "known_resources" {
		t.Fatalf("dry run = %+v", dry)
	}
	if e.backfill(BackfillRequest{Apply: true, Scope: BackfillScope{Kind: ScopeOrganization, OrganizationID: org.String()}}).Status != StatusReconciled {
		t.Fatal("apply did not reconcile")
	}
	if e.spicedbCheck("organization", org, "manager", "user", owner) {
		t.Fatal("stale role relation survived")
	}
}

func TestBackfillPrunesOrphansOnlyWhenAskedAndWithinBudget(t *testing.T) {
	e := newTestEnv(t)
	org, _, _, _ := e.seedTenant()
	e.drain()
	ghostTeam, ghostUser := uuid.NewString(), uuid.NewString()
	e.write(
		touch(parentRelationship(typeTeam, ghostTeam, org.String())),
		touch(Relationship{Resource: ObjectRef{typeOrganization, org.String()}, Relation: "member", Subject: SubjectRef{Type: typeUser, ID: ghostUser}}),
	)

	dry := e.backfill(BackfillRequest{})
	if dry.Status != StatusDrifted || dry.Counters.Orphaned != 2 || dry.OrphanScope != "all" {
		t.Fatalf("dry run = %+v", dry.Counters)
	}
	// Applying without prune repairs nothing it cannot attribute.
	if res := e.backfill(BackfillRequest{Apply: true}); res.Status != StatusDrifted || res.Counters.Pruned != 0 {
		t.Fatalf("apply without prune = %+v", res.Counters)
	}
	// Over the budget nothing at all is pruned.
	if res := e.backfill(BackfillRequest{Apply: true, Prune: true, MaxPrune: 1}); res.Counters.Skipped != 1 || res.Counters.Pruned != 0 || len(e.relationships("team", uuid.MustParse(ghostTeam))) != 1 {
		t.Fatalf("over budget = %+v", res.Counters)
	}
	res := e.backfill(BackfillRequest{Apply: true, Prune: true})
	if res.Status != StatusReconciled || res.Counters.Pruned != 2 {
		t.Fatalf("prune = %+v", res.Counters)
	}
	if len(e.relationships("team", uuid.MustParse(ghostTeam))) != 0 || e.spicedbCheck("organization", org, "read", "user", uuid.MustParse(ghostUser)) {
		t.Fatal("orphans survived the prune")
	}
}

func TestBackfillReportsCrossTenantState(t *testing.T) {
	e := newTestEnv(t)
	_, _, _, wsA := e.seedTenant()
	orgB, ownerB := e.org(), e.user()
	e.member(orgB, ownerB, "owner")
	teamB := e.team(orgB)
	e.workspaceTeam(wsA, teamB, "read")
	e.drain()
	// A second, foreign parent on a workspace hands orgB's owners orgA's workspace.
	e.write(touch(parentRelationship(typeWorkspace, wsA.String(), orgB.String())))

	dry := e.backfill(BackfillRequest{})
	if dry.Status != StatusDrifted || dry.Counters.Invalid != 1 || dry.Counters.MismatchedParents != 1 || len(dry.MismatchedParents) != 1 {
		t.Fatalf("dry run = %+v", dry)
	}
	if got := dry.MismatchedParents[0]; got.ChildID != wsA.String() || got.OrganizationID != orgB.String() || got.ChildType != typeWorkspace {
		t.Fatalf("mismatched parent = %+v", got)
	}
	// Applying rebuilds the workspace's parent from PostgreSQL, which drops the foreign one; the cross-organization grant stays reported and unprojected.
	res := e.backfill(BackfillRequest{Apply: true})
	if res.Status != StatusDrifted || res.Counters.Invalid != 1 {
		t.Fatalf("apply = %+v", res.Counters)
	}
	if e.spicedbCheck("workspace", wsA, "manage", "user", ownerB) {
		t.Fatal("the foreign parent survived the apply")
	}
	if confirm := e.backfill(BackfillRequest{}); confirm.Counters.MismatchedParents != 0 || confirm.Counters.Invalid != 1 {
		t.Fatalf("confirm = %+v", confirm.Counters)
	}
}

func TestBackfillWorkspaceScope(t *testing.T) {
	e := newTestEnv(t)
	_, _, member, ws := e.seedTenant()
	e.drain()
	e.exec(`DELETE FROM workspace_teams WHERE workspace_id = ?`, ws)
	e.exec(`DELETE FROM authz_projection_outbox WHERE processed_at IS NULL`)

	dry := e.backfill(BackfillRequest{Scope: BackfillScope{Kind: ScopeWorkspace, WorkspaceID: ws.String()}})
	if dry.Status != StatusDrifted || dry.Counters.Orphaned != 1 || dry.Scope != ScopeWorkspace {
		t.Fatalf("dry run = %+v", dry)
	}
	if res := e.backfill(BackfillRequest{Apply: true, Prune: true, Scope: BackfillScope{Kind: ScopeWorkspace, WorkspaceID: ws.String()}}); res.Status != StatusReconciled || res.Counters.Pruned != 1 {
		t.Fatalf("prune = %+v", res)
	}
	e.expect(User(member), Workspace(ws), map[Action]bool{WorkspaceRead: false})

	missing := e.backfill(BackfillRequest{Scope: BackfillScope{Kind: ScopeOrganization, OrganizationID: uuid.NewString()}})
	if missing.Status != StatusFailed || missing.Failures[0].Code != CodeNotFound {
		t.Fatalf("missing organization = %+v", missing)
	}
}

func TestScheduledAuditRepairsDrift(t *testing.T) {
	e := newTestEnv(t)
	org, owner, _, _ := e.seedTenant()
	e.exec(`DELETE FROM authz_projection_outbox`)
	e.exec(`INSERT INTO authz_projection_outbox (target_type, primary_id, processed_at) VALUES ('team', ?, now() - interval '8 days')`, uuid.NewString())
	if err := NewAudit(e.db, e.client, discardLog()).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.expect(User(owner), Organization(org), map[Action]bool{OrganizationWrite: true})
	if e.countOutbox(`true`) != 0 {
		t.Fatal("delivered history older than a week was kept")
	}
	if res := e.backfill(BackfillRequest{}); res.Status != StatusReconciled {
		t.Fatalf("after the audit = %+v", res.Counters)
	}
}

func TestBackfillCommand(t *testing.T) {
	e := newTestEnv(t)
	org, owner, _, _ := e.seedTenant()
	e.exec(`DELETE FROM authz_projection_outbox`)
	t.Setenv("DATABASE_URL", e.dsn)
	t.Setenv("AUTHZED_ENABLED", "1")
	t.Setenv("AUTHZED_ENDPOINT", e.cfg.Endpoint)
	t.Setenv("AUTHZED_TOKEN", e.cfg.Token)
	t.Setenv("AUTHZED_INSECURE", "1")

	run := func(args ...string) (int, map[string]any) {
		var stdout, stderr bytes.Buffer
		code := RunCLI(e.ctx, args, &stdout, &stderr)
		var out map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil || strings.Count(stdout.String(), "\n") != 1 {
			t.Fatalf("%v: stdout %q is not one JSON line (stderr %s)", args, stdout.String(), stderr.String())
		}
		return code, out
	}
	if code, out := run("backfill"); code != 2 || out["status"] != StatusDrifted {
		t.Fatalf("dry run exit %d: %v", code, out)
	}
	if code, out := run("backfill", "--apply", "--organization-id="+org.String()); code != 0 || out["status"] != StatusReconciled {
		t.Fatalf("apply exit %d: %v", code, out)
	}
	e.expect(User(owner), Organization(org), map[Action]bool{OrganizationWrite: true})
	if code, out := run("backfill", "--apply", "--prune", "--confirm-prune", "--scope=all", "--expected-endpoint=elsewhere:50051"); code != 1 || out["code"] != string(CodeFailedPrecondition) {
		t.Fatalf("wrong endpoint exit %d: %v", code, out)
	}
	if code, out := run("backfill", "--prune"); code != 1 || out["code"] != string(CodeInvalidRequest) {
		t.Fatalf("misuse exit %d: %v", code, out)
	}
	if code, out := run("schema", "check"); code != 0 || out["status"] != SchemaMatched {
		t.Fatalf("schema check exit %d: %v", code, out)
	}
	if code, out := run("outbox", "status"); code != 0 || out["status"] != "healthy" {
		t.Fatalf("outbox status exit %d: %v", code, out)
	}
	e.exec(`INSERT INTO authz_projection_outbox (target_type, primary_id, is_revocation, dead_lettered_at) VALUES ('user', ?, true, now())`, uuid.NewString())
	if code, out := run("outbox", "status"); code != 2 || out["status"] != "critical" || out["deadLettered"] != float64(1) {
		t.Fatalf("outbox status exit %d: %v", code, out)
	}
	if code, out := run("outbox", "replay"); code != 0 || out["replayed"] != float64(1) {
		t.Fatalf("outbox replay exit %d: %v", code, out)
	}
	if code, out := run("outbox", "drain"); code != 0 || out["status"] != "drained" {
		t.Fatalf("outbox drain exit %d: %v", code, out)
	}

	t.Setenv("AUTHZED_ENABLED", "0")
	if code, out := run("backfill"); code != 1 || out["code"] != string(CodeDisabled) {
		t.Fatalf("disabled exit %d: %v", code, out)
	}
}

func TestSchemaApply(t *testing.T) {
	cfg := spicedbTestEmpty(t)
	client, err := NewClient(cfg, BulkProfile, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := t.Context()

	state, err := CheckSchema(ctx, client)
	if err != nil || state.Status != SchemaDrifted || state.RemoteState != "empty" || state.RemoteDigest != nil || state.DifferenceKinds["schema_missing"] != 1 {
		t.Fatalf("empty check = %+v, %v", state, err)
	}
	state, err = ApplySchema(ctx, client, "")
	if err != nil || state.Status != SchemaApplied || state.RemoteDigest == nil || state.RemoteState != "present" || state.DifferenceCount != 0 {
		t.Fatalf("apply = %+v, %v", state, err)
	}
	if state, err = ApplySchema(ctx, client, ""); err != nil || state.Status != SchemaUnchanged {
		t.Fatalf("reapply = %+v, %v", state, err)
	}

	// A schema someone else wrote is only replaced by an operator who names its digest.
	other := canonicalSchema + "\ndefinition extra {}\n"
	if err := client.WriteSchema(ctx, other); err != nil {
		t.Fatal(err)
	}
	check, err := CheckSchema(ctx, client)
	if err != nil || check.Status != SchemaDrifted || check.DifferenceKinds["definition_removed"] != 1 {
		t.Fatalf("drifted check = %+v, %v", check, err)
	}
	if _, err := ApplySchema(ctx, client, ""); err == nil {
		t.Fatal("overwrote a schema without its digest")
	} else if ae, _ := AsError(err); ae == nil || ae.Code != CodeSchemaChanged {
		t.Fatalf("apply without digest = %v", err)
	}
	if state, err := ApplySchema(ctx, client, *check.RemoteDigest); err != nil || state.Status != SchemaApplied {
		t.Fatalf("apply with digest = %+v, %v", state, err)
	}
}
