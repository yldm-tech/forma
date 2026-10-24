package unsplash

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
)

const (
	accessKey  = "test-access-key"
	userHeader = "X-Test-User"
)

// headerSessions stands in for the credential module's sessions: the acting user comes from a header.
type headerSessions struct{}

func (headerSessions) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.GetHeader(userHeader))
		if err != nil {
			httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
			return
		}
		httpapi.SetActor(c, httpapi.Actor{UserID: id})
		c.Next()
	}
}

func (headerSessions) Optional() gin.HandlerFunc { return func(c *gin.Context) { c.Next() } }

// stub is Unsplash's API: it records every request and answers the search and the download location.
type stub struct {
	srv      *httptest.Server
	mu       sync.Mutex
	requests []*http.Request
	status   int
}

const searchAnswer = `{"total":2,"total_pages":1,"results":[
	{"id":"abc","alt_description":"a red lollipop","urls":{"regular":"https://images.unsplash.com/photo-1?ixid=x1&ixlib=rb-4.0.3"},"links":{"download_location":"https://api.unsplash.com/photos/abc/download?ixid=x1"},"user":{"first_name":"Jane","last_name":"Doe & Co","links":{"html":"https://unsplash.com/@jane"}}},
	{"id":"def","alt_description":null,"urls":{"regular":"https://images.unsplash.com/photo-2?ixid=x2"},"links":{"download_location":"https://api.unsplash.com/photos/def/download?ixid=x2"},"user":{"first_name":"John","last_name":null,"links":{"html":"https://unsplash.com/@john"}}}
]}`

func newStub(t *testing.T) *stub {
	t.Helper()
	s := &stub{status: http.StatusOK}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.Clone(context.Background()))
		status := s.status
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"errors":["OAuth error: The access token is invalid"]}`)
			return
		}
		switch {
		case r.URL.Path == "/search/photos":
			_, _ = io.WriteString(w, searchAnswer)
		case strings.HasSuffix(r.URL.Path, "/download"):
			_, _ = io.WriteString(w, `{"url":"https://images.unsplash.com/photo-1"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stub) received() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.requests...)
}

// host is the stub's host:port, which the service takes as Unsplash's API host.
func (s *stub) host() string {
	u, _ := url.Parse(s.srv.URL)
	return u.Host
}

type env struct {
	engine *gin.Engine
	stub   *stub
	user   uuid.UUID
}

func disabledLimiter() *ratelimit.Limiter {
	return ratelimit.New(config.Config{RateLimitingDisabled: true}, nil, slog.New(slog.DiscardHandler))
}

func newEnv(t *testing.T, key string, limiter *ratelimit.Limiter) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st := newStub(t)
	var cfg config.Config
	cfg.Unsplash.AccessKey = config.Secret(key)
	svc := NewService(cfg, limiter)
	svc.base, _ = url.Parse(st.srv.URL)
	svc.client = st.srv.Client()
	r := gin.New()
	NewHandler(svc, headerSessions{}).Register(r.Group("/api/app", httpapi.ErrorHandler(slog.New(slog.DiscardHandler))))
	return &env{engine: r, stub: st, user: uuid.New()}
}

func (e *env) do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set(userHeader, e.user.String())
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var body struct{ Code, Message string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body %q: %v", rec.Body.String(), err)
	}
	return body.Code, body.Message
}

func TestStatus(t *testing.T) {
	for key, want := range map[string]bool{accessKey: true, "": false} {
		e := newEnv(t, key, disabledLimiter())
		rec := e.do(t, http.MethodGet, "/api/app/unsplash/status", "")
		if rec.Code != http.StatusOK || rec.Body.String() != `{"configured":`+map[bool]string{true: "true", false: "false"}[want]+`}` {
			t.Errorf("key %q: %d %s", key, rec.Code, rec.Body.String())
		}
	}
}

func TestSearchSendsForma2sQueryAndMapsAttribution(t *testing.T) {
	e := newEnv(t, accessKey, disabledLimiter())
	rec := e.do(t, http.MethodGet, "/api/app/unsplash/search?query=red+lollipop&page=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
	}

	reqs := e.stub.received()
	if len(reqs) != 1 {
		t.Fatalf("Unsplash received %d requests, want 1", len(reqs))
	}
	got := reqs[0]
	if got.Method != http.MethodGet || got.URL.Path != "/search/photos" {
		t.Errorf("request %s %s", got.Method, got.URL.Path)
	}
	wantQuery := url.Values{"query": {"red lollipop"}, "orientation": {"landscape"}, "per_page": {"9"}, "page": {"2"}}
	if got.URL.Query().Encode() != wantQuery.Encode() {
		t.Errorf("query = %s, want %s", got.URL.RawQuery, wantQuery.Encode())
	}
	if got.Header.Get("Authorization") != "Client-ID "+accessKey || got.Header.Get("Accept-Version") != "v1" {
		t.Errorf("headers = %v", got.Header)
	}

	var out SearchOutput
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	want := []Image{
		{
			ID:               "abc",
			AltDescription:   "a red lollipop",
			URL:              "https://images.unsplash.com/photo-1?ixid=x1&ixlib=rb-4.0.3&dpr=2&authorLink=https%3A%2F%2Funsplash.com%2F%40jane&authorName=Jane%20Doe%20%26%20Co&utm_source=forma&utm_medium=referral",
			DownloadLocation: "https://api.unsplash.com/photos/abc/download?ixid=x1",
			AuthorName:       "Jane Doe & Co",
		},
		{
			ID:               "def",
			URL:              "https://images.unsplash.com/photo-2?ixid=x2&dpr=2&authorLink=https%3A%2F%2Funsplash.com%2F%40john&authorName=John&utm_source=forma&utm_medium=referral",
			DownloadLocation: "https://api.unsplash.com/photos/def/download?ixid=x2",
			AuthorName:       "John",
		},
	}
	if len(out.Images) != len(want) {
		t.Fatalf("images = %+v", out.Images)
	}
	for i := range want {
		if out.Images[i] != want[i] {
			t.Errorf("image %d\n got %+v\nwant %+v", i, out.Images[i], want[i])
		}
	}
	// The survey page reads the author back from the stored URL's parameters.
	stored, _ := url.Parse(out.Images[0].URL)
	if stored.Query().Get("authorName") != "Jane Doe & Co" || stored.Query().Get("authorLink") != "https://unsplash.com/@jane" {
		t.Errorf("attribution read back = %v", stored.Query())
	}
}

func TestSearchDefaultsToTheFirstPageAndValidates(t *testing.T) {
	e := newEnv(t, accessKey, disabledLimiter())
	if rec := e.do(t, http.MethodGet, "/api/app/unsplash/search?query=cats", ""); rec.Code != http.StatusOK {
		t.Fatalf("code %d", rec.Code)
	}
	if page := e.stub.received()[0].URL.Query().Get("page"); page != "1" {
		t.Errorf("page = %q, want 1", page)
	}
	for _, target := range []string{"/api/app/unsplash/search", "/api/app/unsplash/search?query=cats&page=0", "/api/app/unsplash/search?query=cats&page=x"} {
		if rec := e.do(t, http.MethodGet, target, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", target, rec.Code)
		}
	}
	if n := len(e.stub.received()); n != 1 {
		t.Errorf("Unsplash received %d requests, want only the valid one", n)
	}
}

func TestSearchFailureFromUnsplashIsAnInternalError(t *testing.T) {
	e := newEnv(t, accessKey, disabledLimiter())
	e.stub.mu.Lock()
	e.stub.status = http.StatusUnauthorized
	e.stub.mu.Unlock()
	rec := e.do(t, http.MethodGet, "/api/app/unsplash/search?query=cats", "")
	if code, _ := errorCode(t, rec); rec.Code != http.StatusInternalServerError || code != apperr.CodeInternal {
		t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadPingsTheDownloadLocation(t *testing.T) {
	e := newEnv(t, accessKey, disabledLimiter())
	location := "https://" + e.stub.host() + "/photos/abc/download?ixid=x1"
	rec := e.do(t, http.MethodPost, "/api/app/unsplash/download", `{"downloadUrl":"`+location+`"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
	}
	reqs := e.stub.received()
	if len(reqs) != 1 {
		t.Fatalf("Unsplash received %d requests, want 1", len(reqs))
	}
	if reqs[0].URL.Path != "/photos/abc/download" || reqs[0].URL.Query().Get("ixid") != "x1" || reqs[0].Header.Get("Authorization") != "Client-ID "+accessKey {
		t.Errorf("request %s, headers %v", reqs[0].URL, reqs[0].Header)
	}
}

func TestDownloadRefusesLocationsOutsideUnsplash(t *testing.T) {
	e := newEnv(t, accessKey, disabledLimiter())
	host := e.stub.host()
	hostname, _, _ := strings.Cut(host, ":")
	for _, location := range []string{
		"https://evil.example.com/photos/abc/download",
		"http://" + host + "/photos/abc/download",
		"https://" + hostname + ":1/photos/abc/download",
		"https://user:pass@" + host + "/photos/abc/download",
	} {
		rec := e.do(t, http.MethodPost, "/api/app/unsplash/download", `{"downloadUrl":"`+location+`"}`)
		if _, msg := errorCode(t, rec); rec.Code != http.StatusBadRequest || msg != MessageInvalidURL {
			t.Errorf("%s = %d %s, want 400 %q", location, rec.Code, rec.Body.String(), MessageInvalidURL)
		}
	}
	if rec := e.do(t, http.MethodPost, "/api/app/unsplash/download", `{"downloadUrl":"not a url"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed location = %d, want 400", rec.Code)
	}
	if n := len(e.stub.received()); n != 0 {
		t.Errorf("Unsplash received %d requests, want none", n)
	}
}

func TestUnconfiguredInstanceOffersNoUnsplash(t *testing.T) {
	e := newEnv(t, "", disabledLimiter())
	for _, rec := range []*httptest.ResponseRecorder{
		e.do(t, http.MethodGet, "/api/app/unsplash/search?query=cats", ""),
		e.do(t, http.MethodPost, "/api/app/unsplash/download", `{"downloadUrl":"https://`+e.stub.host()+`/photos/abc/download"}`),
	} {
		if code, _ := errorCode(t, rec); rec.Code != http.StatusNotFound || code != apperr.CodeNotFound {
			t.Errorf("code %d: %s", rec.Code, rec.Body.String())
		}
	}
	if n := len(e.stub.received()); n != 0 {
		t.Errorf("Unsplash received %d requests, want none", n)
	}
}

func TestRoutesNeedASession(t *testing.T) {
	e := newEnv(t, accessKey, disabledLimiter())
	for _, target := range []string{"/api/app/unsplash/status", "/api/app/unsplash/search?query=cats"} {
		rec := httptest.NewRecorder()
		e.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", target, rec.Code)
		}
	}
}

// Both routes charge forma2's per-user action:unsplash limit before Unsplash is called.
func TestSpentLimitIsTooManyRequests(t *testing.T) {
	frozen := time.Date(2026, 1, 1, 12, 0, 30, 0, time.UTC)
	limiter := ratelimit.New(config.Config{}, valkeytest.Client(t), slog.New(slog.DiscardHandler)).WithClock(func() time.Time { return frozen })
	e := newEnv(t, accessKey, limiter)
	if err := limiter.ApplyN(context.Background(), ratelimit.ActionUnsplash, e.user.String(), ratelimit.ActionUnsplash.Limit); err != nil {
		t.Fatal(err)
	}
	for _, rec := range []*httptest.ResponseRecorder{
		e.do(t, http.MethodGet, "/api/app/unsplash/search?query=cats", ""),
		e.do(t, http.MethodPost, "/api/app/unsplash/download", `{"downloadUrl":"https://`+e.stub.host()+`/photos/abc/download"}`),
	} {
		if code, _ := errorCode(t, rec); rec.Code != http.StatusTooManyRequests || code != apperr.CodeTooManyRequests {
			t.Errorf("code %d: %s", rec.Code, rec.Body.String())
		}
	}
	if n := len(e.stub.received()); n != 0 {
		t.Errorf("Unsplash received %d requests, want none", n)
	}
	other := e.do(t, http.MethodGet, "/api/app/unsplash/status", "")
	if other.Code != http.StatusOK {
		t.Errorf("status = %d, want it outside the limit", other.Code)
	}
}
