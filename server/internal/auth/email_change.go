package auth

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// requestEmailChange moves a password user to a new address, forma2's handleEmailUpdate. The current password is required. With verification enabled the change waits for a link sent to the new address; without it, it applies at once. An address another account holds is silently ignored, so the form cannot be used to probe for registered addresses.
func (s *Service) requestEmailChange(ctx context.Context, actor httpapi.Actor, email, password string) error {
	err := s.changeEmail(ctx, actor, email, password)
	if err != nil {
		s.logFailure(ctx, audit.ActionUpdated, actor.UserID.String(), err)
	}
	return err
}

func (s *Service) changeEmail(ctx context.Context, actor httpapi.Actor, email, password string) error {
	acc, err := s.repo.accountByID(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if acc == nil {
		return apperr.NotAuthenticated("Not authenticated")
	}
	newEmail := normalizeEmail(strings.TrimSpace(email))
	if newEmail == normalizeEmail(acc.Email) {
		return nil
	}
	// Charged only for an actual change, as in forma2.
	if err := s.limiter.Apply(ctx, ratelimit.ActionEmailUpdate, acc.ID.String()); err != nil {
		return err
	}
	if !acc.emailIdentity() {
		return apperr.OperationNotAllowed("Email update is not allowed for non-credential users.")
	}
	if password == "" {
		return apperr.InvalidInput(CodePasswordRequired, "Password is required to update email.")
	}
	ok, err := secret.CheckPassword(acc.PasswordHash.String, password)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.Forbidden("Incorrect credentials")
	}
	taken, err := s.repo.emailTaken(ctx, newEmail)
	if err != nil || taken {
		return err
	}

	if s.cfg.EmailVerificationDisabled {
		return s.applyEmailChange(ctx, acc.ID, newEmail, false)
	}
	token, err := s.issueVerification(ctx, s.repo, purposeEmailChange, acc.ID, newEmail, emailChangeTTL)
	if err != nil {
		return err
	}
	// Only the new address is written to; forma2 did not notify the old one.
	if _, err := s.mailer.SendTemplate(ctx, newEmail, acc.Locale, mailer.NewEmailVerification{VerifyLink: s.link(verifyEmailChangePath, token.Value)}); err != nil {
		return errEmailSendFailed()
	}
	s.logEvent(ctx, audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetUser, TargetID: acc.ID.String(), Status: audit.StatusSuccess})
	return nil
}

// confirmEmailChange redeems an email change link. It needs no session: the link is usually opened from the new mailbox, in another browser. The link is single-use and dies with any password reset or change and any other email change.
func (s *Service) confirmEmailChange(ctx context.Context, token string) (string, error) {
	email, err := s.redeemEmailChange(ctx, token)
	if err != nil {
		s.logFailure(ctx, audit.ActionUpdated, "", err)
	}
	return email, err
}

func (s *Service) redeemEmailChange(ctx context.Context, token string) (string, error) {
	if err := s.limiter.ApplyIP(ctx, ratelimit.AuthVerifyEmail); err != nil {
		return "", err
	}
	var userID uuid.UUID
	var oldEmail, newEmail string
	err := s.repo.transaction(ctx, func(tx *Repository) error {
		v, err := tx.consumeVerification(ctx, purposeEmailChange, secret.HashToken(token), s.now())
		if err != nil {
			return err
		}
		if v == nil || !v.UserID.Valid {
			return errInvalidToken()
		}
		acc, err := tx.lockAccount(ctx, v.UserID.UUID)
		if err != nil {
			return err
		}
		if acc == nil {
			return errInvalidToken()
		}
		userID, oldEmail, newEmail = acc.ID, acc.Email, v.Email
		return s.moveEmail(ctx, tx, acc.ID, v.Email, true)
	})
	if err != nil {
		return "", err
	}
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionUpdated, TargetType: audit.TargetUser, TargetID: userID.String(), Status: audit.StatusSuccess,
		OldObject: map[string]any{"email": oldEmail}, NewObject: map[string]any{"email": newEmail, "emailVerified": true},
	})
	return newEmail, nil
}

// applyEmailChange changes the address immediately, for instances with email verification disabled.
func (s *Service) applyEmailChange(ctx context.Context, userID uuid.UUID, email string, verified bool) error {
	var oldEmail string
	err := s.repo.transaction(ctx, func(tx *Repository) error {
		acc, err := tx.lockAccount(ctx, userID)
		if err != nil {
			return err
		}
		if acc == nil {
			return apperr.NotAuthenticated("Not authenticated")
		}
		oldEmail = acc.Email
		return s.moveEmail(ctx, tx, userID, email, verified)
	})
	if err != nil {
		return err
	}
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionUpdated, TargetType: audit.TargetUser, TargetID: userID.String(), Status: audit.StatusSuccess,
		OldObject: map[string]any{"email": oldEmail}, NewObject: map[string]any{"email": email},
	})
	return nil
}

// moveEmail writes the new address and revokes the links that were sent to the old one or bound to it: outstanding email change and password reset links.
func (s *Service) moveEmail(ctx context.Context, tx *Repository, userID uuid.UUID, email string, verified bool) error {
	taken, err := tx.updateEmail(ctx, userID, email, verified, s.now())
	if err != nil {
		return err
	}
	if taken {
		return apperr.Conflict("This email address is already in use.")
	}
	return tx.deleteVerifications(ctx, userID, purposeEmailChange, purposePasswordReset)
}
