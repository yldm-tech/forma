package auth

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newBreachChecker(t *testing.T, handler http.HandlerFunc) (*breachChecker, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	return &breachChecker{client: srv.Client(), rangeURL: srv.URL + "/range/", log: slog.New(slog.NewTextHandler(&logs, nil))}, &logs
}

// forma2's better-auth-hibp.test.ts: only the 5-character prefix is sent, with padding requested; a suffix match on any line is a breach.
func TestBreachCheckerMatchesSuffix(t *testing.T) {
	const password = "Password1"
	digest := sha1Hex(password)
	var gotPath, gotPadding, gotAgent string
	b, _ := newBreachChecker(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotPadding, gotAgent = r.URL.Path, r.Header.Get("Add-Padding"), r.Header.Get("User-Agent")
		// Lowercase and CRLF, as tolerated by forma2.
		_, _ = io.WriteString(w, "00000000000000000000000000000000000:0\r\n"+strings.ToLower(digest[5:])+":12\r\n")
	})
	if !b.compromised(context.Background(), password) {
		t.Fatal("a listed password was not reported")
	}
	if gotPath != "/range/"+digest[:5] || strings.Contains(gotPath, digest[5:]) {
		t.Fatalf("requested %q; only the prefix %s may leave the process", gotPath, digest[:5])
	}
	if gotPadding != "true" || gotAgent != "Forma Password Checker" {
		t.Fatalf("headers Add-Padding %q, User-Agent %q", gotPadding, gotAgent)
	}
	if b.compromised(context.Background(), "Unlisted-Password-1") {
		t.Fatal("an unlisted password was reported")
	}
}

// The check fails open: an unavailable service never blocks a password, and the failure is logged without the password.
func TestBreachCheckerFailsOpen(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
		"rate limited": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) },
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			b, logs := newBreachChecker(t, handler)
			if b.compromised(context.Background(), "Password1") {
				t.Fatal("reported compromised on a failed lookup")
			}
			if !strings.Contains(logs.String(), "fail open") || strings.Contains(logs.String(), "Password1") {
				t.Fatalf("logs: %s", logs.String())
			}
		})
	}
	t.Run("unreachable", func(t *testing.T) {
		b := &breachChecker{client: &http.Client{}, rangeURL: "http://127.0.0.1:1/range/", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		if b.compromised(context.Background(), "Password1") {
			t.Fatal("reported compromised with the service unreachable")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		b, _ := newBreachChecker(t, func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		})
		defer close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if b.compromised(ctx, "Password1") {
			t.Fatal("reported compromised on a timeout")
		}
	})
}

func TestBreachCheckerDisabledMakesNoRequest(t *testing.T) {
	var calls atomic.Int32
	b, _ := newBreachChecker(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	b.disabled = true
	if b.compromised(context.Background(), "Password1") || calls.Load() != 0 {
		t.Fatalf("disabled check made %d requests", calls.Load())
	}
	b.disabled = false
	if b.compromised(context.Background(), "") || calls.Load() != 0 {
		t.Fatal("an empty password was looked up")
	}
}
