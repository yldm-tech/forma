package auth

import (
	"bytes"
	"encoding/base32"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
)

// setupTOTP runs setup for a signed-in browser and returns the response body.
func setupTOTP(t *testing.T, b *browser, password string) map[string]any {
	t.Helper()
	r := b.post("/auth/two-factor/setup", map[string]string{"password": password})
	expectStatus(t, r, http.StatusOK, "")
	return r.json()
}

// codeFor computes the authenticator code for a base32 secret from setup, steps periods away from the env's clock.
func (e *env) codeFor(secret string, steps int) string {
	e.t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		e.t.Fatal(err)
	}
	return totpCodeAt(e.t, key, e.now().Add(time.Duration(steps)*totpPeriod*time.Second))
}

func (e *env) twoFactorEnabled(id uuid.UUID) bool {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM user_totp WHERE user_id = ? AND verified`, id) == 1
}

func (e *env) failedAttempts(id uuid.UUID) int64 {
	e.t.Helper()
	return e.count(`SELECT failed_attempts FROM user_totp WHERE user_id = ?`, id)
}

func verifyTOTP(b *browser, code string) response {
	b.e.t.Helper()
	return b.post("/auth/two-factor/verify-totp", map[string]string{"code": code})
}

func verifyBackup(b *browser, code string) response {
	b.e.t.Helper()
	return b.post("/auth/two-factor/verify-backup-code", map[string]string{"code": code})
}

// forma2's two-factor-auth.test.ts success paths and auth-two-factor.integration.test.ts: setup returns a 32-character secret, the otpauth URI, a QR code and ten backup codes without turning two-factor authentication on; a code from the app turns it on, and the next sign-in asks for it.
func TestTwoFactorEnrolment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)

	res := setupTOTP(t, b, strongPassword)
	secret, _ := res["secret"].(string)
	codes, _ := res["backupCodes"].([]any)
	if len(secret) != 32 || len(codes) != 10 {
		t.Fatalf("setup: secret %q, %d backup codes", secret, len(codes))
	}
	if uri, _ := res["keyUri"].(string); !strings.HasPrefix(uri, "otpauth://totp/Forma:ada@example.com?") || !strings.Contains(uri, "secret="+secret) {
		t.Fatalf("key uri %q", uri)
	}
	if data, _ := res["dataUri"].(string); !strings.HasPrefix(data, "data:image/png;base64,") {
		t.Fatalf("data uri %.40q", data)
	}
	if e.twoFactorEnabled(id) || b.get("/auth/me").json()["twoFactorEnabled"] != false {
		t.Fatal("setup alone turned two-factor authentication on")
	}
	// The secret and codes are stored sealed.
	var sealed []byte
	if err := e.db.Raw(`SELECT secret_ciphertext || backup_codes_ciphertext FROM user_totp WHERE user_id = ?`, id).Row().Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if len(sealed) == 0 || bytes.Contains(sealed, key) || bytes.Contains(sealed, []byte(codes[0].(string))) {
		t.Fatal("the secret or a backup code is stored in the clear")
	}
	// Until it is confirmed, the password alone still signs in.
	e.browser().signIn("ada@example.com", strongPassword)

	expectStatus(t, b.post("/auth/two-factor/enable", map[string]string{"code": "000000"}), http.StatusBadRequest, CodeInvalidCode)
	r := b.post("/auth/two-factor/enable", map[string]string{"code": e.codeFor(secret, 0)})
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["message"] != "Two factor authentication enabled" || !e.twoFactorEnabled(id) || b.get("/auth/me").json()["twoFactorEnabled"] != true {
		t.Fatalf("enable: %s", r.Body.String())
	}
	// Enabling keeps the current session, as in forma2.
	expectStatus(t, b.post("/test/protected", nil), http.StatusOK, "")

	other := e.browser()
	other.signInPending("ada@example.com", strongPassword)
	expectStatus(t, verifyTOTP(other, e.codeFor(secret, 0)), http.StatusOK, "")

	var changes []string
	for _, ev := range e.auditEvents(audit.ActionUpdated) {
		if field(ev, "target", "type") == string(audit.TargetTwoFactorAuth) && field(ev, "target", "id") == id.String() && field(ev, "actor", "id") == id.String() {
			changes = append(changes, fmt.Sprintf("%v:%v", ev["status"], field(ev, "changes", "twoFactorAuth")))
		}
	}
	if want := []string{"success:setup", "failure:<nil>", "success:enabled"}; !slices.Equal(changes, want) {
		t.Fatalf("audit %v, want %v", changes, want)
	}
}

// forma2's two-factor-auth.test.ts error cases for setup and enable, in forma2's order.
func TestTwoFactorSetupAndEnableRules(t *testing.T) {
	t.Parallel()
	e := newEnv(t)

	sso := e.sessionFor(e.seedUser("sso@example.com", "", userOpts{noPassword: true, sso: true}))
	expectStatus(t, sso.post("/auth/two-factor/setup", map[string]string{"password": strongPassword}), http.StatusBadRequest, CodeThirdPartyLogin)
	expectStatus(t, sso.post("/auth/two-factor/enable", map[string]string{"code": "123456"}), http.StatusBadRequest, CodeThirdPartyLogin)

	noPassword := e.sessionFor(e.seedUser("nopw@example.com", "", userOpts{noPassword: true}))
	r := noPassword.post("/auth/two-factor/setup", map[string]string{"password": strongPassword})
	expectStatus(t, r, http.StatusBadRequest, CodePasswordNotSet)
	if r.json()["message"] != "Password is not set for this user" {
		t.Fatalf("setup without password: %s", r.Body.String())
	}
	r = noPassword.post("/auth/two-factor/enable", map[string]string{"code": "123456"})
	expectStatus(t, r, http.StatusBadRequest, CodePasswordNotSet)
	if r.json()["message"] != "User does not have a password set" {
		t.Fatalf("enable without password: %s", r.Body.String())
	}

	e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	r = b.post("/auth/two-factor/setup", map[string]string{"password": "Wrong-Password-1"})
	expectStatus(t, r, http.StatusBadRequest, CodeInvalidPassword)
	if r.json()["message"] != "Incorrect password" {
		t.Fatalf("wrong password: %s", r.Body.String())
	}
	expectStatus(t, b.post("/auth/two-factor/setup", map[string]string{}), http.StatusBadRequest, apperr.CodeValidation)
	expectStatus(t, b.post("/auth/two-factor/enable", map[string]string{"code": "123456"}), http.StatusBadRequest, CodeTwoFactorSetupIncomplete)
	expectStatus(t, b.post("/auth/two-factor/enable", map[string]string{}), http.StatusBadRequest, apperr.CodeValidation)

	// The settings check accepts the previous step, never the next one (otplib's [1, 0]).
	secret := setupTOTP(t, b, strongPassword)["secret"].(string)
	expectStatus(t, b.post("/auth/two-factor/enable", map[string]string{"code": e.codeFor(secret, 1)}), http.StatusBadRequest, CodeInvalidCode)
	expectStatus(t, b.post("/auth/two-factor/enable", map[string]string{"code": e.codeFor(secret, -2)}), http.StatusBadRequest, CodeInvalidCode)
	expectStatus(t, b.post("/auth/two-factor/enable", map[string]string{"code": e.codeFor(secret, -1)}), http.StatusOK, "")

	// Re-enrolling needs the factor disabled first, which takes a code.
	expectStatus(t, b.post("/auth/two-factor/setup", map[string]string{"password": strongPassword}), http.StatusBadRequest, CodeTwoFactorAlreadyEnabled)
	expectStatus(t, b.post("/auth/two-factor/enable", map[string]string{"code": e.codeFor(secret, 0)}), http.StatusBadRequest, CodeTwoFactorAlreadyEnabled)

	expectStatus(t, e.browser().post("/auth/two-factor/setup", map[string]string{"password": strongPassword}), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
}

// An abandoned setup is replaced by the next one: the old secret no longer confirms.
func TestTwoFactorSetupReplacesAnUnfinishedEnrolment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	first := setupTOTP(t, b, strongPassword)["secret"].(string)
	second := setupTOTP(t, b, strongPassword)["secret"].(string)
	if first == second || e.count(`SELECT count(*) FROM user_totp WHERE user_id = ?`, id) != 1 {
		t.Fatal("a second setup did not replace the first")
	}
	expectStatus(t, b.post("/auth/two-factor/enable", map[string]string{"code": e.codeFor(first, 0)}), http.StatusBadRequest, CodeInvalidCode)
	expectStatus(t, b.post("/auth/two-factor/enable", map[string]string{"code": e.codeFor(second, 0)}), http.StatusOK, "")
}

// forma2's two-factor-auth.test.ts disable cases: the password plus a current code or a backup code; the backup code, when given, is the one checked, with or without its hyphen.
func TestDisableTwoFactor(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	plain := e.seedUser("plain@example.com", strongPassword, userOpts{})
	pb := e.browser()
	pb.signIn("plain@example.com", strongPassword)
	expectStatus(t, pb.post("/auth/two-factor/disable", map[string]string{"password": strongPassword, "code": "123456"}), http.StatusBadRequest, CodeTwoFactorNotEnabled)
	e.enrolTOTP(plain, false)
	expectStatus(t, pb.post("/auth/two-factor/disable", map[string]string{"password": strongPassword, "code": e.totpCode(plain, 0)}), http.StatusBadRequest, CodeTwoFactorNotEnabled)

	id := e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	codes := e.totp[id].BackupCodes
	b := e.sessionFor(id)
	disable := func(body map[string]string) response { return b.post("/auth/two-factor/disable", body) }

	r := disable(map[string]string{"password": "Wrong-Password-1", "code": e.totpCode(id, 0)})
	expectStatus(t, r, http.StatusBadRequest, CodeInvalidPassword)
	expectStatus(t, disable(map[string]string{"password": strongPassword}), http.StatusBadRequest, CodeTwoFactorCodeRequired)
	expectStatus(t, disable(map[string]string{"password": strongPassword, "code": e.totpCode(id, 1)}), http.StatusBadRequest, CodeInvalidCode)
	r = disable(map[string]string{"password": strongPassword, "backupCode": "aaaaa-bbbbb", "code": e.totpCode(id, 0)})
	expectStatus(t, r, http.StatusBadRequest, CodeInvalidBackupCode)
	if r.json()["message"] != "Incorrect backup code" {
		t.Fatalf("wrong backup code: %s", r.Body.String())
	}
	if !e.twoFactorEnabled(id) {
		t.Fatal("a refused disable turned two-factor authentication off")
	}

	r = disable(map[string]string{"password": strongPassword, "backupCode": codes[3][:5] + "-" + codes[3][5:]})
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["message"] != "Two factor authentication disabled" || e.count(`SELECT count(*) FROM user_totp WHERE user_id = ?`, id) != 0 {
		t.Fatalf("disable: %s", r.Body.String())
	}
	e.browser().signIn("ada@example.com", strongPassword)

	// With a current code instead; an unhyphenated backup code works as well.
	grace := e.seedUser("grace@example.com", strongPassword, userOpts{twoFactor: true})
	expectStatus(t, e.sessionFor(grace).post("/auth/two-factor/disable", map[string]string{"password": strongPassword, "code": e.totpCode(grace, -1)}), http.StatusOK, "")
	alan := e.seedUser("alan@example.com", strongPassword, userOpts{twoFactor: true})
	expectStatus(t, e.sessionFor(alan).post("/auth/two-factor/disable", map[string]string{"password": strongPassword, "backupCode": e.totp[alan].BackupCodes[0]}), http.StatusOK, "")
	if e.twoFactorEnabled(grace) || e.twoFactorEnabled(alan) {
		t.Fatal("disable left two-factor authentication on")
	}
}

// forma2's backup-code sign-in (Better Auth verify-backup-code): a code signs in once and is then spent; the others still work.
func TestSignInWithBackupCode(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	codes := e.totp[id].BackupCodes

	b := e.browser()
	b.signInPending("ada@example.com", strongPassword)
	r := verifyBackup(b, codes[0][:5]+"-"+codes[0][5:])
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["status"] != "signed_in" || !b.has(sessionCookieName) || b.has(twoFactorCookieName) {
		t.Fatalf("backup code sign-in: %s", r.Body.String())
	}
	left, err := openBackupCodes(e.twoFactor.cipher, id, e.backupCodesCiphertext(id))
	if err != nil || len(left) != 9 || slices.Contains(left, codes[0]) {
		t.Fatalf("after use %v, %v", left, err)
	}

	again := e.browser()
	again.signInPending("ada@example.com", strongPassword)
	expectStatus(t, verifyBackup(again, codes[0]), http.StatusUnauthorized, CodeInvalidBackupCode)
	expectStatus(t, verifyBackup(again, strings.ToUpper(codes[1])), http.StatusUnauthorized, CodeInvalidBackupCode)
	expectStatus(t, verifyBackup(again, codes[1]), http.StatusOK, "")
}

func (e *env) backupCodesCiphertext(id uuid.UUID) []byte {
	e.t.Helper()
	var sealed []byte
	if err := e.db.Raw(`SELECT backup_codes_ciphertext FROM user_totp WHERE user_id = ?`, id).Row().Scan(&sealed); err != nil {
		e.t.Fatal(err)
	}
	return sealed
}

// Better Auth's sign-in check accepts one step either side, and refuses a wrong code with 401.
func TestSignInTOTPWindow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	for _, tc := range []struct {
		steps int
		ok    bool
	}{{-2, false}, {2, false}, {1, true}, {-1, true}} {
		b := e.browser()
		b.signInPending("ada@example.com", strongPassword)
		r := verifyTOTP(b, e.totpCode(id, tc.steps))
		if tc.ok {
			expectStatus(t, r, http.StatusOK, "")
		} else {
			expectStatus(t, r, http.StatusUnauthorized, CodeInvalidCode)
		}
	}
}

// Better Auth's per-challenge budget: five refused codes spend the pending sign-in, and the password must be entered again.
func TestPendingSignInAttemptBudget(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	b := e.browser()
	b.signInPending("ada@example.com", strongPassword)
	for i := 0; i < 3; i++ {
		expectStatus(t, verifyTOTP(b, "000000"), http.StatusUnauthorized, CodeInvalidCode)
	}
	for i := 0; i < 2; i++ {
		expectStatus(t, verifyBackup(b, "aaaaa-bbbbb"), http.StatusUnauthorized, CodeInvalidBackupCode)
	}
	pending := b.cookies[twoFactorCookieName]
	r := verifyTOTP(b, e.totpCode(id, 0))
	expectStatus(t, r, http.StatusBadRequest, CodeTooManyAttempts)
	if c := r.cookie(twoFactorCookieName); c == nil || c.MaxAge >= 0 {
		t.Fatalf("the spent pending sign-in's cookie was not cleared: %v", c)
	}
	replay := e.browser()
	replay.cookies[twoFactorCookieName] = pending
	expectStatus(t, verifyTOTP(replay, e.totpCode(id, 0)), http.StatusUnauthorized, CodeInvalidTwoFactorCookie)
	if e.count(`SELECT count(*) FROM sessions`) != 0 {
		t.Fatal("a spent pending sign-in signed in")
	}

	b.signInPending("ada@example.com", strongPassword)
	expectStatus(t, verifyTOTP(b, e.totpCode(id, 0)), http.StatusOK, "")
	if e.failedAttempts(id) != 0 {
		t.Fatal("a successful second factor left the failure count")
	}
}

// Better Auth's account lockout: ten consecutive refused codes, across pending sign-ins and both kinds of code, lock the second factor for fifteen minutes; a correct code in between resets the count.
func TestSecondFactorLockout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	fail := func(n int) *browser {
		b := e.browser()
		b.signInPending("ada@example.com", strongPassword)
		for i := 0; i < n; i++ {
			expectStatus(t, verifyTOTP(b, "000000"), http.StatusUnauthorized, CodeInvalidCode)
		}
		return b
	}

	fail(5)
	b := fail(4)
	expectStatus(t, verifyTOTP(b, e.totpCode(id, 0)), http.StatusOK, "")
	if e.failedAttempts(id) != 0 {
		t.Fatal("success did not reset the consecutive failure count")
	}

	fail(5)
	b = fail(4)
	expectStatus(t, verifyBackup(b, "aaaaa-bbbbb"), http.StatusUnauthorized, CodeInvalidBackupCode)
	r := verifyTOTP(b, e.totpCode(id, 0))
	expectStatus(t, r, http.StatusTooManyRequests, CodeAccountTemporarilyLocked)
	if retry, _ := strconv.Atoi(r.Header().Get("Retry-After")); retry < 899 || retry > 900 {
		t.Fatalf("Retry-After %q, want about 900", r.Header().Get("Retry-After"))
	}
	// The lock holds for a fresh pending sign-in and for backup codes, and a locked attempt is not charged.
	fresh := e.browser()
	fresh.signInPending("ada@example.com", strongPassword)
	expectStatus(t, verifyBackup(fresh, e.totp[id].BackupCodes[0]), http.StatusTooManyRequests, CodeAccountTemporarilyLocked)
	if e.failedAttempts(id) != 10 {
		t.Fatalf("failed attempts %d while locked, want 10", e.failedAttempts(id))
	}

	e.advance(totpLockoutDuration + time.Second)
	fresh.signInPending("ada@example.com", strongPassword)
	expectStatus(t, verifyTOTP(fresh, e.totpCode(id, 0)), http.StatusOK, "")
	if e.failedAttempts(id) != 0 || e.count(`SELECT count(*) FROM user_totp WHERE user_id = ? AND locked_until IS NOT NULL`, id) != 0 {
		t.Fatal("an expired lock was not cleared")
	}
}

// A second factor removed or never confirmed by the time the code arrives is reported as not enabled, as Better Auth did.
func TestSignInSecondFactorNotEnabled(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	b := e.browser()
	b.signInPending("ada@example.com", strongPassword)
	e.exec(`UPDATE user_totp SET verified = false WHERE user_id = ?`, id)
	expectStatus(t, verifyTOTP(b, e.totpCode(id, 0)), http.StatusBadRequest, CodeTOTPNotEnabled)
	expectStatus(t, verifyBackup(b, e.totp[id].BackupCodes[0]), http.StatusBadRequest, CodeBackupCodesNotEnabled)

	// An unconfirmed enrolment does not stand in the way of a password sign-in.
	e.browser().signIn("ada@example.com", strongPassword)
	expectStatus(t, verifyTOTP(e.browser(), "123456"), http.StatusUnauthorized, CodeInvalidTwoFactorCookie)
}

// Two sign-ins racing with the same backup code: the code works once.
func TestConcurrentBackupCodeUse(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	code := e.totp[id].BackupCodes[0]
	browsers := []*browser{e.browser(), e.browser(), e.browser()}
	for _, b := range browsers {
		b.signInPending("ada@example.com", strongPassword)
	}
	statuses := make([]int, len(browsers))
	var wg sync.WaitGroup
	for i, b := range browsers {
		wg.Go(func() { statuses[i] = verifyBackup(b, code).Code })
	}
	wg.Wait()
	slices.Sort(statuses)
	if !slices.Equal(statuses, []int{http.StatusOK, http.StatusUnauthorized, http.StatusUnauthorized}) {
		t.Fatalf("statuses %v, want one success", statuses)
	}
}

// The /two-factor/* limit: five requests a minute per address and endpoint, each endpoint counted on its own.
func TestTwoFactorRateLimits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	e.enableRateLimits()
	e.inOneWindow(time.Minute, func(run int, ip string) error {
		b := e.browser()
		b.ip = ip
		send := func() response { return verifyTOTP(b, "000000") }
		if err := repeat(5, send, http.StatusUnauthorized, CodeInvalidTwoFactorCookie); err != nil {
			return err
		}
		if err := statusErr(send(), http.StatusTooManyRequests, apperr.CodeTooManyRequests); err != nil {
			return err
		}
		return statusErr(verifyBackup(b, "aaaaa-bbbbb"), http.StatusUnauthorized, CodeInvalidTwoFactorCookie)
	})
	e.inOneWindow(time.Minute, func(run int, ip string) error {
		b := e.sessionFor(e.seedUser(fmt.Sprintf("u%d@example.com", run), strongPassword, userOpts{}))
		b.ip = ip
		send := func() response { return b.post("/auth/two-factor/setup", map[string]string{"password": "Wrong-Password-1"}) }
		if err := repeat(5, send, http.StatusBadRequest, CodeInvalidPassword); err != nil {
			return err
		}
		return statusErr(send(), http.StatusTooManyRequests, apperr.CodeTooManyRequests)
	})
}

// Neither the secret nor a backup code reaches the logs, the audit events included.
func TestTwoFactorSecretsStayOutOfLogs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	res := setupTOTP(t, b, strongPassword)
	secret := res["secret"].(string)
	expectStatus(t, b.post("/auth/two-factor/enable", map[string]string{"code": e.codeFor(secret, 0)}), http.StatusOK, "")
	codes := res["backupCodes"].([]any)
	expectStatus(t, b.post("/auth/two-factor/disable", map[string]string{"password": strongPassword, "backupCode": codes[0].(string)}), http.StatusOK, "")
	logs := e.logs.String()
	for _, s := range []string{secret, codes[0].(string), codes[1].(string), strongPassword} {
		if strings.Contains(logs, s) {
			t.Fatalf("logs contain %q", s)
		}
	}
}
