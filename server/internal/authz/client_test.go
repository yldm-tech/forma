package authz

import (
	"context"
	"testing"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// unreachableClient fails any call that gets past validation with authzed_unavailable, so a validation failure is recognisable by its code and zero attempts.
func unreachableClient(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(config.AuthzConfig{Enabled: true, Endpoint: "127.0.0.1:1", Token: "t", Insecure: true, Consistency: "fully_consistent"}, BulkProfile, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestClientRejectsMalformedRequestsBeforeAnyCall(t *testing.T) {
	c := unreachableClient(t)
	ctx := context.Background()
	valid := Relationship{Resource: ObjectRef{"team", "t"}, Relation: "admin", Subject: SubjectRef{Type: "user", ID: "u"}}
	tooMany := make([]RelationshipUpdate, maxUpdatesPerCall+1)
	for i := range tooMany {
		tooMany[i] = touch(valid)
	}
	cases := map[string]func() error{
		"check without permission": func() error {
			_, err := c.CheckPermission(ctx, ObjectRef{"team", "t"}, "", ObjectRef{"user", "u"})
			return err
		},
		"check without subject id": func() error {
			_, err := c.CheckPermission(ctx, ObjectRef{"team", "t"}, "read", ObjectRef{"user", ""})
			return err
		},
		"lookup without resource type": func() error {
			_, err := c.LookupResources(ctx, "", "read", ObjectRef{"user", "u"})
			return err
		},
		"empty write":    func() error { return c.WriteRelationships(ctx, nil) },
		"oversize write": func() error { return c.WriteRelationships(ctx, tooMany) },
		"write with an empty relation": func() error {
			r := valid
			r.Relation = ""
			return c.WriteRelationships(ctx, []RelationshipUpdate{touch(r)})
		},
		"unbounded delete": func() error { return c.DeleteRelationships(ctx, RelationshipFilter{ResourceType: "team"}) },
		"delete with a subject type only": func() error {
			return c.DeleteRelationships(ctx, RelationshipFilter{ResourceType: "team", Subject: &SubjectFilter{Type: "user"}})
		},
		"read without a limit": func() error {
			_, err := c.ReadRelationships(ctx, RelationshipFilter{ResourceType: "team"}, 0, "")
			return err
		},
		"read over the page size": func() error {
			_, err := c.ReadRelationships(ctx, RelationshipFilter{ResourceType: "team"}, maxRelationshipReads+1, "")
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			e, ok := AsError(call())
			if !ok || e.Code != CodeInvalidRequest || e.Attempts != 0 || e.Retryable {
				t.Fatalf("err = %+v, want authzed_invalid_request before any call", e)
			}
		})
	}
}

func TestNewClientRefusesWhenDisabled(t *testing.T) {
	_, err := NewClient(config.AuthzConfig{}, RequestProfile, discardLog())
	if e, ok := AsError(err); !ok || e.Code != CodeDisabled {
		t.Fatalf("err = %v", err)
	}
	c, cleanup, err := OpenSpiceDB(config.Config{}, discardLog())
	if c != nil || err != nil {
		t.Fatalf("OpenSpiceDB disabled = %v, %v", c, err)
	}
	cleanup()
}

func TestDisabledAuthorizerFailsClosed(t *testing.T) {
	e := newTestEnv(t)
	org, user := e.org(), e.user()
	e.member(org, user, "owner")
	a := NewAuthorizer(config.Config{UserManagementMinimumRole: "manager"}, e.db, nil)
	if _, err := a.Can(e.ctx, User(user), OrganizationRead, Organization(org)); err == nil {
		t.Fatal("a disabled authorizer answered")
	} else if ae, _ := AsError(err); ae.Code != CodeDisabled {
		t.Fatalf("err = %v", err)
	}
	if NewDelivery(e.db, nil, discardLog()) != nil || NewAudit(e.db, nil, discardLog()) != nil {
		t.Fatal("delivery and audit must be off while AuthZed is disabled")
	}
}
