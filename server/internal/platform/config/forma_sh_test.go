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
)

// docker/forma.sh, the one-click installer, rewrites a forma2 installation's compose file and .env for the server this package configures; these tests run it against forma2's files with a docker stand-in on PATH, and check that what it drops and keeps agrees with what Load reads.

const formaSh = "../../../../docker/forma.sh"

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
	src, err := os.ReadFile(formaSh)
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

// installEnv is a forma2 installation in a temporary directory, with docker replaced by a script that logs its arguments and answers the database queries from FAKE_DB_*.
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
printf '%s\n' "$*" >> "$FAKE_DOCKER_LOG"
case "$*" in
  *to_regclass*) echo "$FAKE_DB_TABLES" ;;
  *'FROM "User"'*) echo "$FAKE_DB_FORMA2_USERS" ;;
  *'FROM users'*) echo "$FAKE_DB_GO_USERS" ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(root, "bin", "docker"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for src, dst := range map[string]string{"docker-compose.yml": "docker-compose.yml", "env": ".env"} {
		b, err := os.ReadFile(filepath.Join("testdata", "forma2-install", src))
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
	script, err := filepath.Abs(formaSh)
	if err != nil {
		e.t.Fatal(err)
	}
	source, err := filepath.Abs("../../../../docker")
	if err != nil {
		e.t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = e.root
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(e.root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_DOCKER_LOG="+e.log, "FORMA_SOURCE_DIR="+source, "FORMA_DIR=forma",
	)
	cmd.Env = append(cmd.Env, e.dbVars...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
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

func TestFormaShUpdateMigratesAForma2Install(t *testing.T) {
	e := newInstall(t)
	e.dbVars = []string{"FAKE_DB_TABLES=true,true", "FAKE_DB_FORMA2_USERS=t", "FAKE_DB_GO_USERS=t"}
	out, err := e.run("update")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}

	// The release's compose file replaces forma2's, which is kept.
	release, err := os.ReadFile("../../../../docker/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if e.read("docker-compose.yml") != string(release) {
		t.Error("docker-compose.yml is not the release's")
	}
	fixture, _ := os.ReadFile("testdata/forma2-install/docker-compose.yml")
	fixtureEnv, _ := os.ReadFile("testdata/forma2-install/env")
	backups, _ := filepath.Glob(filepath.Join(e.dir, "docker-compose.yml.forma2-*"))
	envBackups, _ := filepath.Glob(filepath.Join(e.dir, ".env.2*"))
	if len(backups) != 1 || len(envBackups) != 1 || e.read(filepath.Base(backups[0])) != string(fixture) || e.read(filepath.Base(envBackups[0])) != string(fixtureEnv) {
		t.Errorf("backups %v %v, want forma2's compose file and .env unchanged", backups, envBackups)
	}
	for _, name := range []string{"authzed-postgres-bootstrap.sh", "rustfs-init.sh"} {
		if info, err := os.Stat(filepath.Join(e.dir, name)); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", name, info, err)
		}
	}

	env := e.read(".env")
	values := dotenvValues(t, env)
	want := map[string]string{
		// Inline in forma2's compose file; the ${FORMA_RUSTFS_*} references resolved from .env.
		"WEBAPP_URL":                  "https://surveys.example.com",
		"NEXTAUTH_URL":                "https://surveys.example.com",
		"NEXTAUTH_SECRET":             "5a1d0c1b2e3f405162738495a6b7c8d9e0f1a2b3c4d5e6f708192a3b4c5d6e7f",
		"ENCRYPTION_KEY":              "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0",
		"MAIL_FROM":                   "noreply@example.com",
		"MAIL_FROM_NAME":              "Example Surveys",
		"SMTP_PORT":                   "587",
		"SMTP_PASSWORD":               `p@ss "word" #1`,
		"S3_ACCESS_KEY":               "forma-service-5e6f7a8b",
		"S3_SECRET_KEY":               "c2VydmljZS1wYXNzd29yZA==",
		"S3_REGION":                   "us-east-1",
		"S3_BUCKET_NAME":              "forma-uploads",
		"S3_ENDPOINT_URL":             "https://files.surveys.example.com",
		"S3_FORCE_PATH_STYLE":         "1",
		"EMAIL_VERIFICATION_DISABLED": "1",
		"PASSWORD_RESET_DISABLED":     "1",
		"REDIS_URL":                   "redis://redis:6379",
		// Mapped.
		"RUSTFS_ADMIN_USER":     "forma-1a2b3c4d",
		"RUSTFS_ADMIN_PASSWORD": "Zm9ybWEtYWRtaW4tcGFzc3dvcmQ=",
		"COMPOSE_PROFILES":      "storage",
		"WORKER_CONCURRENCY":    "6",
		"MIGRATE_DATABASE_URL":  "postgresql://owner:secret@postgres:5432/forma?connect_timeout=10",
		// Kept as they were.
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

	// Traefik moves into the override file, without forma2's dangling dependencies.
	override := e.read("docker-compose.override.yml")
	for _, s := range []string{"  forma:\n    labels:\n", "traefik.http.routers.forma.rule=Host(`surveys.example.com`)", "  rustfs:\n    labels:\n", "traefik.http.routers.rustfs-s3.rule=Host(`files.surveys.example.com`)", "  traefik:\n    image: \"traefik:v3.6.4\"", "      - forma\n", "/var/run/docker.sock"} {
		if !strings.Contains(override, s) {
			t.Errorf("override lacks %q:\n%s", s, override)
		}
	}
	if strings.Contains(override, "- hub") || strings.Contains(override, "      - rustfs\n") || strings.Contains(override, "environment:") {
		t.Errorf("override keeps more than the proxy:\n%s", override)
	}

	calls := strings.Join(e.dockerCalls(), "\n")
	for _, c := range []string{"compose config -q", "compose pull", "compose up -d --wait postgres", "compose up -d --remove-orphans"} {
		if !strings.Contains(calls, c) {
			t.Errorf("docker was not called with %q:\n%s", c, calls)
		}
	}

	// A second update finds a Go installation and changes nothing.
	before := env
	if out, err := e.run("update"); err != nil {
		t.Fatalf("second update: %v\n%s", err, out)
	}
	if e.read(".env") != before {
		t.Errorf("second update changed .env:\n%s", e.read(".env"))
	}
}

// The Go server keeps its data in its own schema: started on a database where only forma2's tables hold users, it would serve an empty instance whose first sign-up becomes its owner, so the update stops before starting anything.
func TestFormaShUpdateRefusesADatabaseWithoutTheMigratedData(t *testing.T) {
	requireBash(t)
	for name, db := range map[string][]string{
		"go schema absent": {"FAKE_DB_TABLES=true,false", "FAKE_DB_FORMA2_USERS=t"},
		"go schema empty":  {"FAKE_DB_TABLES=true,true", "FAKE_DB_FORMA2_USERS=t", "FAKE_DB_GO_USERS=f"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newInstall(t)
			e.dbVars = db
			out, err := e.run("update")
			if err == nil || !strings.Contains(out, "still holds forma2's data") {
				t.Fatalf("update: %v\n%s", err, out)
			}
			if calls := strings.Join(e.dockerCalls(), "\n"); strings.Contains(calls, "--remove-orphans") {
				t.Errorf("the stack was started:\n%s", calls)
			}
		})
	}
	// A fresh database has nothing to lose.
	t.Run("fresh database", func(t *testing.T) {
		e := newInstall(t)
		e.dbVars = []string{"FAKE_DB_TABLES=false,false"}
		if out, err := e.run("update"); err != nil {
			t.Fatalf("update on an empty database: %v\n%s", err, out)
		}
	})
}

func TestFormaShStripsSchemaPublic(t *testing.T) {
	requireBash(t)
	script, err := filepath.Abs(formaSh)
	if err != nil {
		t.Fatal(err)
	}
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
		if server, err := databaseURL("DATABASE_URL", in); err == nil && server != want {
			t.Errorf("databaseURL(%q) = %q, forma.sh %q", in, server, want)
		}
	}
}
