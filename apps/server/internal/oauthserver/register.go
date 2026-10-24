package oauthserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

const (
	applicationTypeWeb    = "web"
	applicationTypeNative = "native"

	authMethodNone  = "none"
	authMethodBasic = "client_secret_basic"
	authMethodPost  = "client_secret_post"

	grantTypeCode    = "authorization_code"
	grantTypeRefresh = "refresh_token"
)

// Bounds of a registration, so an unauthenticated caller cannot store arbitrary amounts.
const (
	maxRegistrationBytes = 64 << 10
	maxRedirectURIs      = 20
	maxURILength         = 2048
	maxClientNameLength  = 255
)

// blockedSchemes can never be a redirect target: a browser would run or read them in the page's own context.
var blockedSchemes = []string{"javascript", "data", "vbscript", "file", "blob"}

// nativeLoopbackHosts are the loopback hosts a native client may redirect to over plain http (RFC 8252 §7.3), the three Better Auth accepted.
var nativeLoopbackHosts = []string{"localhost", "127.0.0.1", "[::1]"}

// registration is a dynamic client registration request (RFC 7591) as far as it is honoured.
type registration struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri"`
	LogoURI                 string   `json:"logo_uri"`
	ApplicationType         string   `json:"application_type"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	// Contacts are echoed in the answer, as Better Auth did; nothing reads them, so they are not stored.
	Contacts []string `json:"contacts"`
}

// registrationError is an RFC 7591 §3.2.2 error.
type registrationError struct {
	code        string
	description string
	// schema marks a request Better Auth refused in its request schema, before its handler ran; it answered those without Cache-Control.
	schema bool
}

func (e *registrationError) Error() string { return e.code + ": " + e.description }

func invalidMetadata(description string) error {
	return &registrationError{code: "invalid_client_metadata", description: description}
}

func invalidRedirectURI(description string) error {
	return &registrationError{code: "invalid_redirect_uri", description: description}
}

// register is dynamic client registration, open to anyone as forma2's was. Whatever scopes the client asks for, it is registered for every scope and for the MCP resource; what a token grants is still what the user approves and what their workspace permissions allow.
func (h *Handler) register(c *gin.Context) {
	ctx := c.Request.Context()
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxRegistrationBytes))
	if err != nil {
		oauthError(c, http.StatusBadRequest, "invalid_client_metadata", "The registration request is too large or unreadable.")
		return
	}
	reg, err := parseRegistration(body)
	if err == nil {
		err = reg.validate()
	}
	var regErr *registrationError
	if errors.As(err, &regErr) {
		if regErr.schema {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": regErr.code, "error_description": regErr.description})
			return
		}
		oauthError(c, http.StatusBadRequest, regErr.code, regErr.description)
		return
	}
	client := &Client{
		name: reg.ClientName, redirectURIs: reg.RedirectURIs, applicationType: reg.ApplicationType, authMethod: reg.TokenEndpointAuthMethod,
		grantTypes: reg.GrantTypes, responseTypes: reg.ResponseTypes, scopes: Scopes, resources: []string{h.urls.Resource},
	}
	if reg.ClientURI != "" {
		client.clientURI = &reg.ClientURI
	}
	if reg.LogoURI != "" {
		client.logoURI = &reg.LogoURI
	}
	if client.id, err = secret.NewToken(); err != nil {
		h.fail(c, err)
		return
	}
	var clientSecret string
	if client.authMethod != authMethodNone {
		raw, err := secret.NewToken()
		if err != nil {
			h.fail(c, err)
			return
		}
		clientSecret = clientSecretPrefix + raw
		client.secretHash = secret.HashToken(clientSecret)
	}
	if err := h.repo.insertClient(ctx, client); err != nil {
		h.fail(c, err)
		return
	}
	out := gin.H{
		"client_id":                  client.id,
		"client_id_issued_at":        client.createdAt.Unix(),
		"client_name":                client.name,
		"redirect_uris":              client.redirectURIs,
		"grant_types":                client.grantTypes,
		"response_types":             client.responseTypes,
		"token_endpoint_auth_method": client.authMethod,
		"application_type":           client.applicationType,
		"scope":                      strings.Join(client.scopes, " "),
		"contacts":                   orEmpty(reg.Contacts),
		"post_logout_redirect_uris":  []string{},
		"disabled":                   false,
		"dpop_bound_access_tokens":   false,
		"resources":                  client.resources,
	}
	if clientSecret != "" {
		out["client_secret"] = clientSecret
		out["client_secret_expires_at"] = 0
	}
	if client.clientURI != nil {
		out["client_uri"] = *client.clientURI
	}
	if client.logoURI != nil {
		out["logo_uri"] = *client.logoURI
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(http.StatusCreated, out)
}

func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func (h *Handler) fail(c *gin.Context, err error) {
	h.log.ErrorContext(c.Request.Context(), "OAuth request failed", "route", c.FullPath(), "err", err)
	oauthError(c, http.StatusInternalServerError, "server_error", "")
}

// parseRegistration reads the request and fills in application_type "native" when it is absent and a redirect URI is an http loopback, forma2's withInferredApplicationType: MCP clients listen on a loopback port and often do not say they are native, and a web client may not redirect to loopback.
func parseRegistration(body []byte) (*registration, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, invalidMetadata("The registration request must be a JSON object.")
	}
	reg := &registration{}
	if err := json.Unmarshal(body, reg); err != nil {
		return nil, invalidMetadata("The registration request has a field of the wrong type.")
	}
	if _, given := fields["application_type"]; !given && slices.ContainsFunc(reg.RedirectURIs, isHTTPLoopback) {
		reg.ApplicationType = applicationTypeNative
	}
	return reg, nil
}

func isHTTPLoopback(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "http" && slices.Contains(nativeLoopbackHosts, hostOf(u))
}

// hostOf is the host as the URL spells it, with brackets around an IPv6 address and without the port.
func hostOf(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

// validate applies RFC 7591's defaults and Better Auth's rules: a web client redirects only to https outside loopback, a native one to loopback over http, to https, or to its own scheme.
func (r *registration) validate() error {
	if r.ApplicationType == "" {
		r.ApplicationType = applicationTypeWeb
	}
	if r.ApplicationType != applicationTypeWeb && r.ApplicationType != applicationTypeNative {
		return invalidMetadata("application_type must be web or native.")
	}
	if len(r.RedirectURIs) == 0 {
		return invalidRedirectURI("Redirect URIs are required for authorization_code and implicit grant types")
	}
	if len(r.RedirectURIs) > maxRedirectURIs {
		return invalidRedirectURI("Too many redirect_uris.")
	}
	for i, raw := range r.RedirectURIs {
		// Better Auth's request schema refused these three schemes with this message.
		if u, err := url.Parse(raw); err == nil && slices.Contains([]string{"javascript", "data", "vbscript"}, strings.ToLower(u.Scheme)) {
			return &registrationError{code: "invalid_redirect_uri", description: "redirect_uris[" + strconv.Itoa(i) + "]: URL cannot use javascript:, data:, or vbscript: scheme", schema: true}
		}
		if err := r.validateRedirectURI(raw); err != nil {
			return err
		}
	}
	if r.TokenEndpointAuthMethod == "" {
		r.TokenEndpointAuthMethod = authMethodBasic
	}
	if !slices.Contains([]string{authMethodNone, authMethodBasic, authMethodPost}, r.TokenEndpointAuthMethod) {
		return invalidMetadata("token_endpoint_auth_method must be none, client_secret_basic or client_secret_post.")
	}
	if len(r.GrantTypes) == 0 {
		r.GrantTypes = []string{grantTypeCode}
	}
	r.GrantTypes = slices.Compact(slices.Sorted(slices.Values(r.GrantTypes)))
	for _, g := range r.GrantTypes {
		if g != grantTypeCode && g != grantTypeRefresh {
			return invalidMetadata("grant_types may only be authorization_code and refresh_token.")
		}
	}
	if !slices.Contains(r.GrantTypes, grantTypeCode) {
		return invalidMetadata("grant_types must include authorization_code.")
	}
	if len(r.ResponseTypes) == 0 {
		r.ResponseTypes = []string{"code"}
	}
	r.ResponseTypes = slices.Compact(slices.Sorted(slices.Values(r.ResponseTypes)))
	if len(r.ResponseTypes) != 1 || r.ResponseTypes[0] != "code" {
		return invalidMetadata("response_types may only be code.")
	}
	if len(r.ClientName) > maxClientNameLength {
		return invalidMetadata("client_name is too long.")
	}
	for name, raw := range map[string]string{"client_uri": r.ClientURI, "logo_uri": r.LogoURI} {
		if raw == "" {
			continue
		}
		if u, err := url.Parse(raw); err != nil || len(raw) > maxURILength || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return invalidMetadata(name + " must be an http(s) URL.")
		}
	}
	return nil
}

func (r *registration) validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > maxURILength || !u.IsAbs() || u.Fragment != "" || strings.Contains(raw, "#") {
		return invalidRedirectURI("Every redirect_uri must be an absolute URI without a fragment.")
	}
	scheme := strings.ToLower(u.Scheme)
	if slices.Contains(blockedSchemes, scheme) {
		return invalidRedirectURI("The redirect_uri scheme is not allowed.")
	}
	loopback := isHTTPLoopback(raw) || (scheme == "https" && slices.Contains(nativeLoopbackHosts, hostOf(u)))
	switch {
	case scheme == "https" && u.Host == "":
		return invalidRedirectURI("An https redirect_uri needs a host.")
	case r.ApplicationType == applicationTypeWeb && (scheme != "https" || loopback):
		return invalidRedirectURI("A web client must redirect to an https URI that is not a loopback address.")
	case r.ApplicationType == applicationTypeNative && scheme == "http" && !loopback:
		return invalidRedirectURI("A native client may use http only for a loopback redirect_uri.")
	}
	return nil
}
