package notification

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/url"
	"strings"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
)

// The kinds of switch an unsubscribe link may name: a survey's per-response alert, whether an organization's new surveys alert the user, and an organization's weekly summary.
const (
	KindAlert         = "alert"
	KindAutoSubscribe = "unsubscribedOrganizationIds"
	KindWeeklySummary = auth.NotificationWeeklySummary
)

// unsubscribeTypes are the switches an unsubscribe link may name, signed or not.
var unsubscribeTypes = map[string]bool{KindAlert: true, KindAutoSubscribe: true, KindWeeklySummary: true}

// tokenVersion is the v claim of the tokens issued now; a token with any other version does not verify.
const tokenVersion = 1

// signingKeyLabel derives the unsubscribe tokens' signing key from ENCRYPTION_KEY, as config.derivedTokenSecret derives the link survey one under its own label, so no other token verifies as an unsubscribe token and no new configuration is needed. Rotating ENCRYPTION_KEY invalidates the links already sent.
const signingKeyLabel = "forma.unsubscribe.v1"

// UnsubscribeClaims is what an unsubscribe token signs: the user whose switch it is, the kind of switch, and the survey or organization the switch is for. The token neither expires nor can be revoked on its own, as unsubscribe links commonly are: whoever holds the email can turn the switch off, and back on.
type UnsubscribeClaims struct {
	V         int    `json:"v"`
	UserID    string `json:"userId"`
	Type      string `json:"type"`
	ElementID string `json:"elementId"`
}

// Unsubscribe is a verified unsubscribe token.
type Unsubscribe struct {
	UserID    uuid.UUID
	Type      string
	ElementID uuid.UUID
}

// errInvalidToken is any token that does not verify: malformed, signed with another key or algorithm, of another version, naming an unknown switch or an id that is not a UUID.
var errInvalidToken = errors.New("invalid unsubscribe token")

var hs256 = []jose.SignatureAlgorithm{jose.HS256}

func tokenKey(cfg config.Config) []byte {
	mac := hmac.New(sha256.New, cfg.EncryptionKey)
	mac.Write([]byte(signingKeyLabel))
	return mac.Sum(nil)
}

// NewUnsubscribeToken signs an HS256 JWT naming the user's switch of kind for the survey or organization elementID.
func NewUnsubscribeToken(cfg config.Config, userID uuid.UUID, kind string, elementID uuid.UUID) (string, error) {
	if !unsubscribeTypes[kind] {
		return "", errInvalidToken
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: tokenKey(cfg)}, nil)
	if err != nil {
		return "", err
	}
	return jwt.Signed(signer).Claims(UnsubscribeClaims{V: tokenVersion, UserID: userID.String(), Type: kind, ElementID: elementID.String()}).Serialize()
}

// VerifyUnsubscribeToken reads a token NewUnsubscribeToken signed under this instance's key.
func VerifyUnsubscribeToken(cfg config.Config, token string) (Unsubscribe, error) {
	parsed, err := jwt.ParseSigned(token, hs256)
	if err != nil {
		return Unsubscribe{}, errInvalidToken
	}
	var c UnsubscribeClaims
	if err := parsed.Claims(tokenKey(cfg), &c); err != nil {
		return Unsubscribe{}, errInvalidToken
	}
	userID, okUser := rowid.Parse(c.UserID)
	elementID, okElement := rowid.Parse(c.ElementID)
	if c.V != tokenVersion || !unsubscribeTypes[c.Type] || !okUser || !okElement {
		return Unsubscribe{}, errInvalidToken
	}
	return Unsubscribe{UserID: userID, Type: c.Type, ElementID: elementID}, nil
}

// UnsubscribeAPIPath is the public endpoint a token is read and redeemed at, and the URL a mail client's one-click unsubscribe (RFC 8058) posts to.
const UnsubscribeAPIPath = "/api/unsubscribe"

// UnsubscribeLinks are an email's links for one switch of one recipient: Page, the SPA page that says which switch it is and turns it off when the reader confirms, and OneClick, the List-Unsubscribe URL a mail client posts to.
type UnsubscribeLinks struct {
	Page     string
	OneClick string
}

// NewUnsubscribeLinks signs a token for the switch and builds both links on WEBAPP_URL. The token is a query parameter, which the access log and the error reports never record, and which the SPA's telemetry masks.
func NewUnsubscribeLinks(cfg config.Config, userID uuid.UUID, kind string, elementID uuid.UUID) (UnsubscribeLinks, error) {
	token, err := NewUnsubscribeToken(cfg, userID, kind, elementID)
	if err != nil {
		return UnsubscribeLinks{}, err
	}
	q := url.Values{"token": {token}}.Encode()
	page := cfg.AppPageURL("/unsubscribe")
	page.RawQuery = q
	return UnsubscribeLinks{Page: page.String(), OneClick: strings.TrimRight(cfg.WebAppURL.String(), "/") + UnsubscribeAPIPath + "?" + q}, nil
}
