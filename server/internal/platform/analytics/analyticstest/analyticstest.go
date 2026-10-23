// Package analyticstest runs a PostHog ingestion endpoint in the test process and installs a real analytics client that sends to it, so a test can assert the events a feature captured.
package analyticstest

import (
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/analytics"
)

// Message is one message as PostHog received it: a captured event, a $identify or a $groupidentify.
type Message struct {
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	Properties map[string]any `json:"properties"`
	Set        map[string]any `json:"$set"`
}

// Recorder collects what the client sent.
type Recorder struct {
	mu       sync.Mutex
	messages []Message
	flags    map[string]any
	close    func()
}

// Start installs a client for the test's duration. It replaces the process-wide client, so events captured by tests running in parallel reach it too: assert on what the test itself created, and do not call Start from two tests that run at the same time.
func Start(t *testing.T) *Recorder {
	t.Helper()
	r := &Recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/flags/" {
			r.answerFlags(w)
			return
		}
		body, _ := io.ReadAll(req.Body)
		var batch struct {
			Batch []Message `json:"batch"`
		}
		if req.URL.Path != "/batch/" || json.Unmarshal(body, &batch) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		r.messages = append(r.messages, batch.Batch...)
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	client, err := analytics.NewClient("phc_test", srv.URL, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	restore := analytics.Install(client)
	r.close = sync.OnceFunc(client.Close)
	t.Cleanup(func() {
		restore()
		r.close()
		srv.Close()
	})
	return r
}

// SetFlag makes PostHog evaluate the feature flag key to value (a variant name, or true) for everyone. Without it, flag requests fail, as when PostHog is unreachable.
func (r *Recorder) SetFlag(key string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.flags == nil {
		r.flags = map[string]any{}
	}
	r.flags[key] = value
}

// answerFlags is PostHog's /flags answer in its v3 shape, or an error when no flag is set.
func (r *Recorder) answerFlags(w http.ResponseWriter) {
	r.mu.Lock()
	flags := maps.Clone(r.flags)
	r.mu.Unlock()
	if flags == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"featureFlags": flags, "featureFlagPayloads": map[string]any{}})
}

// Flush returns once every message captured so far has reached the recorder, so All is complete. The client sends each message on its own goroutine, so arrival order says nothing about what is still in flight; Flush closes the client instead, and messages captured afterwards are dropped. Call it once the test has captured everything it asserts on.
func (r *Recorder) Flush() {
	r.close()
}

// Events waits until the client has delivered n messages named event, and returns every message with that name. It fails the test if they do not arrive within five seconds.
func (r *Recorder) Events(t *testing.T, event string, n int) []Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := r.named(event)
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("PostHog received %d %q messages, want %d; all: %v", len(got), event, n, r.All())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// All is every message received so far.
func (r *Recorder) All() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.messages)
}

func (r *Recorder) named(event string) []Message {
	var out []Message
	for _, m := range r.All() {
		if m.Event == event {
			out = append(out, m)
		}
	}
	return out
}
