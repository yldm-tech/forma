// Package disable turns off the API keys a user created in an organization once that user may no longer create keys there: they were removed from it, left it, deleted their account, or were demoted below manager (owner decision D-AO02 of 2026-10-25). Only owners and managers hold organization.manage_api_keys, so a key whose creator lost that role would otherwise keep, for good, access its creator no longer has.
//
// A disabled key stays listed with its grants and the reason, so an owner can see what stopped working, but it never authenticates again (apikey.Authenticator). It cannot be enabled again: the person who left is the one who may hold its plaintext, so the organization replaces it with a new key. The functions run on the caller's transaction, so the keys go off together with the membership change that causes it, and each caller records Events once that transaction has committed.
//
// This is its own package, not part of apikey, because the auth module (account deletion) calls it, and apikey's tests import auth.
package disable

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
)

// Reasons a key was disabled (api_keys.disabled_reason).
const (
	ReasonCreatorRemoved = "creator_removed"
	ReasonCreatorLeft    = "creator_left"
	ReasonCreatorDeleted = "creator_deleted"
	ReasonCreatorDemoted = "creator_demoted"
)

// Key is one key CreatedBy disabled.
type Key struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Label          string
}

// CreatedBy disables the enabled keys userID created in organizationID, or in every organization when organizationID is nil, and returns them ordered by id. db is the transaction of the change that causes it; when that transaction retries, the call runs again and its result replaces the earlier one.
func CreatedBy(ctx context.Context, db *gorm.DB, userID uuid.UUID, organizationID *uuid.UUID, reason string) ([]Key, error) {
	query := `UPDATE api_keys SET disabled_at = now(), disabled_reason = ? WHERE created_by = ? AND disabled_at IS NULL`
	args := []any{reason, userID}
	if organizationID != nil {
		query += ` AND organization_id = ?`
		args = append(args, *organizationID)
	}
	keys := []Key{}
	if err := db.WithContext(ctx).Raw(query+` RETURNING id, organization_id, label`, args...).Scan(&keys).Error; err != nil {
		return nil, fmt.Errorf("disable api keys: %w", err)
	}
	slices.SortFunc(keys, func(a, b Key) int { return strings.Compare(a.ID.String(), b.ID.String()) })
	return keys, nil
}

// ManagesKeys is whether an organization role may create API keys (organization.manage_api_keys): owners and managers. A user whose role in an organization drops below it has their keys there disabled.
func ManagesKeys(role string) bool { return role == "owner" || role == "manager" }

// Events are the audit events of keys disabled for reason, one per key: the action "disabled" on the key, in the key's organization, with the label and the reason as the key's new state (the creator is the target of the membership or user event of the same request; the redaction masks a createdBy field). The caller sets the actor (the request's user or API key) and logs them.
func Events(keys []Key, reason string) []audit.Event {
	events := make([]audit.Event, len(keys))
	for i, k := range keys {
		events[i] = audit.Event{
			Action: audit.ActionDisabled, TargetType: audit.TargetAPIKey, TargetID: k.ID.String(), OrganizationID: k.OrganizationID.String(), Status: audit.StatusSuccess,
			NewObject: map[string]string{"label": k.Label, "disabledReason": reason},
		}
	}
	return events
}
