package linksurvey

import (
	"errors"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// The link survey tokens are jsonwebtoken HS256 JWTs. Links already sent carry them, so the claims, secrets and lifetimes below do not change.
const (
	// pinTokenPurpose and pinTokenTTL are forma2's PIN_TOKEN_PURPOSE and PIN_TOKEN_TTL_SECONDS (modules/survey/link/lib/pin-token.ts).
	pinTokenPurpose = "link_survey_pin"
	pinTokenTTL     = time.Hour
	// emailTokenPurpose and emailTokenTTL are forma2's LINK_SURVEY_EMAIL_VERIFICATION_PURPOSE and LINK_SURVEY_TOKEN_TTL (lib/jwt.ts).
	emailTokenPurpose = "link_survey_email_verification"
	emailTokenTTL     = 7 * 24 * time.Hour
)

var hs256 = []jose.SignatureAlgorithm{jose.HS256}

func sign(key string, claims any) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: []byte(key)}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}
	return jwt.Signed(signer).Claims(claims).Serialize()
}

// errTokenExpired is jsonwebtoken's TokenExpiredError: a token whose signature holds but whose exp has passed.
var errTokenExpired = errors.New("token expired")

// verify checks an HS256 token under key and reads its claims into out, plus its registered claims. Like jsonwebtoken it applies exp and nbf without leeway; a token without exp never expires.
func verify(key, token string, now time.Time, out any) error {
	parsed, err := jwt.ParseSigned(token, hs256)
	if err != nil {
		return err
	}
	var registered jwt.Claims
	if err := parsed.Claims([]byte(key), &registered, out); err != nil {
		return err
	}
	if err := registered.ValidateWithLeeway(jwt.Expected{Time: now}, 0); err != nil {
		if errors.Is(err, jwt.ErrExpired) {
			return errTokenExpired
		}
		return err
	}
	return nil
}

type pinClaims struct {
	SurveyID string `json:"surveyId"`
	Purpose  string `json:"purpose"`
	jwt.Claims
}

// NewPinToken is forma2's createLinkSurveyPinToken: proof, for one hour, that the respondent entered the survey's PIN. The response endpoints require it of a PIN-protected survey.
func NewPinToken(cfg config.Config, surveyID uuid.UUID, now time.Time) (string, error) {
	return sign(cfg.LinkSurveyPinSecret(), pinClaims{
		SurveyID: surveyID.String(),
		Purpose:  pinTokenPurpose,
		Claims:   jwt.Claims{IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(pinTokenTTL))},
	})
}

// VerifyPinToken is verifyLinkSurveyPinToken: a valid, unexpired PIN token issued for this survey.
func VerifyPinToken(cfg config.Config, token string, surveyID uuid.UUID, now time.Time) bool {
	if token == "" {
		return false
	}
	var c pinClaims
	if verify(cfg.LinkSurveyPinSecret(), token, now, &c) != nil {
		return false
	}
	return c.Purpose == pinTokenPurpose && rowid.Matches(c.SurveyID, surveyID)
}

type emailClaims struct {
	Email    string  `json:"email"`
	SurveyID *string `json:"surveyId,omitempty"`
	Purpose  *string `json:"purpose,omitempty"`
	jwt.Claims
}

// newEmailToken is createTokenForLinkSurvey: the verified address, encrypted as a link value under ENCRYPTION_KEY, bound to the survey for seven days.
func newEmailToken(cfg config.Config, surveyID uuid.UUID, email string, now time.Time) (string, error) {
	encrypted, err := secret.EncryptLinkValue(email, cfg.EncryptionKey)
	if err != nil {
		return "", err
	}
	id, purpose := surveyID.String(), emailTokenPurpose
	return sign(cfg.LinkSurveyEmailSecret(), emailClaims{
		Email: encrypted, SurveyID: &id, Purpose: &purpose,
		Claims: jwt.Claims{IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(emailTokenTTL))},
	})
}

// VerifyEmailToken is verifyTokenForLinkSurvey: the address a link survey email verification token proves, or "" when it proves none for this survey. The token must be signed with the email secret and name the survey, a purpose, when present, must be email verification, and the address must decrypt.
func VerifyEmailToken(cfg config.Config, token string, surveyID uuid.UUID, now time.Time) string {
	var c emailClaims
	if verify(cfg.LinkSurveyEmailSecret(), token, now, &c) != nil {
		return ""
	}
	if c.SurveyID == nil || !rowid.Matches(*c.SurveyID, surveyID) {
		return ""
	}
	if c.Purpose != nil && *c.Purpose != emailTokenPurpose {
		return ""
	}
	if c.Email == "" {
		return ""
	}
	email, err := secret.DecryptLinkValue(c.Email, cfg.EncryptionKey)
	if err != nil {
		return ""
	}
	return email
}

type contactClaims struct {
	ContactID string `json:"contactId"`
	SurveyID  string `json:"surveyId"`
}

// errContactTokenExpired and errContactTokenInvalid are forma2's two outcomes of a personal link that does not verify: "Survey link has expired" (token_expired) and "Invalid survey token" (invalid_token).
var (
	errContactTokenExpired = errors.New("survey link has expired")
	errContactTokenInvalid = errors.New("invalid survey token")
)

// verifyContactToken is verifyContactSurveyToken: a personal survey link signed with ENCRYPTION_KEY's text whose contact and survey ids are encrypted as link values under the same key. The ids are returned as they were issued.
func verifyContactToken(cfg config.Config, token string, now time.Time) (contactID, surveyID string, err error) {
	var c contactClaims
	if err := verify(string(cfg.EncryptionKeyText), token, now, &c); err != nil {
		if errors.Is(err, errTokenExpired) {
			return "", "", errContactTokenExpired
		}
		return "", "", errContactTokenInvalid
	}
	if c.ContactID == "" || c.SurveyID == "" {
		return "", "", errContactTokenInvalid
	}
	contactID, err1 := secret.DecryptLinkValue(c.ContactID, cfg.EncryptionKey)
	surveyID, err2 := secret.DecryptLinkValue(c.SurveyID, cfg.EncryptionKey)
	if err1 != nil || err2 != nil {
		return "", "", errContactTokenInvalid
	}
	return contactID, surveyID, nil
}

// contactTokenClaims is what a personal survey link carries, in jsonwebtoken's order.
type contactTokenClaims struct {
	ContactID string `json:"contactId"`
	SurveyID  string `json:"surveyId"`
	jwt.Claims
}

// NewContactToken is the getContactSurveyLink token: the contact and survey ids, each encrypted as a link value under ENCRYPTION_KEY, signed HS256 with ENCRYPTION_KEY's text; it expires after expiresIn, or never when that is zero. verifyContactToken reads it.
func NewContactToken(cfg config.Config, contactID, surveyID uuid.UUID, now time.Time, expiresIn time.Duration) (string, error) {
	encryptedContact, err := secret.EncryptLinkValue(contactID.String(), cfg.EncryptionKey)
	if err != nil {
		return "", err
	}
	encryptedSurvey, err := secret.EncryptLinkValue(surveyID.String(), cfg.EncryptionKey)
	if err != nil {
		return "", err
	}
	claims := contactTokenClaims{ContactID: encryptedContact, SurveyID: encryptedSurvey, Claims: jwt.Claims{IssuedAt: jwt.NewNumericDate(now)}}
	if expiresIn > 0 {
		claims.Expiry = jwt.NewNumericDate(now.Add(expiresIn))
	}
	return sign(string(cfg.EncryptionKeyText), claims)
}
