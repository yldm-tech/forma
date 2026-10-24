// Package spicedbtest gives tests a real SpiceDB on the datastore engine production and docker-compose.dev.yml run: SpiceDB at Image over a PostgreSQL of its own. One SpiceDB serves a whole test binary; each test that asks for it gets the schema installed and every relationship of the given definitions removed, so tests in one package that reset definitions must not run in parallel with each other.
//
// The in-memory engine is deliberately not used. In SpiceDB v1.52.0 it assigns a write transaction its revision before it takes the write lock, so two overlapping writes can commit out of revision order; a fully consistent read then resolves the head revision to a snapshot without the later commit, and an acknowledged write or delete is invisible until the next write (authzed/spicedb#3212, fixed in v1.56.1). The projector issues its deletes ten at a time, so tests on that engine saw relationships survive a delete that had succeeded. PostgreSQL takes its revisions from the database's own snapshots and has no such window.
package spicedbtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	authzed "github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Image is the SpiceDB version the tests and docker-compose.dev.yml run.
const Image = "authzed/spicedb:v1.52.0"

// Token is the container's preshared key.
const Token = "forma-test-token"

const (
	// datastoreImage is the PostgreSQL SpiceDB keeps its relationships in, the major pgtest and docker-compose.dev.yml run.
	datastoreImage = "postgres:18"
	// datastoreHost is the datastore's name on the network it shares with the SpiceDB containers.
	datastoreHost     = "spicedb-datastore"
	datastoreUser     = "spicedb"
	datastorePassword = "spicedb"
)

var (
	datastoreOnce sync.Once
	datastore     struct {
		network  *testcontainers.DockerNetwork
		adminDSN string
	}
	datastoreErr error

	once     sync.Once
	endpoint string
	setupErr error
)

// startDatastore runs one PostgreSQL per test binary on a network of its own. Both are reaped by testcontainers' Ryuk when the binary exits.
func startDatastore() {
	ctx := context.Background()
	nw, err := network.New(ctx)
	if err != nil {
		datastoreErr = fmt.Errorf("create network: %w", err)
		return
	}
	pg, err := tcpostgres.Run(ctx, datastoreImage,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername(datastoreUser),
		tcpostgres.WithPassword(datastorePassword),
		network.WithNetwork([]string{datastoreHost}, nw),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		datastoreErr = fmt.Errorf("start datastore: %w", err)
		return
	}
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		datastoreErr = fmt.Errorf("datastore address: %w", err)
		return
	}
	datastore.network, datastore.adminDSN = nw, dsn
}

func createDatabase(ctx context.Context, name string) error {
	db, err := sql.Open("pgx", datastore.adminDSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, "CREATE DATABASE "+name)
	return err
}

// serve migrates a fresh database on the shared datastore, exactly as `spicedb datastore migrate head` does before every deployment, and starts a SpiceDB over it with no schema. The returned container is non-nil whenever one was created, so the caller can clean it up even on error.
func serve(ctx context.Context) (testcontainers.Container, string, error) {
	datastoreOnce.Do(startDatastore)
	if datastoreErr != nil {
		return nil, "", datastoreErr
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	name := "spicedb_" + hex.EncodeToString(b[:])
	if err := createDatabase(ctx, name); err != nil {
		return nil, "", fmt.Errorf("create datastore database: %w", err)
	}
	engine := []string{"--datastore-engine=postgres", fmt.Sprintf("--datastore-conn-uri=postgres://%s:%s@%s:5432/%s?sslmode=disable", datastoreUser, datastorePassword, datastoreHost, name)}

	migrate, err := testcontainers.Run(ctx, Image,
		testcontainers.WithCmd(append([]string{"datastore", "migrate", "head"}, engine...)...),
		network.WithNetwork(nil, datastore.network),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(90*time.Second)),
	)
	if migrate != nil {
		defer func() { _ = testcontainers.TerminateContainer(migrate) }()
	}
	if err != nil {
		return nil, "", fmt.Errorf("migrate datastore: %w", err)
	}
	state, err := migrate.State(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("migrate datastore: %w", err)
	}
	if state.ExitCode != 0 {
		return nil, "", fmt.Errorf("migrate datastore: exit code %d: %s", state.ExitCode, containerLogs(ctx, migrate))
	}

	container, err := testcontainers.Run(ctx, Image,
		testcontainers.WithCmd(append([]string{"serve", "--grpc-preshared-key=" + Token, "--http-enabled=false", "--telemetry-endpoint="}, engine...)...),
		network.WithNetwork(nil, datastore.network),
		testcontainers.WithExposedPorts("50051/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("grpc server started serving").WithStartupTimeout(90*time.Second)),
	)
	if err != nil {
		return container, "", fmt.Errorf("start spicedb: %w", err)
	}
	addr, err := container.PortEndpoint(ctx, "50051/tcp", "")
	return container, addr, err
}

func containerLogs(ctx context.Context, c testcontainers.Container) string {
	r, err := c.Logs(ctx)
	if err != nil {
		return err.Error()
	}
	defer func() { _ = r.Close() }()
	out, _ := io.ReadAll(io.LimitReader(r, 8<<10))
	return string(out)
}

// start runs the binary's shared SpiceDB, reaped by Ryuk when the binary exits.
func start() {
	_, endpoint, setupErr = serve(context.Background())
}

// Config returns the AuthZed configuration of the shared SpiceDB with schema written and every relationship of the given definitions deleted. Tests that need Docker are skipped under `go test -short`.
func Config(t testing.TB, schema string, definitions ...string) config.AuthzConfig {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	once.Do(start)
	if setupErr != nil {
		t.Fatalf("spicedbtest: %v", setupErr)
	}
	client, err := authzed.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpcutil.WithInsecureBearerToken(Token))
	if err != nil {
		t.Fatalf("spicedbtest: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// SpiceDB accepts connections a moment before it accepts a schema write.
	for attempt := 1; ; attempt++ {
		_, err = client.WriteSchema(ctx, &v1.WriteSchemaRequest{Schema: schema})
		if err == nil || attempt == 20 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("spicedbtest: write schema: %v", err)
	}
	for _, definition := range definitions {
		if _, err := client.DeleteRelationships(ctx, &v1.DeleteRelationshipsRequest{RelationshipFilter: &v1.RelationshipFilter{ResourceType: definition}}); err != nil {
			t.Fatalf("spicedbtest: reset %s: %v", definition, err)
		}
	}
	return config.AuthzConfig{Enabled: true, Endpoint: endpoint, Token: Token, Insecure: true, Consistency: "fully_consistent"}
}

// Empty starts a SpiceDB of the test's own with no schema, on a fresh database of the shared datastore, terminated when the test ends, for tests that need to observe the empty state.
func Empty(t testing.TB) config.AuthzConfig {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	container, addr, err := serve(context.Background())
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("spicedbtest: %v", err)
	}
	return config.AuthzConfig{Enabled: true, Endpoint: addr, Token: Token, Insecure: true, Consistency: "fully_consistent"}
}
