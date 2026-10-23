package contact

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
)

// Stable codes of the attribute key failures.
const (
	CodeKeyUnsafe   = "attribute_key_unsafe"
	CodeKeyReserved = "attribute_key_reserved"
	CodeKeyExists   = "attribute_key_exists"
)

// NewKeyInput is forma2's ZCreateContactAttributeKeyAction.
type NewKeyInput struct {
	Key         string
	Name        *string
	Description *string
	DataType    *string
}

// KeyUpdate is forma2's ZUpdateContactAttributeKeyAction; the key and its data type cannot change.
type KeyUpdate struct {
	Name        *string
	Description *string
}

// Keys is forma2's getContactAttributeKeys for a reader of the workspace.
func (s *Service) Keys(ctx context.Context, userID, workspaceID uuid.UUID) ([]Key, error) {
	if err := s.read(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.keys(ctx, workspaceID)
}

// CreateKey is forma2's createContactAttributeKeyAction: a custom key that is a safe identifier and not reserved, named after its key unless a name is given, of type string unless another is given.
func (s *Service) CreateKey(ctx context.Context, userID, workspaceID uuid.UUID, in NewKeyInput) (k *Key, err error) {
	ev := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetContactAttributeKey}
	defer func() { s.record(ctx, ev, err) }()

	if !IsSafeIdentifier(in.Key) {
		return nil, apperr.InvalidInput(CodeKeyUnsafe, "Key must be a safe identifier: only lowercase letters, numbers, and underscores, and must start with a letter")
	}
	if IsReservedKey(in.Key) {
		return nil, apperr.InvalidInput(CodeKeyReserved, ReservedKeyValidationMessage)
	}
	org, err := s.organizationOf(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	ev.OrganizationID = org.String()
	if err := s.write(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	name := TitleCase(in.Key)
	if in.Name != nil {
		name = *in.Name
	}
	dataType := TypeString
	if in.DataType != nil {
		dataType = *in.DataType
	}
	k, err = s.repo.insertKey(ctx, workspaceID, in.Key, name, in.Description, dataType)
	if errors.Is(err, errKeyExists) {
		return nil, apperr.InvalidInput(CodeKeyExists, "Attribute key already exists")
	}
	if err != nil {
		return nil, err
	}
	ev.TargetID, ev.NewObject = k.ID.String(), k
	analytics.Capture(userID.String(), "contact_attribute_key_created", analytics.Properties{
		"organization_id": org.String(),
		"workspace_id":    workspaceID.String(),
		"key":             in.Key,
	}, analytics.Groups{OrganizationID: org.String(), WorkspaceID: workspaceID.String()})
	return k, nil
}

// keyForWrite loads a key, authorizes a change in its workspace and refuses default keys.
func (s *Service) keyForWrite(ctx context.Context, userID, keyID uuid.UUID, ev *audit.Event, refusal string) (*Key, error) {
	k, err := s.repo.keyByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if k == nil {
		return nil, apperr.NotFound("contactAttributeKey", keyID.String())
	}
	org, err := s.organizationOf(ctx, k.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ev.OrganizationID = org.String()
	if err := s.write(ctx, userID, k.WorkspaceID); err != nil {
		return nil, err
	}
	ev.OldObject = k
	if k.Type == KeyTypeDefault {
		return nil, apperr.OperationNotAllowed(refusal)
	}
	return k, nil
}

// UpdateKey is forma2's updateContactAttributeKeyAction: a custom key's name and description.
func (s *Service) UpdateKey(ctx context.Context, userID, keyID uuid.UUID, in KeyUpdate) (k *Key, err error) {
	ev := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetContactAttributeKey, TargetID: keyID.String()}
	defer func() { s.record(ctx, ev, err) }()

	if _, err := s.keyForWrite(ctx, userID, keyID, &ev, "Cannot update default contact attribute key"); err != nil {
		return nil, err
	}
	k, err = s.repo.updateKey(ctx, keyID, in.Name, in.Description)
	if err != nil {
		return nil, err
	}
	if k == nil {
		return nil, apperr.NotFound("contactAttributeKey", keyID.String())
	}
	ev.NewObject = k
	return k, nil
}

// DeleteKey is forma2's deleteContactAttributeKeyAction: a custom key and every value stored under it. Like forma2 it does not look for segments that filter on the key; those filters then match no value.
func (s *Service) DeleteKey(ctx context.Context, userID, keyID uuid.UUID) (k *Key, err error) {
	ev := audit.Event{Action: audit.ActionDeleted, TargetType: audit.TargetContactAttributeKey, TargetID: keyID.String()}
	defer func() { s.record(ctx, ev, err) }()

	k, err = s.keyForWrite(ctx, userID, keyID, &ev, "Cannot delete default contact attribute key")
	if err != nil {
		return nil, err
	}
	deleted, err := s.repo.deleteKey(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, apperr.NotFound("contactAttributeKey", keyID.String())
	}
	return k, nil
}

// DistinctValues is forma2's getDistinctAttributeValuesAction, the suggestions of the segment editor.
func (s *Service) DistinctValues(ctx context.Context, userID, keyID uuid.UUID) ([]string, error) {
	k, err := s.repo.keyByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if k == nil {
		return nil, apperr.NotFound("contactAttributeKey", keyID.String())
	}
	if err := s.read(ctx, userID, k.WorkspaceID); err != nil {
		return nil, err
	}
	return s.repo.distinctValues(ctx, keyID)
}
