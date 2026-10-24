package authz

import (
	"testing"
)

func TestPackGroupsNeverSplitsAGroup(t *testing.T) {
	group := func(n int) []RelationshipUpdate { return make([]RelationshipUpdate, n) }
	batches := packGroups([][]RelationshipUpdate{group(400), group(400), group(400), group(4), group(1000)})
	sizes := make([]int, 0, len(batches))
	for _, b := range batches {
		sizes = append(sizes, len(b))
	}
	if len(sizes) != 3 || sizes[0] != 800 || sizes[1] != 404 || sizes[2] != 1000 {
		t.Fatalf("batch sizes = %v", sizes)
	}
	if packGroups(nil) != nil {
		t.Fatal("empty input produced a batch")
	}
}

func TestChunkingCoversTheLongestList(t *testing.T) {
	if n := chunkCount(0, 401, 200); n != 3 {
		t.Fatalf("chunkCount = %d", n)
	}
	items := make([]int, 401)
	if len(chunk(items, 0)) != 200 || len(chunk(items, 2)) != 1 || chunk(items, 3) != nil || chunk([]int{}, 0) != nil {
		t.Fatal("chunk bounds")
	}
}

func TestLadderUpdatesTouchesOneAndDeletesTheRest(t *testing.T) {
	build := func(relation string) Relationship { return Relationship{Relation: relation} }
	updates := ladderUpdates(organizationRelationNames, "manager", build)
	for _, u := range updates {
		if (u.Relationship.Relation == "manager") != (u.Op == OpTouch) {
			t.Fatalf("updates = %+v", updates)
		}
	}
	for _, u := range ladderUpdates(organizationRelationNames, "", build) {
		if u.Op != OpDelete {
			t.Fatalf("a missing row must delete every relation, got %+v", u)
		}
	}
}

func TestOrganizationAccessIsStrict(t *testing.T) {
	cases := map[string][2]bool{
		`{"accessControl":{"read":true,"write":false}}`:   {true, false},
		`{"accessControl":{"read":true,"write":true}}`:    {true, true},
		`{"accessControl":{"read":"true","write":1}}`:     {false, false},
		`{"accessControl":true}`:                          {false, false},
		`{}`:                                              {false, false},
		`not json`:                                        {false, false},
		`{"accessControl":{"write":true},"extra":"keep"}`: {false, true},
	}
	for raw, want := range cases {
		r, w := organizationAccess([]byte(raw))
		if r != want[0] || w != want[1] {
			t.Errorf("organizationAccess(%s) = %v, %v; want %v", raw, r, w, want)
		}
	}
}

func TestIDArraysKeepOnlyUUIDs(t *testing.T) {
	a := "0190a6e2-7c1e-7a8b-9c3d-4e5f60718293"
	if got := idArray([]string{a, "not-a-uuid"}); got != "{"+a+"}" {
		t.Fatalf("idArray = %s", got)
	}
	first, second := validPairs([]membershipTarget{{a, a}, {a, "x"}}, func(m membershipTarget) (string, string) { return m.OrganizationID, m.UserID })
	if first != "{"+a+"}" || second != "{"+a+"}" {
		t.Fatalf("validPairs = %s %s", first, second)
	}
	if canonicalID("0190A6E2-7C1E-7A8B-9C3D-4E5F60718293") != a || canonicalID("junk") != "junk" {
		t.Fatal("canonicalID")
	}
}
