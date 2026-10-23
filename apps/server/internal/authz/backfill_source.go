package authz

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// PostgreSQL reads for reconciliation. They are deliberately not tenant-scoped, because sweeping every organization is the point, so they are reachable only from the operator command and the scheduled audit, never from a request. They read identifiers, roles, permissions and API key organization access, never key hashes or usage. Errors are returned, never read as absence: absence is what marks a relationship stale, and a failed query taken for "no row" would revoke live access.

// backfillOrganizationPageSize is how many organization ids one keyset page of the sweep reads.
const backfillOrganizationPageSize = 100

// organizationSource is every authorization record one organization owns, a closed unit: each reaches the organization in one or two hops.
type organizationSource struct {
	apiKeyIDs       []string
	apiKeyGrants    []apiKeyWorkspaceTarget
	expected        []Relationship
	invalidAPIKey   []apiKeyWorkspaceTarget
	invalidTeam     []workspaceTeamTarget
	memberships     []membershipTarget
	teamIDs         []string
	teamMemberships []teamMembershipTarget
	workspaceIDs    []string
	workspaceTeams  []workspaceTeamTarget
}

// workspaceSource is the grants on one workspace, for the narrowest repair scope. The workspace need not exist: one whose row is gone is the case most worth repairing.
type workspaceSource struct {
	apiKeyGrants   []apiKeyWorkspaceTarget
	expected       []Relationship
	invalidAPIKey  []apiKeyWorkspaceTarget
	invalidTeam    []workspaceTeamTarget
	organizationID string
	exists         bool
	workspaceTeams []workspaceTeamTarget
}

type backfillSource struct{ db *gorm.DB }

func (s backfillSource) organizationIDPage(ctx context.Context, after string) ([]string, error) {
	var ids []string
	q := s.db.WithContext(ctx)
	var err error
	if after == "" {
		err = q.Raw(`SELECT id::text FROM organizations ORDER BY id LIMIT ?`, backfillOrganizationPageSize).Scan(&ids).Error
	} else {
		err = q.Raw(`SELECT id::text FROM organizations WHERE id > ?::uuid ORDER BY id LIMIT ?`, after, backfillOrganizationPageSize).Scan(&ids).Error
	}
	if err != nil {
		return nil, fmt.Errorf("read organization page: %w", err)
	}
	return ids, nil
}

func (s backfillSource) organizationExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := s.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM organizations WHERE id = ?::uuid)`, id).Scan(&exists).Error
	if err != nil {
		return false, fmt.Errorf("read organization: %w", err)
	}
	return exists, nil
}

type grantRow struct {
	PrincipalID    string
	WorkspaceID    string
	Permission     string
	OrganizationID string
}

func apiKeyRelationships(id, organizationID string, raw []byte) []Relationship {
	out := []Relationship{parentRelationship(typeAPIKey, id, organizationID)}
	read, write := organizationAccess(raw)
	if read {
		out = append(out, Relationship{Resource: ObjectRef{typeOrganization, organizationID}, Relation: relationAPIKeyReader, Subject: SubjectRef{Type: typeAPIKey, ID: id}})
	}
	if write {
		out = append(out, Relationship{Resource: ObjectRef{typeOrganization, organizationID}, Relation: relationAPIKeyWriter, Subject: SubjectRef{Type: typeAPIKey, ID: id}})
	}
	return out
}

func teamGrantRelationship(workspaceID, teamID, permission string) Relationship {
	return Relationship{Resource: ObjectRef{typeWorkspace, workspaceID}, Relation: workspaceTeamRelations[permission], Subject: SubjectRef{Type: typeTeam, ID: teamID, Relation: relationTeamMember}}
}

func apiKeyGrantRelationship(workspaceID, apiKeyID, permission string) Relationship {
	return Relationship{Resource: ObjectRef{typeWorkspace, workspaceID}, Relation: workspaceAPIKeyRelations[permission], Subject: SubjectRef{Type: typeAPIKey, ID: apiKeyID}}
}

// organization reads one organization's records and the exact relationship set the projectors would write for them. Grants that cross organizations are partitioned out: reported as invalid, never projected, never pruned.
func (s backfillSource) organization(ctx context.Context, organizationID string) (organizationSource, error) {
	var src organizationSource
	db := s.db.WithContext(ctx)
	var memberships []struct{ UserID, Role string }
	if err := db.Raw(`SELECT user_id::text AS user_id, role FROM memberships WHERE organization_id = ?::uuid ORDER BY user_id`, organizationID).Scan(&memberships).Error; err != nil {
		return src, fmt.Errorf("read memberships: %w", err)
	}
	var teams, workspaces []string
	if err := db.Raw(`SELECT id::text FROM teams WHERE organization_id = ?::uuid ORDER BY id`, organizationID).Scan(&teams).Error; err != nil {
		return src, fmt.Errorf("read teams: %w", err)
	}
	if err := db.Raw(`SELECT id::text FROM workspaces WHERE organization_id = ?::uuid ORDER BY id`, organizationID).Scan(&workspaces).Error; err != nil {
		return src, fmt.Errorf("read workspaces: %w", err)
	}
	var keys []apiKeyRow
	if err := db.Raw(`SELECT id::text AS id, organization_id::text AS organization_id, organization_access FROM api_keys WHERE organization_id = ?::uuid ORDER BY id`, organizationID).Scan(&keys).Error; err != nil {
		return src, fmt.Errorf("read api keys: %w", err)
	}
	var teamMembers []teamMemberRow
	if err := db.Raw(`
		SELECT tm.team_id::text AS team_id, tm.user_id::text AS user_id, tm.role
		FROM team_members tm JOIN teams t ON t.id = tm.team_id
		WHERE t.organization_id = ?::uuid ORDER BY 1, 2`, organizationID).Scan(&teamMembers).Error; err != nil {
		return src, fmt.Errorf("read team members: %w", err)
	}
	// The principal's organization is read so a grant that crosses organizations is detected rather than projected as if the unit were closed.
	var teamGrants []grantRow
	if err := db.Raw(`
		SELECT wt.team_id::text AS principal_id, wt.workspace_id::text AS workspace_id, wt.permission, t.organization_id::text AS organization_id
		FROM workspace_teams wt JOIN workspaces w ON w.id = wt.workspace_id JOIN teams t ON t.id = wt.team_id
		WHERE w.organization_id = ?::uuid ORDER BY 2, 1`, organizationID).Scan(&teamGrants).Error; err != nil {
		return src, fmt.Errorf("read workspace teams: %w", err)
	}
	// Keyed off the key's organization, so the workspace's is the one compared.
	var keyGrants []grantRow
	if err := db.Raw(`
		SELECT g.api_key_id::text AS principal_id, g.workspace_id::text AS workspace_id, g.permission, w.organization_id::text AS organization_id
		FROM api_key_workspaces g JOIN api_keys k ON k.id = g.api_key_id JOIN workspaces w ON w.id = g.workspace_id
		WHERE k.organization_id = ?::uuid ORDER BY 1, 2`, organizationID).Scan(&keyGrants).Error; err != nil {
		return src, fmt.Errorf("read api key workspaces: %w", err)
	}

	for _, m := range memberships {
		src.memberships = append(src.memberships, membershipTarget{organizationID, m.UserID})
		src.expected = append(src.expected, Relationship{Resource: ObjectRef{typeOrganization, organizationID}, Relation: organizationRoleRelations[m.Role], Subject: SubjectRef{Type: typeUser, ID: m.UserID}})
	}
	for _, t := range teams {
		src.teamIDs = append(src.teamIDs, t)
		src.expected = append(src.expected, parentRelationship(typeTeam, t, organizationID))
	}
	for _, m := range teamMembers {
		src.teamMemberships = append(src.teamMemberships, teamMembershipTarget{m.TeamID, m.UserID})
		src.expected = append(src.expected, Relationship{Resource: ObjectRef{typeTeam, m.TeamID}, Relation: teamRoleRelations[m.Role], Subject: SubjectRef{Type: typeUser, ID: m.UserID}})
	}
	for _, w := range workspaces {
		src.workspaceIDs = append(src.workspaceIDs, w)
		src.expected = append(src.expected, parentRelationship(typeWorkspace, w, organizationID))
	}
	for _, g := range teamGrants {
		target := workspaceTeamTarget{g.WorkspaceID, g.PrincipalID}
		if g.OrganizationID != organizationID {
			src.invalidTeam = append(src.invalidTeam, target)
			continue
		}
		src.workspaceTeams = append(src.workspaceTeams, target)
		src.expected = append(src.expected, teamGrantRelationship(g.WorkspaceID, g.PrincipalID, g.Permission))
	}
	for _, k := range keys {
		src.apiKeyIDs = append(src.apiKeyIDs, k.ID)
		src.expected = append(src.expected, apiKeyRelationships(k.ID, k.OrganizationID, k.OrganizationAccess)...)
	}
	for _, g := range keyGrants {
		target := apiKeyWorkspaceTarget{g.PrincipalID, g.WorkspaceID}
		if g.OrganizationID != organizationID {
			src.invalidAPIKey = append(src.invalidAPIKey, target)
			continue
		}
		src.apiKeyGrants = append(src.apiKeyGrants, target)
		src.expected = append(src.expected, apiKeyGrantRelationship(g.WorkspaceID, g.PrincipalID, g.Permission))
	}
	return src, nil
}

// workspace reads one workspace's grants. Whether a grant crosses organizations is only decidable while the workspace has a row; with none, its grants are stale by construction and the prune path deals with them.
func (s backfillSource) workspace(ctx context.Context, workspaceID string) (workspaceSource, error) {
	var src workspaceSource
	db := s.db.WithContext(ctx)
	var orgs []string
	if err := db.Raw(`SELECT organization_id::text FROM workspaces WHERE id = ?::uuid`, workspaceID).Scan(&orgs).Error; err != nil {
		return src, fmt.Errorf("read workspace: %w", err)
	}
	var teamGrants, keyGrants []grantRow
	if err := db.Raw(`
		SELECT wt.team_id::text AS principal_id, wt.workspace_id::text AS workspace_id, wt.permission, t.organization_id::text AS organization_id
		FROM workspace_teams wt JOIN teams t ON t.id = wt.team_id
		WHERE wt.workspace_id = ?::uuid ORDER BY 1`, workspaceID).Scan(&teamGrants).Error; err != nil {
		return src, fmt.Errorf("read workspace teams: %w", err)
	}
	if err := db.Raw(`
		SELECT g.api_key_id::text AS principal_id, g.workspace_id::text AS workspace_id, g.permission, k.organization_id::text AS organization_id
		FROM api_key_workspaces g JOIN api_keys k ON k.id = g.api_key_id
		WHERE g.workspace_id = ?::uuid ORDER BY 1`, workspaceID).Scan(&keyGrants).Error; err != nil {
		return src, fmt.Errorf("read api key workspaces: %w", err)
	}
	if len(orgs) == 1 {
		src.exists, src.organizationID = true, orgs[0]
		src.expected = append(src.expected, parentRelationship(typeWorkspace, workspaceID, src.organizationID))
	}
	for _, g := range teamGrants {
		target := workspaceTeamTarget{g.WorkspaceID, g.PrincipalID}
		if src.exists && g.OrganizationID != src.organizationID {
			src.invalidTeam = append(src.invalidTeam, target)
			continue
		}
		src.workspaceTeams = append(src.workspaceTeams, target)
		if src.exists {
			src.expected = append(src.expected, teamGrantRelationship(g.WorkspaceID, g.PrincipalID, g.Permission))
		}
	}
	for _, g := range keyGrants {
		target := apiKeyWorkspaceTarget{g.PrincipalID, g.WorkspaceID}
		if src.exists && g.OrganizationID != src.organizationID {
			src.invalidAPIKey = append(src.invalidAPIKey, target)
			continue
		}
		src.apiKeyGrants = append(src.apiKeyGrants, target)
		if src.exists {
			src.expected = append(src.expected, apiKeyGrantRelationship(g.WorkspaceID, g.PrincipalID, g.Permission))
		}
	}
	return src, nil
}

// mismatchedParentEdges returns the observed parent edges PostgreSQL contradicts: the resource exists, but under another organization. A resource with no row is an orphan, handled elsewhere.
func (s backfillSource) mismatchedParentEdges(ctx context.Context, edges []ParentEdge) ([]ParentEdge, error) {
	var out []ParentEdge
	for i := range chunkCount(len(edges)) {
		part := chunk(edges, i)
		byType := map[string][]string{}
		for _, e := range part {
			byType[e.ChildType] = append(byType[e.ChildType], e.ChildID)
		}
		owners := map[string]string{}
		for childType, table := range map[string]string{typeTeam: "teams", typeWorkspace: "workspaces", typeAPIKey: "api_keys"} {
			if len(byType[childType]) == 0 {
				continue
			}
			var rows []parentRow
			if err := s.db.WithContext(ctx).Raw(`SELECT id::text AS id, organization_id::text AS organization_id FROM `+table+` WHERE id = ANY(?::uuid[])`, idArray(byType[childType])).Scan(&rows).Error; err != nil {
				return nil, fmt.Errorf("read %s parents: %w", table, err)
			}
			for _, r := range rows {
				owners[childType+":"+r.ID] = r.OrganizationID
			}
		}
		for _, e := range part {
			if owner, ok := owners[e.ChildType+":"+canonicalID(e.ChildID)]; ok && owner != e.OrganizationID {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

// missingSourceRefs returns the refs PostgreSQL holds no row for, with one query per kind per chunk. An id that is not a uuid can have no row.
func (s backfillSource) missingSourceRefs(ctx context.Context, refs []SourceRef) ([]SourceRef, error) {
	var out []SourceRef
	for i := range chunkCount(len(refs)) {
		part := chunk(refs, i)
		present := map[string]bool{}
		single := map[string]string{refAPIKey: "api_keys", refTeam: "teams", refWorkspace: "workspaces"}
		for kind, table := range single {
			var ids []string
			for _, r := range part {
				if r.Kind == kind {
					ids = append(ids, singleID(r))
				}
			}
			if len(ids) == 0 {
				continue
			}
			var found []string
			if err := s.db.WithContext(ctx).Raw(`SELECT id::text FROM `+table+` WHERE id = ANY(?::uuid[])`, idArray(ids)).Scan(&found).Error; err != nil {
				return nil, fmt.Errorf("read %s: %w", table, err)
			}
			for _, id := range found {
				present[kind+":"+id] = true
			}
		}
		pairs := []struct {
			kind, table, first, second string
			ids                        func(SourceRef) (string, string)
		}{
			{refMembership, "memberships", "organization_id", "user_id", func(r SourceRef) (string, string) { return r.OrganizationID, r.UserID }},
			{refTeamMembership, "team_members", "team_id", "user_id", func(r SourceRef) (string, string) { return r.TeamID, r.UserID }},
			{refWorkspaceTeamGrant, "workspace_teams", "workspace_id", "team_id", func(r SourceRef) (string, string) { return r.WorkspaceID, r.TeamID }},
			{refAPIKeyWorkspaceGrant, "api_key_workspaces", "api_key_id", "workspace_id", func(r SourceRef) (string, string) { return r.APIKeyID, r.WorkspaceID }},
		}
		for _, p := range pairs {
			var of []SourceRef
			for _, r := range part {
				if r.Kind == p.kind {
					of = append(of, r)
				}
			}
			if len(of) == 0 {
				continue
			}
			first, second := validPairs(of, p.ids)
			var found []struct{ A, B string }
			err := s.db.WithContext(ctx).Raw(`
				SELECT x.`+p.first+`::text AS a, x.`+p.second+`::text AS b FROM `+p.table+` x
				JOIN unnest(?::uuid[], ?::uuid[]) AS t(`+p.first+`, `+p.second+`) USING (`+p.first+`, `+p.second+`)`, first, second).Scan(&found).Error
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", p.table, err)
			}
			for _, f := range found {
				present[p.kind+":"+f.A+":"+f.B] = true
			}
		}
		for _, r := range part {
			if !present[presenceKey(r)] {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func singleID(r SourceRef) string {
	switch r.Kind {
	case refAPIKey:
		return r.APIKeyID
	case refTeam:
		return r.TeamID
	default:
		return r.WorkspaceID
	}
}

func presenceKey(r SourceRef) string {
	switch r.Kind {
	case refAPIKey, refTeam, refWorkspace:
		return r.Kind + ":" + canonicalID(singleID(r))
	case refMembership:
		return r.Kind + ":" + canonicalID(r.OrganizationID) + ":" + canonicalID(r.UserID)
	case refTeamMembership:
		return r.Kind + ":" + canonicalID(r.TeamID) + ":" + canonicalID(r.UserID)
	case refWorkspaceTeamGrant:
		return r.Kind + ":" + canonicalID(r.WorkspaceID) + ":" + canonicalID(r.TeamID)
	default:
		return r.Kind + ":" + canonicalID(r.APIKeyID) + ":" + canonicalID(r.WorkspaceID)
	}
}
