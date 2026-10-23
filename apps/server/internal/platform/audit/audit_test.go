package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/logger"
)

func newEmitter(t *testing.T, cfg config.Config) (*Emitter, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	cfg.LogLevel = config.LogLevel(slog.LevelError) // audit events must pass any LOG_LEVEL
	cfg.LogFormat = "json"
	em := New(cfg, logger.New(cfg, &buf))
	em.now = func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }
	return em, &buf
}

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestLogWritesTheEvent(t *testing.T) {
	em, buf := newEmitter(t, config.Config{AuditLogEnabled: true})
	em.Log(context.Background(), Event{
		Action: ActionUpdated, TargetType: TargetOrganization, TargetID: "org-1",
		ActorType: ActorUser, ActorID: "user-1", OrganizationID: "org-1", Status: StatusSuccess,
		OldObject: map[string]any{"name": "Old", "billing": map[string]any{"plan": "free"}, "removed": true},
		NewObject: map[string]any{"name": "New", "billing": map[string]any{"plan": "pro"}},
		EventID:   "evt-1",
		IPAddress: "203.0.113.7",
	})

	got := lines(t, buf)
	if len(got) != 1 {
		t.Fatalf("%d lines, want 1: %s", len(got), buf.String())
	}
	ev := got[0]
	want := map[string]any{
		"level":          "audit",
		"msg":            "audit",
		"action":         "updated",
		"status":         "success",
		"timestamp":      "2026-10-21T12:00:00.000Z",
		"organizationId": "org-1",
		"ipAddress":      Unknown,
		"actor":          map[string]any{"id": "user-1", "type": "user"},
		"target":         map[string]any{"id": "org-1", "type": "organization"},
		// The diff keeps what changed, masks personal data, and leaves removed keys out.
		"changes": map[string]any{"name": redacted, "billing": map[string]any{"plan": "pro"}},
	}
	for k, v := range want {
		if !reflect.DeepEqual(ev[k], v) {
			t.Errorf("%s = %#v, want %#v", k, ev[k], v)
		}
	}
	if _, ok := ev["eventId"]; ok {
		t.Error("eventId recorded on a success")
	}
}

func TestLogRecordsFailuresWithTheirEventID(t *testing.T) {
	em, buf := newEmitter(t, config.Config{AuditLogEnabled: true, AuditLogGetUserIP: true})
	em.Log(context.Background(), Event{
		Action: ActionDeleted, TargetType: TargetInvite, ActorType: ActorUser, Status: StatusFailure,
		EventID: "evt-2", IPAddress: "203.0.113.7", APIURL: "https://app.example.com/api/v1/management/invites",
	})
	ev := lines(t, buf)[0]
	if ev["eventId"] != "evt-2" || ev["ipAddress"] != "203.0.113.7" || ev["apiUrl"] == nil {
		t.Fatalf("event = %v, want eventId, the client address and apiUrl", ev)
	}
	if ev["actor"].(map[string]any)["id"] != Unknown || ev["target"].(map[string]any)["id"] != Unknown || ev["organizationId"] != Unknown {
		t.Fatalf("missing ids not recorded as %q: %v", Unknown, ev)
	}
	if _, ok := ev["changes"]; ok {
		t.Fatal("changes recorded without objects")
	}
}

func TestLogDisabled(t *testing.T) {
	em, buf := newEmitter(t, config.Config{AuditLogEnabled: false})
	em.Log(context.Background(), Event{Action: ActionCreated, TargetType: TargetUser, ActorType: ActorUser, Status: StatusSuccess})
	if buf.Len() != 0 {
		t.Fatalf("logged with AUDIT_LOG_ENABLED=0: %s", buf.String())
	}
}

func TestLogNeverFailsOnAnInvalidEvent(t *testing.T) {
	cases := map[string]Event{
		"unknown action": {Action: "exploded", TargetType: TargetUser, ActorType: ActorUser, Status: StatusSuccess},
		"unknown target": {Action: ActionCreated, TargetType: "planet", ActorType: ActorUser, Status: StatusSuccess},
		"unknown actor":  {Action: ActionCreated, TargetType: TargetUser, ActorType: "robot", Status: StatusSuccess},
		"unknown status": {Action: ActionCreated, TargetType: TargetUser, ActorType: ActorUser, Status: "maybe"},
		"relative apiUrl": {Action: ActionCreated, TargetType: TargetUser, ActorType: ActorUser, Status: StatusSuccess,
			APIURL: "/api/v1/management/me"},
		"unserialisable object": {Action: ActionCreated, TargetType: TargetUser, ActorType: ActorUser, Status: StatusSuccess,
			NewObject: map[string]any{"f": func() {}}},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			em, buf := newEmitter(t, config.Config{AuditLogEnabled: true})
			em.Log(context.Background(), e)
			got := lines(t, buf)
			if len(got) != 1 || got[0]["level"] != "error" || got[0]["msg"] != "Failed to log audit event" {
				t.Fatalf("logged %v, want one application error and no audit event", got)
			}
		})
	}
}

func TestFromRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	var got Event
	r := gin.New()
	r.Use(httpapi.NewClientIPResolver(config.Config{TrustedProxyHopCount: 1}, slog.New(slog.DiscardHandler)).Middleware())
	r.GET("/", func(c *gin.Context) {
		httpapi.SetActor(c, httpapi.Actor{UserID: userID})
		got = FromRequest(c.Request.Context(), Event{Action: ActionUpdated})
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.ServeHTTP(httptest.NewRecorder(), req)

	if got.ActorID != userID.String() || got.ActorType != ActorUser || got.IPAddress != "203.0.113.9" {
		t.Fatalf("FromRequest = %+v", got)
	}
	kept := FromRequest(context.Background(), Event{ActorID: "system-job", ActorType: ActorSystem})
	if kept.ActorID != "system-job" || kept.ActorType != ActorSystem || kept.IPAddress != httpapi.UntrustedClientIP {
		t.Fatalf("FromRequest outside a request = %+v", kept)
	}
}

func TestRedact(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"sensitive keys", map[string]any{"email": "test@example.com", "name": "John", "foo": "bar"},
			map[string]any{"email": redacted, "name": redacted, "foo": "bar"}},
		{"nested", map[string]any{"user": map[string]any{"password": "secret", "profile": map[string]any{"address": "123 St"}}},
			map[string]any{"user": map[string]any{"password": redacted, "profile": map[string]any{"address": redacted}}}},
		{"arrays of objects", []any{map[string]any{"email": "a@b.com"}, map[string]any{"name": "Jane"}},
			[]any{map[string]any{"email": redacted}, map[string]any{"name": redacted}}},
		{"keys compared case-insensitively", map[string]any{"StripeCustomerId": "cus_1", "fileName": "cv.pdf", "UserId": "u1"},
			map[string]any{"StripeCustomerId": redacted, "fileName": redacted, "UserId": redacted}},
		{"number", float64(42), float64(42)},
		{"plain string", "foo", "foo"},
		{"an email is not a URL", "test@example.com", "test@example.com"},
		{"null", nil, nil},
		{"url query values", "https://user:pw@app.example.com/invite?token=abc&x=1&code=9&token=again#frag",
			"https://app.example.com/invite?token=********&x=1&code=********"},
		{"url without query", "https://app.example.com/p", "https://app.example.com/p"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redact(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("redact = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDeepDiff(t *testing.T) {
	cases := []struct {
		name    string
		old     any
		new     any
		want    any
		changed bool
	}{
		{"equal primitives", float64(1), float64(1), nil, false},
		{"equal strings", "a", "a", nil, false},
		{"different primitives", float64(1), float64(2), float64(2), true},
		{"different strings", "a", "b", "b", true},
		{"object field", map[string]any{"a": 1.0}, map[string]any{"a": 2.0}, map[string]any{"a": 2.0}, true},
		{"only changed fields", map[string]any{"a": 1.0, "b": 2.0}, map[string]any{"a": 1.0, "b": 3.0}, map[string]any{"b": 3.0}, true},
		{"nested", map[string]any{"a": map[string]any{"b": 1.0}}, map[string]any{"a": map[string]any{"b": 2.0}}, map[string]any{"a": map[string]any{"b": 2.0}}, true},
		{"added key", map[string]any{"a": 1.0}, map[string]any{"a": 1.0, "b": 2.0}, map[string]any{"b": 2.0}, true},
		{"removed key is not a change", map[string]any{"a": 1.0, "b": 2.0}, map[string]any{"a": 1.0}, nil, false},
		{"set to null", map[string]any{"a": 1.0}, map[string]any{"a": nil}, map[string]any{"a": nil}, true},
		{"array element", map[string]any{"l": []any{"x", "y"}}, map[string]any{"l": []any{"x", "z"}}, map[string]any{"l": map[string]any{"1": "z"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := deepDiff(tc.old, tc.new)
			if changed != tc.changed || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("deepDiff = %#v, %v; want %#v, %v", got, changed, tc.want, tc.changed)
			}
		})
	}
}
