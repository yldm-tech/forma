package contact

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The writes of forma2's /api/v2/management/contacts (apps/web/modules/contacts/api/v2/management/contacts). The management API module validates the input and authorizes the API key before it calls these; they authorize nothing.

// Failures of a managed contact write the API reports in its own format.
var (
	// ErrEmailTaken is an email another contact of the workspace already has.
	ErrEmailTaken = errors.New("contact with this email already exists")
	// ErrUserIDTaken is a userId another contact of the workspace already has.
	ErrUserIDTaken = errors.New("contact with this userId already exists")
)

// MissingKeysError lists the attribute keys a create named that the workspace does not have.
type MissingKeysError struct {
	Keys []string
}

func (e *MissingKeysError) Error() string {
	return "attribute keys not found: " + strings.Join(e.Keys, ", ")
}

// BulkInvalidError is a bulk upload refused before anything was written, with forma2's messages.
type BulkInvalidError struct {
	Issues []string
}

func (e *BulkInvalidError) Error() string { return strings.Join(e.Issues, "; ") }

// Created is a contact a create made, as forma2's v2 API answered it: its attribute values as text, by key.
type Created struct {
	ID          uuid.UUID         `json:"id"`
	CreatedAt   time.Time         `json:"createdAt"`
	WorkspaceID uuid.UUID         `json:"workspaceId"`
	Attributes  map[string]string `json:"attributes"`
}

// CreateManaged is forma2's v2 createContact: a contact whose email (and userId, when given) no other contact of the workspace has, with values only under keys the workspace already has; each value is typed by its key.
func (s *Service) CreateManaged(ctx context.Context, workspaceID uuid.UUID, attributes map[string]string) (*Created, error) {
	if taken, err := s.repo.valueOnOtherContact(ctx, workspaceID, keyEmail, attributes[keyEmail], uuid.Nil); err != nil || taken {
		if taken {
			err = ErrEmailTaken
		}
		return nil, err
	}
	if userID := attributes[keyUserID]; userID != "" {
		if taken, err := s.repo.valueOnOtherContact(ctx, workspaceID, keyUserID, userID, uuid.Nil); err != nil || taken {
			if taken {
				err = ErrUserIDTaken
			}
			return nil, err
		}
	}
	keys, err := s.repo.keys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	byKey := map[string]Key{}
	for _, k := range keys {
		byKey[k.Key] = k
	}
	var missing []string
	var values []keyedColumns
	for _, key := range sortedKeys(attributes) {
		k, ok := byKey[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		values = append(values, keyedColumns{keyID: k.ID, columns: prepareCSVColumns(attributes[key], k.DataType)})
	}
	if len(missing) > 0 {
		return nil, &MissingKeysError{Keys: missing}
	}
	var out *Created
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		id, err := tx.insertContact(ctx, workspaceID)
		if err != nil {
			return err
		}
		if err := tx.upsertAttributes(ctx, id, values); err != nil {
			return err
		}
		c, err := tx.contact(ctx, id, &workspaceID)
		if err != nil {
			return err
		}
		out = &Created{ID: c.ID, CreatedAt: c.CreatedAt, WorkspaceID: workspaceID, Attributes: c.Attributes}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// BulkAttribute is one value of a bulk-uploaded contact, under a key named (and, for a new key, titled) by the caller.
type BulkAttribute struct {
	Key   string
	Name  string
	Value string
}

// BulkContact is one bulk-uploaded contact, which the API has checked carries a valid email.
type BulkContact struct {
	Attributes []BulkAttribute
}

func (c BulkContact) value(key string) (string, bool) {
	for _, a := range c.Attributes {
		if a.Key == key {
			return a.Value, true
		}
	}
	return "", false
}

// BulkUpsert is forma2's upsertBulkContacts: contacts are matched to the workspace's by email, keys the upload introduces are created (refused when not safe identifiers or reserved), typed by their first value and titled as named, and a renamed existing key takes the upload's name; values are typed by their key, and a value an existing typed key cannot read refuses the upload. A contact whose new userId another contact holds is skipped; their indexes are returned. Everything is written in one transaction.
func (s *Service) BulkUpsert(ctx context.Context, workspaceID uuid.UUID, contacts []BulkContact) ([]int, error) {
	var keyOrder, emails, userIDs []string
	seen := map[string]bool{}
	valuesByKey := map[string][]string{}
	names := map[string]string{}
	for _, c := range contacts {
		for _, a := range c.Attributes {
			if !seen[a.Key] {
				seen[a.Key] = true
				keyOrder = append(keyOrder, a.Key)
			}
			if jsTrim(a.Value) != "" {
				valuesByKey[a.Key] = append(valuesByKey[a.Key], a.Value)
			}
			switch a.Key {
			case keyEmail:
				emails = append(emails, a.Value)
			case keyUserID:
				userIDs = append(userIDs, a.Value)
			}
		}
	}
	existingKeys, err := s.repo.keys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	existing := map[string]Key{}
	for _, k := range existingKeys {
		existing[k.Key] = k
	}
	var unsafe, reserved []string
	for _, key := range keyOrder {
		if _, ok := existing[key]; ok {
			continue
		}
		if !IsSafeIdentifier(key) {
			unsafe = append(unsafe, key)
		} else if IsReservedKey(key) {
			reserved = append(reserved, key)
		}
	}
	if len(unsafe) > 0 {
		return nil, &BulkInvalidError{Issues: []string{"Invalid attribute key(s): " + strings.Join(unsafe, ", ") + ". Keys must only contain lowercase letters, numbers, and underscores, and must start with a letter."}}
	}
	if len(reserved) > 0 {
		return nil, &BulkInvalidError{Issues: []string{ReservedKeysIssue(reserved)}}
	}
	types := map[string]string{}
	var typeErrors []string
	for _, key := range keyOrder {
		if k, ok := existing[key]; ok {
			types[key] = k.DataType
		} else if values := valuesByKey[key]; len(values) > 0 {
			types[key] = detectCSVType(values[0])
		} else {
			types[key] = TypeString
		}
		if types[key] == TypeString {
			continue
		}
		var invalid []string
		for _, v := range valuesByKey[key] {
			if !prepareCSVColumns(v, types[key]).parsedAs(types[key]) {
				invalid = append(invalid, v)
			}
		}
		if len(invalid) == 0 {
			continue
		}
		if _, ok := existing[key]; !ok {
			types[key] = TypeString
			continue
		}
		sample := strings.Join(invalid[:min(3, len(invalid))], ", ")
		if more := len(invalid) - 3; more > 0 {
			sample += " (and " + strconv.Itoa(more) + " more)"
		}
		typeErrors = append(typeErrors, fmt.Sprintf(`Attribute "%s" is typed as "%s" but received invalid values: %s`, key, types[key], sample))
	}
	if len(typeErrors) > 0 {
		return nil, &BulkInvalidError{Issues: typeErrors}
	}

	byEmail, err := s.repo.contactsByEmail(ctx, workspaceID, emails)
	if err != nil {
		return nil, err
	}
	owners, err := s.repo.userIDOwners(ctx, workspaceID, userIDs)
	if err != nil {
		return nil, err
	}
	stored := map[uuid.UUID]map[string]attributeRow{}
	for _, c := range byEmail {
		attrs, err := s.repo.attributes(ctx, c.id)
		if err != nil {
			return nil, err
		}
		stored[c.id] = attrs
	}

	type write struct {
		contactID  *uuid.UUID
		attributes []BulkAttribute
	}
	var writes []write
	var skipped []int
	for i, c := range contacts {
		email, _ := c.value(keyEmail)
		match, found := byEmail[email]
		attrs := c.Attributes
		if found {
			var changed []BulkAttribute
			for _, a := range c.Attributes {
				if row, ok := stored[match.id][a.Key]; !ok || row.columns().String() != a.Value {
					changed = append(changed, a)
				}
			}
			if len(changed) == 0 {
				for _, a := range c.Attributes {
					names[a.Key] = a.Name
				}
				continue
			}
			attrs = changed
		}
		if userID, ok := (BulkContact{Attributes: attrs}).value(keyUserID); ok && len(owners[userID]) > 0 {
			skipped = append(skipped, i)
			continue
		}
		for _, a := range c.Attributes {
			names[a.Key] = a.Name
		}
		w := write{attributes: attrs}
		if found {
			id := match.id
			w.contactID = &id
		}
		writes = append(writes, w)
	}

	err = s.repo.transaction(ctx, func(tx *Repository) error {
		keyIDs := map[string]uuid.UUID{}
		for key, k := range existing {
			keyIDs[key] = k.ID
		}
		for _, key := range keyOrder {
			name, used := names[key]
			if !used {
				continue
			}
			k, ok := existing[key]
			if ok && k.Name != nil && *k.Name == name {
				continue
			}
			var id uuid.UUID
			if err := tx.db.WithContext(ctx).Raw(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, data_type) VALUES (?, ?, ?, 'custom', ?)
				ON CONFLICT (workspace_id, key) DO UPDATE SET name = EXCLUDED.name, updated_at = now() RETURNING id`, workspaceID, key, name, types[key]).Row().Scan(&id); err != nil {
				return fmt.Errorf("upsert contact attribute key: %w", err)
			}
			keyIDs[key] = id
		}
		for _, w := range writes {
			var contactID uuid.UUID
			if w.contactID != nil {
				contactID = *w.contactID
			} else {
				id, err := tx.insertContact(ctx, workspaceID)
				if err != nil {
					return err
				}
				contactID = id
			}
			values := make([]keyedColumns, 0, len(w.attributes))
			for _, a := range w.attributes {
				values = append(values, keyedColumns{keyID: keyIDs[a.Key], columns: prepareCSVColumns(a.Value, types[a.Key])})
			}
			if err := tx.upsertAttributes(ctx, contactID, values); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("upsert contacts: %w", err)
	}
	if skipped == nil {
		skipped = []int{}
	}
	return skipped, nil
}
