package main

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
)

func TestOrganizationArgs(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--owner", " Ada@Example.com ", "--name", "Acme Labs"},
		{"create", "--name=Acme Labs", "--owner=Ada@Example.com"},
	} {
		if owner, name, err := parseOrganizationArgs(args); err != nil || owner != "Ada@Example.com" || name != "Acme Labs" {
			t.Errorf("parseOrganizationArgs(%q) = %q %q %v", args, owner, name, err)
		}
	}
	for _, args := range [][]string{
		nil,
		{"create"},
		{"delete", "--owner", "ada@example.com", "--name", "Acme"},
		{"create", "--owner", "ada@example.com"},
		{"create", "--owner", "ada@example.com", "--name"},
		{"create", "--owner", "ada@example.com", "--name", " "},
		{"create", "--owner", "ada@example.com", "--owner", "bob@example.com", "--name", "Acme"},
		{"create", "--owner", "ada@example.com", "--name", "Acme", "--role", "owner"},
	} {
		if _, _, err := parseOrganizationArgs(args); err == nil {
			t.Errorf("parseOrganizationArgs(%q) accepted", args)
		}
	}
	// Refused before any database is opened.
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none")
	if code := run([]string{"organization", "create", "--owner", "ada@example.com"}); code != 1 {
		t.Errorf("run(organization create without --name) = %d, want 1", code)
	}
	t.Setenv("IS_FORMA_CLOUD", "1")
	if code := run([]string{"organization", "create", "--owner", "ada@example.com", "--name", "Acme"}); code != 1 {
		t.Errorf("run(organization create) on Forma Cloud = %d, want 1", code)
	}
}

// The operator gives a user who owns nothing (here, an owner whose organization was deleted) an organization of their own: they own it, it has its default workspace, and the user is opted out of its per-response alerts, as when one is created in the app.
func TestCreateOrganizationForOwner(t *testing.T) {
	db := pgtest.GORM(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	scalar := func(dst any, query string, args ...any) {
		t.Helper()
		if err := db.Raw(query, args...).Row().Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	var user string
	scalar(&user, `INSERT INTO users (name, email) VALUES ('Ada', 'ada@example.com') RETURNING id::text`)
	var deleted string
	scalar(&deleted, `INSERT INTO organizations (name) VALUES ('Deleted') RETURNING id::text`)
	if err := db.Exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, user, deleted).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DELETE FROM organizations WHERE id = ?`, deleted).Error; err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := createOrganization(t.Context(), db, log, &out, "ADA@example.com", " Acme Labs "); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "Created organization Acme Labs (") || !strings.Contains(out.String(), "owned by ada@example.com") {
		t.Fatalf("output %q", out.String())
	}
	var org string
	scalar(&org, `SELECT o.id::text FROM organizations o JOIN memberships m ON m.organization_id = o.id WHERE m.user_id = ? AND m.role = 'owner' AND m.accepted AND o.name = 'Acme Labs'`, user)
	var workspaces, billing int
	scalar(&workspaces, `SELECT count(*) FROM workspaces WHERE organization_id = ?`, org)
	scalar(&billing, `SELECT count(*) FROM organization_billing WHERE organization_id = ?`, org)
	if workspaces != 1 || billing != 1 {
		t.Fatalf("%d workspaces and %d billing rows, want 1 and 1", workspaces, billing)
	}
	var unsubscribed bool
	scalar(&unsubscribed, `SELECT jsonb_exists(notification_settings -> 'unsubscribedOrganizationIds', ?) FROM users WHERE id = ?`, org, user)
	if !unsubscribed {
		t.Fatal("the owner is not opted out of the new organization's alerts")
	}

	if err := createOrganization(t.Context(), db, log, &bytes.Buffer{}, "nobody@example.com", "Acme"); err == nil {
		t.Fatal("an unknown email was accepted")
	}
	if err := db.Exec(`UPDATE users SET is_active = false WHERE id = ?`, user).Error; err != nil {
		t.Fatal(err)
	}
	if err := createOrganization(t.Context(), db, log, &bytes.Buffer{}, "ada@example.com", "Beta"); err == nil || !strings.Contains(err.Error(), "deactivated") {
		t.Fatalf("a deactivated account got an organization: %v", err)
	}
}
