package authz

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// maxReconciliationPasses bounds the read-write-verify loop of one reconcile; a source that keeps moving fails as authzed_projection_unstable.
	maxReconciliationPasses = 3
	// targetChunkSize is how many targets one reconcile call takes, keeping the snapshot query and the widest fan-out (4 updates per membership) under the per-write limit.
	targetChunkSize = 200
)

// projector writes PostgreSQL's authorization state into SpiceDB. Every reconcile reads a snapshot of its targets, writes the relationships the snapshot implies (touching the current value and deleting the alternatives, so a removed row is healed by being named), then reads the snapshot again and repeats if it moved. Only organization, membership, team and workspace, and API key relationships are written; surveys, responses and dashboards are evaluated through their workspace.
type projector struct {
	db     *gorm.DB
	client *Client
	log    *slog.Logger
}

func newProjector(db *gorm.DB, client *Client, log *slog.Logger) *projector {
	return &projector{db: db, client: client, log: log}
}

// project runs one projection and reports any failure as an *Error labelled with operation.
func (p *projector) project(ctx context.Context, operation, name string, run func() error) error {
	started := time.Now()
	if err := run(); err != nil {
		e := normalize(err, operation)
		p.log.WarnContext(ctx, "AuthZed relationship projection failed", "component", "authz", "attempts", e.Attempts, "duration_ms", time.Since(started).Milliseconds(), "error_code", e.Code, "operation", operation, "projection", name, "retryable", e.Retryable)
		return e
	}
	p.log.DebugContext(ctx, "AuthZed relationship projection completed", "component", "authz", "duration_ms", time.Since(started).Milliseconds(), "operation", operation, "projection", name)
	return nil
}

func errUnstable() *Error {
	return newError(codeProjectionUnstable, "reconcile", false, maxReconciliationPasses)
}

// reconcileLoop reads, writes and verifies until a write was made from a snapshot that still holds.
func reconcileLoop[S any](read func() (S, error), write func(S) error, equal func(a, b S) bool) error {
	for range maxReconciliationPasses {
		source, err := read()
		if err != nil {
			return err
		}
		if err := write(source); err != nil {
			return err
		}
		verified, err := read()
		if err != nil {
			return err
		}
		if equal(source, verified) {
			return nil
		}
	}
	return errUnstable()
}

// ---- memberships ----

type membershipTarget struct{ OrganizationID, UserID string }

type membershipRow struct {
	OrganizationID string
	UserID         string
	Role           string
}

// reconcileMemberships projects each named (organization, user) pair: organization#<role>@user for the current role, the other three deleted, all four deleted when there is no row. Membership is projected regardless of accepted, as forma2's contract does.
func (p *projector) reconcileMemberships(ctx context.Context, targets []membershipTarget) error {
	targets = uniqueSorted(targets, func(t membershipTarget) membershipTarget {
		return membershipTarget{canonicalID(t.OrganizationID), canonicalID(t.UserID)}
	}, func(a, b membershipTarget) int {
		return cmpPairs(a.OrganizationID, a.UserID, b.OrganizationID, b.UserID)
	})
	if len(targets) == 0 {
		return nil
	}
	return p.project(ctx, "reconcile_organization_memberships", "organization_membership", func() error {
		read := func() ([]membershipRow, error) {
			orgs, users := validPairs(targets, func(t membershipTarget) (string, string) { return t.OrganizationID, t.UserID })
			var rows []membershipRow
			err := p.db.WithContext(ctx).Raw(`
				SELECT m.organization_id::text AS organization_id, m.user_id::text AS user_id, m.role
				FROM memberships m
				JOIN unnest(?::uuid[], ?::uuid[]) AS t(organization_id, user_id) USING (organization_id, user_id)
				ORDER BY 1, 2`, orgs, users).Scan(&rows).Error
			return rows, err
		}
		write := func(rows []membershipRow) error {
			roles := map[membershipTarget]string{}
			for _, r := range rows {
				roles[membershipTarget{r.OrganizationID, r.UserID}] = r.Role
			}
			groups := make([][]RelationshipUpdate, 0, len(targets))
			for _, t := range targets {
				groups = append(groups, ladderUpdates(organizationRelationNames, organizationRoleRelations[roles[t]], func(relation string) Relationship {
					return Relationship{Resource: ObjectRef{typeOrganization, t.OrganizationID}, Relation: relation, Subject: SubjectRef{Type: typeUser, ID: t.UserID}}
				}))
			}
			return p.writeGroups(ctx, groups)
		}
		return reconcileLoop(read, write, slices.Equal[[]membershipRow])
	})
}

// deleteOrganization removes every relationship on an organization that no longer exists. Its teams, workspaces, memberships and keys cascade in PostgreSQL and arrive as events of their own.
func (p *projector) deleteOrganization(ctx context.Context, organizationID string) error {
	return p.project(ctx, "delete_organization_relationships", "organization_membership", func() error {
		return p.client.DeleteRelationships(ctx, RelationshipFilter{ResourceType: typeOrganization, ResourceID: organizationID})
	})
}

// deleteUser removes a deleted or deactivated user's organization roles and team roles across every tenant.
func (p *projector) deleteUser(ctx context.Context, userID string) error {
	if err := p.project(ctx, "delete_user_organization_relationships", "organization_membership", func() error {
		return p.client.DeleteRelationships(ctx, RelationshipFilter{ResourceType: typeOrganization, Subject: &SubjectFilter{Type: typeUser, ID: userID}})
	}); err != nil {
		return err
	}
	return p.project(ctx, "delete_user_team_relationships", "team_workspace", func() error {
		return p.client.DeleteRelationships(ctx, RelationshipFilter{ResourceType: typeTeam, Subject: &SubjectFilter{Type: typeUser, ID: userID}})
	})
}

// ---- teams and workspaces ----

type teamMembershipTarget struct{ TeamID, UserID string }

type workspaceTeamTarget struct{ WorkspaceID, TeamID string }

type teamWorkspaceTargets struct {
	TeamIDs         []string
	TeamMemberships []teamMembershipTarget
	WorkspaceIDs    []string
	WorkspaceTeams  []workspaceTeamTarget
}

func (t teamWorkspaceTargets) empty() bool {
	return len(t.TeamIDs) == 0 && len(t.TeamMemberships) == 0 && len(t.WorkspaceIDs) == 0 && len(t.WorkspaceTeams) == 0
}

type parentRow struct{ ID, OrganizationID string }

type teamMemberRow struct{ TeamID, UserID, Role string }

type workspaceTeamRow struct{ WorkspaceID, TeamID, Permission string }

type teamWorkspaceSnapshot struct {
	teams          []parentRow
	workspaces     []parentRow
	teamMembers    []teamMemberRow
	workspaceTeams []workspaceTeamRow
}

func (a teamWorkspaceSnapshot) equal(b teamWorkspaceSnapshot) bool {
	return slices.Equal(a.teams, b.teams) && slices.Equal(a.workspaces, b.workspaces) && slices.Equal(a.teamMembers, b.teamMembers) && slices.Equal(a.workspaceTeams, b.workspaceTeams)
}

func (t teamWorkspaceTargets) normalize() teamWorkspaceTargets {
	n := teamWorkspaceTargets{
		TeamMemberships: uniqueSorted(t.TeamMemberships, func(m teamMembershipTarget) teamMembershipTarget {
			return teamMembershipTarget{canonicalID(m.TeamID), canonicalID(m.UserID)}
		}, func(a, b teamMembershipTarget) int { return cmpPairs(a.TeamID, a.UserID, b.TeamID, b.UserID) }),
		WorkspaceTeams: uniqueSorted(t.WorkspaceTeams, func(g workspaceTeamTarget) workspaceTeamTarget {
			return workspaceTeamTarget{canonicalID(g.WorkspaceID), canonicalID(g.TeamID)}
		}, func(a, b workspaceTeamTarget) int { return cmpPairs(a.WorkspaceID, a.TeamID, b.WorkspaceID, b.TeamID) }),
	}
	teams := slices.Clone(t.TeamIDs)
	workspaces := slices.Clone(t.WorkspaceIDs)
	for _, m := range n.TeamMemberships {
		teams = append(teams, m.TeamID)
	}
	for _, g := range n.WorkspaceTeams {
		teams = append(teams, g.TeamID)
		workspaces = append(workspaces, g.WorkspaceID)
	}
	n.TeamIDs = uniqueIDs(teams)
	n.WorkspaceIDs = uniqueIDs(workspaces)
	return n
}

// reconcileTeamWorkspace projects teams and workspaces (their organization parent), team roles (admin or contributor) and workspace team grants (manager_team, writer_team or reader_team to team#member). A workspace team grant is projected only when the team and the workspace belong to the same organization: SpiceDB does not check the tenant of a user's team-derived access, so a cross-organization row would hand one tenant's team members another tenant's workspace. A team or workspace with no row loses every relationship, a missing team also every grant naming it.
func (p *projector) reconcileTeamWorkspace(ctx context.Context, targets teamWorkspaceTargets) error {
	targets = targets.normalize()
	if targets.empty() {
		return nil
	}
	return p.project(ctx, "reconcile_team_workspace_relationships", "team_workspace", func() error {
		read := func() (teamWorkspaceSnapshot, error) {
			var s teamWorkspaceSnapshot
			db := p.db.WithContext(ctx)
			if err := db.Raw(`SELECT id::text AS id, organization_id::text AS organization_id FROM teams WHERE id = ANY(?::uuid[]) ORDER BY id`, idArray(targets.TeamIDs)).Scan(&s.teams).Error; err != nil {
				return s, err
			}
			if err := db.Raw(`SELECT id::text AS id, organization_id::text AS organization_id FROM workspaces WHERE id = ANY(?::uuid[]) ORDER BY id`, idArray(targets.WorkspaceIDs)).Scan(&s.workspaces).Error; err != nil {
				return s, err
			}
			teamIDs, userIDs := validPairs(targets.TeamMemberships, func(m teamMembershipTarget) (string, string) { return m.TeamID, m.UserID })
			if err := db.Raw(`
				SELECT tm.team_id::text AS team_id, tm.user_id::text AS user_id, tm.role
				FROM team_members tm
				JOIN unnest(?::uuid[], ?::uuid[]) AS t(team_id, user_id) USING (team_id, user_id)
				ORDER BY 1, 2`, teamIDs, userIDs).Scan(&s.teamMembers).Error; err != nil {
				return s, err
			}
			workspaceIDs, grantTeamIDs := validPairs(targets.WorkspaceTeams, func(g workspaceTeamTarget) (string, string) { return g.WorkspaceID, g.TeamID })
			err := db.Raw(`
				SELECT wt.workspace_id::text AS workspace_id, wt.team_id::text AS team_id, wt.permission
				FROM workspace_teams wt
				JOIN unnest(?::uuid[], ?::uuid[]) AS t(workspace_id, team_id) USING (workspace_id, team_id)
				JOIN teams te ON te.id = wt.team_id
				JOIN workspaces w ON w.id = wt.workspace_id
				WHERE te.organization_id = w.organization_id
				ORDER BY 1, 2`, workspaceIDs, grantTeamIDs).Scan(&s.workspaceTeams).Error
			return s, err
		}
		write := func(s teamWorkspaceSnapshot) error { return p.writeTeamWorkspace(ctx, targets, s) }
		return reconcileLoop(read, write, teamWorkspaceSnapshot.equal)
	})
}

func (p *projector) writeTeamWorkspace(ctx context.Context, targets teamWorkspaceTargets, s teamWorkspaceSnapshot) error {
	roles := map[teamMembershipTarget]string{}
	for _, m := range s.teamMembers {
		roles[teamMembershipTarget{m.TeamID, m.UserID}] = m.Role
	}
	permissions := map[workspaceTeamTarget]string{}
	for _, g := range s.workspaceTeams {
		permissions[workspaceTeamTarget{g.WorkspaceID, g.TeamID}] = g.Permission
	}

	var groups [][]RelationshipUpdate
	var parents []RelationshipFilter
	for _, t := range s.teams {
		groups = append(groups, []RelationshipUpdate{touch(parentRelationship(typeTeam, t.ID, t.OrganizationID))})
		parents = append(parents, RelationshipFilter{ResourceType: typeTeam, ResourceID: t.ID, Relation: relationParent})
	}
	for _, w := range s.workspaces {
		groups = append(groups, []RelationshipUpdate{touch(parentRelationship(typeWorkspace, w.ID, w.OrganizationID))})
		parents = append(parents, RelationshipFilter{ResourceType: typeWorkspace, ResourceID: w.ID, Relation: relationParent})
	}
	for _, m := range targets.TeamMemberships {
		groups = append(groups, ladderUpdates(teamRelationNames, teamRoleRelations[roles[m]], func(relation string) Relationship {
			return Relationship{Resource: ObjectRef{typeTeam, m.TeamID}, Relation: relation, Subject: SubjectRef{Type: typeUser, ID: m.UserID}}
		}))
	}
	for _, g := range targets.WorkspaceTeams {
		groups = append(groups, ladderUpdates(workspaceTeamRelNames, workspaceTeamRelations[permissions[g]], func(relation string) Relationship {
			return Relationship{Resource: ObjectRef{typeWorkspace, g.WorkspaceID}, Relation: relation, Subject: SubjectRef{Type: typeTeam, ID: g.TeamID, Relation: relationTeamMember}}
		}))
	}

	// Relations are sets: touching the current parent cannot replace a stale one, so every parent is cleared and then restored. Until the rebuild succeeds the event stays pending, and checks on these resources fail closed.
	if err := p.deleteAll(ctx, parents); err != nil {
		return err
	}
	if err := p.writeGroups(ctx, groups); err != nil {
		return err
	}

	existingTeams := map[string]bool{}
	for _, t := range s.teams {
		existingTeams[t.ID] = true
	}
	existingWorkspaces := map[string]bool{}
	for _, w := range s.workspaces {
		existingWorkspaces[w.ID] = true
	}
	var deletions []RelationshipFilter
	for _, id := range targets.TeamIDs {
		if !existingTeams[id] {
			deletions = append(deletions,
				RelationshipFilter{ResourceType: typeTeam, ResourceID: id},
				RelationshipFilter{ResourceType: typeWorkspace, Subject: &SubjectFilter{Type: typeTeam, ID: id, Relation: relationTeamMember}})
		}
	}
	for _, id := range targets.WorkspaceIDs {
		if !existingWorkspaces[id] {
			deletions = append(deletions, RelationshipFilter{ResourceType: typeWorkspace, ResourceID: id})
		}
	}
	return p.deleteAll(ctx, deletions)
}

// ---- API keys ----

type apiKeyWorkspaceTarget struct{ APIKeyID, WorkspaceID string }

type apiKeyTargets struct {
	APIKeyIDs []string
	// Grants are workspace scopes to reconcile even when PostgreSQL no longer holds them, so a grant removed outside an event is still deleted. Each implies its key.
	Grants []apiKeyWorkspaceTarget
}

func (t apiKeyTargets) empty() bool { return len(t.APIKeyIDs) == 0 && len(t.Grants) == 0 }

type apiKeyRow struct {
	ID                 string
	OrganizationID     string
	OrganizationAccess []byte
}

type apiKeyGrantRow struct{ APIKeyID, WorkspaceID, Permission string }

type apiKeySnapshot struct {
	keys   []apiKeyState
	grants []apiKeyGrantRow
}

type apiKeyState struct {
	ID, OrganizationID string
	Read, Write        bool
}

func (a apiKeySnapshot) equal(b apiKeySnapshot) bool {
	return slices.Equal(a.keys, b.keys) && slices.Equal(a.grants, b.grants)
}

// organizationAccess reads api_keys.organization_access strictly: only a literal true grants, so a missing or malformed value denies.
func organizationAccess(raw []byte) (read, write bool) {
	var v struct {
		AccessControl map[string]any `json:"accessControl"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return false, false
	}
	return v.AccessControl["read"] == true, v.AccessControl["write"] == true
}

// reconcileAPIKeys projects each key's organization parent, its organization access flags (organization#api_key_reader and #api_key_writer) and one relation per workspace grant (reader, writer or manager). A grant whose workspace belongs to another organization is never projected. A key with no row loses every relationship that names it.
func (p *projector) reconcileAPIKeys(ctx context.Context, targets apiKeyTargets) error {
	ids := slices.Clone(targets.APIKeyIDs)
	seeded := map[string]map[string]bool{}
	for _, g := range targets.Grants {
		key, workspace := canonicalID(g.APIKeyID), canonicalID(g.WorkspaceID)
		ids = append(ids, key)
		if seeded[key] == nil {
			seeded[key] = map[string]bool{}
		}
		seeded[key][workspace] = true
	}
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	return p.project(ctx, "reconcile_api_key_relationships", "api_key", func() error {
		// Workspace targets are the seeded scopes plus every grant any pass observes, so a grant revoked between passes is still deleted.
		observed := map[string]map[string]bool{}
		for _, id := range ids {
			observed[id] = map[string]bool{}
			for w := range seeded[id] {
				observed[id][w] = true
			}
		}
		read := func() (apiKeySnapshot, error) {
			var s apiKeySnapshot
			var rows []apiKeyRow
			db := p.db.WithContext(ctx)
			if err := db.Raw(`SELECT id::text AS id, organization_id::text AS organization_id, organization_access FROM api_keys WHERE id = ANY(?::uuid[]) ORDER BY id`, idArray(ids)).Scan(&rows).Error; err != nil {
				return s, err
			}
			for _, r := range rows {
				read, write := organizationAccess(r.OrganizationAccess)
				s.keys = append(s.keys, apiKeyState{ID: r.ID, OrganizationID: r.OrganizationID, Read: read, Write: write})
			}
			err := db.Raw(`
				SELECT g.api_key_id::text AS api_key_id, g.workspace_id::text AS workspace_id, g.permission
				FROM api_key_workspaces g
				JOIN api_keys k ON k.id = g.api_key_id
				JOIN workspaces w ON w.id = g.workspace_id
				WHERE g.api_key_id = ANY(?::uuid[]) AND w.organization_id = k.organization_id
				ORDER BY 1, 2`, idArray(ids)).Scan(&s.grants).Error
			return s, err
		}
		write := func(s apiKeySnapshot) error {
			for _, g := range s.grants {
				observed[g.APIKeyID][g.WorkspaceID] = true
			}
			return p.writeAPIKeys(ctx, ids, s, observed)
		}
		return reconcileLoop(read, write, apiKeySnapshot.equal)
	})
}

func (p *projector) writeAPIKeys(ctx context.Context, ids []string, s apiKeySnapshot, observed map[string]map[string]bool) error {
	grants := map[apiKeyWorkspaceTarget]string{}
	for _, g := range s.grants {
		grants[apiKeyWorkspaceTarget{g.APIKeyID, g.WorkspaceID}] = g.Permission
	}
	var groups [][]RelationshipUpdate
	var cleared []RelationshipFilter
	existing := map[string]bool{}
	for _, k := range s.keys {
		existing[k.ID] = true
		groups = append(groups, []RelationshipUpdate{touch(parentRelationship(typeAPIKey, k.ID, k.OrganizationID))})
		access := make([]RelationshipUpdate, 0, 2)
		for _, a := range []struct {
			relation string
			granted  bool
		}{{relationAPIKeyReader, k.Read}, {relationAPIKeyWriter, k.Write}} {
			r := Relationship{Resource: ObjectRef{typeOrganization, k.OrganizationID}, Relation: a.relation, Subject: SubjectRef{Type: typeAPIKey, ID: k.ID}}
			if a.granted {
				access = append(access, touch(r))
			} else {
				access = append(access, remove(r))
			}
		}
		groups = append(groups, access)
		workspaces := make([]string, 0, len(observed[k.ID]))
		for w := range observed[k.ID] {
			workspaces = append(workspaces, w)
		}
		slices.Sort(workspaces)
		for _, w := range workspaces {
			groups = append(groups, ladderUpdates(workspaceAPIKeyRelNames, workspaceAPIKeyRelations[grants[apiKeyWorkspaceTarget{k.ID, w}]], func(relation string) Relationship {
				return Relationship{Resource: ObjectRef{typeWorkspace, w}, Relation: relation, Subject: SubjectRef{Type: typeAPIKey, ID: k.ID}}
			}))
		}
		cleared = append(cleared, RelationshipFilter{ResourceType: typeAPIKey, ResourceID: k.ID, Relation: relationParent})
		for _, relation := range apiKeyAccessRelNames {
			cleared = append(cleared, RelationshipFilter{ResourceType: typeOrganization, Relation: relation, Subject: &SubjectFilter{Type: typeAPIKey, ID: k.ID}})
		}
	}
	// The parent and the access flags are cleared across every organization and rebuilt, so an access grant on a foreign organization cannot survive.
	if err := p.deleteAll(ctx, cleared); err != nil {
		return err
	}
	if err := p.writeGroups(ctx, groups); err != nil {
		return err
	}
	var deletions []RelationshipFilter
	for _, id := range ids {
		if existing[id] {
			continue
		}
		deletions = append(deletions,
			RelationshipFilter{ResourceType: typeAPIKey, ResourceID: id},
			RelationshipFilter{ResourceType: typeOrganization, Subject: &SubjectFilter{Type: typeAPIKey, ID: id}},
			RelationshipFilter{ResourceType: typeWorkspace, Subject: &SubjectFilter{Type: typeAPIKey, ID: id}})
	}
	return p.deleteAll(ctx, deletions)
}

// ---- shared ----

func parentRelationship(resourceType, id, organizationID string) Relationship {
	return Relationship{Resource: ObjectRef{resourceType, id}, Relation: relationParent, Subject: SubjectRef{Type: typeOrganization, ID: organizationID}}
}

// ladderUpdates touches current (one of names, or "" for none) and deletes every other relation of the ladder.
func ladderUpdates(names []string, current string, build func(relation string) Relationship) []RelationshipUpdate {
	updates := make([]RelationshipUpdate, 0, len(names))
	for _, name := range names {
		if name == current {
			updates = append(updates, touch(build(name)))
		} else {
			updates = append(updates, remove(build(name)))
		}
	}
	return updates
}

// writeGroups packs update groups into writes of at most 1,000 updates without splitting a group, so one target's touch and its deletes always land in the same SpiceDB transaction.
func (p *projector) writeGroups(ctx context.Context, groups [][]RelationshipUpdate) error {
	for _, batch := range packGroups(groups) {
		if err := p.client.WriteRelationships(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}

func packGroups(groups [][]RelationshipUpdate) [][]RelationshipUpdate {
	var batches [][]RelationshipUpdate
	var batch []RelationshipUpdate
	for _, g := range groups {
		if len(batch) > 0 && len(batch)+len(g) > maxUpdatesPerCall {
			batches = append(batches, batch)
			batch = nil
		}
		batch = append(batch, g...)
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

// deleteAll runs the deletes ten at a time and returns the first failure of the first window that had one.
func (p *projector) deleteAll(ctx context.Context, filters []RelationshipFilter) error {
	return inWindows(filters, func(f RelationshipFilter) error { return p.client.DeleteRelationships(ctx, f) })
}

// inWindows runs run over items, at most maxParallelDelete at once, and stops after the first window in which any failed.
func inWindows[T any](items []T, run func(T) error) error {
	for start := 0; start < len(items); start += maxParallelDelete {
		window := items[start:min(start+maxParallelDelete, len(items))]
		errs := make([]error, len(window))
		var wg sync.WaitGroup
		for i, item := range window {
			wg.Go(func() { errs[i] = run(item) })
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// canonicalID is the form Forma writes: a uuid in canonical text. Anything else is kept as is, so a stray id in SpiceDB can still be named and deleted.
func canonicalID(s string) string {
	if id, err := uuid.Parse(s); err == nil {
		return id.String()
	}
	return s
}

func uniqueIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, canonicalID(id))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func uniqueSorted[T comparable](items []T, canonical func(T) T, cmp func(a, b T) int) []T {
	out := make([]T, 0, len(items))
	for _, it := range items {
		out = append(out, canonical(it))
	}
	slices.SortFunc(out, cmp)
	return slices.Compact(out)
}

func cmpPairs(a1, a2, b1, b2 string) int {
	if c := strings.Compare(a1, b1); c != 0 {
		return c
	}
	return strings.Compare(a2, b2)
}

// idArray renders the ids that are uuids as a `?::uuid[]` literal. Ids that are not uuids cannot have a row, so they are simply absent from every snapshot.
func idArray(ids []string) string {
	var b strings.Builder
	b.WriteByte('{')
	n := 0
	for _, id := range ids {
		if uuid.Validate(id) != nil {
			continue
		}
		if n > 0 {
			b.WriteByte(',')
		}
		b.WriteString(id)
		n++
	}
	b.WriteByte('}')
	return b.String()
}

// validPairs splits the pairs whose ids are both uuids into two aligned `?::uuid[]` literals for unnest.
func validPairs[T any](items []T, ids func(T) (string, string)) (first, second string) {
	var a, b []string
	for _, it := range items {
		x, y := ids(it)
		if uuid.Validate(x) == nil && uuid.Validate(y) == nil {
			a, b = append(a, x), append(b, y)
		}
	}
	return idArray(a), idArray(b)
}

// chunk returns the i-th slice of targetChunkSize items, empty past the end.
func chunk[T any](items []T, i int) []T {
	start := i * targetChunkSize
	if start >= len(items) {
		return nil
	}
	return items[start:min(start+targetChunkSize, len(items))]
}

// chunkCount is how many calls cover the longest list; call i takes chunk i of every list.
func chunkCount(lengths ...int) int {
	n := 0
	for _, l := range lengths {
		n = max(n, (l+targetChunkSize-1)/targetChunkSize)
	}
	return n
}

// The chunked forms bound each reconcile call; they stop at the first chunk that fails.

func (p *projector) reconcileMembershipsChunked(ctx context.Context, targets []membershipTarget) error {
	for i := range chunkCount(len(targets)) {
		if err := p.reconcileMemberships(ctx, chunk(targets, i)); err != nil {
			return err
		}
	}
	return nil
}

func (p *projector) reconcileTeamWorkspaceChunked(ctx context.Context, t teamWorkspaceTargets) error {
	for i := range chunkCount(len(t.TeamIDs), len(t.TeamMemberships), len(t.WorkspaceIDs), len(t.WorkspaceTeams)) {
		if err := p.reconcileTeamWorkspace(ctx, teamWorkspaceTargets{chunk(t.TeamIDs, i), chunk(t.TeamMemberships, i), chunk(t.WorkspaceIDs, i), chunk(t.WorkspaceTeams, i)}); err != nil {
			return err
		}
	}
	return nil
}

func (p *projector) reconcileAPIKeysChunked(ctx context.Context, t apiKeyTargets) error {
	for i := range chunkCount(len(t.APIKeyIDs), len(t.Grants)) {
		if err := p.reconcileAPIKeys(ctx, apiKeyTargets{chunk(t.APIKeyIDs, i), chunk(t.Grants, i)}); err != nil {
			return err
		}
	}
	return nil
}
