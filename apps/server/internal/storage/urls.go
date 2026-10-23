package storage

import (
	"regexp"
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// storageURLPattern is forma2's STORAGE_URL_PATTERN, a relative URL of a stored file.
var storageURLPattern = regexp.MustCompile(`^/storage/[^/]+/(public|private)/.+`)

// ResolveURLs is forma2's resolveStorageUrlsInObject on a decoded JSON value: every string that is a relative storage URL, at any depth, made absolute, a public file's against the public domain (PUBLIC_URL, else WEBAPP_URL) and a private one's against WEBAPP_URL. Everything else is returned as it was.
func ResolveURLs(cfg config.Config, v any) any {
	switch x := v.(type) {
	case string:
		m := storageURLPattern.FindStringSubmatch(x)
		if m == nil {
			return x
		}
		base := cfg.WebAppURL
		if m[1] == "public" && cfg.PublicURL != nil {
			base = cfg.PublicURL
		}
		return strings.TrimRight(base.String(), "/") + x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = ResolveURLs(cfg, e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = ResolveURLs(cfg, e)
		}
		return out
	}
	return v
}
