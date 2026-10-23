package valkey_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/server/internal/platform/valkey"
)

// sharedForma2Keys lists every "fb:" key literal in the server, by file under internal/, with why sharing the name with forma2 is safe. forma2 and this server share one Valkey during the cutover and after a rollback, so a key that keeps forma2's name must keep forma2's meaning and value format, or be built from ids forma2 never uses. Any other key goes under valkey.KeyPrefix.
var sharedForma2Keys = map[string]string{
	"platform/ratelimit/ratelimit.go fb:":                            "forma2's rate-limit counters: the same fb:rate_limit:<namespace>:<identifier>:<window start> keys, integer counters kept by the same script, so both servers draw on one budget",
	"auth/throttle.go fb:rate_limit:auth:":                           "forma2's shouldLogAuthFailure sorted set: the same hashed identity, bucket and <ms>:<random> members",
	"usagereport/usagereport.go fb:analytics:telemetry_lock":         `forma2's usage report lock, the same "locked" value, so one report is sent per instance`,
	"usagereport/usagereport.go fb:analytics:telemetry_last_sent_ts": "forma2's last report time, the same JSON string of milliseconds",
	"clientapi/environment.go fb:env:":                               "keyed by this server's workspace UUID; forma2's workspace ids are cuids",
	"clientapi/user.go fb:env:":                                      "keyed by this server's workspace UUID; forma2's workspace ids are cuids",
	"linksurvey/repository.go fb:env:":                               "keyed by this server's workspace UUID; forma2's workspace ids are cuids",
	"linksurvey/repository.go fb:response:":                          "keyed by this server's survey UUID; forma2's survey ids are cuids",
	"billing/engine.go fb:org:":                                      "keyed by this server's organization UUID; forma2's organization ids are cuids",
}

func TestKeyPrefixIsNotForma2s(t *testing.T) {
	for _, forma2 := range []string{"fb:", "{forma:jobs}"} {
		// Neither may be a prefix of the other.
		if n := min(len(forma2), len(valkey.KeyPrefix)); forma2[:n] == valkey.KeyPrefix[:n] {
			t.Errorf("KeyPrefix %q overlaps forma2's %q", valkey.KeyPrefix, forma2)
		}
	}
}

// A new "fb:" key fails here until it is either moved under KeyPrefix or listed in sharedForma2Keys with the reason sharing it is safe.
func TestEveryForma2KeyIsReviewed(t *testing.T) {
	root := filepath.Join("..", "..")
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if v, err := strconv.Unquote(lit.Value); err == nil && strings.HasPrefix(v, "fb:") {
				found = append(found, filepath.ToSlash(rel)+" "+v)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range found {
		if _, ok := sharedForma2Keys[k]; !ok {
			t.Errorf("%s: a key in forma2's namespace that is not reviewed; move it under valkey.KeyPrefix or list it in sharedForma2Keys", k)
		}
	}
	for k := range sharedForma2Keys {
		if !slices.Contains(found, k) {
			t.Errorf("%s is listed in sharedForma2Keys but no longer in the code", k)
		}
	}
}
