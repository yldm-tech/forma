package apikey

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

var readAccess = OrganizationAccess{AccessControl: AccessFlags{Read: true}}

func TestAuthenticatesACurrentFormatKey(t *testing.T) {
	e := newDBEnv(t)
	org := e.org()
	ws := e.workspace(org, "Growth")
	id, key := e.v2Key(org, readAccess, grantInput{WorkspaceID: ws, Permission: PermissionRead})

	for name, headers := range map[string]map[string]string{
		"x-api-key":     {"X-Api-Key": key},
		"Bearer":        {"Authorization": "Bearer " + key},
		"padded header": {"X-Api-Key": "  " + key + " "},
	} {
		t.Run(name, func(t *testing.T) {
			p := e.authenticate(headers, Options{})
			if p == nil {
				t.Fatal("valid key rejected")
			}
			if p.APIKeyID != id || p.OrganizationID != org || p.OrganizationAccess != readAccess {
				t.Fatalf("principal = %+v", p)
			}
			want := WorkspaceGrant{WorkspaceID: ws, WorkspaceName: "Growth", Permission: PermissionRead}
			if len(p.WorkspacePermissions) != 1 || p.WorkspacePermissions[0] != want {
				t.Fatalf("workspace permissions = %+v, want [%+v]", p.WorkspacePermissions, want)
			}
			if p.Actor() != authz.APIKey(id) {
				t.Fatalf("actor = %+v", p.Actor())
			}
		})
	}
}

func TestRejectsUnknownWrongAndDeletedKeys(t *testing.T) {
	e := newDBEnv(t)
	org := e.org()
	ws := e.workspace(org, "Growth")
	id, key := e.v2Key(org, readAccess, grantInput{WorkspaceID: ws, Permission: PermissionRead})
	s := key[len(Prefix):]

	// A row whose lookup hash matches but whose bcrypt hash is of another secret: bcrypt is the verification, not the lookup.
	forged, err := secret.HashPassword("another-secret")
	if err != nil {
		t.Fatal(err)
	}
	forgedSecret := "forged_secret_value"
	forgedID := e.insert(`INSERT INTO api_keys (organization_id, label, hashed_key, lookup_hash) VALUES (?, 'forged', ?, ?) RETURNING id`, org, forged, sha256Hex(forgedSecret))
	e.exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, 'manage')`, forgedID, ws)

	cases := map[string]string{
		"unknown key":                     Prefix + "unknownsecret",
		"wrong bcrypt hash":               Prefix + forgedSecret,
		"bare secret of a current key":    s,
		"key with a character changed":    key[:len(key)-1] + string(rune(key[len(key)-1])^1),
		"prefix only":                     Prefix,
		"current key with trailing junk":  key + "!",
		"unknown legacy key":              "0123456789abcdef0123456789abcdef",
		"lookup hash itself as a key":     sha256Hex(s),
		"bcrypt hash itself as a key":     forged,
		"current key with a doubled pref": Prefix + key,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if p := e.authenticate(map[string]string{"X-Api-Key": raw}, Options{AllowOrganizationOnly: true}); p != nil {
				t.Fatalf("authenticated as %s", p.APIKeyID)
			}
		})
	}
	if recs := e.logs.records(t, "API key bcrypt verification failed"); len(recs) != 1 || recs[0]["apiKeyId"] != forgedID.String() {
		t.Fatalf("bcrypt failure logs = %+v, want one naming the key id", recs)
	}
	if strings.Contains(e.logs.String(), forgedSecret) || strings.Contains(e.logs.String(), s) {
		t.Fatal("a secret reached the log")
	}

	e.exec(`DELETE FROM api_keys WHERE id = ?`, id)
	if p := e.authenticate(map[string]string{"X-Api-Key": key}, Options{AllowOrganizationOnly: true}); p != nil {
		t.Fatal("deleted key still authenticates")
	}
}

func TestAuthenticatesALegacySHA256KeyThroughXAPIKeyOnly(t *testing.T) {
	e := newDBEnv(t)
	org := e.org()
	ws := e.workspace(org, "Legacy")
	raw := "9f86d081884c7d659a2feaa0c55ad015"
	id := e.legacyKey(org, raw, grantInput{WorkspaceID: ws, Permission: PermissionWrite})

	p := e.authenticate(map[string]string{"X-Api-Key": raw}, Options{})
	if p == nil || p.APIKeyID != id || p.OrganizationID != org {
		t.Fatalf("legacy key: principal = %+v", p)
	}
	if len(p.WorkspacePermissions) != 1 || p.WorkspacePermissions[0].Permission != PermissionWrite || p.WorkspacePermissions[0].WorkspaceName != "Legacy" {
		t.Fatalf("legacy key grants = %+v", p.WorkspacePermissions)
	}
	if p.OrganizationAccess != (OrganizationAccess{}) {
		t.Fatalf("legacy key access = %+v", p.OrganizationAccess)
	}
	if p := e.authenticate(map[string]string{"Authorization": "Bearer " + raw}, Options{}); p != nil {
		t.Fatal("a legacy key authenticated as a Bearer token")
	}
	// Sending the legacy key in the current format finds the legacy row by the same hash, whose stored value bcrypt cannot read.
	if p := e.authenticate(map[string]string{"X-Api-Key": Prefix + raw}, Options{}); p != nil {
		t.Fatal("a legacy key authenticated with the current prefix")
	}
	if recs := e.logs.records(t, "Secret verification failed due to invalid hash format"); len(recs) != 1 {
		t.Fatalf("invalid hash logs = %d, want 1", len(recs))
	}
}

func TestOrganizationOnlyKeysNeedARouteThatAcceptsThem(t *testing.T) {
	e := newDBEnv(t)
	org := e.org()
	id, key := e.v2Key(org, readAccess)
	headers := map[string]string{"X-Api-Key": key}

	if p := e.authenticate(headers, Options{}); p != nil {
		t.Fatal("an organization-only key authenticated on a workspace route")
	}
	// The use is recorded before the route's rule is applied, as forma2 did.
	if e.lastUsedAt(id) == nil {
		t.Fatal("a rejected organization-only use was not recorded")
	}
	p := e.authenticate(headers, Options{AllowOrganizationOnly: true})
	if p == nil || p.APIKeyID != id || len(p.WorkspacePermissions) != 0 || p.WorkspacePermissions == nil {
		t.Fatalf("organization-only key on an opted-in route: %+v", p)
	}
}

func TestCrossOrganizationGrantsAreNeverHonoured(t *testing.T) {
	e := newDBEnv(t)
	org, other := e.org(), e.org()
	own, victim := e.workspace(org, "Own"), e.workspace(other, "Victim")
	// Create refuses a foreign workspace; such a row can only come from a defect, so it is written directly.
	_, mixed := e.v2Key(org, readAccess, grantInput{WorkspaceID: own, Permission: PermissionManage}, grantInput{WorkspaceID: victim, Permission: PermissionManage})
	onlyForeignID, onlyForeign := e.v2Key(org, readAccess, grantInput{WorkspaceID: victim, Permission: PermissionManage})

	p := e.authenticate(map[string]string{"X-Api-Key": mixed}, Options{})
	if p == nil || len(p.WorkspacePermissions) != 1 || p.WorkspacePermissions[0].WorkspaceID != own {
		t.Fatalf("mixed key grants = %+v, want only the own workspace", p)
	}
	recs := e.logs.records(t, "Cross-organization API-key workspace grants were filtered")
	if len(recs) != 1 || recs[0]["component"] != "authorization" || recs[0]["crossOrganizationGrantCount"] != float64(1) {
		t.Fatalf("cross-organization logs = %+v", recs)
	}
	for _, leaked := range []string{victim.String(), other.String(), p.APIKeyID.String()} {
		if strings.Contains(e.logs.String(), leaked) {
			t.Fatalf("log names %s", leaked)
		}
	}

	if p := e.authenticate(map[string]string{"X-Api-Key": onlyForeign}, Options{}); p != nil {
		t.Fatal("a key left with only foreign grants authenticated on a workspace route")
	}
	p = e.authenticate(map[string]string{"X-Api-Key": onlyForeign}, Options{AllowOrganizationOnly: true})
	if p == nil || p.APIKeyID != onlyForeignID || len(p.WorkspacePermissions) != 0 {
		t.Fatalf("foreign-only key on an opted-in route: %+v", p)
	}
}

func TestLastUsedAtIsWrittenAtMostEveryThirtySeconds(t *testing.T) {
	cases := []struct {
		name    string
		last    *time.Duration
		written bool
	}{
		{"never used", nil, true},
		{"used 10 s ago", new(-10 * time.Second), false},
		{"used 29.999 s ago", new(-30*time.Second + time.Millisecond), false},
		{"used exactly 30 s ago", new(-30 * time.Second), true},
		{"used a minute ago", new(-time.Minute), true},
	}
	for _, legacy := range []bool{false, true} {
		for _, tc := range cases {
			name := tc.name
			if legacy {
				name = "legacy key " + name
			}
			t.Run(name, func(t *testing.T) {
				e := newDBEnv(t)
				org := e.org()
				ws := e.workspace(org, "W")
				grant := grantInput{WorkspaceID: ws, Permission: PermissionRead}
				var id uuid.UUID
				var raw string
				if legacy {
					raw = uuid.NewString()
					id = e.legacyKey(org, raw, grant)
				} else {
					id, raw = e.v2Key(org, readAccess, grant)
				}
				var before *time.Time
				if tc.last != nil {
					at := e.now().Add(*tc.last)
					before = &at
					e.exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, at, id)
				}
				if p := e.authenticate(map[string]string{"X-Api-Key": raw}, Options{}); p == nil {
					t.Fatal("key rejected")
				}
				got := e.lastUsedAt(id)
				switch {
				case tc.written && (got == nil || !got.Equal(e.now())):
					t.Fatalf("last_used_at = %v, want %v", got, e.now())
				case !tc.written && (got == nil || !got.Equal(*before)):
					t.Fatalf("last_used_at = %v, want it left at %v", got, before)
				}
			})
		}
	}
}

func TestUnknownCurrentFormatKeyCostsABcryptComparison(t *testing.T) {
	e := newDBEnv(t)
	org := e.org()
	e.v2Key(org, readAccess)

	start := time.Now()
	if ok, _ := secret.CheckPassword("", "x"); ok {
		t.Fatal("dummy comparison matched")
	}
	bcryptCost := time.Since(start)

	start = time.Now()
	if p := e.authenticate(map[string]string{"X-Api-Key": Prefix + "doesnotexist"}, Options{}); p != nil {
		t.Fatal("unknown key authenticated")
	}
	if elapsed := time.Since(start); elapsed < bcryptCost/2 {
		t.Fatalf("unknown key took %v, a bcrypt comparison takes %v: the control path skipped bcrypt", elapsed, bcryptCost)
	}
}

func TestRequireMiddleware(t *testing.T) {
	e := newDBEnv(t)
	org := e.org()
	ws := e.workspace(org, "W")
	id, key := e.v2Key(org, readAccess, grantInput{WorkspaceID: ws, Permission: PermissionRead})
	_, orgOnly := e.v2Key(org, readAccess)

	r := gin.New()
	api := r.Group("/api", httpapi.ErrorHandler(e.log))
	handler := func(c *gin.Context) {
		p, _ := PrincipalFrom(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"apiKeyId": p.APIKeyID})
	}
	api.GET("/workspace-route", e.auth.Require(Options{}), handler)
	api.GET("/organization-route", e.auth.Require(Options{AllowOrganizationOnly: true}), handler)

	cases := []struct {
		path, key string
		want      int
	}{
		{"/api/workspace-route", key, http.StatusOK},
		{"/api/workspace-route", "", http.StatusUnauthorized},
		{"/api/workspace-route", Prefix + "nope", http.StatusUnauthorized},
		{"/api/workspace-route", orgOnly, http.StatusUnauthorized},
		{"/api/organization-route", orgOnly, http.StatusOK},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.key != "" {
			req.Header.Set("X-Api-Key", tc.key)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s with %q = %d, want %d: %s", tc.path, tc.key, w.Code, tc.want, w.Body)
		}
		if tc.want == http.StatusUnauthorized && !strings.Contains(w.Body.String(), `"code":"not_authenticated"`) {
			t.Errorf("401 body = %s", w.Body)
		}
		if tc.want == http.StatusOK && tc.key == key && !strings.Contains(w.Body.String(), id.String()) {
			t.Errorf("principal not recorded: %s", w.Body)
		}
	}
}
