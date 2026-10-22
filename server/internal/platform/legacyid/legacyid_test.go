package legacyid

import (
	"testing"

	"github.com/google/uuid"
)

func TestResolve(t *testing.T) {
	native := uuid.MustParse("0199a3b2-7c4d-7e8f-9a0b-1c2d3e4f5a6b")
	cases := []struct {
		in   string
		want uuid.UUID
		ok   bool
	}{
		{in: native.String(), want: native, ok: true},
		{in: "clsurveyaaaaaaaaaaaaaaaa", want: uuid.NewSHA1(Namespace, []byte("clsurveyaaaaaaaaaaaaaaaa")), ok: true},
		// Only the canonical 36-character spelling is a UUID; uuid.Parse's other spellings are legacy ids like any other string.
		{in: "0199a3b27c4d7e8f9a0b1c2d3e4f5a6b", want: uuid.NewSHA1(Namespace, []byte("0199a3b27c4d7e8f9a0b1c2d3e4f5a6b")), ok: true},
		{in: "", want: uuid.Nil, ok: false},
	}
	for _, c := range cases {
		got, ok := Resolve(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("Resolve(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFromLegacyIsAVersion5UUIDInTheNamespace(t *testing.T) {
	id := FromLegacy("clworkspaceaaaaaaaaaaaaa")
	if id.Version() != 5 {
		t.Fatalf("version %d, want 5", id.Version())
	}
	// A known answer, so a change to the namespace or the function cannot pass unnoticed: every migrated row depends on it.
	// Computed independently with Python's uuid.uuid5.
	if got, want := id.String(), "5ddc5569-9576-53f6-9bc5-b00ec07484ce"; got != want {
		t.Fatalf("FromLegacy = %s, want %s", got, want)
	}
}
