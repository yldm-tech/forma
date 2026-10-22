package apikey

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Permissions a key can hold on a workspace (the api_key_workspaces.permission check constraint), forma2's ApiKeyPermission. In SpiceDB manage includes write and write includes read.
const (
	PermissionRead   = "read"
	PermissionWrite  = "write"
	PermissionManage = "manage"
)

// OrganizationAccess is forma2's ZOrganizationAccess: organization-level flags beside the per-workspace grants. Write implies read in SpiceDB (organization#api_key_writer is included in read_access).
type OrganizationAccess struct {
	AccessControl AccessFlags `json:"accessControl"`
}

// AccessFlags is one organization access group.
type AccessFlags struct {
	Read  bool `json:"read"`
	Write bool `json:"write"`
}

// grantsAnything reports whether the access grants anything.
func (a OrganizationAccess) grantsAnything() bool {
	return a.AccessControl.Read || a.AccessControl.Write
}

// storedAccess reads the organization_access column the way the SpiceDB projection does: a flag is granted only when it is exactly true, so a missing or malformed value denies.
func storedAccess(raw []byte) OrganizationAccess {
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return OrganizationAccess{}
	}
	group, _ := doc["accessControl"].(map[string]any)
	return OrganizationAccess{AccessControl: AccessFlags{Read: group["read"] == true, Write: group["write"] == true}}
}

// WorkspaceGrant is one workspace a key may act on, and how.
type WorkspaceGrant struct {
	WorkspaceID   uuid.UUID `json:"workspaceId"`
	WorkspaceName string    `json:"workspaceName"`
	Permission    string    `json:"permission"`
}

// View is what the admin UI sees of a key. The hashes, the creator and the last use stay on the server; forma2 sent both hashes to the browser in its create and update responses.
type View struct {
	ID                   uuid.UUID          `json:"id"`
	Label                string             `json:"label"`
	CreatedAt            time.Time          `json:"createdAt"`
	OrganizationAccess   OrganizationAccess `json:"organizationAccess"`
	WorkspacePermissions []WorkspaceGrant   `json:"workspacePermissions"`
}

// Created is a new key with its plaintext, which is returned this once and never stored.
type Created struct {
	View
	Key string `json:"key"`
}
