package auth

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/platform/turnstile"
)

// signupInput is a validated sign-up request.
type signupInput struct {
	Name           string
	Email          string
	Password       string
	InviteToken    string
	UserLocale     string
	TurnstileToken string
	// TimeZone is the browser's IANA zone, already validated (nil when the browser sent none or one this build cannot load).
	TimeZone *string
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
	if s.cfg.TurnstileConfigured() {
		// Fails closed: an account is not created while Cloudflare cannot vouch for the token.
		ok, err := s.turnstile.Verify(ctx, in.TurnstileToken, httpapi.ClientIP(ctx), turnstile.ActionSignUp)
		if err != nil {
			s.log.WarnContext(ctx, "Turnstile verification unavailable; refusing sign-up", "err", err)
		}
		if !ok {
			return issuedToken{}, errCaptchaFailed()
		}
	}
	// The form sends an empty string for "no invite"; anything blank means none.
	inviteToken := strings.TrimSpace(in.InviteToken)
	invited := false
	if inviteToken != "" {
		ok, err := s.invites.MatchForSignup(ctx, inviteToken, in.Email)
		if err != nil {
			return issuedToken{}, err
		}
		// Refused before anything is written, so an unusable invite cannot leave an account behind.
		if !ok {
			s.log.WarnContext(ctx, "Rejected sign-up with an unusable invite token")
			return issuedToken{}, errInviteTokenInvalid()
		}
		invited = true
	}
	// A valid invite is a grant of its own: it admits its holder whatever the instance's sign-up policy says.
	if !invited {
		allowed, err := s.uninvitedSignupAllowed(ctx)
		if err != nil {
			return issuedToken{}, err
		}
		if !allowed {
			return issuedToken{}, errSignupDisabled()
		}
	}
	// Invitees are exempt from Forma Cloud's personal domain refusal unless SIGNUP_DOMAIN_CHECK_ON_INVITES says otherwise.
	if s.cfg.IsFormaCloud && (!invited || s.cfg.SignupDomainCheckOnInvites) && isBlockedEmailDomain(in.Email) {
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
	var notifyInviter func(context.Context)
	var provisioned authz.Organizations
	created := false
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		// On a closed instance only the first user gets in uninvited. The check above ran before the slow password hash, so concurrent first sign-ups are serialized here and the check repeated under the lock: exactly one of them becomes the administrator.
		if !invited && !s.cfg.SignupEnabled() {
			allowed, err := tx.firstUserLocked(ctx)
			if err != nil {
				return err
			}
			if !allowed {
				return errSignupDisabled()
			}
		}
		var err error
		userID, created, err = tx.insertUser(ctx, in.Name, email, locale)
		if err != nil || !created {
			return err
		}
		if err := tx.setPassword(ctx, userID, hash, s.now()); err != nil {
			return err
		}
		// An invitee joins the inviting organization instead of getting one of their own. The invite is re-checked where it is granted; if it was revoked or used since, the account goes too.
		if invited {
			var ok bool
			orgID, notifyInviter, ok, err = s.invites.RedeemForSignup(ctx, tx.db, inviteToken, userID, in.Name, email)
			if err == nil && !ok {
				err = errInviteTokenInvalid()
			}
		} else if orgID, err = tx.provisionOrganization(ctx, userID, in.Name, in.TimeZone); err == nil {
			provisioned, err = authz.ReadOrganizations(ctx, tx.db, orgID)
		}
		if err != nil {
			return err
		}
		intent, err = s.issueVerification(ctx, tx, purposeSignupIntent, userID, email, signupIntentTTL)
		return err
	})
	if err != nil {
		return issuedToken{}, err
	}
	// An address that already had an account gets the same answer and nothing else: its invite is not consumed.
	if !created {
		return s.decoyIntent()
	}
	if !invited {
		// The new owner and their first workspace reach SpiceDB before the answer instead of waiting for the worker; an invitee's membership is the invite module's to deliver, when it tells the inviter.
		s.delivery.DeliverOrganizations(ctx, provisioned, userID)
		s.billing.OrganizationCreated(ctx, orgID)
	}
	if notifyInviter != nil {
		notifyInviter(ctx)
	}

	// Better Auth sent this mail even with verification disabled, and a failed send never failed the sign-up: the account exists either way and the response must not vary with what happened to one address.
	s.sendVerificationEmail(ctx, &account{ID: userID, Email: email, Locale: locale})
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionCreated, TargetType: audit.TargetUser, TargetID: userID.String(), OrganizationID: orgID.String(), Status: audit.StatusSuccess,
		NewObject: map[string]any{"id": userID.String(), "name": in.Name, "email": email, "locale": locale},
	})
	s.captureCredentialSignUp(ctx, userID, email, orgID, invited)
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
