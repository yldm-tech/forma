package auth

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// SAML single sign-on replaces forma2's BoxyHQ SAML Jackson bridge (apps/web/modules/auth/saml and the "saml" generic OAuth provider). forma2 sent the browser through Jackson's OAuth facade; here the AuthnRequest, the assertion consumer service and the sign-in are one flow:
//
//  1. The SPA starts a sign-in like any other provider's. The state cookie binds the flow to the browser, and an AuthnRequest goes to the IdP's HTTP-Redirect endpoint with the state as RelayState; its request ID is kept server-side under the RelayState.
//  2. The IdP posts the response to the consumer service, a cross-site POST that carries no SameSite=Lax cookie. The RelayState names the request, which is consumed, so the response must answer that request (InResponseTo) and is accepted once. A valid assertion is kept under a one-time code and the browser is sent, with a 303, to the SAML callback.
//  3. The callback is a same-origin GET that carries the state cookie: it checks the cookie, redeems the code against the state and signs in through the rules every provider shares.
const (
	providerSAML = "saml"
	// SAMLEntityID is forma2's SAML_AUDIENCE: the service provider's entity ID and the audience every assertion must name. It is the same on every installation and registered at identity providers as it is.
	SAMLEntityID = "https://saml.forma.ylam.ai"
	// samlTenant and samlProduct are forma2's SAML_TENANT and SAML_PRODUCT, the key of its one connection.
	samlTenant  = "forma.ylam.ai"
	samlProduct = "forma"
	// samlConnectionName is the name forma2 gave the preloaded connection.
	samlConnectionName = "SAML SSO"
	// samlACSPath is forma2's SAML_PATH, the assertion consumer service under WEBAPP_URL that identity providers are configured with.
	samlACSPath = "/api/auth/saml/callback"
	// samlMetadataPath serves the service provider's metadata, which forma2 did not publish.
	samlMetadataPath = "/api/auth/saml/metadata"
	// samlCallbackPath is where the consumer service sends the browser to finish the sign-in with its state cookie.
	samlCallbackPath = "/api/auth/callback/" + providerSAML
	// SAMLConnectionDir is forma2's SAML_XML_DIR, relative to the working directory: the first *.xml file in it is the identity provider's metadata, read at start-up.
	SAMLConnectionDir = "saml-connection"
	// samlAssertionTTL is how long an accepted assertion waits for the browser's redirect to the callback.
	samlAssertionTTL = 5 * time.Minute
	// samlResponseLimit bounds the consumer service's form body.
	samlResponseLimit = 2 << 20
)

// SAML attribute names, as forma2 documented them for its identity providers, and the claim URIs Jackson read in their place.
const (
	samlAttributeEmailURI     = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"
	samlAttributeGivenNameURI = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname"
	samlAttributeSurnameURI   = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname"
	samlBearerMethod          = "urn:oasis:names:tc:SAML:2.0:cm:bearer"
)

// CodeSAMLConnection is the error a SAML sign-in starts with when no usable connection is configured; the sign-in page shows forma2's saml_connection_error for it.
const CodeSAMLConnection = "saml_connection_error"

func errSAMLConnection() error {
	return &apperr.Error{Status: http.StatusNotFound, Code: CodeSAMLConnection, Message: "No SAML connection is configured."}
}

// samlRequestPayload is the AuthnRequest a RelayState names.
type samlRequestPayload struct {
	RequestID string `json:"requestId"`
}

// samlAssertionPayload is what an accepted assertion established, waiting for the callback.
type samlAssertionPayload struct {
	RelayState string `json:"relayState"`
	Subject    string `json:"subject"`
	Email      string `json:"email"`
	Name       string `json:"name"`
}

func (s *SSO) samlEnabled() bool { return s.cfg.SSO.SAMLEnabled() }

// samlURL is a SAML endpoint under WEBAPP_URL, built as forma2 concatenated it, since identity providers match the URL as a string.
func (s *SSO) samlURL(path string) url.URL {
	u, _ := url.Parse(strings.TrimRight(s.cfg.WebAppURL.String(), "/") + path)
	return *u
}

// serviceProvider is the service provider for the configured connection, or nil when there is none.
func (s *SSO) serviceProvider(ctx context.Context) (*saml.ServiceProvider, error) {
	conn, err := s.svc.repo.samlConnection(ctx, samlTenant, samlProduct)
	if err != nil || conn == nil {
		return nil, err
	}
	idp, err := parseIdPMetadata([]byte(conn.IdPMetadata))
	if err != nil {
		return nil, fmt.Errorf("SAML connection %s: %w", conn.ID, err)
	}
	return s.newServiceProvider(idp), nil
}

// newServiceProvider configures crewjam/saml with forma2's fixed entity ID and the consumer service URL. Requests carry no signature and ask for no NameID format, as Jackson's did, so the IdP applies the format it is configured with; IdP-initiated responses are refused.
func (s *SSO) newServiceProvider(idp *saml.EntityDescriptor) *saml.ServiceProvider {
	return &saml.ServiceProvider{
		EntityID:                    SAMLEntityID,
		AcsURL:                      s.samlURL(samlACSPath),
		MetadataURL:                 s.samlURL(samlMetadataPath),
		IDPMetadata:                 idp,
		AuthnNameIDFormat:           saml.UnspecifiedNameIDFormat,
		ValidateAudienceRestriction: requireAudience,
	}
}

// requireAudience demands an audience restriction naming this service provider. crewjam/saml accepts an assertion without any restriction, and one where any of several restrictions matches; SAML Core §2.5.1.4 requires every restriction to be met.
func requireAudience(a *saml.Assertion) error {
	if len(a.Conditions.AudienceRestrictions) == 0 {
		return errors.New("assertion has no audience restriction")
	}
	for _, r := range a.Conditions.AudienceRestrictions {
		if r.Audience.Value != SAMLEntityID {
			return fmt.Errorf("assertion audience is %q, not %q", r.Audience.Value, SAMLEntityID)
		}
	}
	return nil
}

// parseIdPMetadata reads an identity provider's metadata: an EntityDescriptor, or the first IdP entity of an EntitiesDescriptor. It must name its entity and offer an HTTP-Redirect sign-on endpoint and a usable signing certificate, so a connection that cannot sign anyone in is refused when it is loaded rather than at sign-in.
func parseIdPMetadata(raw []byte) (*saml.EntityDescriptor, error) {
	raw = bytes.TrimSpace(raw)
	var idp *saml.EntityDescriptor
	var single saml.EntityDescriptor
	if err := xml.Unmarshal(raw, &single); err == nil {
		idp = &single
	} else {
		var group saml.EntitiesDescriptor
		if groupErr := xml.Unmarshal(raw, &group); groupErr != nil {
			return nil, fmt.Errorf("parse metadata: %w", err)
		}
		for i := range group.EntityDescriptors {
			if len(group.EntityDescriptors[i].IDPSSODescriptors) > 0 {
				idp = &group.EntityDescriptors[i]
				break
			}
		}
		if idp == nil {
			return nil, errors.New("metadata describes no identity provider")
		}
	}
	if strings.TrimSpace(idp.EntityID) == "" {
		return nil, errors.New("metadata has no entityID")
	}
	if len(idp.IDPSSODescriptors) == 0 {
		return nil, errors.New("metadata describes no identity provider")
	}
	redirect, certs := false, 0
	for _, d := range idp.IDPSSODescriptors {
		for _, svc := range d.SingleSignOnServices {
			if svc.Binding == saml.HTTPRedirectBinding && svc.Location != "" {
				redirect = true
			}
		}
		for _, kd := range d.KeyDescriptors {
			if kd.Use != "" && kd.Use != "signing" {
				continue
			}
			for _, c := range kd.KeyInfo.X509Data.X509Certificates {
				der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(c.Data), ""))
				if err != nil {
					return nil, fmt.Errorf("decode signing certificate: %w", err)
				}
				if _, err := x509.ParseCertificate(der); err != nil {
					return nil, fmt.Errorf("parse signing certificate: %w", err)
				}
				certs++
			}
		}
	}
	if !redirect {
		return nil, errors.New("the identity provider offers no HTTP-Redirect single sign-on endpoint")
	}
	if certs == 0 {
		return nil, errors.New("metadata has no signing certificate")
	}
	return idp, nil
}

// startSAML sends the browser to the identity provider with an AuthnRequest. The state cookie and the AuthnRequest are stored together; the RelayState is the flow's state, which the consumer service finds the request by.
func (s *SSO) startSAML(ctx context.Context, target string) (string, issuedToken, error) {
	sp, err := s.serviceProvider(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "SAML connection is unusable", "err", err)
		return "", issuedToken{}, errSAMLConnection()
	}
	if sp == nil {
		return "", issuedToken{}, errSAMLConnection()
	}
	req, err := sp.MakeAuthenticationRequest(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return "", issuedToken{}, fmt.Errorf("build AuthnRequest: %w", err)
	}
	state, err := secret.NewToken()
	if err != nil {
		return "", issuedToken{}, err
	}
	cookie, err := secret.NewToken()
	if err != nil {
		return "", issuedToken{}, err
	}
	// The state is URL-safe base64, which crewjam/saml appends to the query unescaped.
	authURL, err := req.Redirect(state, sp)
	if err != nil {
		return "", issuedToken{}, fmt.Errorf("encode AuthnRequest: %w", err)
	}
	expires := s.svc.now().Add(oauthStateTTL)
	err = s.svc.repo.transaction(ctx, func(tx *Repository) error {
		payload := oauthState{Provider: providerSAML, State: state, CallbackURL: target}
		if err := tx.insertPayloadVerification(ctx, purposeOAuthState, uuid.Nil, "", secret.HashToken(cookie), expires, payload); err != nil {
			return err
		}
		return tx.insertPayloadVerification(ctx, purposeSAMLRequest, uuid.Nil, "", secret.HashToken(state), expires, samlRequestPayload{RequestID: req.ID})
	})
	if err != nil {
		return "", issuedToken{}, err
	}
	return authURL.String(), issuedToken{Value: cookie, ExpiresAt: expires, MaxAge: oauthStateTTL}, nil
}

// consumeResponse is the assertion consumer service: it validates the IdP's response to the request the RelayState names and returns where the browser goes next. Every failure is a redirect to the sign-in page with an error code, where forma2 answered with an unhandled 500; the cause is logged without the response, which carries personal data.
func (s *SSO) consumeResponse(ctx context.Context, encoded, relayState string) callbackResult {
	if relayState == "" {
		// An IdP-initiated response answers no request of ours. forma2 sent it to the sign-in page without signing anyone in.
		return s.failure(callbackRestart)
	}
	row, err := s.svc.repo.consumePayloadVerification(ctx, purposeSAMLRequest, secret.HashToken(relayState), s.svc.now())
	if err != nil {
		s.log.ErrorContext(ctx, "SAML response failed to load its request", "err", err)
		return s.failure(callbackRestart)
	}
	if row == nil {
		return s.failure(callbackRestart)
	}
	var request samlRequestPayload
	if err := json.Unmarshal(row.Payload, &request); err != nil || request.RequestID == "" {
		s.log.ErrorContext(ctx, "SAML request is unreadable", "err", err)
		return s.failure(callbackRestart)
	}
	sp, err := s.serviceProvider(ctx)
	if err != nil || sp == nil {
		s.log.ErrorContext(ctx, "SAML connection is unusable", "err", err)
		return s.failure(callbackProviderNotFound)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded), ""))
	if err != nil || len(raw) == 0 {
		s.log.WarnContext(ctx, "SAML response is not base64")
		return s.failure(callbackInvalidCode)
	}
	assertion, err := parseSAMLResponse(sp, raw, request.RequestID)
	if err != nil {
		s.log.WarnContext(ctx, "SAML response rejected", "reason", samlRejection(err))
		return s.failure(callbackInvalidCode)
	}
	id := identityFromAssertion(assertion)
	if id.Subject == "" {
		s.log.WarnContext(ctx, "SAML assertion names no subject")
		return s.failure(callbackNoUserInfo)
	}
	if id.Email == "" {
		return s.failure(callbackEmailNotFound)
	}
	code, err := secret.NewToken()
	if err != nil {
		s.log.ErrorContext(ctx, "SAML code generation failed", "err", err)
		return s.failure(callbackCreateFailed)
	}
	payload := samlAssertionPayload{RelayState: relayState, Subject: id.Subject, Email: id.Email, Name: id.Name}
	if err := s.svc.repo.insertPayloadVerification(ctx, purposeSAMLAssertion, uuid.Nil, "", secret.HashToken(code), s.svc.now().Add(samlAssertionTTL), payload); err != nil {
		s.log.ErrorContext(ctx, "SAML assertion could not be stored", "err", err)
		return s.failure(callbackCreateFailed)
	}
	return callbackResult{redirect: s.pageWithQuery(samlCallbackPath, url.Values{"code": {code}, "state": {relayState}})}
}

// parseSAMLResponse validates a response with crewjam/saml: the XML is well formed and round-trips; the Response or every Assertion is signed by a certificate in the IdP's metadata, and only signed content is read; the Destination and every bearer confirmation's Recipient are the consumer service; the response and its confirmations answer requestID; the issue instants, confirmation and conditions are within their time windows; the issuer is the IdP; and the audience is this service provider. On top of that it requires a bearer confirmation and conditions, which crewjam/saml dereferences without checking, so a signed assertion that lacks them is refused instead of crashing the request.
func parseSAMLResponse(sp *saml.ServiceProvider, raw []byte, requestID string) (assertion *saml.Assertion, err error) {
	defer func() {
		if r := recover(); r != nil {
			assertion, err = nil, fmt.Errorf("malformed assertion: %v", r)
		}
	}()
	assertion, err = sp.ParseXMLResponse(raw, []string{requestID}, sp.AcsURL)
	if err != nil {
		return nil, err
	}
	bearer := false
	for _, c := range assertion.Subject.SubjectConfirmations {
		if c.Method == samlBearerMethod && c.SubjectConfirmationData != nil {
			bearer = true
		}
	}
	if !bearer {
		return nil, errors.New("assertion has no bearer subject confirmation")
	}
	return assertion, nil
}

// samlRejection is a rejected response's cause, safe to log: crewjam/saml's diagnostic, except an XML parsing error, which can quote the document.
func samlRejection(err error) string {
	var invalid *saml.InvalidResponseError
	if errors.As(err, &invalid) && invalid.PrivateErr != nil {
		err = invalid.PrivateErr
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "invalid xml") || strings.HasPrefix(msg, "cannot unmarshal") {
		return "invalid xml"
	}
	return msg
}

// identityFromAssertion maps an assertion as Jackson's profile and forma2's mapper did: the subject is the NameID; the address is the email attribute (or its claim URI), else a NameID in the emailAddress format; the name is the name attribute, else firstName and lastName (or the given name and surname claim URIs) joined. SAML never attests the address.
func identityFromAssertion(a *saml.Assertion) ssoIdentity {
	attrs := map[string]string{}
	for _, statement := range a.AttributeStatements {
		for _, attr := range statement.Attributes {
			if _, seen := attrs[attr.Name]; seen {
				continue
			}
			for _, v := range attr.Values {
				if value := strings.TrimSpace(v.Value); value != "" {
					attrs[attr.Name] = value
					break
				}
			}
		}
	}
	first := func(names ...string) string {
		for _, n := range names {
			if v := attrs[n]; v != "" {
				return v
			}
		}
		return ""
	}
	var id ssoIdentity
	if a.Subject.NameID != nil {
		id.Subject = strings.TrimSpace(a.Subject.NameID.Value)
	}
	id.Email = first("email", samlAttributeEmailURI)
	if id.Email == "" && a.Subject.NameID != nil && a.Subject.NameID.Format == string(saml.EmailAddressNameIDFormat) {
		id.Email = id.Subject
	}
	id.Name = first("name")
	if id.Name == "" {
		var parts []string
		for _, part := range []string{first("firstName", samlAttributeGivenNameURI), first("lastName", samlAttributeSurnameURI)} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		id.Name = strings.Join(parts, " ")
	}
	return id
}

// finishSAML redeems the code the consumer service issued, which must belong to the flow whose state cookie the browser presented, and signs in.
func (s *SSO) finishSAML(ctx context.Context, state oauthState, code, acceptLanguage string, meta clientMeta) callbackResult {
	row, err := s.svc.repo.consumePayloadVerification(ctx, purposeSAMLAssertion, secret.HashToken(code), s.svc.now())
	if err != nil {
		s.log.ErrorContext(ctx, "SAML callback failed to load its assertion", "err", err)
		return s.failure(callbackInvalidCode)
	}
	if row == nil {
		return s.failure(callbackInvalidCode)
	}
	var accepted samlAssertionPayload
	if err := json.Unmarshal(row.Payload, &accepted); err != nil {
		s.log.ErrorContext(ctx, "SAML assertion is unreadable", "err", err)
		return s.failure(callbackInvalidCode)
	}
	if !secretEqual(accepted.RelayState, state.State) {
		return s.failure(callbackStateMismatch)
	}
	res, err := s.signIn(ctx, providerSAML, ssoIdentity{Subject: accepted.Subject, Email: accepted.Email, Name: accepted.Name}, state.CallbackURL, acceptLanguage, meta)
	if err != nil {
		s.log.ErrorContext(ctx, "SSO sign-in failed", "err", err, "provider", providerSAML)
		return s.failure(callbackCreateFailed)
	}
	return res
}

// serviceProviderMetadata is the service provider's metadata: the entity ID, the consumer service with the HTTP-POST binding, and signed assertions wanted.
func (s *SSO) serviceProviderMetadata() ([]byte, error) {
	md := s.newServiceProvider(nil).Metadata()
	for i := range md.SPSSODescriptors {
		d := &md.SPSSODescriptors[i]
		acs := d.AssertionConsumerServices[:0]
		for _, e := range d.AssertionConsumerServices {
			if e.Binding == saml.HTTPPostBinding {
				acs = append(acs, e)
			}
		}
		d.AssertionConsumerServices = acs
	}
	out, err := xml.MarshalIndent(md, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode SAML metadata: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}
