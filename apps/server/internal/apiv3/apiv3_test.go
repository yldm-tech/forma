package apiv3

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
)

// Ported from forma2's api/v3/lib/cursor-pagination.test.ts, response.test.ts and schemas.test.ts.

func id(s string) string { return s }

func TestPaginate(t *testing.T) {
	items := []string{"c", "a", "e", "b", "d"}
	page, next, err := Paginate(items, id, 2, "")
	if err != nil || strings.Join(page, "") != "ab" || next == nil {
		t.Fatalf("first page %v %v %v", page, next, err)
	}
	page, next, err = Paginate(items, id, 2, *next)
	if err != nil || strings.Join(page, "") != "cd" || next == nil {
		t.Fatalf("second page %v %v %v", page, next, err)
	}
	page, next, err = Paginate(items, id, 2, *next)
	if err != nil || strings.Join(page, "") != "e" || next != nil {
		t.Fatalf("last page %v %v %v", page, next, err)
	}
	// A cursor names an id, not a position: the page after a deleted id is still right.
	page, _, err = Paginate([]string{"a", "c"}, id, 5, base64.RawURLEncoding.EncodeToString([]byte("b")))
	if err != nil || strings.Join(page, "") != "c" {
		t.Fatalf("after a deleted id %v %v", page, err)
	}
	page, next, err = Paginate(items, id, 5, "")
	if err != nil || len(page) != 5 || next != nil {
		t.Fatal("exactly one page has no next cursor")
	}
	for _, cursor := range []string{"%%%%", "YQ==", "a b", base64.RawURLEncoding.EncodeToString(nil) + "="} {
		_, _, err := Paginate(items, id, 2, cursor)
		e, ok := apperr.As(err)
		if !ok || e.Status != http.StatusBadRequest || e.Message != "Invalid pagination cursor" {
			t.Errorf("cursor %q: %v", cursor, err)
		}
	}
}

func queryOf(t *testing.T, query string) (WorkspaceListQuery, error) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/x?"+query, nil)
	return ParseWorkspaceListQuery(c)
}

func TestParseWorkspaceListQuery(t *testing.T) {
	const ws = "01a16183-afe9-73b2-b513-33766453f231"
	q, err := queryOf(t, "workspaceId="+ws)
	if err != nil || q.Limit != DefaultLimit || q.WorkspaceID.String() != ws || q.Cursor != "" {
		t.Fatalf("defaults %+v %v", q, err)
	}
	q, err = queryOf(t, "workspaceId="+ws+"&limit=250&cursor=abc")
	if err != nil || q.Limit != 250 || q.Cursor != "abc" {
		t.Fatalf("explicit %+v %v", q, err)
	}
	cases := map[string]string{
		"":                                         "workspaceId: Invalid input: expected string, received undefined",
		"workspaceId=clxx1234567890123456789":      "workspaceId: Invalid UUID",
		"workspaceId=" + ws + "&limit=abc":         "limit: Invalid input: expected number, received NaN",
		"workspaceId=" + ws + "&limit=1.5":         "limit: Invalid input: expected int, received number",
		"workspaceId=" + ws + "&limit=0":           "limit: Too small: expected number to be >=1",
		"workspaceId=" + ws + "&limit=251":         "limit: Too big: expected number to be <=250",
		"workspaceId=" + ws + "&cursor=":           "cursor: Too small: expected string to have >=1 characters",
		"workspaceId=" + ws + "&workspaceId=" + ws: "workspaceId: Invalid input: expected string, received array",
		"workspaceId=" + ws + "&b=1&a=2":           `query: Unrecognized keys: "a", "b"`,
	}
	for query, want := range cases {
		_, err := queryOf(t, query)
		e, ok := apperr.As(err)
		if !ok {
			t.Errorf("%q accepted", query)
			continue
		}
		params, _ := e.Details[invalidParamsKey].([]InvalidParam)
		if e.Message != "Invalid query parameters" || len(params) != 1 || params[0].Name+": "+params[0].Reason != want {
			t.Errorf("%q: %+v", query, params)
		}
	}
}

func TestErrorHandler(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := httpserver.NewEngine(config.Config{}, log)
	g := r.Group("/api/v3", ErrorHandler(log))
	g.GET("/limited", func(c *gin.Context) { httpapi.Fail(c, apperr.TooManyRequests(42)) })
	g.GET("/broken", func(c *gin.Context) { httpapi.Fail(c, errors.New("database is on fire")) })
	g.GET("/bad", func(c *gin.Context) {
		httpapi.Fail(c, BadRequest("Invalid request body", InvalidParam{Name: "body", Reason: "Malformed JSON"}))
	})

	cases := []struct {
		path, title, code string
		status            int
	}{
		{"/api/v3/limited", "Too Many Requests", "too_many_requests", http.StatusTooManyRequests},
		{"/api/v3/broken", "Internal Server Error", "internal_server_error", http.StatusInternalServerError},
		{"/api/v3/bad", "Bad Request", "bad_request", http.StatusBadRequest},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		var p Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if rec.Code != c.status || p.Status != c.status || p.Title != c.title || p.Code != c.code || p.Instance != c.path || p.RequestID != rec.Header().Get("X-Request-Id") || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: %d %s %v", c.path, rec.Code, rec.Body.String(), rec.Header())
		}
		if strings.Contains(rec.Body.String(), "on fire") {
			t.Error("an internal error reached the client")
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v3/limited", nil))
	if rec.Header().Get("Retry-After") != "42" {
		t.Errorf("Retry-After %q", rec.Header().Get("Retry-After"))
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v3/bad", nil))
	if !strings.Contains(rec.Body.String(), `"invalid_params":[{"name":"body","reason":"Malformed JSON"}]`) {
		t.Errorf("invalid params %s", rec.Body.String())
	}
}
