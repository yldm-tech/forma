package contact

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AttributeError is a value SetAttribute refused, with the reason a workflow run shows.
type AttributeError struct{ Message string }

func (e *AttributeError) Error() string { return e.Message }

// SetAttribute writes one value under an existing attribute key of the workspace onto one of its contacts, a workflow's set_contact_attribute step. The key must exist and must not be unique: email and userId identify a contact, and a workflow writing them could merge two people into one. The value must read as the key's type, strictly as for the contact editor, except that a number key takes the number's text, which is all a template produces. It creates no key and writes nothing for a contact of another workspace.
func SetAttribute(ctx context.Context, db *gorm.DB, workspaceID, contactID uuid.UUID, key, value string) error {
	r := NewRepository(db)
	k, err := SettableAttributeKey(ctx, db, workspaceID, key)
	if err != nil {
		return err
	}
	owner, err := r.optionalID(ctx, "load contact workspace", `SELECT workspace_id FROM contacts WHERE id = ?`, contactID)
	if err != nil {
		return err
	}
	if owner == nil || *owner != workspaceID {
		return &AttributeError{Message: "The response's contact no longer exists in this workspace"}
	}
	v := Value{String: value}
	if k.DataType == TypeNumber {
		if f, ok := jsNumber(value); ok && !math.IsNaN(f) && !math.IsInf(f, 0) {
			v = Value{Number: f, IsNumber: true}
		}
	}
	cols, typeErr := validateValue(v, k.DataType, key)
	if typeErr != nil {
		return &AttributeError{Message: typeErr.Message}
	}
	return r.retrying(ctx, func(tx *Repository) error {
		if err := tx.upsertAttributes(ctx, contactID, []keyedColumns{{keyID: k.ID, columns: cols}}); err != nil {
			return err
		}
		return tx.touchContact(ctx, contactID)
	})
}

// SettableAttributeKey is the workspace's attribute key a workflow may write under, SetAttribute's check of the key alone, which enabling a workflow runs ahead of any response: an *AttributeError when the key does not exist or is unique.
func SettableAttributeKey(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID, key string) (*Key, error) {
	keys, err := NewRepository(db).keys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].Key != key {
			continue
		}
		if keys[i].IsUnique {
			return nil, &AttributeError{Message: fmt.Sprintf("Attribute %q is unique to each contact and cannot be set by a workflow", key)}
		}
		return &keys[i], nil
	}
	return nil, &AttributeError{Message: fmt.Sprintf("Attribute %q does not exist in this workspace", key)}
}
