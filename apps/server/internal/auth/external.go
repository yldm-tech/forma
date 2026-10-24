package auth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // The Have I Been Pwned range API is keyed by SHA-1; only a 5-character prefix leaves the process and the digest is never stored or used to authenticate.
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	hibpRangeURL     = "https://api.pwnedpasswords.com/range/"
	hibpTimeout      = 3 * time.Second
	turnstileURL     = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	turnstileTimeout = 5 * time.Second
	// externalResponseLimit bounds what is read from either service. A padded range response is about 30 KB.
	externalResponseLimit = 1 << 20
)

// breachChecker looks a password up in the Have I Been Pwned corpus with the k-anonymity range API, as forma2's better-auth-hibp.ts did.
type breachChecker struct {
	client   *http.Client
	rangeURL string
	disabled bool
	log      *slog.Logger
}

// compromised reports true only when the password is confirmed in the corpus. It fails open: a timeout, network error or non-200 answer is logged and the password allowed, so an air-gapped instance can still sign users up.
func (b *breachChecker) compromised(ctx context.Context, password string) bool {
	if b.disabled || password == "" {
		return false
	}
	sum := sha1.Sum([]byte(password)) //nolint:gosec // see the import
	digest := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := digest[:5], digest[5:]

	ctx, cancel := context.WithTimeout(ctx, hibpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.rangeURL+prefix, nil)
	if err != nil {
		b.log.WarnContext(ctx, "HIBP breach check failed; allowing password (fail open)", "err", err)
		return false
	}
	// Padding hides the real number of matching suffixes from anyone watching the response size.
	req.Header.Set("Add-Padding", "true")
	req.Header.Set("User-Agent", "Forma Password Checker")
	res, err := b.client.Do(req)
	if err != nil {
		b.log.WarnContext(ctx, "HIBP breach check failed; allowing password (fail open)", "err", err)
		return false
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b.log.WarnContext(ctx, "HIBP breach check unreachable; allowing password (fail open)", "status", res.StatusCode)
		return false
	}
	scanner := bufio.NewScanner(io.LimitReader(res.Body, externalResponseLimit))
	for scanner.Scan() {
		hashSuffix, _, _ := strings.Cut(strings.TrimSpace(scanner.Text()), ":")
		if strings.EqualFold(hashSuffix, suffix) {
			return true
		}
	}
	if err := scanner.Err(); err != nil {
		b.log.WarnContext(ctx, "HIBP breach check failed; allowing password (fail open)", "err", err)
	}
	return false
}

// turnstileVerifier checks a Cloudflare Turnstile token, as forma2's verifyTurnstileToken did.
type turnstileVerifier struct {
	client    *http.Client
	verifyURL string
	secret    string
}

// verify reports whether Cloudflare accepted the token. Any failure to get an answer counts as not accepted.
func (v *turnstileVerifier) verify(ctx context.Context, token string) bool {
	ctx, cancel := context.WithTimeout(ctx, turnstileTimeout)
	defer cancel()
	body, err := json.Marshal(map[string]string{"secret": v.secret, "response": token})
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.verifyURL, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := v.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return false
	}
	var out struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, externalResponseLimit)).Decode(&out); err != nil {
		return false
	}
	return out.Success
}
