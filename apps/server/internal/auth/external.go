package auth

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // The Have I Been Pwned range API is keyed by SHA-1; only a 5-character prefix leaves the process and the digest is never stored or used to authenticate.
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	hibpRangeURL = "https://api.pwnedpasswords.com/range/"
	hibpTimeout  = 3 * time.Second
	// externalResponseLimit bounds what is read of a range response, about 30 KB padded.
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
