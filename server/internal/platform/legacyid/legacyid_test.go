package legacyid

import (
	"testing"

	"github.com/google/uuid"
)

func TestNamespaceIsDerived(t *testing.T) {
	if got := uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://github.com/yldm-tech/forma/legacy-id")); got != Namespace {
		t.Fatalf("Namespace = %s, want %s", Namespace, got)
	}
}

func TestResolve(t *testing.T) {
	const cuid = "clegacyenvaaaaaaaaaaaaaa1"
	if got, want := Resolve(cuid), uuid.NewSHA1(Namespace, []byte(cuid)); got != want {
		t.Errorf("Resolve(%q) = %s, want uuidv5 %s", cuid, got, want)
	}
	if got := ID(cuid); got.Version() != 5 {
		t.Errorf("ID(%q) is version %d, want 5", cuid, got.Version())
	}
	id := uuid.MustParse("0199f3a4-5b6c-7d8e-9f00-112233445566")
	if got := Resolve(id.String()); got != id {
		t.Errorf("Resolve(uuid) = %s, want the uuid itself", got)
	}
	if Resolve("a") == Resolve("b") {
		t.Error("different old ids resolved to the same uuid")
	}
}
