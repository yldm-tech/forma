package config

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/repotest"
)

// deploy/docker/forma.sh, the one-click installer, rewrites an installation's .env for the server this package configures; these tests run it with docker and sudo stand-ins on PATH, and check that what it drops and keeps agrees with what Load reads.

// formaSh is the path of deploy/docker/forma.sh.
func formaSh(t *testing.T) string {
	t.Helper()
	return filepath.Join(repotest.Root(t), "deploy/docker/forma.sh")
}

// readEnvNames is every variable the loaders of this package read.
func readEnvNames() map[string]bool {
	names := map[string]bool{}
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for i := range t.NumField() {
			f := t.Field(i)
			if name, _, _ := strings.Cut(f.Tag.Get("env"), ","); name != "" {
				names[name] = true
			} else if f.Type.Kind() == reflect.Struct {
				walk(f.Type)
			}
		}
	}
	for _, v := range []any{Config{}, migrateEnv{}, startupEnv{}, healthcheckEnv{}, authzEnv{}, bullMQEnv{}} {
		walk(reflect.TypeOf(v))
	}
	return names
}

// scriptList is a space-separated list variable of forma.sh.
func scriptList(t *testing.T, name string) []string {
	t.Helper()
	src, err := os.ReadFile(formaSh(t))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^` + name + `="([^"]*)"$`).FindSubmatch(src)
	if m == nil {
		t.Fatalf("forma.sh has no %s", name)
	}
	return strings.Fields(string(m[1]))
}

func TestFormaShCommentsOutOnlyIgnoredVariables(t *testing.T) {
	read := readEnvNames()
	for _, name := range scriptList(t, "FORMA_IGNORED_VARIABLES") {
		if read[name] {
			t.Errorf("forma.sh comments out %s, which the server reads", name)
		}
	}
	for _, name := range []string{"WORKER_CONCURRENCY", "GATEWAY_RATE_LIMITING", "LOG_LEVEL", "DATABASE_URL", "MIGRATE_DATABASE_URL"} {
		if !read[name] {
			t.Errorf("readEnvNames misses %s", name)
		}
	}
}

// requireBash also marks the test parallel: the script runs in its own directory and process, and the tests that set the environment run before parallel ones.
func requireBash(t *testing.T) {
	t.Helper()
	t.Parallel()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
}

// installEnv is an installation in a temporary directory: the release's compose file and an .env that still sets retired variables, with docker and sudo replaced by a script that logs its arguments and answers the database queries from FAKE_DB_*.
type installEnv struct {
	t      *testing.T
	root   string
	dir    string
	log    string
	dbVars []string
}

func newInstall(t *testing.T) *installEnv {
	t.Helper()
	requireBash(t)
	root := t.TempDir()
	e := &installEnv{t: t, root: root, dir: filepath.Join(root, "forma"), log: filepath.Join(root, "docker.log")}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
printf '%s %s\n' "$(basename "$0")" "$*" >> "$FAKE_DOCKER_LOG"
case "$*" in
  *to_regclass*) echo "$FAKE_DB_TABLES" ;;
  *'FROM "User"'*) echo "$FAKE_DB_USERS" ;;
esac
exit 0
`
	for _, name := range []string{"docker", "sudo"} {
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte(fake), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for src, dst := range map[string]string{filepath.Join(repotest.Root(t), "deploy/docker/docker-compose.yml"): "docker-compose.yml", filepath.Join("testdata", "install", "env"): ".env"} {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(e.dir, dst), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *installEnv) run(args ...string) (string, error) {
	e.t.Helper()
	script := formaSh(e.t)
	source := filepath.Join(repotest.Root(e.t), "deploy/docker")
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = e.root
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(e.root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_DOCKER_LOG="+e.log, "FORMA_SOURCE_DIR="+source, "FORMA_DIR=forma",
	)
	cmd.Env = append(cmd.Env, e.dbVars...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

func (e *installEnv) read(name string) string {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.dir, name))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *installEnv) dockerCalls() []string {
	b, _ := os.ReadFile(e.log)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// dotenvValues parses the KEY=value and KEY="value" lines forma.sh writes.
func dotenvValues(t *testing.T, content string) map[string]string {
	t.Helper()
	values := map[string]string{}
	for line := range strings.SplitSeq(content, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf(".env line %q", line)
		}
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			var b strings.Builder
			inner := value[1 : len(value)-1]
			for i := 0; i < len(inner); i++ {
				if inner[i] == '\\' && i+1 < len(inner) {
					i++
				}
				b.WriteByte(inner[i])
			}
			value = b.String()
		}
		values[key] = value
	}
	return values
}

func TestFormaShUpdateMigratesTheEnv(t *testing.T) {
	e := newInstall(t)
	fixtureEnv := e.read(".env")
	out, err := e.run("update")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	release, err := os.ReadFile(filepath.Join(repotest.Root(t), "deploy/docker/docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if e.read("docker-compose.yml") != string(release) {
		t.Error("docker-compose.yml is not the release's")
	}
	envBackups, _ := filepath.Glob(filepath.Join(e.dir, ".env.2*"))
	if len(envBackups) != 1 || e.read(filepath.Base(envBackups[0])) != fixtureEnv {
		t.Errorf("backups %v, want the previous .env unchanged", envBackups)
	}
	for _, name := range []string{"authzed-postgres-bootstrap.sh", "rustfs-init.sh"} {
		if info, err := os.Stat(filepath.Join(e.dir, name)); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", name, info, err)
		}
	}

	env := e.read(".env")
	values := dotenvValues(t, env)
	want := map[string]string{
		// Mapped from FORMA_RUSTFS_*.
		"RUSTFS_ADMIN_USER":     "forma-1a2b3c4d",
		"RUSTFS_ADMIN_PASSWORD": "Zm9ybWEtYWRtaW4tcGFzc3dvcmQ=",
		"S3_ACCESS_KEY":         "forma-service-5e6f7a8b",
		"S3_SECRET_KEY":         "c2VydmljZS1wYXNzd29yZA==",
		"S3_REGION":             "us-east-1",
		"S3_BUCKET_NAME":        "forma-uploads",
		"S3_FORCE_PATH_STYLE":   "1",
		"COMPOSE_PROFILES":      "storage",
		// BULLMQ_WORKER_COUNT x BULLMQ_WORKER_CONCURRENCY.
		"WORKER_CONCURRENCY": "6",
		// Without Prisma's ?schema=public.
		"MIGRATE_DATABASE_URL": "postgresql://owner:secret@postgres:5432/forma?connect_timeout=10",
		// Kept as they were.
		"WEBAPP_URL":        "https://surveys.example.com",
		"ENCRYPTION_KEY":    "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0",
		"POSTGRES_PASSWORD": "pg$$secret",
		"AUTHZED_ENABLED":   "true",
		"LOG_LEVEL":         "warn",
	}
	for k, v := range want {
		if values[k] != v {
			t.Errorf(".env %s = %q, want %q", k, values[k], v)
		}
	}
	read := readEnvNames()
	composeLevel := []string{"POSTGRES_PASSWORD", "POSTGRES_PASSWORD_URL_ENCODED", "AUTHZED_DATABASE_PASSWORD", "RUSTFS_ADMIN_USER", "RUSTFS_ADMIN_PASSWORD", "COMPOSE_PROFILES"}
	for k := range values {
		if !read[k] && !slices.Contains(composeLevel, k) {
			t.Errorf(".env keeps %s, which neither the server nor the compose file reads", k)
		}
	}
	for _, gone := range []string{"CRON_SECRET", "OIDC_SIGNING_ALGORITHM", "FORMA_AUTHZED_V6_MIGRATION_ACKNOWLEDGED", "BULLMQ_WORKER_CONCURRENCY", "BULLMQ_WORKER_COUNT", "FORMA_RUSTFS_ADMIN_USER"} {
		if !regexp.MustCompile(`(?m)^# [^\n]*: ` + gone + `=`).MatchString(env) {
			t.Errorf(".env does not keep %s as a comment", gone)
		}
	}
	if info, err := os.Stat(filepath.Join(e.dir, ".env")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf(".env mode: %v %v", info, err)
	}

	calls := strings.Join(e.dockerCalls(), "\n")
	for _, c := range []string{"compose config -q", "compose pull", "compose up -d --remove-orphans"} {
		if !strings.Contains(calls, c) {
			t.Errorf("docker was not called with %q:\n%s", c, calls)
		}
	}

	// A second update changes nothing.
	before := env
	if out, err := e.run("update"); err != nil {
		t.Fatalf("second update: %v\n%s", err, out)
	}
	if e.read(".env") != before {
		t.Errorf("second update changed .env:\n%s", e.read(".env"))
	}
}

// The compose file the retired Next.js release's installer wrote: its data lives in tables this server does not read, so neither install nor update touches the directory, and nothing is started.
func TestFormaShRefusesAForma2Install(t *testing.T) {
	const forma2Compose = "x-environment: &environment\n  environment:\n    WEBAPP_URL: \"https://surveys.example.com\"\nservices:\n  forma:\n    image: ghcr.io/yldm-tech/forma:3db326f\n"
	for _, command := range []string{"install", "update"} {
		t.Run(command, func(t *testing.T) {
			e := newInstall(t)
			if err := os.WriteFile(filepath.Join(e.dir, "docker-compose.yml"), []byte(forma2Compose), 0o600); err != nil {
				t.Fatal(err)
			}
			env := e.read(".env")
			out, err := e.run(command)
			if err == nil || !strings.Contains(out, "retired Next.js release") {
				t.Fatalf("%s: %v\n%s", command, err, out)
			}
			if e.read("docker-compose.yml") != forma2Compose || e.read(".env") != env {
				t.Errorf("%s changed the installation", command)
			}
			if calls := strings.Join(e.dockerCalls(), "\n"); strings.Contains(calls, "compose up") || strings.Contains(calls, "compose pull") {
				t.Errorf("the stack was started:\n%s", calls)
			}
		})
	}
}

// The server does not read forma2's tables: started on a database where only they hold users, it would serve an empty instance whose first sign-up becomes its administrator, so the update stops before starting anything but PostgreSQL, as the server itself would (database.RefuseUnreadForma2Data). FORMA_IGNORE_FORMA2_DATA=1 skips the check.
func TestFormaShUpdateRefusesUnreadForma2Data(t *testing.T) {
	for name, tc := range map[string]struct {
		db     []string
		ignore bool
		refuse bool
	}{
		"users table empty":    {db: []string{"FAKE_DB_TABLES=true,true", "FAKE_DB_USERS=true,false"}, refuse: true},
		"users table missing":  {db: []string{"FAKE_DB_TABLES=true,false", "FAKE_DB_USERS=true,false"}, refuse: true},
		"own users":            {db: []string{"FAKE_DB_TABLES=true,true", "FAKE_DB_USERS=true,true"}},
		"forma2 table empty":   {db: []string{"FAKE_DB_TABLES=true,true", "FAKE_DB_USERS=false,false"}},
		"no forma2 table":      {db: []string{"FAKE_DB_TABLES=false,true"}},
		"started empty anyway": {db: []string{"FAKE_DB_TABLES=true,true", "FAKE_DB_USERS=true,false"}, ignore: true},
	} {
		t.Run(name, func(t *testing.T) {
			e := newInstall(t)
			e.dbVars = tc.db
			if tc.ignore {
				f, err := os.OpenFile(filepath.Join(e.dir, ".env"), os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.WriteString("FORMA_IGNORE_FORMA2_DATA=1\n"); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			}
			out, err := e.run("update")
			calls := strings.Join(e.dockerCalls(), "\n")
			if tc.refuse {
				if err == nil || !strings.Contains(out, "retired Next.js release") || !strings.Contains(out, "FORMA_IGNORE_FORMA2_DATA=1") {
					t.Fatalf("update: %v\n%s", err, out)
				}
				if strings.Contains(calls, "compose up -d --remove-orphans") {
					t.Errorf("the stack was started:\n%s", calls)
				}
				return
			}
			if err != nil {
				t.Fatalf("update: %v\n%s", err, out)
			}
			if !strings.Contains(calls, "compose up -d --remove-orphans") {
				t.Errorf("the stack was not started:\n%s", calls)
			}
			if tc.ignore && strings.Contains(calls, "psql") {
				t.Errorf("the database was queried although FORMA_IGNORE_FORMA2_DATA=1:\n%s", calls)
			}
		})
	}
}

func TestFormaShStripsSchemaPublic(t *testing.T) {
	requireBash(t)
	script := formaSh(t)
	cases := map[string]string{
		"postgresql://u:p@db:5432/forma?schema=public":                     "postgresql://u:p@db:5432/forma",
		"postgresql://u:p@db/forma?schema=public&sslmode=disable":          "postgresql://u:p@db/forma?sslmode=disable",
		"postgresql://u:p@db/forma?sslmode=disable&SCHEMA=public":          "postgresql://u:p@db/forma?sslmode=disable",
		"postgresql://u:p@db/forma?a=1&schema=public&b=2":                  "postgresql://u:p@db/forma?a=1&b=2",
		"postgresql://u:p@db/forma?schema=tenant":                          "postgresql://u:p@db/forma?schema=tenant",
		"postgresql://u:p@db/forma?search_path=public&application_name=fb": "postgresql://u:p@db/forma?search_path=public&application_name=fb",
	}
	for in, want := range cases {
		got, err := exec.Command("bash", "-c", `source "$1" && strip_schema_public "$2"`, "forma-sh-test", script, in).Output()
		if err != nil || string(got) != want {
			t.Errorf("strip_schema_public(%q) = %q, %v; want %q", in, got, err, want)
		}
		// The server agrees, except that it refuses another schema where the script leaves it for the server to report.
		if server, err := databaseURL("DATABASE_URL", in, true); err == nil && server != want {
			t.Errorf("databaseURL(%q) = %q, forma.sh %q", in, server, want)
		}
	}
}
