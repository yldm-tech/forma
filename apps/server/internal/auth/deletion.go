package auth

import (
	"context"
	"slices"

	"github.com/yldm-tech/forma/apps/server/internal/apikey/disable"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// accountDeletionTTLMinutes is what the confirmation mail says about accountDeletionTTL.
const accountDeletionTTLMinutes = 60

// deleteAccount deletes the signed-in password user's account after checking their password. forma2 required the password on every deletion request, closing Better Auth's "recent session is enough" path.
func (s *Service) deleteAccount(ctx context.Context, actor httpapi.Actor, password string) (string, error) {
	if err := s.limiter.ApplyIP(ctx, ratelimit.BADeleteUser); err != nil {
		return "", err
	}
	acc, err := s.currentAccount(ctx, actor)
	if err != nil {
		return "", err
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
	var removed removedAccount
	if err := s.serializableJobTransaction(ctx, func(tx *Repository, jtx *jobs.Tx) error {
		var err error
		removed, err = s.removeAccount(ctx, tx, jtx, acc)
		return err
	}); err != nil {
		return "", err
	}
	s.accountDeleted(ctx, acc, removed)
	return s.afterDeletionURL(), nil
}

// requestAccountDeletion mails a confirmation link to a user who has no password to confirm with (an SSO user).
func (s *Service) requestAccountDeletion(ctx context.Context, actor httpapi.Actor) error {
	if err := s.limiter.Apply(ctx, ratelimit.ActionAccountDeletion, actor.UserID.String()); err != nil {
		return err
	}
	acc, err := s.currentAccount(ctx, actor)
	if err != nil {
		return err
	}
	if acc.emailIdentity() {
		return apperr.Forbidden("Password confirmation is required to delete this account.")
	}
	token, err := s.issueVerification(ctx, s.repo, purposeAccountDeletion, acc.ID, normalizeEmail(acc.Email), accountDeletionTTL)
	if err != nil {
		return err
	}
	// A mailer without SMTP sends nothing; that is a failed send too, so the page never claims a link went out.
	sent, err := s.mailer.SendTemplate(ctx, acc.Email, acc.Locale, mailer.DeleteAccountConfirmation{
		DeleteLink:            s.link(deleteAccountPath, token.Value),
		LinkValidityInMinutes: accountDeletionTTLMinutes,
	})
	if err != nil || !sent {
		return errEmailSendFailed()
	}
	return nil
}

// deletionPreview lists the organizations that would be deleted with the caller's account: those removeAccount takes, read through the same query. It names only the caller's own organizations.
func (s *Service) deletionPreview(ctx context.Context, actor httpapi.Actor) ([]soleOwnedOrganization, error) {
	acc, err := s.currentAccount(ctx, actor)
	if err != nil {
		return nil, err
	}
	return s.repo.soleOwnedOrganizationSummaries(ctx, acc.ID)
}

// confirmAccountDeletion redeems an emailed deletion link. It must be redeemed from a session of the same user, so a forwarded or intercepted link alone deletes nothing.
func (s *Service) confirmAccountDeletion(ctx context.Context, actor httpapi.Actor, token string) (string, error) {
	if err := s.limiter.ApplyIP(ctx, ratelimit.BADeleteUserCallback); err != nil {
		return "", err
	}
	var acc *account
	var removed removedAccount
	err := s.serializableJobTransaction(ctx, func(tx *Repository, jtx *jobs.Tx) error {
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
		removed, err = s.removeAccount(ctx, tx, jtx, acc)
		return err
	})
	if err != nil {
		return "", err
	}
	s.accountDeleted(ctx, acc, removed)
	return s.afterDeletionURL(), nil
}

// removedAccount is what removeAccount leaves to do once its transaction has committed.
type removedAccount struct {
	// stripeCustomers are the Stripe customers of the organizations deleted with the account.
	stripeCustomers []string
	// disabledKeys are the API keys the user had created in the organizations that remain.
	disabledKeys []disable.Key
}

// removeAccount deletes the user together with every organization they are the only active owner of, as forma2 did with multiple organizations enabled (always, since its licence checks were removed), and disables the API keys they created in every other organization: created_by is set to null with the user, so they are disabled first, while the creator can still be matched. It must run in a Serializable transaction (Service.serializableJobTransaction): the organization module's owner guards are serializable, and only a serializable deletion conflicts with them, so a co-owner's concurrent demotion, removal or own deletion aborts one side instead of leaving the organization without an owner. SpiceDB relationships follow through the projection outbox triggers on the deleted rows.
func (s *Service) removeAccount(ctx context.Context, tx *Repository, jtx *jobs.Tx, acc *account) (removedAccount, error) {
	soleOwned, err := tx.soleOwnedOrganizations(ctx, acc.ID)
	if err != nil {
		return removedAccount{}, err
	}
	customers, err := tx.stripeCustomers(ctx, soleOwned)
	if err != nil {
		return removedAccount{}, err
	}
	keys, err := disable.CreatedBy(ctx, tx.db, acc.ID, nil, disable.ReasonCreatorDeleted)
	if err != nil {
		return removedAccount{}, err
	}
	// Keys of the organizations deleted with the account go with them; only those that stay listed are reported.
	keys = slices.DeleteFunc(keys, func(k disable.Key) bool { return slices.Contains(soleOwned, k.OrganizationID) })
	if err := tx.deleteAccount(ctx, acc.ID, soleOwned); err != nil {
		return removedAccount{}, err
	}
	// forma2 deleted the Brevo contact, by address, with the user.
	return removedAccount{stripeCustomers: customers, disabledKeys: keys}, s.contacts.Deleted(ctx, jtx, acc.Email)
}

// accountDeleted records the deletion and the API keys it disabled and, as forma2's deleteOrganization did for each organization that went with the account, cancels their Stripe subscriptions.
func (s *Service) accountDeleted(ctx context.Context, acc *account, removed removedAccount) {
	for _, customer := range removed.stripeCustomers {
		s.billing.OrganizationDeleted(ctx, &customer)
	}
	s.logDeletion(ctx, acc)
	for _, e := range disable.Events(removed.disabledKeys, disable.ReasonCreatorDeleted) {
		e.ActorID = acc.ID.String()
		s.logEvent(ctx, e)
	}
	analytics.Capture(acc.ID.String(), "delete_account", nil, analytics.Groups{})
}

func (s *Service) logDeletion(ctx context.Context, acc *account) {
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionDeleted, TargetType: audit.TargetUser, TargetID: acc.ID.String(), ActorID: acc.ID.String(), Status: audit.StatusSuccess,
		OldObject: map[string]any{"id": acc.ID.String(), "name": acc.Name, "email": acc.Email, "locale": acc.Locale, "emailVerified": acc.EmailVerified},
	})
}

// afterDeletionURL is where the SPA goes once the account is gone, with a full page load: Forma Cloud asks why in a survey, anywhere else it is the sign-in page under the app's base path.
func (s *Service) afterDeletionURL() string {
	if s.cfg.IsFormaCloud {
		return cloudAccountDeletionSurveyURL
	}
	return config.AppBasePath + loginPath
}
