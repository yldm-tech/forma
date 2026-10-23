package v3api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

func self(s string) string { return s }

// The cases of forma2's cursor-pagination.test.ts.
func TestPaginate(t *testing.T) {
	page, next, err := Paginate([]string{}, self, 10, "")
	if err != nil || len(page) != 0 || next != nil {
		t.Fatalf("empty: %v %v %v", page, next, err)
	}
	page, next, _ = Paginate([]string{"a", "b"}, self, 5, "")
	if !slices.Equal(page, []string{"a", "b"}) || next != nil {
		t.Fatalf("one page: %v %v", page, next)
	}
	page, next, _ = Paginate([]string{"a", "b"}, self, 2, "")
	if len(page) != 2 || next != nil {
		t.Fatalf("exact page: %v %v", page, next)
	}

	items := []string{"c", "a", "b"}
	page, next, _ = Paginate(items, self, 2, "")
	if !slices.Equal(page, []string{"a", "b"}) || next == nil {
		t.Fatalf("page 1: %v %v", page, next)
	}
	page, last, _ := Paginate(items, self, 2, *next)
	if !slices.Equal(page, []string{"c"}) || last != nil {
		t.Fatalf("page 2: %v %v", page, last)
	}
	if !slices.Equal(items, []string{"c", "a", "b"}) {
		t.Fatal("the input was reordered")
	}

	// A stale cursor (its row deleted between pages) still pages by id > cursor.
	_, next, _ = Paginate([]string{"a", "b", "c"}, self, 1, "")
	page, last, _ = Paginate([]string{"b", "c"}, self, 5, *next)
	if !slices.Equal(page, []string{"b", "c"}) || last != nil {
		t.Fatalf("stale cursor: %v %v", page, last)
	}

	for _, cursor := range []string{"%%%%", "not base64url!", "YQ==", "YR", "_w"} {
		_, _, err := Paginate([]string{"a"}, self, 5, cursor)
		var p *Problem
		if !errors.As(err, &p) || p.Status != http.StatusBadRequest || p.Detail != "Invalid pagination cursor" || len(p.InvalidParams) != 1 || p.InvalidParams[0].Name != "cursor" {
			t.Fatalf("cursor %q: %v", cursor, err)
		}
	}
}

func serve(t *testing.T, handler gin.HandlerFunc, method, target, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil))))
	r.Handle(method, "/api/v3/things/:thingId", handler)
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, target, reader))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestProblemRendering(t *testing.T) {
	rec, body := serve(t, func(c *gin.Context) {
		Fail(c, Unprocessable("Unable to update tag", InvalidParam{Name: "name", Reason: "tag_name_already_exists"}))
	}, http.MethodPatch, "/api/v3/things/1", "")
	if rec.Code != http.StatusUnprocessableEntity || rec.Header().Get("Content-Type") != "application/problem+json" || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	if body["title"] != "Unprocessable Content" || body["status"] != float64(422) || body["code"] != "unprocessable_content" || body["instance"] != "/api/v3/things/1" ||
		body["requestId"] == "" || body["requestId"] != rec.Header().Get("X-Request-Id") {
		t.Fatalf("body %v", body)
	}

	// An unexpected failure is a generic 500; its cause stays in the logs.
	rec, body = serve(t, func(c *gin.Context) { Fail(c, errors.New("database is on fire")) }, http.MethodGet, "/api/v3/things/1", "")
	if rec.Code != http.StatusInternalServerError || body["detail"] != InternalDetail || strings.Contains(rec.Body.String(), "fire") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec, body = serve(t, func(c *gin.Context) { Fail(c, Unexpected("Unable to delete tag", errors.New("boom"))) }, http.MethodGet, "/api/v3/things/1", "")
	if rec.Code != http.StatusInternalServerError || body["detail"] != "Unable to delete tag" || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}

	// A rate-limit rejection carries Retry-After and, as in forma2, no instance.
	rec, body = serve(t, func(c *gin.Context) { Fail(c, apperr.TooManyRequests(42)) }, http.MethodGet, "/api/v3/things/1", "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "42" || body["code"] != "too_many_requests" || body["instance"] != nil {
		t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
}

type renameBody struct {
	Name *string `json:"name"`
}

func TestReadBody(t *testing.T) {
	handler := func(c *gin.Context) {
		var b renameBody
		if err := ReadBody(c, &b); err != nil {
			Fail(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
	cases := []struct {
		body   string
		status int
		param  InvalidParam
	}{
		{`{"name":"x"}`, http.StatusNoContent, InvalidParam{}},
		{`{"name":`, http.StatusBadRequest, InvalidParam{Name: "body", Reason: "Malformed JSON input, please check your request body"}},
		{``, http.StatusBadRequest, InvalidParam{Name: "body", Reason: "Malformed JSON input, please check your request body"}},
		{`{"name":"x"} {}`, http.StatusBadRequest, InvalidParam{Name: "body", Reason: "Malformed JSON input, please check your request body"}},
		{`{"name":"x","extra":1}`, http.StatusBadRequest, InvalidParam{Name: "body", Reason: `Unrecognized key: "extra"`}},
		{`{"name":1}`, http.StatusBadRequest, InvalidParam{Name: "name", Reason: "Invalid input: expected string, received number"}},
		{`["x"]`, http.StatusBadRequest, InvalidParam{Name: "body", Reason: "Invalid input: expected object, received array"}},
		{`{"name":"` + strings.Repeat("x", int(MaxBodyBytes)) + `"}`, http.StatusRequestEntityTooLarge, InvalidParam{}},
	}
	for _, tc := range cases {
		rec, body := serve(t, handler, http.MethodPatch, "/api/v3/things/1", tc.body)
		if rec.Code != tc.status {
			t.Fatalf("%.40s: %d %s", tc.body, rec.Code, rec.Body.String())
		}
		if tc.param.Name == "" {
			continue
		}
		params, _ := body["invalid_params"].([]any)
		if body["detail"] != "Invalid request body" || len(params) != 1 {
			t.Fatalf("%s: %s", tc.body, rec.Body.String())
		}
		if p := params[0].(map[string]any); p["name"] != tc.param.Name || p["reason"] != tc.param.Reason {
			t.Fatalf("%s: %v, want %+v", tc.body, p, tc.param)
		}
	}
}

func TestReadQuery(t *testing.T) {
	handler := func(c *gin.Context) {
		q := ReadQuery(c, "workspaceId", "limit", "cursor")
		q.UUID("workspaceId")
		limit, cursor := ReadListQuery(q)
		if err := q.Err(); err != nil {
			Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"limit": limit, "cursor": cursor})
	}
	const ws = "0190f2c4-0000-7000-8000-000000000001"
	rec, body := serve(t, handler, http.MethodGet, "/api/v3/things/1?workspaceId="+ws, "")
	if rec.Code != http.StatusOK || body["limit"] != float64(ListDefaultLimit) || body["cursor"] != "" {
		t.Fatalf("defaults: %d %s", rec.Code, rec.Body.String())
	}
	rec, body = serve(t, handler, http.MethodGet, "/api/v3/things/1?workspaceId="+ws+"&limit=250&cursor=YQ", "")
	if rec.Code != http.StatusOK || body["limit"] != float64(250) || body["cursor"] != "YQ" {
		t.Fatalf("explicit: %d %s", rec.Code, rec.Body.String())
	}
	for query, names := range map[string][]string{
		"":                                          {"workspaceId"},
		"?workspaceId=nope":                         {"workspaceId"},
		"?workspaceId=" + ws + "&limit=0":           {"limit"},
		"?workspaceId=" + ws + "&limit=251":         {"limit"},
		"?workspaceId=" + ws + "&limit=1.5":         {"limit"},
		"?workspaceId=" + ws + "&cursor=":           {"cursor"},
		"?workspaceId=" + ws + "&other=1":           {"query"},
		"?workspaceId=" + ws + "&workspaceId=" + ws: {"workspaceId"},
	} {
		rec, body := serve(t, handler, http.MethodGet, "/api/v3/things/1"+query, "")
		params, _ := body["invalid_params"].([]any)
		var got []string
		for _, p := range params {
			got = append(got, p.(map[string]any)["name"].(string))
		}
		if rec.Code != http.StatusBadRequest || body["detail"] != "Invalid query parameters" || !slices.Equal(got, names) {
			t.Fatalf("%q: %d %s", query, rec.Code, rec.Body.String())
		}
	}

	rec, body = serve(t, func(c *gin.Context) {
		if _, err := ParamUUID(c, "thingId"); err != nil {
			Fail(c, err)
		}
	}, http.MethodGet, "/api/v3/things/1", "")
	if rec.Code != http.StatusBadRequest || body["detail"] != "Invalid route parameters" {
		t.Fatalf("params: %d %s", rec.Code, rec.Body.String())
	}
}
