package emailembed

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/repotest"
)

var update = flag.Bool("update", false, "rewrite locales/*.json from apps/web/locales")

// locales/*.json must stay the keys of apps/web/locales the preview uses; run with -update after changing apps/web/locales.
func TestLocalesMatchWebLocales(t *testing.T) {
	for _, locale := range Locales {
		b, err := os.ReadFile(filepath.Join(repotest.Root(t), "apps/web/locales", locale+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var web map[string]any
		if err := json.Unmarshal(b, &web); err != nil {
			t.Fatal(err)
		}
		messages := map[string]string{}
		for _, key := range catalogKeys {
			var v any = web
			for _, part := range strings.Split(key, ".") {
				m, _ := v.(map[string]any)
				v = m[part]
			}
			s, ok := v.(string)
			if !ok {
				t.Fatalf("apps/web/locales/%s.json has no %s", locale, key)
			}
			messages[key] = s
		}
		want, err := json.MarshalIndent(messages, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, '\n')
		path := filepath.Join("locales", locale+".json")
		if *update {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from apps/web/locales/%s.json; run go test ./internal/survey/emailembed -run TestLocalesMatchWebLocales -update", path, locale)
		}
	}
}
