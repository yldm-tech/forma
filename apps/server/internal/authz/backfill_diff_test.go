package authz

import (
	"slices"
	"testing"
)

func rel(resType, resID, relation, subType, subID, subRel string) Relationship {
	return Relationship{Resource: ObjectRef{resType, resID}, Relation: relation, Subject: SubjectRef{Type: subType, ID: subID, Relation: subRel}}
}

func TestToSourceRefNamesTheRecordARelationshipImplies(t *testing.T) {
	cases := []struct {
		r    Relationship
		want SourceRef
		ok   bool
	}{
		{rel("organization", "o", "owner", "user", "u", ""), SourceRef{Kind: refMembership, OrganizationID: "o", UserID: "u"}, true},
		{rel("organization", "o", "billing", "user", "u", ""), SourceRef{Kind: refMembership, OrganizationID: "o", UserID: "u"}, true},
		{rel("organization", "o", "api_key_writer", "api_key", "k", ""), SourceRef{Kind: refAPIKey, APIKeyID: "k"}, true},
		{rel("team", "t", "organization", "organization", "o", ""), SourceRef{Kind: refTeam, TeamID: "t"}, true},
		{rel("team", "t", "admin", "user", "u", ""), SourceRef{Kind: refTeamMembership, TeamID: "t", UserID: "u"}, true},
		{rel("workspace", "w", "organization", "organization", "o", ""), SourceRef{Kind: refWorkspace, WorkspaceID: "w"}, true},
		{rel("workspace", "w", "writer_team", "team", "t", "member"), SourceRef{Kind: refWorkspaceTeamGrant, TeamID: "t", WorkspaceID: "w"}, true},
		{rel("workspace", "w", "manager", "api_key", "k", ""), SourceRef{Kind: refAPIKeyWorkspaceGrant, APIKeyID: "k", WorkspaceID: "w"}, true},
		{rel("api_key", "k", "organization", "organization", "o", ""), SourceRef{Kind: refAPIKey, APIKeyID: "k"}, true},
		// The relation name alone is not enough: an API key relation with a team subject is outside the vocabulary.
		{rel("workspace", "w", "reader", "team", "t", "member"), SourceRef{}, false},
		{rel("organization", "o", "owner", "api_key", "k", ""), SourceRef{}, false},
		{rel("survey", "s", "workspace", "workspace", "w", ""), SourceRef{}, false},
	}
	for _, tc := range cases {
		got, ok := toSourceRef(tc.r)
		if ok != tc.ok || got != tc.want {
			t.Errorf("toSourceRef(%+v) = %+v, %v; want %+v, %v", tc.r, got, ok, tc.want, tc.ok)
		}
	}
}

func TestToParentEdgeCoversBothShapesOfAnAPIKeyOwnership(t *testing.T) {
	cases := []struct {
		r    Relationship
		want ParentEdge
		ok   bool
	}{
		{rel("workspace", "w", "organization", "organization", "o", ""), ParentEdge{"w", "workspace", "o", "organization"}, true},
		{rel("api_key", "k", "organization", "organization", "o", ""), ParentEdge{"k", "api_key", "o", "organization"}, true},
		{rel("organization", "o", "api_key_reader", "api_key", "k", ""), ParentEdge{"k", "api_key", "o", "api_key_reader"}, true},
		{rel("organization", "o", "owner", "user", "u", ""), ParentEdge{}, false},
		{rel("survey", "s", "organization", "organization", "o", ""), ParentEdge{}, false},
	}
	for _, tc := range cases {
		got, ok := toParentEdge(tc.r)
		if ok != tc.ok || got != tc.want {
			t.Errorf("toParentEdge(%+v) = %+v, %v", tc.r, got, ok)
		}
	}
}

func TestSummarizeClassifiesAndDeduplicates(t *testing.T) {
	o := summarize([]Relationship{
		rel("organization", "o", "member", "user", "u", ""),
		rel("organization", "o", "owner", "user", "u", ""),
		rel("survey", "s", "workspace", "workspace", "w", ""),
		rel("workspace", "w", "custom", "user", "u", ""),
		rel("workspace", "w", "custom", "user", "u2", ""),
		rel("team", "t", "organization", "organization", "o", ""),
	})
	if o.ignored != 1 || len(o.sourceRefs) != 2 || len(o.managed) != 3 || len(o.parentEdges) != 1 || len(o.unmanaged) != 1 {
		t.Fatalf("summary = %+v", o)
	}
}

func TestMismatchedPermissionsComparesExactRelationSets(t *testing.T) {
	expected := []Relationship{
		rel("organization", "o", "owner", "user", "u", ""),
		rel("organization", "o", "api_key_reader", "api_key", "k", ""),
		rel("organization", "o", "api_key_writer", "api_key", "k", ""),
		rel("team", "t", "organization", "organization", "o", ""),
		rel("organization", "o", "member", "user", "absent", ""),
	}
	observed := []Relationship{
		rel("organization", "o", "owner", "user", "u", ""),
		rel("organization", "o", "member", "user", "u", ""),
		rel("organization", "o", "api_key_reader", "api_key", "k", ""),
		rel("team", "t", "organization", "organization", "other", ""),
	}
	got := mismatchedPermissions(expected, observed)
	if len(got) != 2 {
		t.Fatalf("mismatches = %+v", got)
	}
	byKind := map[string]PermissionMismatch{}
	for _, m := range got {
		byKind[m.Source.Kind] = m
	}
	if m := byKind[refMembership]; !slices.Equal(m.ExpectedRelations, []string{"owner"}) || !slices.Equal(m.ObservedRelations, []string{"member", "owner"}) {
		t.Errorf("membership mismatch = %+v", m)
	}
	if m := byKind[refAPIKey]; !slices.Equal(m.ExpectedRelations, []string{"api_key_reader", "api_key_writer"}) || !slices.Equal(m.ObservedRelations, []string{"api_key_reader"}) {
		t.Errorf("api key mismatch = %+v", m)
	}
}

func TestUnprojectedRefsIsTheMissingDirection(t *testing.T) {
	a := SourceRef{Kind: refTeam, TeamID: "a"}
	b := SourceRef{Kind: refTeam, TeamID: "b"}
	got := unprojectedRefs([]SourceRef{a, b, a}, []SourceRef{b})
	if len(got) != 1 || got[0] != a {
		t.Fatalf("unprojected = %+v", got)
	}
}

func TestRunStatus(t *testing.T) {
	cases := []struct {
		r    BackfillResult
		want string
	}{
		{BackfillResult{}, StatusReconciled},
		{BackfillResult{Counters: BackfillCounters{Orphaned: 2, Pruned: 2}}, StatusReconciled},
		{BackfillResult{Counters: BackfillCounters{Orphaned: 2, Pruned: 1}}, StatusDrifted},
		{BackfillResult{Counters: BackfillCounters{Missing: 1}}, StatusDrifted},
		{BackfillResult{Counters: BackfillCounters{Invalid: 1}}, StatusDrifted},
		{BackfillResult{Counters: BackfillCounters{Unmanaged: 1}}, StatusDrifted},
		{BackfillResult{Counters: BackfillCounters{MismatchedParents: 1}}, StatusDrifted},
		{BackfillResult{Truncated: true}, StatusDrifted},
		{BackfillResult{Counters: BackfillCounters{Failed: 1, Missing: 3}}, StatusFailed},
	}
	for _, tc := range cases {
		if got := runStatus(tc.r); got != tc.want {
			t.Errorf("runStatus(%+v) = %s, want %s", tc.r.Counters, got, tc.want)
		}
	}
}
