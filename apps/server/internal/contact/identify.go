package contact

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// The SDK's identify (forma2's client user route) runs without a signed-in user: the workspace id in the SDK snippet is public, so these methods authorize nothing and must only ever be reached through the client API, which scopes every call to the workspace the route resolved.

// Identified is a contact as the SDK's identify finds it: its id and its values as text, by key.
type Identified struct {
	ID         uuid.UUID
	Attributes map[string]string
}

// FindOrCreateByUserID is forma2's findOrCreateContact: the workspace's contact whose userId attribute holds userID, created on the first identify. A read that misses takes a transaction-scoped advisory lock on the workspace and userId and reads again before it creates, so two concurrent first identifies of one user leave one contact. Like forma2's lock it is cooperative: the CSV import and the management API do not take it. A workspace without a userId attribute key cannot hold the contact and fails.
func (s *Service) FindOrCreateByUserID(ctx context.Context, workspaceID uuid.UUID, userID string) (*Identified, error) {
	id, err := s.repo.contactIDByUserID(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	if id == nil {
		err = s.repo.transaction(ctx, func(tx *Repository) error {
			if err := tx.db.WithContext(ctx).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "contact:"+workspaceID.String()+":"+userID).Error; err != nil {
				return fmt.Errorf("lock contact creation: %w", err)
			}
			if id, err = tx.contactIDByUserID(ctx, workspaceID, userID); err != nil || id != nil {
				return err
			}
			created, err := tx.createContactWithUserID(ctx, workspaceID, userID)
			id = &created
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	attrs, err := s.repo.attributes(ctx, *id)
	if err != nil {
		return nil, err
	}
	return &Identified{ID: *id, Attributes: textValues(attrs)}, nil
}

// IdentifyAttributes is forma2's updateAttributes as the SDK's identify calls it: merged over what is stored, nothing deleted. The rules are those of the contact editor's update (see updateAttributes); an email that is not an address is a 400 "Validation failed: Invalid email".
func (s *Service) IdentifyAttributes(ctx context.Context, workspaceID, contactID uuid.UUID, in []AttributeInput) (messages, errs []Message, err error) {
	keys, err := s.repo.keys(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	return s.updateAttributes(ctx, workspaceID, contactID, in, keys, false)
}

// contactIDByUserID is the workspace's contact whose userId attribute holds userID, the oldest such value when several contacts share it, nil when none does.
func (r *Repository) contactIDByUserID(ctx context.Context, workspaceID uuid.UUID, userID string) (*uuid.UUID, error) {
	return r.optionalID(ctx, "find contact by userId", `SELECT ca.contact_id FROM contact_attributes ca JOIN contact_attribute_keys k ON k.id = ca.attribute_key_id
		WHERE k.workspace_id = ? AND k.key = 'userId' AND ca.value_text = ? ORDER BY ca.created_at, ca.contact_id LIMIT 1`, workspaceID, userID)
}

func (r *Repository) createContactWithUserID(ctx context.Context, workspaceID uuid.UUID, userID string) (uuid.UUID, error) {
	keyID, err := r.optionalID(ctx, "load userId attribute key", `SELECT id FROM contact_attribute_keys WHERE workspace_id = ? AND key = 'userId'`, workspaceID)
	if err != nil {
		return uuid.Nil, err
	}
	if keyID == nil {
		return uuid.Nil, fmt.Errorf("workspace %s has no userId attribute key", workspaceID)
	}
	id, err := r.insertContact(ctx, workspaceID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := r.upsertAttributes(ctx, id, []keyedColumns{{keyID: *keyID, columns: textColumns(userID)}}); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// messageTemplates are forma2's MESSAGE_TEMPLATES: the English an SDK or API caller reads for each attribute message.
var messageTemplates = map[string]string{
	"email_or_userid_required":        "Either email or userId is required. The existing values were preserved.",
	"attribute_type_validation_error": "{error} (attribute '{key}' has dataType: {dataType})",
	"email_already_exists":            "The email already exists for this workspace and was not updated.",
	"userid_already_exists":           "The userId already exists for this workspace and was not updated.",
	"invalid_attribute_keys":          "Skipped creating attribute(s) with invalid key(s): {keys}. Keys must only contain lowercase letters, numbers, and underscores, and must start with a letter.",
	"reserved_attribute_keys":         "{issue}",
	"attribute_limit_exceeded":        "Could not create {count} new attribute(s) as it would exceed the maximum limit of {limit} attribute classes. Existing attributes were updated successfully.",
	"new_attribute_created":           "Created new attribute '{key}' with type '{dataType}'",
	"invalid_language_ignored":        "Ignored invalid language code '{language}'. The existing value was preserved.",
}

// Text is forma2's formatAttributeMessage: the message's English template with its parameters filled in, or the bare code when it has no template.
func (m Message) Text() string {
	text, ok := messageTemplates[m.Code]
	if !ok {
		text = m.Code
	}
	for k, v := range m.Params {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	return text
}
