package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/billing"
	"github.com/yldm-tech/forma/server/internal/invite"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/mailer/mailertest"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/platform/valkey/valkeytest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const (
	webApp = "http://app.example.com"
	// strongPassword satisfies the policy and is not in the stubbed breach corpus.
	strongPassword = "Correct-Horse-9"
)

// env is one isolated instance of the module: a fresh database and Valkey, a captured mailer and log, stubbed Have I Been Pwned and Turnstile services, a controllable clock, and the HTTP routes as the server mounts them.
type env struct {
	t         *testing.T
	cfg       config.Config
	db        *gorm.DB
	rdb       *redis.Client
	mail      *mailertest.Transport
	logs      *syncBuffer
	svc       *Service
	sessions  *Sessions
	twoFactor *TwoFactor
	sso       *SSO
	engine    *gin.Engine

	// breached holds the SHA-1 suffixes the stubbed range API reports; hibpStatus overrides its status.
	breached   map[string]bool
	hibpStatus int
	hibpCalls  int
	// turnstileOK is the stubbed Turnstile verdict.
	turnstileOK bool

	mu    sync.Mutex
	clock time.Time
	// totp holds the second factor seeded for each user.
	totp map[uuid.UUID]TOTPMaterial
}

// syncBuffer lets handlers log while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func baseConfig() config.Config {
	u, _ := url.Parse(webApp)
	return config.Config{
		WebAppURL:                         u,
		RateLimitingDisabled:              true,
		TrustedProxyHopCount:              1,
		AuditLogEnabled:                   true,
		AuditLogGetUserIP:                 true,
		MailFrom:                          "noreply@forma.ylam.ai",
		MailFromName:                      "Forma",
		SMTPHost:                          "smtp.example.com",
		SMTPPort:                          "587",
		PasswordResetTokenLifetimeMinutes: 30,
		SessionMaxAge:                     86400,
		EncryptionKey:                     bytes.Repeat([]byte{0x42}, 32),
		EncryptionKeyVersion:              1,
	}
}

func newEnv(t *testing.T, configure ...func(*config.Config)) *env {
	t.Helper()
	cfg := baseConfig()
	for _, f := range configure {
		f(&cfg)
	}
	e := &env{t: t, cfg: cfg, db: pgtest.GORM(t), rdb: valkeytest.Client(t), mail: &mailertest.Transport{}, logs: &syncBuffer{}, breached: map[string]bool{}, turnstileOK: true, clock: time.Now(), totp: map[uuid.UUID]TOTPMaterial{}}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	var transport mailer.Transport = e.mail
	if !cfg.SMTPConfigured() {
		transport = nil
	}
	m, err := mailer.NewWithTransport(cfg, transport, log)
	if err != nil {
		t.Fatal(err)
	}
	e.sessions = NewSessions(cfg, e.db, log)
	e.sessions.now = e.now
	limiter, emitter := ratelimit.New(cfg, e.rdb, log), audit.New(cfg, log)
	// Sign-up redeems invites through the real invite module; nothing on that path asks SpiceDB, so it runs without one.
	invites := invite.NewService(cfg, invite.NewRepository(e.db), authz.NewAuthorizer(cfg, e.db, nil), limiter, m, emitter, log)
	enqueuer, err := jobs.NewEnqueuer(log)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := billing.NewLifecycle(cfg, billing.NewStripe(cfg, log), e.db, enqueuer, log)
	e.svc = NewService(cfg, NewRepository(e.db), e.sessions, invites, limiter, m, emitter, e.rdb, lifecycle, log)
	e.svc.now = e.now
	e.svc.failures.now = e.now
	cipher, err := secret.NewCipher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.twoFactor = NewTwoFactor(cfg, e.svc, cipher)

	hibp := httptest.NewServer(http.HandlerFunc(e.serveRange))
	t.Cleanup(hibp.Close)
	e.svc.breaches.rangeURL = hibp.URL + "/range/"
	turnstile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		e.mu.Lock()
		ok := e.turnstileOK
		e.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": ok})
	}))
	t.Cleanup(turnstile.Close)
	e.svc.turnstile.verifyURL = turnstile.URL

	r := gin.New()
	r.Use(httpapi.NewClientIPResolver(cfg, log).Middleware())
	group := r.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg))
	NewHandler(cfg, e.svc, e.sessions, e.twoFactor).Register(group)
	e.sso = NewSSO(cfg, e.svc, log)
	ssoHandler := NewSSOHandler(cfg, e.sso)
	ssoHandler.Register(group)
	ssoHandler.RegisterCallbacks(r)
	group.POST("/test/protected", e.sessions.Require(), func(c *gin.Context) {
		a, _ := httpapi.ActorFrom(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"userId": a.UserID, "sessionId": a.SessionID})
	})
	group.GET("/test/optional", e.sessions.Optional(), func(c *gin.Context) {
		_, ok := httpapi.ActorFrom(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"authenticated": ok})
	})
	e.engine = r
	return e
}

func (e *env) now() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clock
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clock = e.clock.Add(d)
}

func (e *env) serveRange(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	e.hibpCalls++
	status := e.hibpStatus
	e.mu.Unlock()
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	prefix := strings.TrimPrefix(r.URL.Path, "/range/")
	var body strings.Builder
	body.WriteString("0018A45C4D1DEF81644B54AB7F969B88D65:1\r\n")
	for suffix := range e.breached {
		if strings.HasPrefix(suffix, prefix) {
			body.WriteString(strings.TrimPrefix(suffix, prefix) + ":42\r\n")
		}
	}
	_, _ = io.WriteString(w, body.String())
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func (e *env) setTurnstile(ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.turnstileOK = ok
}

func (e *env) setHIBPStatus(status int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hibpStatus = status
}

func (e *env) hibpCallCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hibpCalls
}

// breach adds a password to the stubbed corpus.
func (e *env) breach(password string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.breached[sha1Hex(password)] = true
}

// enableRateLimits turns the limiter on, against this env's Valkey, with its clock frozen. Windows are aligned to the wall clock, so a scenario that straddled a boundary on a busy machine saw its count split across two windows. The clock is frozen at the start of a UTC day, which starts a window of every rule length that divides a day: the limiter derives a key's TTL from its clock while Valkey expires the key in real time, so a frozen instant near a window's end would expire the count mid-scenario, and the start of a window gives each key the full window to live.
func (e *env) enableRateLimits() {
	cfg := e.cfg
	cfg.RateLimitingDisabled = false
	frozen := time.Now().UTC().Truncate(24 * time.Hour)
	e.svc.limiter = ratelimit.New(cfg, e.rdb, slog.New(slog.NewTextHandler(io.Discard, nil))).WithClock(func() time.Time { return frozen })
}

// inOneWindow runs a rate-limit scenario once; enableRateLimits froze the limiter's clock, so every request lands in the same window however long the run takes. The scenario reports a failed expectation as an error.
func (e *env) inOneWindow(scenario func(run int, ip string) error) {
	e.t.Helper()
	if err := scenario(0, "198.51.100.10"); err != nil {
		e.t.Fatal(err)
	}
}

// statusErr reports a response that does not have the status and, when given, the error code.
func statusErr(r response, status int, code string) error {
	if r.Code != status {
		return fmt.Errorf("status %d, want %d: %s", r.Code, status, r.Body.String())
	}
	if code != "" && r.code() != code {
		return fmt.Errorf("code %q, want %q: %s", r.code(), code, r.Body.String())
	}
	return nil
}

// repeat sends the same request n times and expects the same outcome each time.
func repeat(n int, send func() response, status int, code string) error {
	for i := 0; i < n; i++ {
		if err := statusErr(send(), status, code); err != nil {
			return fmt.Errorf("request %d: %w", i+1, err)
		}
	}
	return nil
}

// browser is one cookie jar talking to the routes from one client address.
type browser struct {
	e       *env
	ip      string
	cookies map[string]*http.Cookie
}

func (e *env) browser() *browser {
	return &browser{e: e, ip: "203.0.113.10", cookies: map[string]*http.Cookie{}}
}

type response struct {
	*httptest.ResponseRecorder
	t *testing.T
}

func (r response) json() map[string]any {
	r.t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		r.t.Fatalf("body %q: %v", r.Body.String(), err)
	}
	return m
}

func (r response) code() string {
	r.t.Helper()
	code, _ := r.json()["code"].(string)
	return code
}

// cookie returns the Set-Cookie header the response sent for name, or nil.
func (r response) cookie(name string) *http.Cookie {
	for _, c := range r.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (b *browser) do(method, path string, body any) response {
	b.e.t.Helper()
	return b.request(method, "/api/app"+path, body, nil)
}

// request sends to any path of the server, with extra headers.
func (b *browser) request(method, target string, body any, header http.Header) response {
	b.e.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			b.e.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, reader)
	for k, v := range header {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", b.e.cfg.WebAppURL.Scheme+"://"+b.e.cfg.WebAppURL.Host)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	req.Header.Set("X-Forwarded-For", b.ip)
	req.Header.Set("User-Agent", "forma-test")
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	b.e.engine.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
			continue
		}
		b.cookies[c.Name] = &http.Cookie{Name: c.Name, Value: c.Value}
	}
	return response{ResponseRecorder: rec, t: b.e.t}
}

func (b *browser) post(path string, body any) response {
	b.e.t.Helper()
	return b.do(http.MethodPost, path, body)
}

func (b *browser) get(path string) response {
	b.e.t.Helper()
	return b.do(http.MethodGet, path, nil)
}

func (b *browser) has(cookie string) bool {
	_, ok := b.cookies[cookie]
	return ok
}

// expectStatus fails the test unless the response has the status and, when given, the error code.
func expectStatus(t *testing.T, r response, status int, code string) {
	t.Helper()
	if err := statusErr(r, status, code); err != nil {
		t.Fatal(err)
	}
}

// userOpts shape a seeded account.
type userOpts struct {
	name       string
	unverified bool
	inactive   bool
	noPassword bool
	sso        bool
	twoFactor  bool
	locale     string
}

// seedUser writes an account directly, bypassing sign-up.
func (e *env) seedUser(email, password string, o userOpts) uuid.UUID {
	e.t.Helper()
	ctx := context.Background()
	if o.name == "" {
		o.name = "Ada Lovelace"
	}
	if o.locale == "" {
		o.locale = "en-US"
	}
	repo := NewRepository(e.db)
	id, created, err := repo.insertUser(ctx, o.name, email, o.locale)
	if err != nil || !created {
		e.t.Fatalf("seed user %s: created %v, %v", email, created, err)
	}
	e.exec(`UPDATE users SET email_verified = ?, is_active = ? WHERE id = ?`, !o.unverified, !o.inactive, id)
	if !o.noPassword {
		hash, err := secret.HashPassword(password)
		if err != nil {
			e.t.Fatal(err)
		}
		if err := repo.setPassword(ctx, id, hash, time.Now()); err != nil {
			e.t.Fatal(err)
		}
	}
	if o.sso {
		e.exec(`INSERT INTO user_identities (user_id, provider, issuer, subject) VALUES (?, 'google', '', ?)`, id, "google-"+id.String())
	}
	if o.twoFactor {
		e.enrolTOTP(id, true)
	}
	return id
}

func (e *env) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
}

// insertID runs an INSERT ... RETURNING id.
func (e *env) insertID(query string, args ...any) uuid.UUID {
	e.t.Helper()
	id, err := e.svc.repo.scanID(context.Background(), query, args...)
	if err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return id
}

func (e *env) count(query string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.Raw(query, args...).Scan(&n).Error; err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return n
}

// signIn signs a browser in and fails the test unless it worked.
func (b *browser) signIn(email, password string) {
	b.e.t.Helper()
	r := b.post("/auth/sign-in", map[string]string{"email": email, "password": password})
	expectStatus(b.e.t, r, http.StatusOK, "")
	if r.json()["status"] != "signed_in" {
		b.e.t.Fatalf("sign-in: %s", r.Body.String())
	}
}

var tokenPattern = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

// sent returns the messages delivered to an address.
func (e *env) sent(to string) []mailer.Message {
	var out []mailer.Message
	for _, m := range e.mail.Messages() {
		if m.To == to {
			out = append(out, m)
		}
	}
	return out
}

// lastToken extracts the token from the last mail to an address whose subject matches.
func (e *env) lastToken(to, subject string) string {
	e.t.Helper()
	msgs := e.sent(to)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Subject != subject {
			continue
		}
		m := tokenPattern.FindStringSubmatch(msgs[i].HTML)
		if m == nil {
			e.t.Fatalf("no token in %q", msgs[i].Subject)
		}
		return m[1]
	}
	e.t.Fatalf("no %q mail to %s among %d", subject, to, len(msgs))
	return ""
}

const (
	subjectVerification = "Please verify your email to use Forma"
	subjectReset        = "Reset your Forma password"
	subjectChanged      = "Your Forma password has been changed"
	subjectEmailChange  = "Email change verification"
	subjectDeletion     = "Confirm your Forma account deletion"
)

// auditEvents returns the audit records logged so far with the given action.
func (e *env) auditEvents(action audit.Action) []map[string]any {
	e.t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(e.logs.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			e.t.Fatalf("log line %q: %v", line, err)
		}
		if m["msg"] == "audit" && m["action"] == string(action) {
			out = append(out, m)
		}
	}
	return out
}

func field(m map[string]any, path ...string) any {
	var v any = m
	for _, p := range path {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[p]
	}
	return v
}

// enrolTOTP writes a second factor for the user with a fresh key and ten backup codes, as setup (verified false) or enable (verified true) leaves it, and remembers it for totpCode.
func (e *env) enrolTOTP(id uuid.UUID, verified bool) {
	e.t.Helper()
	codes, err := newBackupCodes()
	if err != nil {
		e.t.Fatal(err)
	}
	key := make([]byte, totpKeySize)
	if _, err := rand.Read(key); err != nil {
		e.t.Fatal(err)
	}
	m := TOTPMaterial{Key: key, BackupCodes: codes}
	sealedKey, sealedCodes, err := SealTOTP(e.twoFactor.cipher, id, m)
	if err != nil {
		e.t.Fatal(err)
	}
	e.exec(`INSERT INTO user_totp (user_id, secret_ciphertext, backup_codes_ciphertext, verified) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET secret_ciphertext = excluded.secret_ciphertext, backup_codes_ciphertext = excluded.backup_codes_ciphertext, verified = excluded.verified`,
		id, sealedKey, sealedCodes, verified)
	e.mu.Lock()
	e.totp[id] = m
	e.mu.Unlock()
}

// totpCode is the user's authenticator code steps periods away from the env's clock.
func (e *env) totpCode(id uuid.UUID, steps int) string {
	e.t.Helper()
	e.mu.Lock()
	m, ok := e.totp[id]
	e.mu.Unlock()
	if !ok {
		e.t.Fatalf("no second factor seeded for %s", id)
	}
	return totpCodeAt(e.t, m.Key, e.now().Add(time.Duration(steps)*totpPeriod*time.Second))
}

func totpCodeAt(t *testing.T, key []byte, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(base32NoPadding.EncodeToString(key), at, totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// signInPending runs the password step for ada@example.com, seeded with strongPassword and a second factor, and leaves the browser holding the pending sign-in.
func (b *browser) signInPending() {
	b.e.t.Helper()
	r := b.post("/auth/sign-in", credentials("ada@example.com", strongPassword))
	expectStatus(b.e.t, r, http.StatusOK, "")
	if r.json()["status"] != "two_factor_required" {
		b.e.t.Fatalf("sign-in: %s", r.Body.String())
	}
}
