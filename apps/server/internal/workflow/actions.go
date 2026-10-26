package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
)

// Bounds of the actions after send_email. A message stays well under what each chat accepts (Slack 40,000 characters, Teams about 28 KB); Discord's 2,000 is applied when the message is sent.
const (
	maxURLLength            = 2048
	maxHeaders              = 20
	maxHeaderNameLength     = 256
	maxHeaderValueLength    = 4096
	maxHTTPBodyLength       = 100_000
	maxMessageLength        = 4000
	maxSecretLength         = 4096
	maxSealedLength         = 8192
	maxHintLength           = 160
	maxAttributeKeyLength   = 256
	maxAttributeValueLength = 4000
	maxTagIDLength          = 64
)

// HTTPMethods are the methods an http_request action may use.
var HTTPMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// SecretValue is a value the API accepts in plaintext and stores only sealed: an http_request header's value or signing secret, or a chat message's webhook URL. A write carries the new plaintext in Value, which the service seals into Sealed (base64 of the secret.Cipher output) with a Hint the builder shows in its place; what is stored and read back has Sealed and Hint only. Empty, it is a secret the author has not entered yet, which a draft may hold.
type SecretValue struct {
	Value  *string `json:"value,omitempty"`
	Sealed string  `json:"sealed,omitempty"`
	Hint   string  `json:"hint,omitempty"`
}

// HTTPHeader is one header of an http_request action: a plain Value, or a Secret.
type HTTPHeader struct {
	Name   string       `json:"name"`
	Value  *string      `json:"value,omitempty"`
	Secret *SecretValue `json:"secret,omitempty"`
}

// HTTPRequestConfig is the http_request action's configuration. Body is a JSON template (Template.RenderJSON), empty for no body; SigningSecret, when set, signs every request the way webhooks are signed (Standard Webhooks).
type HTTPRequestConfig struct {
	Method        string       `json:"method"`
	URL           string       `json:"url"`
	Headers       []HTTPHeader `json:"headers"`
	Body          string       `json:"body"`
	SigningSecret *SecretValue `json:"signingSecret"`
}

// ChatMessageConfig is the configuration of slack_message, discord_message and teams_message: the channel's incoming-webhook URL, which is itself the credential and so a secret, and a text template.
type ChatMessageConfig struct {
	WebhookURL SecretValue `json:"webhookUrl"`
	Message    string      `json:"message"`
}

// TagResponseConfig is the tag_response action's configuration: the workspace tag the response gets.
type TagResponseConfig struct {
	TagID string `json:"tagId"`
}

// SetContactAttributeConfig is the set_contact_attribute action's configuration: an existing attribute key of the workspace and a text template for its value.
type SetContactAttributeConfig struct {
	AttributeKey string `json:"attributeKey"`
	Value        string `json:"value"`
}

// hasConfig reports whether the node is an action whose configuration was read.
func (n Node) hasConfig() bool {
	return n.Type == NodeAction && (n.Email != nil || n.HTTP != nil || n.Chat != nil || n.Tag != nil || n.Attribute != nil)
}

// IsChatAction reports whether the action type is one of the chat messages.
func IsChatAction(actionType string) bool {
	return actionType == ActionSlackMessage || actionType == ActionDiscordMessage || actionType == ActionTeamsMessage
}

// ---- Parsing ----

// secretValue reads a SecretValue object at at.
func (p *parser) secretValue(v any, present bool, at []any) SecretValue {
	var s SecretValue
	m, ok := p.object(v, present, at)
	if !ok {
		return s
	}
	s.Value = p.optStr(m, "value", at, -1, maxSecretLength)
	if sealed := p.optStr(m, "sealed", at, -1, maxSealedLength); sealed != nil {
		s.Sealed = *sealed
	}
	if hint := p.optStr(m, "hint", at, -1, maxHintLength); hint != nil {
		s.Hint = *hint
	}
	if s.Value != nil && s.Sealed != "" {
		p.add(at, "A secret carries either a new value or its sealed form, not both")
	}
	return s
}

func (p *parser) httpRequest(c map[string]any, parsed bool, at []any) *HTTPRequestConfig {
	h := &HTTPRequestConfig{Method: "POST", Headers: []HTTPHeader{}}
	if !parsed {
		return h
	}
	switch method := c["method"]; {
	case isOneOf(method, HTTPMethods):
		h.Method = method.(string)
	default:
		p.fail(path(at, "method"), oneOf(HTTPMethods))
	}
	h.URL = p.str(c, "url", at, -1, maxURLLength)
	if raw, present := c["headers"]; present {
		list, ok := raw.([]any)
		if !ok {
			p.expected(path(at, "headers"), "array", raw, present)
		}
		if len(list) > maxHeaders {
			p.add(path(at, "headers"), fmt.Sprintf("Too big: expected array to have <=%d items", maxHeaders))
		}
		for i, item := range list {
			h.Headers = append(h.Headers, p.httpHeader(item, path(at, "headers", i)))
		}
	}
	if body := p.optStr(c, "body", at, -1, maxHTTPBodyLength); body != nil {
		h.Body = *body
	}
	if raw, present := c["signingSecret"]; present && raw != nil {
		s := p.secretValue(raw, present, path(at, "signingSecret"))
		h.SigningSecret = &s
	}
	return h
}

func (p *parser) httpHeader(v any, at []any) HTTPHeader {
	var h HTTPHeader
	m, ok := p.object(v, true, at)
	if !ok {
		return h
	}
	h.Name = p.str(m, "name", at, -1, maxHeaderNameLength)
	h.Value = p.optStr(m, "value", at, -1, maxHeaderValueLength)
	if raw, present := m["secret"]; present {
		s := p.secretValue(raw, present, path(at, "secret"))
		h.Secret = &s
	}
	switch {
	case h.Value != nil && h.Secret != nil:
		p.add(at, "A header has either a value or a secret, not both")
	case h.Value == nil && h.Secret == nil:
		p.add(at, "A header needs a value or a secret")
	}
	return h
}

func (p *parser) chatMessage(c map[string]any, parsed bool, at []any) *ChatMessageConfig {
	m := &ChatMessageConfig{}
	if !parsed {
		return m
	}
	raw, present := c["webhookUrl"]
	m.WebhookURL = p.secretValue(raw, present, path(at, "webhookUrl"))
	m.Message = p.str(c, "message", at, -1, maxMessageLength)
	return m
}

func (p *parser) tagResponse(c map[string]any, parsed bool, at []any) *TagResponseConfig {
	t := &TagResponseConfig{}
	if parsed {
		t.TagID = p.str(c, "tagId", at, -1, maxTagIDLength)
	}
	return t
}

func (p *parser) setContactAttribute(c map[string]any, parsed bool, at []any) *SetContactAttributeConfig {
	a := &SetContactAttributeConfig{}
	if parsed {
		a.AttributeKey = p.str(c, "attributeKey", at, -1, maxAttributeKeyLength)
		a.Value = p.str(c, "value", at, -1, maxAttributeValueLength)
	}
	return a
}

func isOneOf(v any, values []string) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, want := range values {
		if s == want {
			return true
		}
	}
	return false
}

// ---- Executability ----

// ActionProblem is a config field that keeps an action from running: blank (Invalid false) or set to something it cannot run with. Field is dotted within the config, "headers.0.name".
type ActionProblem struct {
	Field   string
	Invalid bool
}

func (a ActionProblem) message(n Node) string {
	if a.Invalid {
		return fmt.Sprintf("%s node %s has an invalid %s", n.ActionType, n.ID, a.Field)
	}
	return fmt.Sprintf("%s node %s is missing %s", n.ActionType, n.ID, a.Field)
}

// fieldPath splits a dotted field into an issue path, its numeric parts as indexes.
func fieldPath(field string) []any {
	parts := strings.Split(field, ".")
	out := make([]any, len(parts))
	for i, part := range parts {
		if n, err := strconv.Atoi(part); err == nil {
			out[i] = n
		} else {
			out[i] = part
		}
	}
	return out
}

// ActionProblems are what an action still needs before it can run, in form order: the rule ParseExecutable, enable and the builder share.
func ActionProblems(n Node) []ActionProblem {
	var out []ActionProblem
	missing := func(field string) { out = append(out, ActionProblem{Field: field}) }
	invalid := func(field string) { out = append(out, ActionProblem{Field: field, Invalid: true}) }
	blank := func(s string) bool { return jsvalue.Trim(s) == "" }
	switch {
	case n.Email != nil:
		for _, field := range BlankContentFields(*n.Email) {
			missing(field)
		}
	case n.HTTP != nil:
		h := n.HTTP
		switch {
		case blank(h.URL):
			missing("url")
		case !ValidRequestURL(h.URL):
			invalid("url")
		}
		for i, header := range h.Headers {
			at := fmt.Sprintf("headers.%d.", i)
			switch {
			case blank(header.Name):
				missing(at + "name")
			case !validHeaderName(header.Name) || reservedHeaders[strings.ToLower(header.Name)]:
				invalid(at + "name")
			}
			switch {
			case header.Secret != nil && header.Secret.Sealed == "":
				missing(at + "secret")
			case header.Value != nil && strings.ContainsFunc(*header.Value, isControl):
				invalid(at + "value")
			}
		}
		if !blank(h.Body) && (h.Method == "GET" || !json.Valid([]byte(h.Body))) {
			invalid("body")
		}
		if h.SigningSecret != nil && h.SigningSecret.Sealed == "" {
			missing("signingSecret")
		}
	case n.Chat != nil:
		if n.Chat.WebhookURL.Sealed == "" {
			missing("webhookUrl")
		}
		if blank(n.Chat.Message) {
			missing("message")
		}
	case n.Tag != nil:
		switch {
		case blank(n.Tag.TagID):
			missing("tagId")
		case !isUUID(n.Tag.TagID):
			invalid("tagId")
		}
	case n.Attribute != nil:
		if blank(n.Attribute.AttributeKey) {
			missing("attributeKey")
		}
		if blank(n.Attribute.Value) {
			missing("value")
		}
	}
	return out
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil && len(s) == 36
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// headerToken is RFC 9110's token, the characters a header name may have.
var headerToken = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

func validHeaderName(name string) bool { return headerToken.MatchString(name) }

// reservedHeaders are the headers the runner writes itself: the transport's, the JSON body's type, and the Standard Webhooks headers that identify and sign the request.
var reservedHeaders = map[string]bool{
	"host": true, "content-length": true, "content-type": true, "transfer-encoding": true, "connection": true, "keep-alive": true,
	"upgrade": true, "te": true, "trailer": true, "proxy-authorization": true, "proxy-connection": true,
	"webhook-id": true, "webhook-timestamp": true, "webhook-signature": true,
}

// ValidRequestURL reports whether raw can be an http_request target as written: an absolute http or https URL with a host and no credentials, which belong in a header. Where it points is the SSRF guard's to decide, when the request is made.
func ValidRequestURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" || u.Hostname() == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// origin is a URL's scheme and host, lower-cased, the part a sealed header is bound to; empty for a URL without one.
func origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// chatHosts are the hosts and path prefixes each chat's incoming webhooks live under. Teams has three: Office 365 connectors (*.webhook.office.com) and the Power Automate workflows that replace them (*.logic.azure.com, *.api.powerplatform.com).
var chatHosts = map[string]struct {
	exact      []string
	suffixes   []string
	pathPrefix string
}{
	ActionSlackMessage:   {exact: []string{"hooks.slack.com"}, pathPrefix: "/services/"},
	ActionDiscordMessage: {exact: []string{"discord.com", "discordapp.com", "ptb.discord.com", "canary.discord.com"}, pathPrefix: "/api/webhooks/"},
	ActionTeamsMessage:   {suffixes: []string{".webhook.office.com", ".logic.azure.com", ".api.powerplatform.com"}, pathPrefix: "/"},
}

// ValidChatWebhookURL reports whether raw is an https incoming-webhook URL of the action's chat, so a chat step cannot be pointed at an arbitrary server.
func ValidChatWebhookURL(actionType, raw string) bool {
	hosts, ok := chatHosts[actionType]
	if !ok {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || !strings.HasPrefix(u.EscapedPath(), hosts.pathPrefix) || len(u.EscapedPath()) <= len(hosts.pathPrefix) {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range hosts.exact {
		if host == h {
			return true
		}
	}
	for _, suffix := range hosts.suffixes {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}
	return false
}
