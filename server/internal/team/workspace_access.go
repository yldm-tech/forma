package team

import "github.com/google/uuid"

// hasWorkspaceAccessChanges is forma2's rule for when a team update touches workspace access: any grant added, removed or re-scoped. A team admin may be a plain organization member, and the update carries the team's whole grant list, so without this check they could give their own team manage on every workspace of the organization. Round-tripping the stored grants unchanged, in any order, is not a change.
//
// A repeated workspace is a change: a map keeps the last entry for a duplicate, so an escalated grant could otherwise hide behind a matching one.
func hasWorkspaceAccessChanges(current []WorkspaceAccess, submitted []WorkspaceInput) bool {
	currentByWorkspace := make(map[uuid.UUID]string, len(current))
	for _, g := range current {
		currentByWorkspace[g.WorkspaceID] = g.Permission
	}
	submittedByWorkspace := make(map[uuid.UUID]string, len(submitted))
	for _, g := range submitted {
		submittedByWorkspace[g.WorkspaceID] = g.Permission
	}
	if len(submittedByWorkspace) != len(submitted) || len(currentByWorkspace) != len(submittedByWorkspace) {
		return true
	}
	for id, permission := range submittedByWorkspace {
		if stored, ok := currentByWorkspace[id]; !ok || stored != permission {
			return true
		}
	}
	return false
}
