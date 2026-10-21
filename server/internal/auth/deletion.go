package auth

import (
	"context"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// accountDeletionTTLMinutes is what the confirmation mail says about accountDeletionTTL.
const accountDeletionTTLMinutes = 60

// deleteAccount deletes the signed-in password user's account after checking their password. forma2 required the password on every deletion request, closing Better Auth's "recent session is enough" path.
func (s *Service) deleteAccount(ctx context.Context, actor httpapi.Actor, password string) (string, error) {
	if err := s.limiter.ApplyIP(ctx, ratelimit.BADeleteUser); err != nil {
		return "", err
	}
	acc, err := s.repo.accountByID(ctx, actor.UserID)
	if err != nil {
		return "", err
	}
	if acc == nil {
		return "", apperr.NotAuthenticated("Not authenticated")
	}
	if !acc.hasPassword() {
		return "", errPasswordNotSet()
	}
	ok, err := secret.CheckPassword(acc.PasswordHash.String, password)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errInvalidPassword()
	}
	if err := s.repo.transaction(ctx, func(tx *Repository) error {
		return s.removeAccount(ctx, tx, acc)
	}); err != nil {
		return "", err
	}
	s.logDeletion(ctx, acc)
	return s.afterDeletionURL(), nil
}

// requestAccountDeletion mails a confirmation link to a user who has no password to confirm with (an SSO user).
func (s *Service) requestAccountDeletion(ctx context.Context, actor httpapi.Actor) error {
	if err := s.limiter.Apply(ctx, ratelimit.ActionAccountDeletion, actor.UserID.String()); err != nil {
		return err
	}
	acc, err := s.repo.accountByID(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if acc == nil {
		return apperr.NotAuthenticated("Not authenticated")
	}
	if acc.emailIdentity() {
		return apperr.Forbidden("Password confirmation is required to delete this account.")
	}
	token, err := s.issueVerification(ctx, s.repo, purposeAccountDeletion, acc.ID, normalizeEmail(acc.Email), accountDeletionTTL)
	if err != nil {
		return err
	}
	if _, err := s.mailer.SendTemplate(ctx, acc.Email, acc.Locale, mailer.DeleteAccountConfirmation{
		DeleteLink:            s.link(deleteAccountPath, token.Value),
		LinkValidityInMinutes: accountDeletionTTLMinutes,
	}); err != nil {
		return errEmailSendFailed()
	}
	return nil
}

// confirmAccountDeletion redeems an emailed deletion link. It must be redeemed from a session of the same user, so a forwarded or intercepted link alone deletes nothing.
func (s *Service) confirmAccountDeletion(ctx context.Context, actor httpapi.Actor, token string) (string, error) {
	if err := s.limiter.ApplyIP(ctx, ratelimit.BADeleteUserCallback); err != nil {
		return "", err
	}
	var acc *account
	err := s.repo.transaction(ctx, func(tx *Repository) error {
		v, err := tx.consumeUserVerification(ctx, purposeAccountDeletion, secret.HashToken(token), actor.UserID, s.now())
		if err != nil {
			return err
		}
		if v == nil {
			return errInvalidToken()
		}
		if acc, err = tx.lockAccount(ctx, actor.UserID); err != nil {
			return err
		}
		if acc == nil {
			return errInvalidToken()
		}
		return s.removeAccount(ctx, tx, acc)
	})
	if err != nil {
		return "", err
	}
	s.logDeletion(ctx, acc)
	return s.afterDeletionURL(), nil
}

// removeAccount deletes the user together with every organization they are the only owner of, as forma2 did with multiple organizations enabled (always, since its licence checks were removed). SpiceDB relationships follow through the projection outbox triggers on the deleted rows.
func (s *Service) removeAccount(ctx context.Context, tx *Repository, acc *account) error {
	soleOwned, err := tx.soleOwnedOrganizations(ctx, acc.ID)
	if err != nil {
		return err
	}
	return tx.deleteAccount(ctx, acc.ID, soleOwned)
}

func (s *Service) logDeletion(ctx context.Context, acc *account) {
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionDeleted, TargetType: audit.TargetUser, TargetID: acc.ID.String(), ActorID: acc.ID.String(), Status: audit.StatusSuccess,
		OldObject: map[string]any{"id": acc.ID.String(), "name": acc.Name, "email": acc.Email, "locale": acc.Locale, "emailVerified": acc.EmailVerified},
	})
}

// afterDeletionURL is where the SPA goes once the account is gone: Forma Cloud asks why in a survey.
func (s *Service) afterDeletionURL() string {
	if s.cfg.IsFormaCloud {
		return cloudAccountDeletionSurveyURL
	}
	return loginPath
}
