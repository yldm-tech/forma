package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakePinger struct{ err error }

func (f fakePinger) PingContext(context.Context) error { return f.err }

func serve(t *testing.T, db Pinger, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(db, slog.New(slog.NewTextHandler(io.Discard, nil))).Register(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s body %q: %v", path, rec.Body.String(), err)
	}
	return rec, body
}

func TestLiveStaysUpWhenDatabaseIsDown(t *testing.T) {
	rec, body := serve(t, fakePinger{err: errors.New("connection refused")}, "/health")

	if rec.Code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("got %d %v, want 200 status=ok", rec.Code, body)
	}
}

func TestReady(t *testing.T) {
	cases := []struct {
		name       string
		pingErr    error
		wantCode   int
		wantStatus string
		wantDB     bool
	}{
		{"database reachable", nil, http.StatusOK, "ok", true},
		{"database down", errors.New("connection refused"), http.StatusServiceUnavailable, "error", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, body := serve(t, fakePinger{err: tc.pingErr}, "/health/ready")

			if rec.Code != tc.wantCode {
				t.Errorf("code = %d, want %d", rec.Code, tc.wantCode)
			}
			if body["status"] != tc.wantStatus || body["main_database"] != tc.wantDB {
				t.Errorf("body = %v, want status=%s main_database=%v", body, tc.wantStatus, tc.wantDB)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}
