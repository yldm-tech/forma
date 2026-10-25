package rowid

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParse(t *testing.T) {
	id := uuid.MustParse("01a16211-77de-7d59-ada1-7e2ea6ddb527")
	cases := []struct {
		raw  string
		want uuid.UUID
		ok   bool
	}{
		{id.String(), id, true},
		{strings.ToUpper(id.String()), id, true},
		// uuid.Parse also reads braced, urn and undashed forms; none of them is a row id.
		{"{" + id.String() + "}", uuid.Nil, false},
		{"urn:uuid:" + id.String(), uuid.Nil, false},
		{strings.ReplaceAll(id.String(), "-", ""), uuid.Nil, false},
		{"clxbivtla014iye2vfrn436xd", uuid.Nil, false},
		{"", uuid.Nil, false},
	}
	for _, tc := range cases {
		got, ok := Parse(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Parse(%q) = %s, %v; want %s, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMatches(t *testing.T) {
	id := uuid.MustParse("01a16211-77de-7d59-ada1-7e2ea6ddb527")
	if !Matches(id.String(), id) || !Matches(strings.ToUpper(id.String()), id) {
		t.Fatal("a matching claim was refused")
	}
	if Matches("", uuid.Nil) || Matches("clxbivtla014iye2vfrn436xd", id) || Matches(uuid.NewString(), id) {
		t.Fatal("a foreign claim matched")
	}
}
