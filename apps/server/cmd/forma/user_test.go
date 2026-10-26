package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
)

func TestUserArgs(t *testing.T) {
	if active, email, err := parseUserArgs([]string{"activate", " Ada@Example.com "}); err != nil || !active || email != "Ada@Example.com" {
		t.Fatalf("activate = %v %q %v", active, email, err)
	}
	if active, _, err := parseUserArgs([]string{"deactivate", "ada@example.com"}); err != nil || active {
		t.Fatalf("deactivate = %v %v", active, err)
	}
	for _, args := range [][]string{nil, {"activate"}, {"activate", " "}, {"enable", "ada@example.com"}, {"activate", "a@example.com", "b@example.com"}} {
		if _, _, err := parseUserArgs(args); err == nil {
			t.Errorf("parseUserArgs(%q) accepted", args)
		}
	}
	// Refused before any database is opened.
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none")
	if code := run([]string{"user", "suspend", "ada@example.com"}); code != 1 {
		t.Errorf("run(user suspend) = %d, want 1", code)
	}
}

// The operator's switch turns the account itself on and off, matched by email in any case; turned back on, the organizations that suspended it keep it suspended and are named.
func TestSetAccountActive(t *testing.T) {
	db := pgtest.GORM(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	var user, acme, other string
	for dst, query := range map[*string]string{
		&user:  `INSERT INTO users (name, email) VALUES ('Ada', 'ada@example.com') RETURNING id::text`,
		&acme:  `INSERT INTO organizations (name) VALUES ('Acme') RETURNING id::text`,
		&other: `INSERT INTO organizations (name) VALUES ('Other') RETURNING id::text`,
	} {
		if err := db.Raw(query).Row().Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO memberships (user_id, organization_id, role, accepted, suspended_at) VALUES (?, ?, 'member', true, now())`, user, acme)
	exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, user, other)
	isActive := func() bool {
		t.Helper()
		var v bool
		if err := db.Raw(`SELECT is_active FROM users WHERE id = ?`, user).Row().Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	set := func(active bool) string {
		t.Helper()
		var out bytes.Buffer
		if err := setAccountActive(t.Context(), db, &out, "ADA@example.com", active); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	if out := set(false); isActive() || out != "ada@example.com is now deactivated\n" {
		t.Fatalf("deactivate: active %v, %q", isActive(), out)
	}
	if out := set(false); isActive() || out != "ada@example.com is already deactivated\n" {
		t.Fatalf("deactivate again: active %v, %q", isActive(), out)
	}
	out := set(true)
	if !isActive() || !strings.HasPrefix(out, "ada@example.com is now active\n") || !strings.Contains(out, "Still suspended in Acme:") || strings.Contains(out, "Other") {
		t.Fatalf("activate: active %v, %q", isActive(), out)
	}
	var suspended int
	if err := db.Raw(`SELECT count(*) FROM memberships WHERE user_id = ? AND suspended_at IS NOT NULL`, user).Row().Scan(&suspended); err != nil || suspended != 1 {
		t.Fatalf("suspended memberships = %d (%v), want Acme's left as it was", suspended, err)
	}
	if err := setAccountActive(t.Context(), db, &bytes.Buffer{}, "nobody@example.com", true); err == nil {
		t.Fatal("an unknown email was accepted")
	}
}
