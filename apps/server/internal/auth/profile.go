package auth

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// profileChange is a validated edit of the signed-in user's own name and language; a nil field is left as it is.
type profileChange struct {
	Name   *string
	Locale *string
}

// updateProfile is the name and language half of forma2's updateUserAction (the address half is requestEmailChange). An identity provider owns an SSO user's name, so a different name is refused for anyone but a password user; echoing the current name back is not a change and passes.
func (s *Service) updateProfile(ctx context.Context, actor httpapi.Actor, in profileChange) (*account, error) {
	acc, err := s.changeProfile(ctx, actor, in)
	if err != nil {
		s.logFailure(ctx, audit.ActionUpdated, actor.UserID.String())
	}
	return acc, err
}

func (s *Service) changeProfile(ctx context.Context, actor httpapi.Actor, in profileChange) (*account, error) {
	acc, err := s.currentAccount(ctx, actor)
	if err != nil {
		return nil, err
	}
	name, locale := acc.Name, acc.Locale
	if in.Name != nil && *in.Name != acc.Name {
		if !acc.emailIdentity() {
			return nil, apperr.OperationNotAllowed("Name update is not allowed for non-credential users.")
		}
		name = *in.Name
	}
	if in.Locale != nil {
		locale = *in.Locale
	}
	if name == acc.Name && locale == acc.Locale {
		return acc, nil
	}
	if err := s.repo.updateProfile(ctx, acc.ID, name, locale); err != nil {
		return nil, err
	}
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionUpdated, TargetType: audit.TargetUser, TargetID: acc.ID.String(), Status: audit.StatusSuccess,
		OldObject: map[string]any{"name": acc.Name, "locale": acc.Locale}, NewObject: map[string]any{"name": name, "locale": locale},
	})
	acc.Name, acc.Locale = name, locale
	return acc, nil
}

func (r *Repository) updateProfile(ctx context.Context, userID uuid.UUID, name, locale string) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE users SET name = ?, locale = ?, updated_at = now() WHERE id = ?`, name, locale, userID,
	).Error; err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	return nil
}
