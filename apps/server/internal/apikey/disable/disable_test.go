package disable

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
)

func insert(t *testing.T, db *gorm.DB, query string, args ...any) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Raw(query, args...).Row().Scan(&id); err != nil {
		t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

func TestCreatedBy(t *testing.T) {
	t.Parallel()
	db, ctx := pgtest.GORM(t), context.Background()
	orgA := insert(t, db, `INSERT INTO organizations (name) VALUES ('A') RETURNING id`)
	orgB := insert(t, db, `INSERT INTO organizations (name) VALUES ('B') RETURNING id`)
	ada := insert(t, db, `INSERT INTO users (name, email) VALUES ('Ada', 'ada@example.com') RETURNING id`)
	grace := insert(t, db, `INSERT INTO users (name, email) VALUES ('Grace', 'grace@example.com') RETURNING id`)
	key := func(org, user uuid.UUID, label string) uuid.UUID {
		return insert(t, db, `INSERT INTO api_keys (organization_id, created_by, label, hashed_key, lookup_hash) VALUES (?, ?, ?, 'x', ?) RETURNING id`, org, user, label, uuid.NewString())
	}
	adaA, adaB, graceA := key(orgA, ada, "ada A"), key(orgB, ada, "ada B"), key(orgA, grace, "grace A")
	reason := func(id uuid.UUID) string {
		var r *string
		if err := db.Raw(`SELECT disabled_reason FROM api_keys WHERE id = ?`, id).Row().Scan(&r); err != nil {
			t.Fatal(err)
		}
		if r == nil {
			return ""
		}
		return *r
	}

	got, err := CreatedBy(ctx, db, ada, &orgA, ReasonCreatorRemoved)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (Key{ID: adaA, OrganizationID: orgA, Label: "ada A"}) {
		t.Fatalf("CreatedBy in A = %+v", got)
	}
	if reason(adaA) != ReasonCreatorRemoved || reason(adaB) != "" || reason(graceA) != "" {
		t.Fatal("a key outside the creator and organization was disabled")
	}

	// Every organization; a key already disabled keeps its first reason and is not returned again.
	got, err = CreatedBy(ctx, db, ada, nil, ReasonCreatorDeleted)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != adaB || reason(adaA) != ReasonCreatorRemoved || reason(adaB) != ReasonCreatorDeleted {
		t.Fatalf("CreatedBy everywhere = %+v", got)
	}
	if got, err = CreatedBy(ctx, db, ada, nil, ReasonCreatorDeleted); err != nil || len(got) != 0 {
		t.Fatalf("a second call disabled %+v (%v)", got, err)
	}

	// The schema keeps the two columns together and the reason within its set.
	for _, q := range []string{
		`UPDATE api_keys SET disabled_reason = 'creator_left' WHERE id = ?`,
		`UPDATE api_keys SET disabled_at = now() WHERE id = ?`,
		`UPDATE api_keys SET disabled_at = now(), disabled_reason = 'expired' WHERE id = ?`,
	} {
		if err := db.Exec(q, graceA).Error; err == nil {
			t.Errorf("%s was accepted", q)
		}
	}
}

func TestManagesKeys(t *testing.T) {
	t.Parallel()
	for role, want := range map[string]bool{"owner": true, "manager": true, "member": false, "billing": false, "": false} {
		if ManagesKeys(role) != want {
			t.Errorf("ManagesKeys(%q) = %v", role, !want)
		}
	}
}

func TestEvents(t *testing.T) {
	t.Parallel()
	k := Key{ID: uuid.New(), OrganizationID: uuid.New(), Label: "CI"}
	events := Events([]Key{k}, ReasonCreatorLeft)
	want := audit.Event{Action: audit.ActionDisabled, TargetType: audit.TargetAPIKey, TargetID: k.ID.String(), OrganizationID: k.OrganizationID.String(), Status: audit.StatusSuccess}
	if len(events) != 1 {
		t.Fatalf("%d events", len(events))
	}
	e := events[0]
	obj, _ := e.NewObject.(map[string]string)
	e.NewObject = nil
	if e != want || obj["label"] != "CI" || obj["disabledReason"] != ReasonCreatorLeft {
		t.Fatalf("event %+v %v", events[0], obj)
	}
}
