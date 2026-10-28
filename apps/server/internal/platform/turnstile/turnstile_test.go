package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// cloudflare is a stand-in for siteverify that records each request and answers with status and body.
type cloudflare struct {
	mu       sync.Mutex
	status   int
	body     string
	requests []map[string]string
}

func (c *cloudflare) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]string
		if r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&got) != nil {
			t.Errorf("siteverify got a request that is not JSON")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.requests = append(c.requests, got)
		w.WriteHeader(c.status)
		_, _ = io.WriteString(w, c.body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *cloudflare) answer(status int, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status, c.body = status, body
}

func (c *cloudflare) last() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		return nil
	}
	return c.requests[len(c.requests)-1]
}

func (c *cloudflare) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func TestVerify(t *testing.T) {
	cf := &cloudflare{}
	v := NewWithEndpoint("s3cret", cf.serve(t).URL)
	ctx := context.Background()

	cases := []struct {
		name        string
		status      int
		body        string
		want        bool
		unavailable bool
	}{
		{"accepted", 200, `{"success":true,"error-codes":[],"hostname":"forma.example","action":"submit_response"}`, true, false},
		// Cloudflare's test keys answer without an action, as does a widget a page rendered before its form named one.
		{"accepted without an action", 200, `{"success":true,"error-codes":[],"hostname":"example.com"}`, true, false},
		// A token minted by the sign-up widget is not a response's, and the reverse.
		{"accepted for another action", 200, `{"success":true,"error-codes":[],"hostname":"forma.example","action":"sign_up"}`, false, false},
		{"invalid token", 200, `{"success":false,"error-codes":["invalid-input-response"]}`, false, false},
		{"spent or expired token", 200, `{"success":false,"error-codes":["timeout-or-duplicate"]}`, false, false},
		{"refused without a reason", 200, `{"success":false}`, false, false},
		{"Cloudflare's own failure", 200, `{"success":false,"error-codes":["internal-error"]}`, false, true},
		{"wrong secret key", 200, `{"success":false,"error-codes":["invalid-input-secret"]}`, false, true},
		{"server error", 500, `{"success":true}`, false, true},
		{"rate limited", 429, ``, false, true},
		{"not JSON", 200, `<html>`, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cf.answer(tc.status, tc.body)
			ok, err := v.Verify(ctx, "tok", "203.0.113.7", ActionSubmitResponse)
			if ok != tc.want || errors.Is(err, ErrUnavailable) != tc.unavailable {
				t.Fatalf("Verify = %v, %v; want %v, unavailable %v", ok, err, tc.want, tc.unavailable)
			}
			if !tc.unavailable && err != nil {
				t.Fatalf("a verdict came with an error: %v", err)
			}
			if got := cf.last(); got["secret"] != "s3cret" || got["response"] != "tok" || got["remoteip"] != "203.0.113.7" {
				t.Fatalf("request %v", got)
			}
		})
	}
}

// A missing or overlong token is refused without asking Cloudflare, which would only refuse it too.
func TestVerifyRefusesMissingTokenLocally(t *testing.T) {
	cf := &cloudflare{}
	v := NewWithEndpoint("s3cret", cf.serve(t).URL)
	cf.answer(200, `{"success":true}`)
	for _, token := range []string{"", strings.Repeat("a", maxTokenLength+1)} {
		if ok, err := v.Verify(context.Background(), token, "203.0.113.7", ActionSubmitResponse); ok || err != nil {
			t.Fatalf("token of %d characters: %v, %v", len(token), ok, err)
		}
	}
	if cf.count() != 0 {
		t.Fatalf("%d requests reached Cloudflare", cf.count())
	}
}

// The client address goes along only when the proxy chain vouched for it.
func TestVerifyRemoteIP(t *testing.T) {
	cf := &cloudflare{}
	v := NewWithEndpoint("s3cret", cf.serve(t).URL)
	cf.answer(200, `{"success":true}`)
	for _, ip := range []string{"", httpapi.UntrustedClientIP} {
		if ok, err := v.Verify(context.Background(), "tok", ip, ActionSubmitResponse); !ok || err != nil {
			t.Fatalf("Verify with %q = %v, %v", ip, ok, err)
		}
		if _, sent := cf.last()["remoteip"]; sent {
			t.Fatalf("remoteip sent for %q: %v", ip, cf.last())
		}
	}
}

// Cloudflare not answering is unavailability, not a verdict: an unreachable endpoint and one that answers after the timeout.
func TestVerifyProviderDown(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	if ok, err := NewWithEndpoint("s", down.URL).Verify(context.Background(), "tok", "", ActionSubmitResponse); ok || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unreachable: %v, %v", ok, err)
	}

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })
	v := NewWithEndpoint("s", slow.URL)
	v.client.Timeout = 50 * time.Millisecond
	if ok, err := v.Verify(context.Background(), "tok", "", ActionSubmitResponse); ok || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("timed out: %v, %v", ok, err)
	}
}
