package secret

import (
	"bytes"
	"testing"
)

func TestNewTokenIsUniqueAndURLSafe(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewToken()
	if a == b || len(a) != 43 {
		t.Fatalf("tokens %q %q: want two distinct 43-char tokens", a, b)
	}
	for _, c := range a {
		urlSafe := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !urlSafe {
			t.Fatalf("token %q contains %q, not URL-safe", a, c)
		}
	}
}

func TestHashTokenIsStable(t *testing.T) {
	if !bytes.Equal(HashToken("abc"), HashToken("abc")) || bytes.Equal(HashToken("abc"), HashToken("abd")) {
		t.Fatal("HashToken must be deterministic and distinguish inputs")
	}
}

func TestPasswords(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := CheckPassword(h, "correct horse battery"); err != nil || !ok {
		t.Fatalf("right password rejected: %v", err)
	}
	if ok, err := CheckPassword(h, "wrong"); err != nil || ok {
		t.Fatalf("wrong password accepted: %v", err)
	}
	if ok, err := CheckPassword("", "anything"); err != nil || ok {
		t.Fatalf("missing hash accepted: %v", err)
	}
}

// A hash written by forma2's bcryptjs (cost 12, $2b$ prefix) must verify, or every migrated user is locked out.
func TestVerifiesBcryptjsHashes(t *testing.T) {
	// Generated with bcryptjs 3.0.3, the version forma2 ships: hashSync("Password123!", 12).
	const hash = "$2b$12$YV/xESRyZOgC6WIYEJBc9uVo0VjvYM2.BxgoOx/O098h9CYEDEk72"
	ok, err := CheckPassword(hash, "Password123!")
	if err != nil {
		t.Fatalf("CheckPassword: %v", err)
	}
	if !ok {
		t.Fatal("bcryptjs hash did not verify")
	}
}
