package auth

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

const (
	testIdPEntityID = "https://idp.example.com/metadata"
	testIdPSSOURL   = "https://idp.example.com/sso"
	testACSURL      = webApp + "/api/auth/saml/callback"
)

// testSAMLKeys are the trusted identity provider's key and a stranger's, generated once for the package.
var testSAMLKeys = sync.OnceValue(func() [2]*testSAMLKey {
	return [2]*testSAMLKey{newTestSAMLKey(), newTestSAMLKey()}
})

type testSAMLKey struct {
	key  *rsa.PrivateKey
	cert *x509.Certificate
}

func newTestSAMLKey() *testSAMLKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "idp.example.com"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(10 * 365 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	return &testSAMLKey{key: key, cert: cert}
}

// sign returns el's enveloped RSA-SHA256 signature, as an identity provider computes it.
func (k *testSAMLKey) sign(t *testing.T, el *etree.Element) *etree.Element {
	t.Helper()
	ctx := dsig.NewDefaultSigningContext(dsig.TLSCertKeyStore(tls.Certificate{Certificate: [][]byte{k.cert.Raw}, PrivateKey: k.key, Leaf: k.cert}))
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	if err := ctx.SetSignatureMethod(dsig.RSASHA256SignatureMethod); err != nil {
		t.Fatal(err)
	}
	signed, err := ctx.SignEnveloped(el)
	if err != nil {
		t.Fatal(err)
	}
	children := signed.ChildElements()
	return children[len(children)-1]
}

// idpMetadata is the trusted identity provider's metadata; bindings defaults to HTTP-Redirect.
func idpMetadata(t *testing.T, bindings ...string) string {
	t.Helper()
	if len(bindings) == 0 {
		bindings = []string{saml.HTTPRedirectBinding}
	}
	var services []saml.Endpoint
	for _, b := range bindings {
		services = append(services, saml.Endpoint{Binding: b, Location: testIdPSSOURL})
	}
	md := saml.EntityDescriptor{
		EntityID: testIdPEntityID,
		IDPSSODescriptors: []saml.IDPSSODescriptor{{
			SSODescriptor: saml.SSODescriptor{RoleDescriptor: saml.RoleDescriptor{
				ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
				KeyDescriptors: []saml.KeyDescriptor{{Use: "signing", KeyInfo: saml.KeyInfo{X509Data: saml.X509Data{
					X509Certificates: []saml.X509Certificate{{Data: base64.StdEncoding.EncodeToString(testSAMLKeys()[0].cert.Raw)}},
				}}}},
			}},
			SingleSignOnServices: services,
		}},
	}
	out, err := xml.Marshal(md)
	if err != nil {
		t.Fatal(err)
	}
	return xml.Header + string(out)
}

// samlEnv is an env with SAML enabled and the test identity provider as its connection.
func samlEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, withSAML)
	if err := e.svc.repo.saveSAMLConnection(context.Background(), samlTenant, samlProduct, samlConnectionName, testIdPEntityID, idpMetadata(t)); err != nil {
		t.Fatal(err)
	}
	return e
}

func withSAML(cfg *config.Config) {
	cfg.SSO.SAMLDatabaseURL = "postgres://forma:secret@db:5432/forma-saml"
}

// samlFlow is one sign-in started in a browser: the AuthnRequest's ID and the RelayState the identity provider returns.
type samlFlow struct {
	b          *browser
	requestID  string
	relayState string
}

func (b *browser) samlStart(callbackURL string) samlFlow {
	b.e.t.Helper()
	authURL := b.ssoStart("saml", callbackURL)
	req, relayState := decodeAuthnRequest(b.e.t, authURL)
	return samlFlow{b: b, requestID: req.ID, relayState: relayState}
}

// decodeAuthnRequest reads an HTTP-Redirect binding URL as the identity provider does.
func decodeAuthnRequest(t *testing.T, authURL string) (saml.AuthnRequest, string) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme+"://"+u.Host+u.Path != testIdPSSOURL {
		t.Fatalf("authorization URL %s", authURL)
	}
	deflated, err := base64.StdEncoding.DecodeString(u.Query().Get("SAMLRequest"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(flate.NewReader(bytes.NewReader(deflated)))
	if err != nil {
		t.Fatal(err)
	}
	var req saml.AuthnRequest
	if err := xml.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	return req, u.Query().Get("RelayState")
}

// assertionSpec is what the identity provider asserts; zero fields take the values of a valid response to the flow.
type assertionSpec struct {
	requestID    string
	issuer       string
	audience     string
	recipient    string
	destination  string
	nameID       string
	nameIDFormat string
	attributes   [][2]string
	issueInstant time.Time
	notBefore    time.Time
	notOnOrAfter time.Time
	status       string
	// signer signs; nil means the trusted key.
	signer         *testSAMLKey
	signAssertion  bool
	signResponse   bool
	noConditions   bool
	noConfirmation bool
}

func (f samlFlow) spec() assertionSpec {
	return assertionSpec{
		requestID: f.requestID, nameID: "ada@example.com", nameIDFormat: string(saml.EmailAddressNameIDFormat),
		attributes:    [][2]string{{"email", "ada@example.com"}, {"firstName", "Ada"}, {"lastName", "Lovelace"}},
		signAssertion: true, signResponse: true,
	}
}

func (s assertionSpec) withDefaults() assertionSpec {
	now := time.Now().UTC().Truncate(time.Second)
	if s.issuer == "" {
		s.issuer = testIdPEntityID
	}
	if s.audience == "" {
		s.audience = SAMLEntityID
	}
	if s.recipient == "" {
		s.recipient = testACSURL
	}
	if s.destination == "" {
		s.destination = testACSURL
	}
	if s.issueInstant.IsZero() {
		s.issueInstant = now
	}
	if s.notBefore.IsZero() {
		s.notBefore = now.Add(-time.Minute)
	}
	if s.notOnOrAfter.IsZero() {
		s.notOnOrAfter = now.Add(5 * time.Minute)
	}
	if s.status == "" {
		s.status = saml.StatusSuccess
	}
	if s.signer == nil {
		s.signer = testSAMLKeys()[0]
	}
	return s
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("id-%x", b)
}

// assertion builds the Assertion element, signed when the spec says so.
func (s assertionSpec) assertion(t *testing.T) *etree.Element {
	t.Helper()
	s = s.withDefaults()
	a := &saml.Assertion{
		ID: randomID(), IssueInstant: s.issueInstant, Version: "2.0",
		Issuer:          saml.Issuer{Format: "urn:oasis:names:tc:SAML:2.0:nameid-format:entity", Value: s.issuer},
		Subject:         &saml.Subject{NameID: &saml.NameID{Format: s.nameIDFormat, Value: s.nameID}},
		AuthnStatements: []saml.AuthnStatement{{AuthnInstant: s.issueInstant, SessionIndex: "1"}},
	}
	if !s.noConfirmation {
		a.Subject.SubjectConfirmations = []saml.SubjectConfirmation{{
			Method:                  samlBearerMethod,
			SubjectConfirmationData: &saml.SubjectConfirmationData{NotOnOrAfter: s.notOnOrAfter, Recipient: s.recipient, InResponseTo: s.requestID},
		}}
	}
	if !s.noConditions {
		a.Conditions = &saml.Conditions{NotBefore: s.notBefore, NotOnOrAfter: s.notOnOrAfter}
		if s.audience != "-" {
			a.Conditions.AudienceRestrictions = []saml.AudienceRestriction{{Audience: saml.Audience{Value: s.audience}}}
		}
	}
	if len(s.attributes) > 0 {
		var attrs []saml.Attribute
		for _, kv := range s.attributes {
			attrs = append(attrs, saml.Attribute{Name: kv[0], NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic", Values: []saml.AttributeValue{{Type: "xs:string", Value: kv[1]}}})
		}
		a.AttributeStatements = []saml.AttributeStatement{{Attributes: attrs}}
	}
	if s.signAssertion {
		a.Signature = s.signer.sign(t, a.Element())
	}
	return a.Element()
}

// response wraps assertion elements in a Response, signed when the spec says so.
func (s assertionSpec) response(t *testing.T, assertions ...*etree.Element) *etree.Element {
	t.Helper()
	s = s.withDefaults()
	r := &saml.Response{
		ID: randomID(), InResponseTo: s.requestID, Version: "2.0", IssueInstant: s.issueInstant, Destination: s.destination,
		Issuer: &saml.Issuer{Format: "urn:oasis:names:tc:SAML:2.0:nameid-format:entity", Value: s.issuer},
		Status: saml.Status{StatusCode: saml.StatusCode{Value: s.status}},
	}
	build := func() *etree.Element {
		el := r.Element()
		for _, a := range assertions {
			el.AddChild(a.Copy())
		}
		return el
	}
	if s.signResponse {
		r.Signature = s.signer.sign(t, build())
	}
	return build()
}

// encode is the form value an identity provider posts.
func encode(t *testing.T, el *etree.Element) string {
	t.Helper()
	doc := etree.NewDocument()
	doc.SetRoot(el.Copy())
	raw, err := doc.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// signedResponse is the encoded response for a spec.
func (s assertionSpec) signedResponse(t *testing.T) string {
	t.Helper()
	return encode(t, s.response(t, s.assertion(t)))
}

// postACS posts to the assertion consumer service as the identity provider's page does: a cross-site form POST that carries none of the browser's SameSite=Lax cookies.
func (e *env) postACS(samlResponse, relayState string) response {
	e.t.Helper()
	form := url.Values{"SAMLResponse": {samlResponse}, "RelayState": {relayState}}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/callback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://idp.example.com")
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	if len(rec.Result().Cookies()) != 0 {
		e.t.Fatalf("the consumer service set cookies: %v", rec.Result().Cookies())
	}
	return response{ResponseRecorder: rec, t: e.t}
}

func expectSeeOther(t *testing.T, r response, want string) {
	t.Helper()
	if r.Code != http.StatusSeeOther || r.Header().Get("Location") != want {
		t.Fatalf("got %d to %q, want a 303 to %q: %s", r.Code, r.Header().Get("Location"), want, r.Body.String())
	}
}

// follow makes the browser follow a redirect to the app.
func (b *browser) follow(r response) response {
	b.e.t.Helper()
	loc, err := url.Parse(r.Header().Get("Location"))
	if err != nil || URLOrigin(loc) != webApp {
		b.e.t.Fatalf("redirect to %q", r.Header().Get("Location"))
	}
	return b.request(http.MethodGet, loc.RequestURI(), nil, http.Header{"Accept-Language": {"fr-FR, ja-JP;q=0.9"}})
}

// complete posts the response for the flow and follows the consumer service's redirect to the callback.
func (f samlFlow) complete(samlResponse string) response {
	f.b.e.t.Helper()
	r := f.b.e.postACS(samlResponse, f.relayState)
	if r.Code != http.StatusSeeOther {
		f.b.e.t.Fatalf("consumer service answered %d", r.Code)
	}
	loc, _ := url.Parse(r.Header().Get("Location"))
	if loc.Path != samlCallbackPath {
		return r
	}
	return f.b.follow(r)
}

func TestSAMLStart(t *testing.T) {
	t.Parallel()
	e := samlEnv(t)
	b := e.browser()

	r := b.post("/auth/sso/sign-in", map[string]string{"provider": "SAML", "callbackUrl": "/surveys"})
	expectStatus(t, r, http.StatusOK, "")
	req, relayState := decodeAuthnRequest(t, r.json()["url"].(string))
	if req.Destination != testIdPSSOURL || req.AssertionConsumerServiceURL != testACSURL || req.ProtocolBinding != saml.HTTPPostBinding ||
		req.Issuer == nil || req.Issuer.Value != SAMLEntityID || req.NameIDPolicy == nil || req.NameIDPolicy.Format != nil || req.ID == "" {
		t.Fatalf("AuthnRequest %+v", req)
	}
	c := r.cookie("forma.oauth_state")
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 600 || c.Value == relayState || len(relayState) < 43 {
		t.Fatalf("state cookie %+v, RelayState %q", c, relayState)
	}
	// The request ID stays on the server, found by the RelayState's hash; nothing in the browser names it.
	if n := e.count(`SELECT count(*) FROM verifications WHERE purpose = 'saml_request' AND token_hash = sha256(?::bytea) AND payload->>'requestId' = ?`, relayState, req.ID); n != 1 {
		t.Fatalf("%d request rows", n)
	}
	if n := e.count(`SELECT count(*) FROM verifications WHERE purpose = 'oauth_state' AND token_hash = sha256(?::bytea) AND payload->>'provider' = 'saml'`, c.Value); n != 1 {
		t.Fatalf("%d state rows", n)
	}

	t.Run("options offers SAML after the OAuth providers", func(t *testing.T) {
		e := newEnv(t, withProvider(providerGoogle), withSAML)
		got := e.browser().get("/auth/options").json()["ssoProviders"].([]any)
		if len(got) != 2 || got[0] != "google" || got[1] != "saml" {
			t.Fatalf("ssoProviders = %v", got)
		}
	})

	t.Run("without SAML_DATABASE_URL SAML is not a provider", func(t *testing.T) {
		e := newEnv(t)
		expectStatus(t, e.browser().post("/auth/sso/sign-in", map[string]string{"provider": "saml"}), http.StatusNotFound, "oauth_provider_not_found")
	})

	t.Run("without a connection the sign-in reports it", func(t *testing.T) {
		e := newEnv(t, withSAML)
		expectStatus(t, e.browser().post("/auth/sso/sign-in", map[string]string{"provider": "saml"}), http.StatusNotFound, CodeSAMLConnection)
	})
}

func TestSAMLSignUp(t *testing.T) {
	t.Parallel()
	e := samlEnv(t)
	b := e.browser()
	f := b.samlStart("/surveys?x=1")

	ack := e.postACS(f.spec().signedResponse(t), f.relayState)
	loc, _ := url.Parse(ack.Header().Get("Location"))
	if ack.Code != http.StatusSeeOther || loc.Path != samlCallbackPath || loc.Query().Get("state") != f.relayState || len(loc.Query().Get("code")) < 43 {
		t.Fatalf("consumer service answered %d to %q", ack.Code, ack.Header().Get("Location"))
	}
	expectRedirect(t, b.follow(ack), webApp+"/surveys?x=1")
	if !b.has("forma.session_token") || b.has("forma.oauth_state") {
		t.Fatalf("cookies after callback: %v", b.cookies)
	}
	// forma2's never-attests policy: the user SAML creates is verified.
	u := e.userByEmail("ada@example.com")
	if u == nil || u.Name != "Ada Lovelace" || !u.EmailVerified || u.Locale != "ja-JP" {
		t.Fatalf("user %+v", u)
	}
	if n := e.count(`SELECT count(*) FROM user_identities WHERE user_id = ? AND provider = 'saml' AND issuer = '' AND subject = 'ada@example.com'`, u.ID); n != 1 {
		t.Fatalf("%d saml identities", n)
	}

	t.Run("a linked sign-in syncs the name and never verifies the address", func(t *testing.T) {
		e.exec(`UPDATE users SET email_verified = false WHERE id = ?`, u.ID)
		b := e.browser()
		f := b.samlStart("")
		spec := f.spec()
		spec.attributes = [][2]string{{"email", "ADA@example.com"}, {"name", "Ada King"}, {"firstName", "Ignored"}}
		expectRedirect(t, f.complete(spec.signedResponse(t)), appBase+"/")
		got := e.userByEmail("ada@example.com")
		if got.ID != u.ID || got.Name != "Ada King" || got.EmailVerified || !b.has("forma.session_token") {
			t.Fatalf("user %+v", got)
		}
	})

	t.Run("the legacy callback path finishes the flow too", func(t *testing.T) {
		b := e.browser()
		f := b.samlStart("")
		ack := e.postACS(f.spec().signedResponse(t), f.relayState)
		loc, _ := url.Parse(ack.Header().Get("Location"))
		r := b.request(http.MethodGet, "/api/auth/oauth2/callback/saml?"+loc.RawQuery, nil, nil)
		expectRedirect(t, r, appBase+"/")
		if !b.has("forma.session_token") {
			t.Fatal("no session")
		}
	})
}

// An address that already has an account is linked only through forma2's verify-before-link recovery, as for every provider.
func TestSAMLRecovery(t *testing.T) {
	t.Parallel()
	e := samlEnv(t)
	e.seedUser("grace@example.com", strongPassword, userOpts{})
	b := e.browser()
	f := b.samlStart("")
	spec := f.spec()
	spec.nameID, spec.attributes = "u-42", [][2]string{{samlAttributeEmailURI, "Grace@example.com"}}
	expectRedirect(t, f.complete(spec.signedResponse(t)), appBase+"/auth/verification-requested?purpose=sso_recovery")
	if b.has("forma.session_token") || e.count(`SELECT count(*) FROM user_identities WHERE provider = 'saml'`) != 0 {
		t.Fatal("the identity was linked before the mailbox was proven")
	}
	token := e.recoveryToken("grace@example.com")
	done := e.browser().post("/auth/sso/recovery/complete", map[string]string{"token": token})
	expectStatus(t, done, http.StatusOK, "")
	if n := e.count(`SELECT count(*) FROM user_identities WHERE provider = 'saml' AND subject = 'u-42'`); n != 1 {
		t.Fatalf("%d saml identities after recovery", n)
	}
}

// Attribute mapping: Jackson's names and claim URIs, forma2's name precedence, and the NameID as subject.
func TestIdentityFromAssertion(t *testing.T) {
	assertion := func(format, nameID string, attrs ...[2]string) *saml.Assertion {
		a := &saml.Assertion{Subject: &saml.Subject{NameID: &saml.NameID{Format: format, Value: nameID}}}
		var list []saml.Attribute
		for _, kv := range attrs {
			list = append(list, saml.Attribute{Name: kv[0], Values: []saml.AttributeValue{{Value: kv[1]}}})
		}
		a.AttributeStatements = []saml.AttributeStatement{{Attributes: list}}
		return a
	}
	email := string(saml.EmailAddressNameIDFormat)
	cases := []struct {
		name string
		in   *saml.Assertion
		want ssoIdentity
	}{
		{"name wins over first and last", assertion(email, "7", [2]string{"email", "eve@example.com"}, [2]string{"name", "Eve N."}, [2]string{"firstName", "Eve"}, [2]string{"lastName", "Ng"}),
			ssoIdentity{Subject: "7", Email: "eve@example.com", Name: "Eve N."}},
		{"first and last joined", assertion(email, "7", [2]string{"email", "eve@example.com"}, [2]string{"firstName", "Eve"}, [2]string{"lastName", "Ng"}),
			ssoIdentity{Subject: "7", Email: "eve@example.com", Name: "Eve Ng"}},
		{"an empty part is dropped", assertion(email, "7", [2]string{"email", "eve@example.com"}, [2]string{"lastName", "Ng"}),
			ssoIdentity{Subject: "7", Email: "eve@example.com", Name: "Ng"}},
		{"claim URIs", assertion("", "u-1", [2]string{samlAttributeEmailURI, "eve@example.com"}, [2]string{samlAttributeGivenNameURI, "Eve"}, [2]string{samlAttributeSurnameURI, "Ng"}),
			ssoIdentity{Subject: "u-1", Email: "eve@example.com", Name: "Eve Ng"}},
		{"an emailAddress NameID is the address when no attribute carries one", assertion(email, " eve@example.com "),
			ssoIdentity{Subject: "eve@example.com", Email: "eve@example.com"}},
		{"another NameID format is not an address", assertion(string(saml.PersistentNameIDFormat), "u-1"), ssoIdentity{Subject: "u-1"}},
		{"no NameID, no subject", &saml.Assertion{Subject: &saml.Subject{}}, ssoIdentity{}},
	}
	for _, tc := range cases {
		if got := identityFromAssertion(tc.in); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// Every response that is not a signed, current answer to this browser's own request, for this service provider, fails closed: no user, no identity, no session.
func TestSAMLResponseRejected(t *testing.T) {
	t.Parallel()
	e := samlEnv(t)
	past := time.Now().Add(-10 * time.Minute)
	stranger := testSAMLKeys()[1]

	cases := []struct {
		name string
		// build returns the encoded response for the flow.
		build func(t *testing.T, f samlFlow) string
		want  string
	}{
		{"unsigned", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.signAssertion, s.signResponse = false, false
			return s.signedResponse(t)
		}, "invalid_code"},
		{"signed by another key", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.signer = stranger
			return s.signedResponse(t)
		}, "invalid_code"},
		{"tampered after signing", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.signResponse = false
			a := s.assertion(t)
			a.FindElement("./Subject/NameID").SetText("eve@example.com")
			return encode(t, s.response(t, a))
		}, "invalid_code"},
		{"tampered attribute inside a signed response", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			r := s.response(t, s.assertion(t))
			r.FindElement("./Assertion/AttributeStatement/Attribute/AttributeValue").SetText("eve@example.com")
			return encode(t, r)
		}, "invalid_code"},
		{"wrapped: a forged assertion beside the signed one, inside an unsigned response", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.signResponse = false
			signed := s.assertion(t)
			forged := signed.Copy()
			forged.FindElement("./Subject/NameID").SetText("eve@example.com")
			forged.RemoveChild(forged.FindElement("./Signature"))
			// The genuine assertion hides inside the forged one, where its signature still verifies.
			forged.AddChild(signed)
			return encode(t, s.response(t, forged))
		}, "invalid_code"},
		{"wrapped: the signature moved onto a forged assertion that carries the original", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.signResponse = false
			signed := s.assertion(t)
			forged := signed.Copy()
			forged.FindElement("./Subject/NameID").SetText("eve@example.com")
			sig := forged.FindElement("./Signature")
			original := signed.Copy()
			original.RemoveChild(original.FindElement("./Signature"))
			obj := sig.CreateElement("ds:Object")
			obj.AddChild(original)
			return encode(t, s.response(t, forged))
		}, "invalid_code"},
		{"wrapped: a forged assertion added to a signed response", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.signAssertion = false
			r := s.response(t, s.assertion(t))
			forged := r.FindElement("./Assertion").Copy()
			forged.FindElement("./Subject/NameID").SetText("eve@example.com")
			r.InsertChildAt(0, forged)
			return encode(t, r)
		}, "invalid_code"},
		{"for another service provider", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.audience = "https://sp.example.com"
			return s.signedResponse(t)
		}, "invalid_code"},
		{"without an audience restriction", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.audience = "-"
			return s.signedResponse(t)
		}, "invalid_code"},
		{"for another recipient", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.recipient = "https://evil.example.com/acs"
			return s.signedResponse(t)
		}, "invalid_code"},
		{"for another destination", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.destination = "https://evil.example.com/acs"
			return s.signedResponse(t)
		}, "invalid_code"},
		{"from another issuer", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.issuer = "https://evil.example.com"
			return s.signedResponse(t)
		}, "invalid_code"},
		{"answering another request", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.requestID = "id-another"
			return s.signedResponse(t)
		}, "invalid_code"},
		{"expired", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.notOnOrAfter = past
			return s.signedResponse(t)
		}, "invalid_code"},
		{"issued too long ago", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.issueInstant = past
			return s.signedResponse(t)
		}, "invalid_code"},
		{"not yet valid", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.notBefore = time.Now().Add(10 * time.Minute)
			return s.signedResponse(t)
		}, "invalid_code"},
		{"without conditions", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.noConditions = true
			return s.signedResponse(t)
		}, "invalid_code"},
		{"without a bearer confirmation", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.noConfirmation = true
			return s.signedResponse(t)
		}, "invalid_code"},
		{"a failed status", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.status = "urn:oasis:names:tc:SAML:2.0:status:Requester"
			return s.signedResponse(t)
		}, "invalid_code"},
		{"not base64", func(t *testing.T, f samlFlow) string { return "%%%" }, "invalid_code"},
		{"not XML", func(t *testing.T, f samlFlow) string { return base64.StdEncoding.EncodeToString([]byte("<a>")) }, "invalid_code"},
		{"without an address", func(t *testing.T, f samlFlow) string {
			s := f.spec()
			s.nameIDFormat = string(saml.PersistentNameIDFormat)
			s.attributes = [][2]string{{"firstName", "Ada"}}
			return s.signedResponse(t)
		}, "email_not_found"},
	}
	// Each case fails for its own reason, not an earlier one; the log names it.
	reasons := map[string]string{
		"unsigned":               "signature element not present",
		"signed by another key":  "Could not verify certificate against trusted certs",
		"tampered after signing": "cannot validate signature on Assertion",
		"tampered attribute inside a signed response":                                    "cannot validate signature on Response",
		"wrapped: a forged assertion beside the signed one, inside an unsigned response": "signature element not present",
		"wrapped: the signature moved onto a forged assertion that carries the original": "cannot validate signature on Assertion",
		"wrapped: a forged assertion added to a signed response":                         "cannot validate signature on Response",
		"for another service provider":                                                   "assertion audience is",
		"without an audience restriction":                                                "assertion has no audience restriction",
		"for another recipient":                                                          "Recipient is not",
		"for another destination":                                                        "Destination",
		"from another issuer":                                                            "response Issuer does not match",
		"answering another request":                                                      "InResponseTo",
		"expired":                                                                        "is expired",
		"issued too long ago":                                                            "IssueInstant expired",
		"not yet valid":                                                                  "not yet valid",
		"without conditions":                                                             "malformed assertion",
		"without a bearer confirmation":                                                  "no bearer subject confirmation",
		"a failed status":                                                                "status:Requester",
		"not base64":                                                                     "SAML response is not base64",
		"not XML":                                                                        "invalid XML",
		"without an address":                                                             "",
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := e.browser()
			f := b.samlStart("")
			before := len(e.logs.String())
			r := f.b.e.postACS(tc.build(t, f), f.relayState)
			expectSeeOther(t, r, loginError(tc.want))
			if logged := e.logs.String()[before:]; !strings.Contains(logged, reasons[tc.name]) {
				t.Fatalf("logged %s, want the reason %q", logged, reasons[tc.name])
			}
			if b.has("forma.session_token") {
				t.Fatal("a session was set")
			}
		})
	}
	if e.count(`SELECT count(*) FROM users`) != 0 || e.count(`SELECT count(*) FROM user_identities`) != 0 || e.count(`SELECT count(*) FROM verifications WHERE purpose = 'saml_assertion'`) != 0 {
		t.Fatal("a rejected response created something")
	}
	if strings.Contains(e.logs.String(), "eve@example.com") || strings.Contains(e.logs.String(), "ada@example.com") {
		t.Fatal("a rejected response's content reached the log")
	}
}

// The request a response answers is consumed with it, and the code it yields works once, only in the browser whose state cookie started the flow.
func TestSAMLReplay(t *testing.T) {
	t.Parallel()
	e := samlEnv(t)

	b := e.browser()
	f := b.samlStart("")
	body := f.spec().signedResponse(t)
	ack := e.postACS(body, f.relayState)
	if ack.Code != http.StatusSeeOther {
		t.Fatalf("status %d", ack.Code)
	}

	t.Run("the same response again", func(t *testing.T) {
		expectSeeOther(t, e.postACS(body, f.relayState), loginError("please_restart_the_process"))
	})

	t.Run("the same response for another flow", func(t *testing.T) {
		other := e.browser().samlStart("")
		expectSeeOther(t, e.postACS(body, other.relayState), loginError("invalid_code"))
		// The other flow's request was spent on the replay.
		expectSeeOther(t, e.postACS(other.spec().signedResponse(t), other.relayState), loginError("please_restart_the_process"))
	})

	t.Run("an IdP-initiated response", func(t *testing.T) {
		expectSeeOther(t, e.postACS(body, ""), loginError("please_restart_the_process"))
	})

	t.Run("the code in a browser without the state cookie", func(t *testing.T) {
		expectRedirect(t, e.browser().follow(ack), loginError("please_restart_the_process"))
	})

	t.Run("the code with another flow's state cookie", func(t *testing.T) {
		other := e.browser()
		other.samlStart("")
		expectRedirect(t, other.follow(ack), loginError("state_mismatch"))
	})

	t.Run("the code in its browser, then again", func(t *testing.T) {
		b := e.browser()
		f := b.samlStart("")
		ack := e.postACS(f.spec().signedResponse(t), f.relayState)
		expectRedirect(t, b.follow(ack), appBase+"/")
		expectRedirect(t, b.follow(ack), loginError("please_restart_the_process"))
		loc, _ := url.Parse(ack.Header().Get("Location"))
		if n := e.count(`SELECT count(*) FROM verifications WHERE purpose = 'saml_assertion' AND token_hash = sha256(?::bytea) AND consumed_at IS NULL`, loc.Query().Get("code")); n != 0 {
			t.Fatal("the code is still redeemable")
		}
	})

	t.Run("a code for one flow cannot finish another", func(t *testing.T) {
		b := e.browser()
		f := b.samlStart("")
		ack := e.postACS(f.spec().signedResponse(t), f.relayState)
		loc, _ := url.Parse(ack.Header().Get("Location"))
		other := e.browser()
		otherFlow := other.samlStart("")
		r := other.request(http.MethodGet, samlCallbackPath+"?"+url.Values{"code": {loc.Query().Get("code")}, "state": {otherFlow.relayState}}.Encode(), nil, nil)
		expectRedirect(t, r, loginError("state_mismatch"))
		if other.has("forma.session_token") {
			t.Fatal("a session was set")
		}
	})
}

func TestSAMLDisabled(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.postACS("x", "y")
	if r.Code != http.StatusForbidden || r.Header().Get("Cache-Control") != "private, no-store" ||
		r.Body.String() != `{"code":"forbidden","message":"SAML SSO is not enabled in your Forma license","details":{}}` {
		t.Fatalf("got %d %s", r.Code, r.Body.String())
	}
	if r := e.browser().request(http.MethodGet, "/api/auth/saml/metadata", nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("metadata status %d", r.Code)
	}
	if r := e.browser().request(http.MethodGet, "/api/auth/oauth2/callback/saml", nil, nil); r.Code != http.StatusFound || r.Header().Get("Location") != loginError("oauth_provider_not_found") {
		t.Fatalf("legacy callback %d %q", r.Code, r.Header().Get("Location"))
	}
}

func TestSAMLMetadata(t *testing.T) {
	t.Parallel()
	e := newEnv(t, withSAML)
	r := e.browser().request(http.MethodGet, "/api/auth/saml/metadata", nil, nil)
	if r.Code != http.StatusOK || !strings.HasPrefix(r.Header().Get("Content-Type"), "application/samlmetadata+xml") {
		t.Fatalf("got %d %s", r.Code, r.Header().Get("Content-Type"))
	}
	var md saml.EntityDescriptor
	if err := xml.Unmarshal(r.Body.Bytes(), &md); err != nil {
		t.Fatal(err)
	}
	if md.EntityID != SAMLEntityID || len(md.SPSSODescriptors) != 1 {
		t.Fatalf("metadata %s", r.Body.String())
	}
	sp := md.SPSSODescriptors[0]
	if len(sp.AssertionConsumerServices) != 1 || sp.AssertionConsumerServices[0].Location != testACSURL || sp.AssertionConsumerServices[0].Binding != saml.HTTPPostBinding ||
		sp.WantAssertionsSigned == nil || !*sp.WantAssertionsSigned {
		t.Fatalf("SP descriptor %s", r.Body.String())
	}
}

func TestParseIdPMetadata(t *testing.T) {
	good := idpMetadata(t)
	if md, err := parseIdPMetadata([]byte("\n  " + good)); err != nil || md.EntityID != testIdPEntityID {
		t.Fatalf("EntityDescriptor: %v", err)
	}
	group := `<md:EntitiesDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata"><md:EntityDescriptor entityID="https://sp.example.com"><md:SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"/></md:EntityDescriptor>` +
		strings.TrimPrefix(good, xml.Header) + `</md:EntitiesDescriptor>`
	if md, err := parseIdPMetadata([]byte(group)); err != nil || md.EntityID != testIdPEntityID {
		t.Fatalf("EntitiesDescriptor: %v", err)
	}
	for name, raw := range map[string]string{
		"not XML":              "connection",
		"POST binding only":    idpMetadata(t, saml.HTTPPostBinding),
		"no signing key":       strings.Replace(good, `use="signing"`, `use="encryption"`, 1),
		"a broken key":         strings.Replace(good, base64.StdEncoding.EncodeToString(testSAMLKeys()[0].cert.Raw), "AAAA", 1),
		"no entityID":          strings.Replace(good, `entityID="`+testIdPEntityID+`"`, `entityID=""`, 1),
		"no identity provider": `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="x"><md:SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"/></md:EntityDescriptor>`,
	} {
		if _, err := parseIdPMetadata([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// forma2's preload-connection.test.ts: the first XML file becomes the connection, replacing the previous one; anything wrong is logged and leaves the stored connection alone.
func TestPreloadSAMLConnection(t *testing.T) {
	t.Parallel()
	e := newEnv(t, withSAML)
	ctx := context.Background()
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stored := func() *samlConnection {
		c, err := e.svc.repo.samlConnection(ctx, samlTenant, samlProduct)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	e.sso.PreloadSAMLConnection(ctx, filepath.Join(dir, "missing"))
	e.sso.PreloadSAMLConnection(ctx, dir)
	if stored() != nil || strings.Count(e.logs.String(), "Error preloading connection") != 2 {
		t.Fatalf("logs %s", e.logs.String())
	}

	write("notes.txt", "ignored")
	write("b.xml", strings.Replace(idpMetadata(t), testIdPEntityID, "https://other.example.com", 1))
	write("a.xml", idpMetadata(t))
	e.sso.PreloadSAMLConnection(ctx, dir)
	c := stored()
	if c == nil || c.IdPEntityID != testIdPEntityID || c.Name != "SAML SSO" {
		t.Fatalf("connection %+v", c)
	}

	t.Run("a new file replaces the connection", func(t *testing.T) {
		if err := os.Remove(filepath.Join(dir, "a.xml")); err != nil {
			t.Fatal(err)
		}
		e.sso.PreloadSAMLConnection(ctx, dir)
		if c := stored(); c == nil || c.IdPEntityID != "https://other.example.com" || e.count(`SELECT count(*) FROM saml_connections`) != 1 {
			t.Fatalf("connection %+v", c)
		}
	})

	t.Run("invalid metadata keeps the stored connection", func(t *testing.T) {
		write("a.xml", "<EntityDescriptor/>")
		e.sso.PreloadSAMLConnection(ctx, dir)
		if c := stored(); c == nil || c.IdPEntityID != "https://other.example.com" || !strings.Contains(e.logs.String(), "metadata is not valid (a.xml)") {
			t.Fatalf("connection %+v, logs %s", c, e.logs.String())
		}
	})

	t.Run("an empty file is skipped", func(t *testing.T) {
		write("a.xml", "  \n")
		e.sso.PreloadSAMLConnection(ctx, dir)
		if c := stored(); c == nil || !strings.Contains(e.logs.String(), "No preloaded connection metadata found") {
			t.Fatalf("connection %+v", c)
		}
	})

	t.Run("with SAML off nothing is read", func(t *testing.T) {
		off := newEnv(t)
		write("a.xml", idpMetadata(t))
		off.sso.PreloadSAMLConnection(ctx, dir)
		if off.count(`SELECT count(*) FROM saml_connections`) != 0 {
			t.Fatal("a connection was stored")
		}
	})
}
