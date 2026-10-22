package legacyid

import (
	"testing"

	"github.com/google/uuid"
)

func TestNamespaceIsDerivedAsDocumented(t *testing.T) {
	if got := uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://forma.ylam.ai/ns/legacy-id")); got != Namespace {
		t.Fatalf("namespace %s, derivation gives %s", Namespace, got)
	}
}

func TestResolve(t *testing.T) {
	native := uuid.MustParse("01a16211-77de-7d59-ada1-7e2ea6ddb527")
	cases := []struct {
		raw  string
		want uuid.UUID
		ok   bool
	}{
		{native.String(), native, true},
		// uuid.Parse also reads braced and urn forms; only the canonical 36-character form is a UUID here, anything else is a forma2 id.
		{"{" + native.String() + "}", ID("{" + native.String() + "}"), true},
		{"urn:uuid:" + native.String(), ID("urn:uuid:" + native.String()), true},
		{"clxbivtla014iye2vfrn436xd", ID("clxbivtla014iye2vfrn436xd"), true},
		{"", uuid.Nil, false},
	}
	for _, tc := range cases {
		got, ok := Resolve(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Resolve(%q) = %s, %v; want %s, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestIDIsStableAndVersion5(t *testing.T) {
	a, b := ID("clxbivtla014iye2vfrn436xd"), ID("clxbivtla014iye2vfrn436xd")
	if a != b || a.Version() != 5 {
		t.Fatalf("ID = %s, %s (version %d)", a, b, a.Version())
	}
	if ID("clxbivtla014iye2vfrn436xe") == a {
		t.Fatal("two old ids map to one id")
	}
}

func TestMatches(t *testing.T) {
	native := uuid.MustParse("01a16211-77de-7d59-ada1-7e2ea6ddb527")
	migrated := ID("clxbivtla014iye2vfrn436xd")
	if !Matches(native.String(), native) || !Matches("clxbivtla014iye2vfrn436xd", migrated) {
		t.Fatal("a matching claim was refused")
	}
	if Matches("", uuid.Nil) || Matches("clxbivtla014iye2vfrn436xd", native) || Matches(migrated.String(), native) {
		t.Fatal("a foreign claim matched")
	}
}
