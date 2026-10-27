package instance

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/buildinfo"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/apps/server/internal/storage"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const testUserHeader = "X-Test-User"

// headerSessions stands in for the credential module's sessions, which have their own tests.
type headerSessions struct{}

func (headerSessions) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.GetHeader(testUserHeader))
		if err != nil {
			httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
			return
		}
		httpapi.SetActor(c, httpapi.Actor{UserID: id})
		c.Next()
	}
}

func (headerSessions) Optional() gin.HandlerFunc { return func(c *gin.Context) { c.Next() } }

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func engine(t *testing.T, cfg config.Config, releases *Releases) *gin.Engine {
	t.Helper()
	bucket, err := storage.NewBucket(cfg, discard)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	NewHandler(cfg, bucket, releases, headerSessions{}).Register(r.Group("/api/app", httpapi.ErrorHandler(discard)))
	return r
}

func get(r http.Handler, path string, user bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if user {
		req.Header.Set(testUserHeader, uuid.NewString())
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestInstanceNeedsASession(t *testing.T) {
	t.Parallel()
	r := engine(t, config.Config{}, nil)
	for _, path := range []string{"/api/app/instance", "/api/app/instance/latest-release"} {
		if rec := get(r, path, false); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a session = %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestInstanceReportsStorage(t *testing.T) {
	t.Parallel()
	endpoint, _ := url.Parse("http://localhost:9000")
	configured := config.S3Config{BucketName: "uploads", AccessKey: "key", SecretKey: "secret", EndpointURL: endpoint}
	for _, tc := range []struct {
		name string
		s3   config.S3Config
		want bool
	}{
		{"no bucket", config.S3Config{}, false},
		{"bucket", configured, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(engine(t, config.Config{S3: tc.s3}, nil), "/api/app/instance", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			var info Info
			if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
				t.Fatal(err)
			}
			if info != (Info{StorageConfigured: tc.want, CustomScripts: config.CustomScriptsNeedPublicURL, Version: buildinfo.Version}) {
				t.Errorf("info = %+v", info)
			}
		})
	}
}

// The SPA learns whether it may offer custom head scripts, and what to tell the user when it may not.
func TestInstanceReportsCustomScripts(t *testing.T) {
	t.Parallel()
	webApp, _ := url.Parse("https://forma.example.com")
	surveys, _ := url.Parse("https://surveys.example.com")
	for _, tc := range []struct {
		name string
		cfg  config.Config
		want config.CustomScripts
	}{
		{"no PUBLIC_URL", config.Config{WebAppURL: webApp}, config.CustomScriptsNeedPublicURL},
		{"a survey domain", config.Config{WebAppURL: webApp, PublicURL: surveys}, config.CustomScriptsAvailable},
		{"Forma Cloud", config.Config{WebAppURL: webApp, PublicURL: surveys, IsFormaCloud: true}, config.CustomScriptsFormaCloud},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(engine(t, tc.cfg, nil), "/api/app/instance", true)
			var info map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil || rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			if info["customScripts"] != string(tc.want) {
				t.Errorf("customScripts = %v, want %q", info["customScripts"], tc.want)
			}
		})
	}
}

// github answers the latest-release lookup with status and body, counting the requests.
func github(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func releasesFor(cache *redis.Client, srv *httptest.Server) *Releases {
	r := NewReleases(cache, discard)
	r.url = srv.URL
	return r
}

func latest(t *testing.T, r http.Handler) *string {
	t.Helper()
	rec := get(r, "/api/app/instance/latest-release", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var res LatestRelease
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.TagName
}

// A found release is cached for an hour, so GitHub is asked once.
func TestLatestReleaseIsCached(t *testing.T) {
	t.Parallel()
	cache := valkeytest.Client(t)
	srv, hits := github(t, http.StatusOK, `{"tag_name":"v0.2.99","name":"v0.2.99"}`)
	r := engine(t, config.Config{}, releasesFor(cache, srv))
	for range 2 {
		if tag := latest(t, r); tag == nil || *tag != "v0.2.99" {
			t.Fatalf("tag = %v", tag)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("GitHub was asked %d times", hits.Load())
	}
	if ttl := cache.TTL(t.Context(), cacheKey).Val(); ttl <= failureTTL || ttl > successTTL {
		t.Errorf("ttl = %v", ttl)
	}
}

// A failed lookup answers null and is remembered for a few minutes only.
func TestLatestReleaseFailureIsCachedBriefly(t *testing.T) {
	t.Parallel()
	cache := valkeytest.Client(t)
	srv, hits := github(t, http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
	r := engine(t, config.Config{}, releasesFor(cache, srv))
	if tag := latest(t, r); tag != nil {
		t.Fatalf("tag = %q", *tag)
	}
	if tag := latest(t, r); tag != nil || hits.Load() != 1 {
		t.Fatalf("tag = %v after %d lookups", tag, hits.Load())
	}
	if ttl := cache.TTL(t.Context(), cacheKey).Val(); ttl <= 0 || ttl > failureTTL {
		t.Errorf("ttl = %v", ttl)
	}
}

// An answer without a string tag_name is no release, cached like a found one, as forma2 did.
func TestLatestReleaseWithoutATag(t *testing.T) {
	t.Parallel()
	cache := valkeytest.Client(t)
	srv, _ := github(t, http.StatusOK, `{"tag_name":42}`)
	r := engine(t, config.Config{}, releasesFor(cache, srv))
	if tag := latest(t, r); tag != nil {
		t.Fatalf("tag = %q", *tag)
	}
	if ttl := cache.TTL(t.Context(), cacheKey).Val(); ttl < time.Hour-time.Minute {
		t.Errorf("ttl = %v", ttl)
	}
}
