package auth

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// signupInput is a validated sign-up request.
type signupInput struct {
	Name           string
	Email          string
	Password       string
	InviteToken    string
	UserLocale     string
	TurnstileToken string
}

// signup creates an account with forma2's createUserAction rules and returns the sign-up intent token the browser keeps.
//
// The outcome never depends on whether the address was already registered: a duplicate is answered exactly like a new account, a decoy intent token included, and nothing is written, mailed or audited for it.
func (s *Service) signup(ctx context.Context, in signupInput) (issuedToken, error) {
	intent, err := s.createAccount(ctx, in)
	if err != nil {
		s.logFailure(ctx, audit.ActionCreated, "")
	}
	return intent, err
}

func (s *Service) createAccount(ctx context.Context, in signupInput) (issuedToken, error) {
	if err := s.limiter.ApplyIP(ctx, ratelimit.AuthSignup); err != nil {
		return issuedToken{}, err
	}
	if s.cfg.TurnstileConfigured() && !s.turnstile.verify(ctx, in.TurnstileToken) {
		return issuedToken{}, errCaptchaFailed()
	}
	// The form sends an empty string for "no invite"; anything blank means none.
	if strings.TrimSpace(in.InviteToken) != "" {
		// Invites are issued by the organization module, which has not been ported yet. Until it is, no invite can be verified, and forma2 refused an invite it could not verify before creating anything.
		return issuedToken{}, errInviteTokenInvalid()
	}
	allowed, err := s.uninvitedSignupAllowed(ctx)
	if err != nil {
		return issuedToken{}, err
	}
	if !allowed {
		return issuedToken{}, errSignupDisabled()
	}
	if s.cfg.IsFormaCloud && isBlockedEmailDomain(in.Email) {
		return issuedToken{}, errEmailDomainNotAllowed()
	}
	if s.cfg.EmailAuthDisabled {
		return issuedToken{}, errEmailPasswordDisabled()
	}
	// Checked before anything is written, so a rejected password leaves no account behind.
	if s.breaches.compromised(ctx, in.Password) {
		return issuedToken{}, errPasswordCompromised()
	}
	// Hashed on both paths: a duplicate costs the same bcrypt work as a new account.
	hash, err := secret.HashPassword(in.Password)
	if err != nil {
		return issuedToken{}, err
	}

	email := normalizeEmail(in.Email)
	locale := in.UserLocale
	if locale == "" {
		locale = defaultLocale
	}
	var userID, orgID uuid.UUID
	var intent issuedToken
	created := false
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		var err error
		userID, created, err = tx.insertUser(ctx, in.Name, email, locale)
		if err != nil || !created {
			return err
		}
		if err := tx.setPassword(ctx, userID, hash, s.now()); err != nil {
			return err
		}
		if orgID, err = tx.provisionOrganization(ctx, userID, in.Name); err != nil {
			return err
		}
		intent, err = s.issueVerification(ctx, tx, purposeSignupIntent, userID, email, signupIntentTTL)
		return err
	})
	if err != nil {
		return issuedToken{}, err
	}
	if !created {
		return s.decoyIntent()
	}

	// Better Auth sent this mail even with verification disabled, and a failed send never failed the sign-up: the account exists either way and the response must not vary with what happened to one address.
	s.sendVerificationEmail(ctx, &account{ID: userID, Email: email, Locale: locale})
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionCreated, TargetType: audit.TargetUser, TargetID: userID.String(), OrganizationID: orgID.String(), Status: audit.StatusSuccess,
		NewObject: map[string]any{"id": userID.String(), "name": in.Name, "email": email, "locale": locale},
	})
	return intent, nil
}

// defaultLocale is the users.locale default and forma2's DEFAULT_LOCALE.
const defaultLocale = "en-US"

// decoyIntent is an intent token that was never stored. A duplicate sign-up sets it like a real one, so the Set-Cookie header does not tell the caller the address is registered; verifying with it later classifies as invalid.
func (s *Service) decoyIntent() (issuedToken, error) {
	token, err := secret.NewToken()
	if err != nil {
		return issuedToken{}, err
	}
	return issuedToken{Value: token, ExpiresAt: s.now().Add(signupIntentTTL), MaxAge: signupIntentTTL}, nil
}

// uninvitedSignupAllowed is forma2's isUninvitedSignupAllowed: public sign-up is open (multiple organizations are always enabled), or the instance has no user yet and this is its first administrator.
func (s *Service) uninvitedSignupAllowed(ctx context.Context) (bool, error) {
	if s.cfg.SignupEnabled() {
		return true, nil
	}
	hasUser, err := s.repo.hasAnyUser(ctx)
	return !hasUser, err
}
