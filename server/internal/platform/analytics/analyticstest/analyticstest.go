// Package analyticstest runs a PostHog ingestion endpoint in the test process and installs a real analytics client that sends to it, so a test can assert the events a feature captured.
package analyticstest

import (
	"encoding/json"
	"io"
	"log/slog"
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
}

// Start installs a client for the test's duration. It replaces the process-wide client, so a test that calls it must not run in parallel with another that captures events.
func Start(t *testing.T) *Recorder {
	t.Helper()
	r := &Recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	t.Cleanup(func() {
		restore()
		client.Close()
		srv.Close()
	})
	return r
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
