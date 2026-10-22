package authz

import (
	"cmp"
	"encoding/json"
	"slices"
)

// The pure half of reconciliation: turning observed SpiceDB relationships into the PostgreSQL records they imply. Nothing here decides a relationship is stale; it only names the record, so the projector can look it up and decide, which is what makes repair safe against a record recreated between the read and the reconcile.

// unprojectedResourceTypes are schema definitions no projector writes (they are evaluated through their workspace). Their relationships are counted as ignored, never pruned.
var unprojectedResourceTypes = map[string]bool{"dashboard": true, "response": true, "survey": true}

// managedResourceTypes are the types the projectors write, in a fixed order so a sweep is reproducible.
var managedResourceTypes = []string{typeAPIKey, typeOrganization, typeTeam, typeWorkspace}

// Source record kinds.
const (
	refAPIKey               = "apiKey"
	refAPIKeyWorkspaceGrant = "apiKeyWorkspaceGrant"
	refMembership           = "membership"
	refTeam                 = "team"
	refTeamMembership       = "teamMembership"
	refWorkspace            = "workspace"
	refWorkspaceTeamGrant   = "workspaceTeamGrant"
)

// SourceRef names a PostgreSQL record a relationship implies, which is what an operator can act on.
type SourceRef struct {
	Kind           string `json:"kind"`
	APIKeyID       string `json:"apiKeyId,omitempty"`
	OrganizationID string `json:"organizationId,omitempty"`
	TeamID         string `json:"teamId,omitempty"`
	UserID         string `json:"userId,omitempty"`
	WorkspaceID    string `json:"workspaceId,omitempty"`
}

func (r SourceRef) key() string {
	b, _ := json.Marshal(r)
	return string(b)
}

// RelationshipRef identifies a relationship outside the vocabulary by its resource and relation.
type RelationshipRef struct {
	ObjectID   string `json:"objectId"`
	ObjectType string `json:"objectType"`
	Relation   string `json:"relation"`
}

// ParentEdge is a resource claiming to belong to an organization, either through its own organization relation or through an organization-level API key grant. A wrong one is a cross-tenant privilege escalation, so it is verified against PostgreSQL and reported, never fixed automatically.
type ParentEdge struct {
	ChildID        string `json:"childId"`
	ChildType      string `json:"childType"`
	OrganizationID string `json:"organizationId"`
	Relation       string `json:"relation"`
}

// PermissionMismatch is a record present on both sides whose role or grant relations differ.
type PermissionMismatch struct {
	ExpectedRelations []string  `json:"expectedRelations"`
	ObservedRelations []string  `json:"observedRelations"`
	Source            SourceRef `json:"source"`
}

var (
	organizationRoleRelationSet = setOf(organizationRelationNames)
	apiKeyAccessRelationSet     = setOf(apiKeyAccessRelNames)
	teamRoleRelationSet         = setOf(teamRelationNames)
	workspaceTeamRelationSet    = setOf(workspaceTeamRelNames)
	workspaceAPIKeyRelationSet  = setOf(workspaceAPIKeyRelNames)
)

func setOf(names []string) map[string]bool {
	s := make(map[string]bool, len(names))
	for _, n := range names {
		s[n] = true
	}
	return s
}

// toSourceRef names the record a relationship implies; ok is false outside the vocabulary. The subject type matters as much as the relation: workspace#reader@api_key and workspace#reader_team@team#member are different grants.
func toSourceRef(r Relationship) (SourceRef, bool) {
	res, sub := r.Resource, r.Subject
	switch res.Type {
	case typeOrganization:
		if sub.Type == typeUser && organizationRoleRelationSet[r.Relation] {
			return SourceRef{Kind: refMembership, OrganizationID: res.ID, UserID: sub.ID}, true
		}
		if sub.Type == typeAPIKey && apiKeyAccessRelationSet[r.Relation] {
			return SourceRef{Kind: refAPIKey, APIKeyID: sub.ID}, true
		}
	case typeTeam:
		if sub.Type == typeOrganization && r.Relation == relationParent {
			return SourceRef{Kind: refTeam, TeamID: res.ID}, true
		}
		if sub.Type == typeUser && teamRoleRelationSet[r.Relation] {
			return SourceRef{Kind: refTeamMembership, TeamID: res.ID, UserID: sub.ID}, true
		}
	case typeWorkspace:
		if sub.Type == typeOrganization && r.Relation == relationParent {
			return SourceRef{Kind: refWorkspace, WorkspaceID: res.ID}, true
		}
		if sub.Type == typeTeam && workspaceTeamRelationSet[r.Relation] {
			return SourceRef{Kind: refWorkspaceTeamGrant, TeamID: sub.ID, WorkspaceID: res.ID}, true
		}
		if sub.Type == typeAPIKey && workspaceAPIKeyRelationSet[r.Relation] {
			return SourceRef{Kind: refAPIKeyWorkspaceGrant, APIKeyID: sub.ID, WorkspaceID: res.ID}, true
		}
	case typeAPIKey:
		if sub.Type == typeOrganization && r.Relation == relationParent {
			return SourceRef{Kind: refAPIKey, APIKeyID: res.ID}, true
		}
	}
	return SourceRef{}, false
}

// toParentEdge is the organization a relationship says a resource belongs to. An organization-level API key grant states the same fact from the other side, so it is verified the same way: otherwise a grant naming another tenant's key would pass as "the key exists".
func toParentEdge(r Relationship) (ParentEdge, bool) {
	if r.Resource.Type == typeOrganization && r.Subject.Type == typeAPIKey && apiKeyAccessRelationSet[r.Relation] {
		return ParentEdge{ChildID: r.Subject.ID, ChildType: typeAPIKey, OrganizationID: r.Resource.ID, Relation: r.Relation}, true
	}
	if r.Relation != relationParent || r.Subject.Type != typeOrganization {
		return ParentEdge{}, false
	}
	switch r.Resource.Type {
	case typeAPIKey, typeTeam, typeWorkspace:
		return ParentEdge{ChildID: r.Resource.ID, ChildType: r.Resource.Type, OrganizationID: r.Subject.ID, Relation: r.Relation}, true
	}
	return ParentEdge{}, false
}

// observation classifies what a read saw. Every list is de-duplicated and deterministically ordered, so two runs over unchanged state report identically.
type observation struct {
	ignored     int
	managed     []Relationship
	parentEdges []ParentEdge
	sourceRefs  []SourceRef
	unmanaged   []RelationshipRef
}

func relationshipKey(r Relationship) string {
	b, _ := json.Marshal(r)
	return string(b)
}

func summarize(relationships []Relationship) observation {
	refs := map[string]SourceRef{}
	managed := map[string]Relationship{}
	unmanaged := map[RelationshipRef]bool{}
	edges := map[ParentEdge]bool{}
	var o observation
	for _, r := range relationships {
		if unprojectedResourceTypes[r.Resource.Type] {
			o.ignored++
			continue
		}
		if e, ok := toParentEdge(r); ok {
			edges[e] = true
		}
		if ref, ok := toSourceRef(r); ok {
			refs[ref.key()] = ref
			managed[relationshipKey(r)] = r
			continue
		}
		unmanaged[RelationshipRef{ObjectID: r.Resource.ID, ObjectType: r.Resource.Type, Relation: r.Relation}] = true
	}
	o.sourceRefs = sortedValues(refs)
	o.managed = sortedValues(managed)
	for e := range edges {
		o.parentEdges = append(o.parentEdges, e)
	}
	slices.SortFunc(o.parentEdges, func(a, b ParentEdge) int {
		return cmp.Or(cmp.Compare(a.ChildType, b.ChildType), cmp.Compare(a.ChildID, b.ChildID), cmp.Compare(a.OrganizationID, b.OrganizationID), cmp.Compare(a.Relation, b.Relation))
	})
	for u := range unmanaged {
		o.unmanaged = append(o.unmanaged, u)
	}
	slices.SortFunc(o.unmanaged, func(a, b RelationshipRef) int {
		return cmp.Or(cmp.Compare(a.ObjectType, b.ObjectType), cmp.Compare(a.ObjectID, b.ObjectID), cmp.Compare(a.Relation, b.Relation))
	})
	return o
}

func sortedValues[T any](m map[string]T) []T {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]T, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

// unprojectedRefs are records PostgreSQL holds that SpiceDB has no relationship for at all; without this direction an empty SpiceDB would read as clean.
func unprojectedRefs(expected, observed []SourceRef) []SourceRef {
	seen := map[string]bool{}
	for _, r := range observed {
		seen[r.key()] = true
	}
	missing := map[string]SourceRef{}
	for _, r := range expected {
		if !seen[r.key()] {
			missing[r.key()] = r
		}
	}
	return sortedValues(missing)
}

// mismatchedPermissions compares, per record present on both sides, the exact set of non-parent relationships. Parent edges are verified separately, where a cross-tenant parent can be told apart from a missing row.
func mismatchedPermissions(expected, observed []Relationship) []PermissionMismatch {
	type group struct {
		source SourceRef
		keys   []string
		names  []string
	}
	byRecord := func(rs []Relationship) map[string]*group {
		groups := map[string]*group{}
		for _, r := range rs {
			ref, ok := toSourceRef(r)
			if !ok {
				continue
			}
			g := groups[ref.key()]
			if g == nil {
				g = &group{source: ref}
				groups[ref.key()] = g
			}
			if r.Relation != relationParent {
				g.keys = append(g.keys, relationshipKey(r))
				g.names = append(g.names, r.Relation)
			}
		}
		return groups
	}
	exp, obs := byRecord(expected), byRecord(observed)
	var keys []string
	for k := range exp {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var out []PermissionMismatch
	for _, k := range keys {
		e, o := exp[k], obs[k]
		if o == nil {
			continue
		}
		if slices.Equal(slices.Sorted(slices.Values(e.keys)), slices.Sorted(slices.Values(o.keys))) {
			continue
		}
		out = append(out, PermissionMismatch{ExpectedRelations: sortedUnique(e.names), ObservedRelations: sortedUnique(o.names), Source: e.source})
	}
	return out
}

func sortedUnique(names []string) []string {
	out := slices.Sorted(slices.Values(names))
	out = slices.Compact(out)
	if out == nil {
		out = []string{}
	}
	return out
}
