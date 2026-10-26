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

// View is what the admin UI sees of a key. The hashes stay on the server; forma2 sent both hashes to the browser in its create and update responses.
type View struct {
	ID                   uuid.UUID          `json:"id"`
	Label                string             `json:"label"`
	CreatedAt            time.Time          `json:"createdAt"`
	OrganizationAccess   OrganizationAccess `json:"organizationAccess"`
	WorkspacePermissions []WorkspaceGrant   `json:"workspacePermissions"`
	// LastUsedAt is the last request the key authenticated, recorded at most every usageWriteInterval; nil when it was never used.
	LastUsedAt *time.Time `json:"lastUsedAt"`
	// CreatedBy is the user who created the key; nil when their account is gone, or the key was imported without one.
	CreatedBy *Creator `json:"createdBy"`
	// DisabledAt and DisabledReason are set together once the key was disabled (apikey/disable), and are nil while it works.
	DisabledAt     *time.Time `json:"disabledAt"`
	DisabledReason *string    `json:"disabledReason"`
	// KeyHint is the key's last HintLength characters, which the list shows after the prefix to tell keys apart; nil for a key created before hints were kept.
	KeyHint *string `json:"keyHint"`
	// ExpiresAt is when the key stops working, chosen at creation; nil for a key that works until it is deleted. An expired key stays listed, like a disabled one.
	ExpiresAt *time.Time `json:"expiresAt"`
}

// Creator is the user who created a key, as the organization's managers may see them.
type Creator struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// FormerMember is set when the creator no longer belongs to the key's organization.
	FormerMember bool `json:"formerMember"`
}

// Created is a new key with its plaintext, which is returned this once and never stored.
type Created struct {
	View
	Key string `json:"key"`
}
