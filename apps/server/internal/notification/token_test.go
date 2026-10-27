package notification

import (
	"net/url"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

var (
	tokenUser   = uuid.MustParse("0192f5c8-0000-7000-8000-000000000001")
	tokenSurvey = uuid.MustParse("0192f5c8-0000-7000-8000-000000000002")
)

func tokenConfig(t *testing.T, key string) config.Config {
	t.Helper()
	u, err := url.Parse("https://app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return config.Config{EncryptionKey: []byte(key), WebAppURL: u}
}

// signWith signs claims as an HS256 JWT under key, to forge tokens the server must refuse.
func signWith(t *testing.T, key []byte, claims any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// A token names one user's switch of one kind for one survey or organization, and verifies only under the key derived from this instance's ENCRYPTION_KEY.
func TestUnsubscribeTokenRoundTrip(t *testing.T) {
	t.Parallel()
	cfg := tokenConfig(t, "0123456789abcdefghijklmnopqrstuv")
	for _, kind := range []string{KindAlert, KindAutoSubscribe, KindWeeklySummary} {
		token, err := NewUnsubscribeToken(cfg, tokenUser, kind, tokenSurvey)
		if err != nil {
			t.Fatal(err)
		}
		got, err := VerifyUnsubscribeToken(cfg, token)
		if err != nil || got != (Unsubscribe{UserID: tokenUser, Type: kind, ElementID: tokenSurvey}) {
			t.Errorf("%s: %+v, %v", kind, got, err)
		}
	}
	if _, err := NewUnsubscribeToken(cfg, tokenUser, "evil", tokenSurvey); err == nil {
		t.Error("signed a token for an unknown switch")
	}
}

// Anything but an untouched token of this instance is refused: another instance's key, the encryption key itself or the link survey secret, an edited payload, another algorithm, another version, an unknown switch, ids that are not UUIDs.
func TestUnsubscribeTokenRefusesForgeries(t *testing.T) {
	t.Parallel()
	cfg := tokenConfig(t, "0123456789abcdefghijklmnopqrstuv")
	other := tokenConfig(t, "vutsrqponmlkjihgfedcba9876543210")
	good, err := NewUnsubscribeToken(cfg, tokenUser, KindAlert, tokenSurvey)
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := NewUnsubscribeToken(other, tokenUser, KindAlert, tokenSurvey)
	claims := func(v int, kind, user, element string) UnsubscribeClaims {
		return UnsubscribeClaims{V: v, UserID: user, Type: kind, ElementID: element}
	}
	key := tokenKey(cfg)
	parts := strings.Split(good, ".")
	otherUser := signWith(t, []byte("another key of thirty-two bytes!"), claims(1, KindAlert, uuid.NewString(), tokenSurvey.String()))
	cases := map[string]string{
		"another instance":         foreign,
		"the encryption key":       signWith(t, cfg.EncryptionKey, claims(1, KindAlert, tokenUser.String(), tokenSurvey.String())),
		"the link survey secret":   signWith(t, []byte(cfg.LinkSurveyEmailSecret()), claims(1, KindAlert, tokenUser.String(), tokenSurvey.String())),
		"a payload from elsewhere": parts[0] + "." + strings.Split(otherUser, ".")[1] + "." + parts[2],
		"a cut signature":          parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])-2],
		"version 2":                signWith(t, key, claims(2, KindAlert, tokenUser.String(), tokenSurvey.String())),
		"an unknown switch":        signWith(t, key, claims(1, "evil", tokenUser.String(), tokenSurvey.String())),
		"a user that is no UUID":   signWith(t, key, claims(1, KindAlert, "clusr0000000000000001", tokenSurvey.String())),
		"an element that is no id": signWith(t, key, claims(1, KindAlert, tokenUser.String(), "")),
		"empty":                    "",
		"garbage":                  "a.b.c",
	}
	for name, token := range cases {
		if got, err := VerifyUnsubscribeToken(cfg, token); err == nil {
			t.Errorf("%s verified: %+v", name, got)
		}
	}
}

// The email's links: the public SPA page and the one-click API URL, both on WEBAPP_URL with the token as ?token=.
func TestUnsubscribeLinks(t *testing.T) {
	t.Parallel()
	cfg := tokenConfig(t, "0123456789abcdefghijklmnopqrstuv")
	links, err := NewUnsubscribeLinks(cfg, tokenUser, KindWeeklySummary, tokenSurvey)
	if err != nil {
		t.Fatal(err)
	}
	page, err := url.Parse(links.Page)
	if err != nil || page.Scheme+"://"+page.Host+page.Path != "https://app.example.com/app/unsubscribe" {
		t.Fatalf("page %s", links.Page)
	}
	oneClick, err := url.Parse(links.OneClick)
	if err != nil || oneClick.Scheme+"://"+oneClick.Host+oneClick.Path != "https://app.example.com/api/unsubscribe" || oneClick.Query().Get("token") != page.Query().Get("token") {
		t.Fatalf("one-click %s", links.OneClick)
	}
	if got, err := VerifyUnsubscribeToken(cfg, page.Query().Get("token")); err != nil || got.Type != KindWeeklySummary {
		t.Errorf("page token %+v, %v", got, err)
	}
}
