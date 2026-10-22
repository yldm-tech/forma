package contact

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/userinput"
)

// List is forma2's getContactsAction: a page of the workspace's contacts, newest first, optionally searched. A search of only whitespace is no search.
func (s *Service) List(ctx context.Context, userID, workspaceID uuid.UUID, offset int, search string) ([]Contact, error) {
	if err := s.read(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.listContacts(ctx, workspaceID, offset, jsTrim(search))
}

// Detail is what the contact page shows: the contact and its values with their keys.
type Detail struct {
	Contact    *Contact          `json:"contact"`
	Attributes []AttributeDetail `json:"attributes"`
}

// Get is forma2's contact page loader (getContactAuth): a reader of the workspace sees a contact of that workspace; any other contact, existing or not, is not found.
func (s *Service) Get(ctx context.Context, userID, workspaceID, contactID uuid.UUID) (*Detail, error) {
	if err := s.read(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	c, err := s.repo.contact(ctx, contactID, &workspaceID)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, apperr.NotFound("Contact", "")
	}
	attrs, err := s.repo.attributeDetails(ctx, contactID, workspaceID)
	if err != nil {
		return nil, err
	}
	return &Detail{Contact: c, Attributes: attrs}, nil
}

// contactForWrite resolves a contact to its workspace and authorizes a change there.
func (s *Service) contactForWrite(ctx context.Context, userID, contactID uuid.UUID, ev *audit.Event) (uuid.UUID, error) {
	ws, err := s.repo.contactWorkspace(ctx, contactID)
	if err != nil {
		return uuid.Nil, err
	}
	if ws == nil {
		return uuid.Nil, apperr.NotFound("contact", contactID.String())
	}
	org, err := s.organizationOf(ctx, *ws)
	if err != nil {
		return uuid.Nil, err
	}
	ev.OrganizationID = org.String()
	if err := s.write(ctx, userID, *ws); err != nil {
		return uuid.Nil, err
	}
	return *ws, nil
}

// Delete is forma2's deleteContactAction: the contact and everything hanging off it.
func (s *Service) Delete(ctx context.Context, userID, contactID uuid.UUID) (old *Contact, err error) {
	ev := audit.Event{Action: audit.ActionDeleted, TargetType: audit.TargetContact, TargetID: contactID.String()}
	defer func() { s.record(ctx, ev, err) }()

	if _, err := s.contactForWrite(ctx, userID, contactID, &ev); err != nil {
		return nil, err
	}
	old, err = s.repo.contact(ctx, contactID, nil)
	if err != nil {
		return nil, err
	}
	deleted, err := s.repo.deleteContact(ctx, contactID)
	if err != nil {
		return nil, err
	}
	if !deleted || old == nil {
		return nil, apperr.NotFound("contact", contactID.String())
	}
	ev.OldObject = old
	return old, nil
}

// AttributeInput is one submitted value, in submission order.
type AttributeInput struct {
	Key   string
	Value Value
}

// Message is forma2's TAttributeUpdateMessage: a code and parameters the client translates.
type Message struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params"`
}

// UpdateResult is forma2's UpdateContactAttributesResult, plus the refusals of new keys forma2 computed and dropped.
type UpdateResult struct {
	UpdatedAttributes    map[string]string `json:"updatedAttributes"`
	Messages             []Message         `json:"messages,omitempty"`
	Errors               []Message         `json:"errors,omitempty"`
	UpdatedAttributeKeys []Key             `json:"updatedAttributeKeys,omitempty"`
}

// protectedKeys are the default keys a full-form update never deletes when they are missing from it.
var protectedKeys = []string{keyEmail, keyUserID, keyFirstName, keyLastName}

// UpdateAttributes is forma2's updateContactAttributesAction: the contact editor submits every value, and keys missing from it are deleted. The rules are forma2's updateAttributes with deleteRemovedAttributes.
func (s *Service) UpdateAttributes(ctx context.Context, userID, contactID uuid.UUID, in []AttributeInput) (res *UpdateResult, err error) {
	ev := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetContact, TargetID: contactID.String()}
	defer func() { s.record(ctx, ev, err) }()

	workspaceID, err := s.contactForWrite(ctx, userID, contactID, &ev)
	if err != nil {
		return nil, err
	}
	before, err := s.repo.keys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	messages, errs, err := s.updateAttributes(ctx, workspaceID, contactID, in, before)
	if err != nil {
		return nil, err
	}
	current, err := s.repo.attributes(ctx, contactID)
	if err != nil {
		return nil, err
	}
	res = &UpdateResult{UpdatedAttributes: textValues(current), Messages: messages, Errors: errs}
	after, err := s.repo.keys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, k := range after {
		if !slices.ContainsFunc(before, func(b Key) bool { return b.Key == k.Key }) {
			res.UpdatedAttributeKeys = append(res.UpdatedAttributeKeys, k)
		}
	}
	ev.NewObject = map[string]any{"contactId": contactID, "attributes": res.UpdatedAttributes}
	return res, nil
}

func textValues(attrs map[string]attributeRow) map[string]string {
	out := make(map[string]string, len(attrs))
	for k, a := range attrs {
		out[k] = a.columns().String()
	}
	return out
}

// updateAttributes is forma2's updateAttributes (apps/web/modules/contacts/lib/attributes.ts) with deleteRemovedAttributes set.
//
// email and userId stay unique within the workspace: a value another contact holds is not written. A contact keeps at least one of them: when the update would leave both empty, the current values are kept. Values for existing keys must match the key's type strictly; a mismatch skips that value only. New keys must be safe, unreserved identifiers, are typed from their value, and are created only while the workspace stays within MaxAttributeKeys.
func (s *Service) updateAttributes(ctx context.Context, workspaceID, contactID uuid.UUID, in []AttributeInput, keys []Key) (messages, errs []Message, err error) {
	submitted := map[string]Value{}
	for _, a := range in {
		submitted[a.Key] = a.Value
	}
	current, err := s.repo.attributes(ctx, contactID)
	if err != nil {
		return nil, nil, err
	}
	currentText := textValues(current)

	emailValue, emailSubmitted := submitted[keyEmail]
	userIDValue, userIDSubmitted := submitted[keyUserID]
	emailExists, userIDExists := false, false
	if emailSubmitted && emailValue.Text() != "" {
		if !userinput.ValidEmailFormat(emailValue.Text()) || userinput.UTF16Len(emailValue.Text()) > userinput.EmailMaxLength {
			return nil, nil, apperr.InvalidInput(apperr.CodeValidation, "Validation failed: Invalid email")
		}
		if emailExists, err = s.repo.valueOnOtherContact(ctx, workspaceID, keyEmail, emailValue.Text(), contactID); err != nil {
			return nil, nil, err
		}
	}
	if userIDSubmitted && userIDValue.Text() != "" {
		if userIDExists, err = s.repo.valueOnOtherContact(ctx, workspaceID, keyUserID, userIDValue.Text(), contactID); err != nil {
			return nil, nil, err
		}
	}

	// The values to write, in submission order.
	writes := append([]AttributeInput(nil), in...)
	set := func(key string, v Value) {
		for i := range writes {
			if writes[i].Key == key {
				writes[i].Value = v
				return
			}
		}
		writes = append(writes, AttributeInput{Key: key, Value: v})
	}
	remove := func(key string) {
		writes = slices.DeleteFunc(writes, func(a AttributeInput) bool { return a.Key == key })
	}

	final := func(submitted bool, v Value, exists bool, current string) string {
		if !submitted || exists {
			return current
		}
		return jsTrim(v.Text())
	}
	if final(emailSubmitted, emailValue, emailExists, currentText[keyEmail]) == "" && final(userIDSubmitted, userIDValue, userIDExists, currentText[keyUserID]) == "" {
		if c := currentText[keyEmail]; c != "" {
			set(keyEmail, Value{String: c})
		}
		if c := currentText[keyUserID]; c != "" {
			set(keyUserID, Value{String: c})
		}
		messages = append(messages, Message{Code: "email_or_userid_required", Params: map[string]string{}})
	}
	if emailExists {
		remove(keyEmail)
	}
	if userIDExists {
		remove(keyUserID)
	}

	keyByName := make(map[string]Key, len(keys))
	for _, k := range keys {
		keyByName[k.Key] = k
	}
	var removed []uuid.UUID
	for name, a := range current {
		if _, ok := submitted[name]; !ok && !slices.Contains(protectedKeys, name) {
			removed = append(removed, a.KeyID)
		}
	}

	var existing []keyedColumns
	var fresh []AttributeInput
	for _, a := range writes {
		k, ok := keyByName[a.Key]
		if !ok {
			fresh = append(fresh, a)
			continue
		}
		cols, typeErr := validateValue(a.Value, k.DataType, a.Key)
		if typeErr != nil {
			messages = append(messages, Message{Code: "attribute_type_validation_error", Params: map[string]string{"key": a.Key, "dataType": k.DataType, "error": typeErr.Message}})
			continue
		}
		existing = append(existing, keyedColumns{keyID: k.ID, columns: cols})
	}
	if emailExists {
		messages = append(messages, Message{Code: "email_already_exists", Params: map[string]string{}})
	}
	if userIDExists {
		messages = append(messages, Message{Code: "userid_already_exists", Params: map[string]string{}})
	}

	var invalid, reserved []string
	var valid []AttributeInput
	for _, a := range fresh {
		switch {
		case !IsSafeIdentifier(a.Key):
			invalid = append(invalid, a.Key)
		case IsReservedKey(a.Key):
			reserved = append(reserved, a.Key)
		default:
			valid = append(valid, a)
		}
	}
	if len(invalid) > 0 {
		errs = append(errs, Message{Code: "invalid_attribute_keys", Params: map[string]string{"keys": strings.Join(invalid, ", ")}})
	}
	if len(reserved) > 0 {
		errs = append(errs, Message{Code: "reserved_attribute_keys", Params: map[string]string{"issue": reservedKeyIssue(reserved)}})
	}
	var created []newKey
	createdValues := map[string]Columns{}
	if len(valid) > 0 {
		if len(keys)+len(valid) > MaxAttributeKeys {
			messages = append(messages, Message{Code: "attribute_limit_exceeded", Params: map[string]string{"count": itoa(len(valid)), "limit": itoa(MaxAttributeKeys)}})
		} else {
			for _, a := range valid {
				dataType := detectSDKType(a.Value)
				created = append(created, newKey{key: a.Key, dataType: dataType})
				createdValues[a.Key] = prepareColumns(a.Value, dataType)
				messages = append(messages, Message{Code: "new_attribute_created", Params: map[string]string{"key": a.Key, "dataType": dataType}})
			}
		}
	}

	err = s.repo.retrying(ctx, func(tx *Repository) error {
		if err := tx.deleteAttributes(ctx, contactID, removed); err != nil {
			return err
		}
		if err := tx.upsertAttributes(ctx, contactID, existing); err != nil {
			return err
		}
		if len(created) > 0 {
			// Another writer may create the same key concurrently: creation skips existing keys and the values land on whichever row won.
			if err := tx.createKeys(ctx, workspaceID, created); err != nil {
				return err
			}
			ws, err := tx.keys(ctx, workspaceID)
			if err != nil {
				return err
			}
			var values []keyedColumns
			for _, k := range ws {
				if cols, ok := createdValues[k.Key]; ok {
					values = append(values, keyedColumns{keyID: k.ID, columns: cols})
				}
			}
			if err := tx.upsertAttributes(ctx, contactID, values); err != nil {
				return err
			}
		}
		return tx.touchContact(ctx, contactID)
	})
	if err != nil {
		return nil, nil, err
	}
	return messages, errs, nil
}

func itoa(n int) string {
	return jsNumberString(float64(n))
}
