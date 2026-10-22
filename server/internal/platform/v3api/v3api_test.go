package v3api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func serve(t *testing.T, handler gin.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, "/api/v3/things", handler)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestProblemResponses(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := serve(t, func(c *gin.Context) {
		Fail(c, log, Unprocessable("Survey document failed validation", InvalidParam{Name: "blocks.0.id", Reason: "duplicated", Code: CodeDuplicateIdentifier, FirstUsedAt: "blocks.1.id"}))
	}, http.MethodGet, "/api/v3/things", "")
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	params := body["invalid_params"].([]any)
	if rec.Code != 422 || rec.Header().Get("Content-Type") != "application/problem+json" || rec.Header().Get("Cache-Control") != CacheNoStore ||
		body["title"] != "Unprocessable Content" || body["status"] != float64(422) || body["code"] != "unprocessable_content" || body["instance"] != "/api/v3/things" ||
		len(params) != 1 || params[0].(map[string]any)["firstUsedAt"] != "blocks.1.id" || params[0].(map[string]any)["missingId"] != nil {
		t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}

	rec = serve(t, func(c *gin.Context) { Fail(c, log, errors.New("disk on fire")) }, http.MethodGet, "/api/v3/things", "")
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "fire") || !strings.Contains(rec.Body.String(), `"detail":"An unexpected error occurred."`) {
		t.Fatalf("internal error leaked: %s", rec.Body.String())
	}

	rec = serve(t, func(c *gin.Context) {
		Fail(c, log, TooManyRequests("Maximum number of requests reached. Please try again later.", 42))
	}, http.MethodGet, "/api/v3/things", "")
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "42" {
		t.Fatalf("rate limit %d %v", rec.Code, rec.Header())
	}
}

func TestInputHelpers(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	read := func(c *gin.Context) {
		raw, err := ReadJSON(c)
		if err != nil {
			Fail(c, log, err)
			return
		}
		if err := AllowQuery(c, "lang"); err != nil {
			Fail(c, log, err)
			return
		}
		OK(c, raw)
	}
	if rec := serve(t, read, http.MethodPost, "/api/v3/things?lang=de", `{"a": 1}`); rec.Code != 200 || rec.Body.String() != `{"data":{"a":1}}` {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, read, http.MethodPost, "/api/v3/things", `{"a"`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "Malformed JSON input") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, read, http.MethodPost, "/api/v3/things?b=1&a=2", `{}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), `Unrecognized keys: \"a\", \"b\"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, read, http.MethodPost, "/api/v3/things", `"`+strings.Repeat("x", MaxBodyBytes)+`"`); rec.Code != 413 {
		t.Fatalf("%d", rec.Code)
	}
}
