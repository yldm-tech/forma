package instance

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yldm-tech/forma/server/internal/platform/valkey"
)

// latestReleaseURL is the GitHub release of this repository that release.yml publishes.
const latestReleaseURL = "https://api.github.com/repos/yldm-tech/forma/releases/latest"

// cacheKey holds the latest release of this repository. forma2 caches the same shape under "fb:github:latest_release" but for another repository (forma/forma), so sharing its key would show either server the other's release.
const cacheKey = valkey.KeyPrefix + "github:latest_release"

// forma2's timings: GitHub gets five seconds, a found release is cached for an hour and a failed lookup for five minutes, so the notice comes back soon after GitHub recovers while the deployment stays under GitHub's unauthenticated rate limit.
const (
	fetchTimeout = 5 * time.Second
	successTTL   = time.Hour
	failureTTL   = 5 * time.Minute
)

// Releases looks up the latest stable release on GitHub through the shared cache.
type Releases struct {
	cache  *redis.Client
	client *http.Client
	url    string
	log    *slog.Logger
}

func NewReleases(cache *redis.Client, log *slog.Logger) *Releases {
	return &Releases{cache: cache, client: &http.Client{Timeout: fetchTimeout}, url: latestReleaseURL, log: log}
}

// cachedRelease is forma2's CachedRelease.
type cachedRelease struct {
	TagName *string `json:"tagName"`
}

// Latest is forma2's getLatestStableFbRelease: the tag of the latest release, or nil when GitHub is rate limited, unreachable or answers something unexpected. It never fails; a cache outage only costs a lookup.
func (r *Releases) Latest(ctx context.Context) *string {
	b, err := r.cache.Get(ctx, cacheKey).Bytes()
	if err == nil {
		var cached cachedRelease
		if json.Unmarshal(b, &cached) == nil {
			return cached.TagName
		}
	} else if !errors.Is(err, redis.Nil) {
		r.log.WarnContext(ctx, "Failed to read cached Forma release", "err", err)
	}
	tag, failed := r.fetch(ctx)
	ttl := successTTL
	if failed {
		ttl = failureTTL
	}
	b, _ = json.Marshal(cachedRelease{TagName: tag})
	if err := r.cache.Set(ctx, cacheKey, b, ttl).Err(); err != nil {
		r.log.WarnContext(ctx, "Failed to cache Forma release", "err", err)
	}
	return tag
}

func (r *Releases) fetch(ctx context.Context) (tag *string, failed bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		r.log.WarnContext(ctx, "Failed to fetch latest stable Forma release from GitHub", "err", err)
		return nil, true
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := r.client.Do(req)
	if err != nil {
		r.log.WarnContext(ctx, "Failed to fetch latest stable Forma release from GitHub", "err", err)
		return nil, true
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		r.log.WarnContext(ctx, "Failed to fetch latest stable Forma release from GitHub", "status", res.StatusCode)
		return nil, true
	}
	var release map[string]any
	if err := json.NewDecoder(res.Body).Decode(&release); err != nil {
		r.log.WarnContext(ctx, "Failed to fetch latest stable Forma release from GitHub", "err", err)
		return nil, true
	}
	if name, ok := release["tag_name"].(string); ok {
		return &name, false
	}
	return nil, false
}
