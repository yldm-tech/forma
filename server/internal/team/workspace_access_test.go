package team

import (
	"testing"

	"github.com/google/uuid"
)

// forma2's workspace-access.test.ts.
func TestHasWorkspaceAccessChanges(t *testing.T) {
	ws1, ws2, victim := uuid.New(), uuid.New(), uuid.New()
	stored := func(pairs ...any) []WorkspaceAccess {
		var out []WorkspaceAccess
		for i := 0; i < len(pairs); i += 2 {
			out = append(out, WorkspaceAccess{WorkspaceID: pairs[i].(uuid.UUID), Permission: pairs[i+1].(string)})
		}
		return out
	}
	submitted := func(pairs ...any) []WorkspaceInput {
		var out []WorkspaceInput
		for i := 0; i < len(pairs); i += 2 {
			out = append(out, WorkspaceInput{WorkspaceID: pairs[i].(uuid.UUID), Permission: pairs[i+1].(string)})
		}
		return out
	}
	cases := []struct {
		name      string
		current   []WorkspaceAccess
		submitted []WorkspaceInput
		want      bool
	}{
		{"the stored grants unchanged", stored(ws1, "read", ws2, "manage"), submitted(ws1, "read", ws2, "manage"), false},
		{"the same grants in a different order", stored(ws1, "read", ws2, "manage"), submitted(ws2, "manage", ws1, "read"), false},
		{"no grants before or after", nil, nil, false},
		{"a workspace added", stored(ws1, "read"), submitted(ws1, "read", victim, "manage"), true},
		{"a permission escalated", stored(ws1, "read"), submitted(ws1, "manage"), true},
		{"a permission lowered", stored(ws1, "manage"), submitted(ws1, "read"), true},
		{"a workspace removed", stored(ws1, "read", ws2, "read"), submitted(ws1, "read"), true},
		{"a workspace swapped for another", stored(ws1, "read"), submitted(victim, "read"), true},
		{"a duplicate padding the length to mask an added workspace", stored(ws1, "read", ws2, "read"), submitted(ws1, "read", ws1, "read", victim, "manage"), true},
		{"a duplicate hiding an escalation behind a matching entry", stored(ws1, "read"), submitted(ws1, "manage", ws1, "read"), true},
		{"an exact duplicate of the stored grant", stored(ws1, "read"), submitted(ws1, "read", ws1, "read"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasWorkspaceAccessChanges(tc.current, tc.submitted); got != tc.want {
				t.Fatalf("hasWorkspaceAccessChanges = %v, want %v", got, tc.want)
			}
		})
	}
}
