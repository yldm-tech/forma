package apikey

import (
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

func TestHint(t *testing.T) {
	for key, want := range map[string]string{
		"fbk_abcdefgh-_xY": "-_xY",
		"abcd":             "abcd",
		"abc":              "",
		"":                 "",
	} {
		if got := Hint(key); got != want {
			t.Errorf("Hint(%q) = %q, want %q", key, got, want)
		}
	}
	k, err := newKey()
	if err != nil {
		t.Fatal(err)
	}
	if k.Hint != k.Key[len(k.Key)-HintLength:] {
		t.Fatalf("hint %q of %q is not its last %d characters", k.Hint, k.Key, HintLength)
	}
}

// A new key keeps its last four characters, so the list can tell keys apart, and the expiry chosen when it was created, if any (F11, admin-ops-05 phase 2).
func TestCreateKeepsTheHintAndTheExpiry(t *testing.T) {
	e := newAppEnv(t)
	org, owner := e.org(), e.user()
	e.member(org, owner, "owner")
	ws := e.workspace(org, "Growth")
	e.drain()
	token := e.session(owner)

	// Sent with nanoseconds and an offset: stored, returned and listed as the same instant, in microseconds.
	expires := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Microsecond)
	body := createBody("Expiring", readAccess, grant(ws, PermissionRead))
	body["expiresAt"] = expires.In(time.FixedZone("JST", 9*60*60)).Add(123 * time.Nanosecond).Format(time.RFC3339Nano)
	res := e.do(http.MethodPost, keysPath(org), token, body)
	if res.status != http.StatusCreated {
		t.Fatalf("create = %d %s", res.status, res.body)
	}
	var created Created
	res.decode(t, &created)
	hint := created.Key[len(created.Key)-HintLength:]
	if created.KeyHint == nil || *created.KeyHint != hint {
		t.Fatalf("created hint = %v, want %q", created.KeyHint, hint)
	}
	if created.ExpiresAt == nil || !created.ExpiresAt.Equal(expires) {
		t.Fatalf("created expiresAt = %v, want %v", created.ExpiresAt, expires)
	}
	var stored struct {
		KeyHint   string
		ExpiresAt time.Time
	}
	if err := e.db.Raw(`SELECT key_hint, expires_at FROM api_keys WHERE id = ?`, created.ID).Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.KeyHint != hint || !stored.ExpiresAt.Equal(expires) {
		t.Fatalf("stored = %+v", stored)
	}
	k := e.listed(org, created.ID, token)
	if k.KeyHint == nil || *k.KeyHint != hint || k.ExpiresAt == nil || !k.ExpiresAt.Equal(expires) {
		t.Fatalf("listed = %+v", k)
	}

	// Without an expiry, or with a null one, the key never expires.
	for name, b := range map[string]map[string]any{"absent": createBody("Forever", readAccess), "null": createBody("Forever", readAccess)} {
		if name == "null" {
			b["expiresAt"] = nil
		}
		res := e.do(http.MethodPost, keysPath(org), token, b)
		var c Created
		res.decode(t, &c)
		if res.status != http.StatusCreated || c.ExpiresAt != nil || c.KeyHint == nil {
			t.Fatalf("%s expiry: create = %d %s", name, res.status, res.body)
		}
		if l := e.listed(org, c.ID, token); l.ExpiresAt != nil {
			t.Fatalf("%s expiry: listed expiresAt = %v", name, l.ExpiresAt)
		}
	}

	// A key whose plaintext was never seen by this release (a key created before hints, the seeded one) lists without a hint.
	legacy := e.legacyKey(org, uuid.NewString())
	if l := e.listed(org, legacy, token); l.KeyHint != nil || l.ExpiresAt != nil {
		t.Fatalf("a key without a hint = %+v", l)
	}
}

func TestCreateRefusesAnExpiryThatIsNotAFutureTime(t *testing.T) {
	e := newAppEnv(t)
	org, owner := e.org(), e.user()
	e.member(org, owner, "owner")
	e.drain()
	token := e.session(owner)

	for name, tc := range map[string]struct {
		value any
		rule  string
	}{
		"in the past":       {time.Now().Add(-time.Minute).Format(time.RFC3339), "future"},
		"a date alone":      {time.Now().AddDate(0, 1, 0).Format(time.DateOnly), "datetime"},
		"not a time":        {"next week", "datetime"},
		"a number":          {1767225600, "type"},
		"without an offset": {time.Now().AddDate(0, 1, 0).Format("2006-01-02T15:04:05"), "datetime"},
		// Year 10000 once in UTC: stored, it would fail every JSON response that carries it.
		"past year 9999 in UTC": {"9999-12-31T23:59:59-12:00", "max"},
		// Year 10000 in a server zone ahead of UTC, as pgx reads it back.
		"in year 9999": {"9999-12-31T23:59:59Z", "max"},
		"at the bound": {"9999-01-01T00:00:00Z", "max"},
	} {
		t.Run(name, func(t *testing.T) {
			body := createBody("Expiring", readAccess)
			body["expiresAt"] = tc.value
			res := e.do(http.MethodPost, keysPath(org), token, body)
			if res.status != http.StatusBadRequest || res.code(t) != apperr.CodeValidation {
				t.Fatalf("create = %d %s", res.status, res.body)
			}
			var env struct {
				Details struct {
					Fields []apperr.FieldError `json:"fields"`
				} `json:"details"`
			}
			res.decode(t, &env)
			if len(env.Details.Fields) != 1 || env.Details.Fields[0].Field != "expiresAt" || env.Details.Fields[0].Rule != tc.rule {
				t.Fatalf("fields = %+v, want expiresAt/%s", env.Details.Fields, tc.rule)
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM api_keys`); n != 0 {
		t.Fatalf("%d keys created", n)
	}
}

// farZoneEnv, when set, makes TestMain put the test binary in the zone furthest ahead of UTC before any goroutine starts.
const farZoneEnv = "FORMA_APIKEY_TEST_FAR_ZONE"

// The latest expiry accepted can be returned and listed even by a server whose zone is as far ahead of UTC as any, since the database driver reads times back in the local zone.
//
// The zone is time.Local, which every time.Now reads, so changing it here would race with goroutines an earlier test left behind (a closed gRPC transport still finishing). The test runs instead in a copy of the test binary whose TestMain sets the zone before anything else runs.
func TestTheLatestExpiryIsListedInAnyZone(t *testing.T) {
	if os.Getenv(farZoneEnv) == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestTheLatestExpiryIsListedInAnyZone$", "-test.v", "-test.timeout=10m")
		cmd.Env = append(os.Environ(), farZoneEnv+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("in UTC+14: %v\n%s", err, out)
		}
		return
	}
	if _, offset := time.Now().Zone(); offset != 14*60*60 {
		t.Fatalf("local offset = %d, want UTC+14", offset)
	}

	e := newAppEnv(t)
	org, owner := e.org(), e.user()
	e.member(org, owner, "owner")
	e.drain()
	token := e.session(owner)

	body := createBody("Far future", readAccess)
	body["expiresAt"] = "9998-12-31T23:59:59.999999Z"
	res := e.do(http.MethodPost, keysPath(org), token, body)
	if res.status != http.StatusCreated {
		t.Fatalf("create = %d %s", res.status, res.body)
	}
	var created Created
	res.decode(t, &created)
	want := time.Date(9998, time.December, 31, 23, 59, 59, 999999000, time.UTC)
	if l := e.listed(org, created.ID, token); l.ExpiresAt == nil || !l.ExpiresAt.Equal(want) {
		t.Fatalf("listed expiresAt = %v, want %v", l.ExpiresAt, want)
	}
}

// An expired key is refused from the instant it expires, on every route and in both headers, and its refused uses are not recorded; it stays listed, and can be relabelled and deleted.
func TestAnExpiredKeyNeverAuthenticates(t *testing.T) {
	e := newAppEnv(t)
	org, owner := e.org(), e.user()
	e.member(org, owner, "owner")
	ws := e.workspace(org, "Growth")
	e.drain()
	token := e.session(owner)

	expires := e.now().Add(time.Hour)
	body := createBody("Expiring", readAccess, grant(ws, PermissionRead))
	body["expiresAt"] = expires.Format(time.RFC3339Nano)
	res := e.do(http.MethodPost, keysPath(org), token, body)
	if res.status != http.StatusCreated {
		t.Fatalf("create = %d %s", res.status, res.body)
	}
	var created Created
	res.decode(t, &created)

	setClock := func(at time.Time) {
		e.mu.Lock()
		e.clock = at
		e.mu.Unlock()
	}
	setClock(expires.Add(-time.Microsecond))
	if e.authenticate(map[string]string{"X-Api-Key": created.Key}, Options{}) == nil {
		t.Fatal("the key is refused before it expires")
	}
	used := e.lastUsedAt(created.ID)
	if used == nil {
		t.Fatal("the use before the expiry was not recorded")
	}

	for _, at := range []time.Time{expires, expires.Add(24 * time.Hour)} {
		setClock(at)
		for _, opts := range []Options{{}, {AllowOrganizationOnly: true}} {
			if e.authenticate(map[string]string{"X-Api-Key": created.Key}, opts) != nil {
				t.Fatalf("the key authenticates at %v, its expiry %v", at, expires)
			}
			if e.authenticate(map[string]string{"Authorization": "Bearer " + created.Key}, opts) != nil {
				t.Fatalf("the key authenticates as a bearer token at %v, its expiry %v", at, expires)
			}
		}
	}
	if at := e.lastUsedAt(created.ID); at == nil || !at.Equal(*used) {
		t.Fatalf("a refused use was recorded: %v, want %v", at, used)
	}
	if warned := e.logs.records(t, "An expired API key was used"); len(warned) == 0 || warned[0]["apiKeyId"] != created.ID.String() {
		t.Fatalf("no warning names the expired key: %v", warned)
	}

	if k := e.listed(org, created.ID, token); k.ExpiresAt == nil || !k.ExpiresAt.Equal(expires) || k.DisabledAt != nil {
		t.Fatalf("listed = %+v", k)
	}
	if res := e.do(http.MethodPatch, keyPath(org, created.ID), token, map[string]string{"label": "Old"}); res.status != http.StatusOK {
		t.Fatalf("relabel an expired key = %d %s", res.status, res.body)
	}
	if res := e.do(http.MethodDelete, keyPath(org, created.ID), token, nil); res.status != http.StatusNoContent {
		t.Fatalf("delete an expired key = %d %s", res.status, res.body)
	}
}
