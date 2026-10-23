package billing

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// forma2's metering tests: one event per response or workflow run, identified by it and timestamped with its creation; nothing without a customer or off Forma Cloud; a Stripe failure logged and swallowed.
func TestMeter(t *testing.T) {
	t.Parallel()
	f := newFakeStripe(t)
	fail := false
	f.mux.HandleFunc("POST /v1/billing/meter_events", func(w http.ResponseWriter, _ *http.Request) {
		if fail {
			writeJSON(w, http.StatusBadRequest, stripeErrorBody("invalid_request_error", "parameter_invalid", "", "bad"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "billing.meter_event", "event_name": "response_created"})
	})
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	cloud := config.Config{IsFormaCloud: true}
	m := NewMeter(cloud, stripeFor(t, cloud, f, log), log)
	customer := "cus_123"
	responseID, runID := uuid.New(), uuid.New()
	created := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	m.ResponseCreated(context.Background(), &customer, responseID, created)
	m.WorkflowRunCreated(context.Background(), &customer, runID, time.Time{})
	calls := f.requests(http.MethodPost, "/v1/billing/meter_events")
	if len(calls) != 2 {
		t.Fatalf("%d meter events", len(calls))
	}
	for k, v := range map[string]string{"event_name": "response_created", "identifier": "response_created:" + responseID.String(), "timestamp": "1772359200", "payload[stripe_customer_id]": "cus_123", "payload[value]": "1"} {
		if calls[0].Form.Get(k) != v {
			t.Errorf("response event %s = %q, want %q", k, calls[0].Form.Get(k), v)
		}
	}
	if calls[1].Form.Get("event_name") != "workflow_run_created" || calls[1].Form.Get("identifier") != "workflow_run_created:"+runID.String() || calls[1].Form.Has("timestamp") {
		t.Errorf("workflow run event %v", calls[1].Form)
	}

	m.ResponseCreated(context.Background(), nil, responseID, created)
	empty := ""
	m.WorkflowRunCreated(context.Background(), &empty, runID, created)
	NewMeter(config.Config{}, stripeFor(t, config.Config{}, f, log), log).ResponseCreated(context.Background(), &customer, responseID, created)
	if f.count() != 2 {
		t.Fatalf("%d calls; no customer and self-hosted must not meter", f.count())
	}

	fail = true
	m.ResponseCreated(context.Background(), &customer, responseID, created)
	if !strings.Contains(logs.String(), "Failed to record Stripe meter event for response_created") {
		t.Fatal("the failure was not logged")
	}
}
