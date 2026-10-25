package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// The profile page's sessions card: where the user is signed in, and the controls to end one of those sessions or every one but the current.

// listSessions returns the signed-in user's sessions that have not expired, the caller's own first.
func (s *Service) listSessions(ctx context.Context, actor httpapi.Actor) ([]userSession, error) {
	return s.sessions.listFor(ctx, actor.UserID, actor.SessionID)
}

// revokeSession ends one of the signed-in user's sessions, which may be the caller's own. A session of another user, or one that has already ended, is not found.
func (s *Service) revokeSession(ctx context.Context, actor httpapi.Actor, id uuid.UUID) error {
	found, err := s.sessions.deleteFor(ctx, actor.UserID, id)
	if err != nil {
		return err
	}
	if !found {
		return apperr.NotFound("Session", "")
	}
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionUserSignedOut, TargetType: audit.TargetUser, TargetID: actor.UserID.String(), ActorID: actor.UserID.String(), Status: audit.StatusSuccess,
		NewObject: map[string]any{"provider": "session", "authMethod": "revoke_session", "reason": "user_initiated", "sessionId": id.String(), "current": id == actor.SessionID},
	})
	return nil
}

// revokeOtherSessions ends every session of the signed-in user except the caller's own and returns how many it ended.
func (s *Service) revokeOtherSessions(ctx context.Context, actor httpapi.Actor) (int64, error) {
	revoked, err := s.sessions.RevokeAll(ctx, nil, actor.UserID, actor.SessionID)
	if err != nil {
		return 0, err
	}
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionUserSignedOut, TargetType: audit.TargetUser, TargetID: actor.UserID.String(), ActorID: actor.UserID.String(), Status: audit.StatusSuccess,
		NewObject: map[string]any{"provider": "session", "authMethod": "revoke_other_sessions", "reason": "user_initiated", "revoked": revoked},
	})
	return revoked, nil
}
