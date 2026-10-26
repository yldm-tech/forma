package workflow

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/webhook"
)

// A definition's secrets are sealed under ENCRYPTION_KEY for the one place they may be used, so a sealed value pasted anywhere else does not open: an http_request header value or signing secret for its workspace and the origin of the request's URL, which also means changing the URL's host asks for the secrets again instead of sending them to the new host; a chat webhook URL for its workspace and chat.

func headerSecretContext(workspaceID uuid.UUID, rawURL string) []byte {
	return []byte("workflow.http_request.header|" + workspaceID.String() + "|" + origin(rawURL))
}

func signingSecretContext(workspaceID uuid.UUID, rawURL string) []byte {
	return []byte("workflow.http_request.signing_secret|" + workspaceID.String() + "|" + origin(rawURL))
}

func chatSecretContext(workspaceID uuid.UUID, actionType string) []byte {
	return []byte("workflow." + actionType + ".webhook_url|" + workspaceID.String())
}

// Reasons a secret is refused when a definition is written.
const (
	secretForOtherDestination = "This secret was stored for another destination. Enter it again."
	signingSecretInvalid      = "A signing secret is whsec_ followed by the base64 of at least 24 random bytes."
)

func chatURLInvalid(actionType string) string {
	return fmt.Sprintf("Enter the incoming-webhook URL %s gives you.", map[string]string{
		ActionSlackMessage: "Slack", ActionDiscordMessage: "Discord", ActionTeamsMessage: "Microsoft Teams",
	}[actionType])
}

// errSecretUnavailable is a sealed secret that does not open in its context.
var errSecretUnavailable = errors.New("secret cannot be opened")

// openSecret is the plaintext of a sealed secret.
func openSecret(c *secret.Cipher, v SecretValue, context []byte) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(v.Sealed)
	if v.Sealed == "" || err != nil {
		return "", errSecretUnavailable
	}
	plain, err := c.Open(raw, context)
	if err != nil {
		return "", errSecretUnavailable
	}
	return string(plain), nil
}

// sealSecrets prepares a definition for storage in the workspace: every secret written in plaintext is checked and sealed, a sealed one must open where its node now sends, and an emptied one stays empty, which only a draft may hold. A secret that fails is an issue at its path under at; the error is a failure to seal.
func sealSecrets(c *secret.Cipher, workspaceID uuid.UUID, d *Definition, at []any) ([]Issue, error) {
	var issues []Issue
	seal := func(v *SecretValue, where []any, context []byte, check func(string) string, hint func(string) string) error {
		switch {
		case v.Value != nil:
			plain := jsvalue.Trim(*v.Value)
			if plain == "" {
				*v = SecretValue{}
				return nil
			}
			if msg := check(plain); msg != "" {
				issues = append(issues, Issue{Path: where, Message: msg})
				return nil
			}
			sealed, err := c.Seal([]byte(plain), context)
			if err != nil {
				return fmt.Errorf("seal workflow secret: %w", err)
			}
			*v = SecretValue{Sealed: base64.StdEncoding.EncodeToString(sealed), Hint: hint(plain)}
		case v.Sealed != "":
			if _, err := openSecret(c, *v, context); err != nil {
				issues = append(issues, Issue{Path: where, Message: secretForOtherDestination})
			}
		default:
			v.Hint = ""
		}
		return nil
	}
	anything := func(string) string { return "" }
	for i := range d.Nodes {
		n := &d.Nodes[i]
		config := path(at, "nodes", i, "config")
		switch {
		case n.HTTP != nil:
			for j := range n.HTTP.Headers {
				if s := n.HTTP.Headers[j].Secret; s != nil {
					if err := seal(s, path(config, "headers", j, "secret"), headerSecretContext(workspaceID, n.HTTP.URL), anything, tailHint); err != nil {
						return nil, err
					}
				}
			}
			if s := n.HTTP.SigningSecret; s != nil {
				check := func(v string) string {
					if !webhook.ValidSecret(v) {
						return signingSecretInvalid
					}
					return ""
				}
				if err := seal(s, path(config, "signingSecret"), signingSecretContext(workspaceID, n.HTTP.URL), check, tailHint); err != nil {
					return nil, err
				}
			}
		case n.Chat != nil:
			actionType := n.ActionType
			check := func(v string) string {
				if !ValidChatWebhookURL(actionType, v) {
					return chatURLInvalid(actionType)
				}
				return ""
			}
			if err := seal(&n.Chat.WebhookURL, path(config, "webhookUrl"), chatSecretContext(workspaceID, actionType), check, urlHint); err != nil {
				return nil, err
			}
		}
	}
	return issues, nil
}

// tailHint is what the builder shows for a sealed header or signing secret: its last four characters, only when it is long enough that they say little about the rest.
func tailHint(plain string) string {
	r := []rune(plain)
	if len(r) < 12 {
		return ""
	}
	return "…" + string(r[len(r)-4:])
}

// urlHint is what the builder shows for a sealed webhook URL: its host and the last four characters of its path.
func urlHint(plain string) string {
	u, err := url.Parse(plain)
	if err != nil {
		return ""
	}
	p := []rune(strings.TrimSuffix(u.Path, "/"))
	if len(p) < 12 {
		return u.Host
	}
	return u.Host + "/…" + string(p[len(p)-4:])
}
