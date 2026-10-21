package auth

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// forgotPassword mails a reset link when the address belongs to an account that can reset its password, and answers the same whether or not it does. The audit trail records a reset only when a link was actually sent.
func (s *Service) forgotPassword(ctx context.Context, email string) error {
	if err := s.requestReset(ctx, email); err != nil {
		s.logFailure(ctx, audit.ActionPasswordReset, "", err)
		return err
	}
	return nil
}

func (s *Service) requestReset(ctx context.Context, email string) error {
	// forma2 charged the limit before looking at PASSWORD_RESET_DISABLED here, and the other way round when redeeming.
	if err := s.limiter.ApplyIP(ctx, ratelimit.AuthForgotPassword); err != nil {
		return err
	}
	if s.cfg.PasswordResetDisabled {
		return errPasswordResetDisabled()
	}
	acc, err := s.repo.accountByEmail(ctx, email)
	if err != nil {
		return err
	}
	if acc == nil || !s.canResetPassword(acc) {
		return nil
	}
	if err := s.sendResetLink(ctx, acc); err != nil {
		s.log.ErrorContext(ctx, "Password reset request failed", "err", err, "userId", acc.ID.String())
		return nil
	}
	s.logEvent(ctx, audit.Event{Action: audit.ActionPasswordReset, TargetType: audit.TargetUser, TargetID: acc.ID.String(), Status: audit.StatusSuccess})
	return nil
}

// canResetPassword is forma2's rule: a password user may always reset; an SSO user who still has a password (one left over from before moving to SSO) only while email and password sign-in is enabled, since on an SSO-only instance the password could never be used.
func (s *Service) canResetPassword(acc *account) bool {
	if !acc.hasPassword() {
		return false
	}
	return acc.emailIdentity() || !s.cfg.EmailAuthDisabled
}

// sendResetLink issues a reset token and mails it. A mailer without SMTP counts as a failed send.
func (s *Service) sendResetLink(ctx context.Context, acc *account) error {
	lifetime := time.Duration(s.cfg.PasswordResetTokenLifetimeMinutes) * time.Minute
	token, err := s.issueVerification(ctx, s.repo, purposePasswordReset, acc.ID, normalizeEmail(acc.Email), lifetime)
	if err != nil {
		return err
	}
	sent, err := s.mailer.SendTemplate(ctx, acc.Email, acc.Locale, mailer.PasswordReset{
		VerifyLink:            s.link(resetPasswordPath, token.Value),
		LinkValidityInMinutes: s.cfg.PasswordResetTokenLifetimeMinutes,
	})
	if err != nil {
		return err
	}
	if !sent {
		return errMailNotSent
	}
	return nil
}

// resetPassword redeems a reset token. A breached password is refused before the token is touched, so the user can retry with the same link. Success signs every session out and invalidates outstanding email change links and pending second-factor sign-ins, which were authorized by the old password.
func (s *Service) resetPassword(ctx context.Context, token, password string) error {
	if s.cfg.PasswordResetDisabled {
		return errPasswordResetDisabled()
	}
	if err := s.limiter.ApplyIP(ctx, ratelimit.AuthResetPassword); err != nil {
		return err
	}
	if s.breaches.compromised(ctx, password) {
		return errPasswordCompromised()
	}
	hash, err := secret.HashPassword(password)
	if err != nil {
		return err
	}
	var acc *account
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		v, err := tx.consumeVerification(ctx, purposePasswordReset, secret.HashToken(token), s.now())
		if err != nil {
			return err
		}
		if v == nil || !v.UserID.Valid {
			return errInvalidPasswordResetToken()
		}
		if acc, err = tx.lockAccount(ctx, v.UserID.UUID); err != nil {
			return err
		}
		if acc == nil {
			return errInvalidPasswordResetToken()
		}
		return s.replacePassword(ctx, tx, acc.ID, hash, uuid.Nil)
	})
	if err != nil {
		return err
	}
	s.notifyPasswordChanged(ctx, acc)
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionUpdated, TargetType: audit.TargetUser, TargetID: acc.ID.String(), ActorID: acc.ID.String(), Status: audit.StatusSuccess,
		NewObject: map[string]bool{"passwordResetMarker": true},
	})
	return nil
}

// replacePassword stores a new hash and ends everything the old password authorized: every session but keepSession, outstanding email change links and pending second-factor sign-ins.
func (s *Service) replacePassword(ctx context.Context, tx *Repository, userID uuid.UUID, hash string, keepSession uuid.UUID) error {
	if err := tx.setPassword(ctx, userID, hash, s.now()); err != nil {
		return err
	}
	if _, err := s.sessions.RevokeAll(ctx, tx.db, userID, keepSession); err != nil {
		return err
	}
	return tx.deleteVerifications(ctx, userID, purposeEmailChange, purposeTwoFactor)
}

// notifyPasswordChanged sends the security notice. It is best-effort: the password has already changed.
func (s *Service) notifyPasswordChanged(ctx context.Context, acc *account) {
	sent, err := s.mailer.SendTemplate(ctx, acc.Email, acc.Locale, mailer.PasswordChanged{})
	if err == nil && !sent {
		err = errMailNotSent
	}
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to send password-reset notification email", "err", err, "userId", acc.ID.String())
	}
}

// changePassword replaces the signed-in user's password after checking the current one. It applies the sign-up password policy and breach check, keeps the current session and signs every other one out.
func (s *Service) changePassword(ctx context.Context, actor httpapi.Actor, current, next string) error {
	if err := s.limiter.ApplyIP(ctx, ratelimit.BAChangePassword); err != nil {
		return err
	}
	acc, err := s.repo.accountByID(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if acc == nil {
		return apperr.NotAuthenticated("Not authenticated")
	}
	if !acc.hasPassword() {
		return errPasswordNotSet()
	}
	ok, err := secret.CheckPassword(acc.PasswordHash.String, current)
	if err != nil {
		return err
	}
	if !ok {
		return errInvalidPassword()
	}
	if s.breaches.compromised(ctx, next) {
		return errPasswordCompromised()
	}
	hash, err := secret.HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.repo.transaction(ctx, func(tx *Repository) error {
		return s.replacePassword(ctx, tx, acc.ID, hash, actor.SessionID)
	}); err != nil {
		return err
	}
	s.notifyPasswordChanged(ctx, acc)
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionUpdated, TargetType: audit.TargetUser, TargetID: acc.ID.String(), Status: audit.StatusSuccess,
		NewObject: map[string]bool{"passwordChangedMarker": true},
	})
	return nil
}

// requestResetLink mails the signed-in user a reset link, forma2's in-app "reset password" on the profile page. It is offered to password users only.
func (s *Service) requestResetLink(ctx context.Context, actor httpapi.Actor) error {
	err := s.sendOwnResetLink(ctx, actor)
	if err != nil {
		s.logFailure(ctx, audit.ActionPasswordReset, actor.UserID.String(), err)
		return err
	}
	s.logEvent(ctx, audit.Event{Action: audit.ActionPasswordReset, TargetType: audit.TargetUser, TargetID: actor.UserID.String(), Status: audit.StatusSuccess})
	return nil
}

func (s *Service) sendOwnResetLink(ctx context.Context, actor httpapi.Actor) error {
	if s.cfg.PasswordResetDisabled {
		return errPasswordResetDisabled()
	}
	acc, err := s.repo.accountByID(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if acc == nil {
		return apperr.NotAuthenticated("Not authenticated")
	}
	if !acc.emailIdentity() {
		return apperr.OperationNotAllowed("Password reset is not allowed for this user.")
	}
	if err := s.sendResetLink(ctx, acc); err != nil {
		s.log.ErrorContext(ctx, "Password reset request failed", "err", err, "userId", acc.ID.String())
		return errEmailSendFailed()
	}
	return nil
}
