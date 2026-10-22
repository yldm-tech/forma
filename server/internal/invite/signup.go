package invite

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MatchForSignup reports whether token is a live link to an invite issued to email, compared trimmed and without regard to case: forma2's resolveInviteMatch, which sign-up asks before it creates anything. The credential module calls it; an unusable invite is refused there with invite_token_invalid.
func (s *Service) MatchForSignup(ctx context.Context, token, email string) (bool, error) {
	inv, status, err := s.lookup(ctx, s.repo, token, false)
	if err != nil || status != StatusValid {
		return false, err
	}
	return sameEmail(inv.Email, email), nil
}

// RedeemForSignup grants the invite to the account a sign-up just created, inside the sign-up's transaction, so a sign-up whose invite was revoked or used in the meantime leaves no account behind (ok is false and nothing was written). notify sends the inviter's mail and must be called after the commit.
func (s *Service) RedeemForSignup(ctx context.Context, tx *gorm.DB, token string, userID uuid.UUID, name, email string) (organizationID uuid.UUID, notify func(context.Context), ok bool, err error) {
	repo := &Repository{db: tx}
	inv, status, err := s.lookup(ctx, repo, token, true)
	if err != nil || status != StatusValid || !sameEmail(inv.Email, email) {
		return uuid.Nil, nil, false, err
	}
	a, err := s.grant(ctx, repo, inv, userID)
	if err != nil {
		return uuid.Nil, nil, false, err
	}
	return a.organizationID, func(ctx context.Context) { s.accepted(ctx, a, userID, name) }, true, nil
}
