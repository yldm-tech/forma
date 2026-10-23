// Package spicedbtest gives tests a real SpiceDB. One in-memory container serves a whole test binary; each test that asks for it gets the schema installed and every relationship removed, so tests in one package must not run in parallel with each other.
package spicedbtest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	authzed "github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// Image is the SpiceDB version the tests and docker-compose.dev.yml run.
const Image = "authzed/spicedb:v1.52.0"

// Token is the container's preshared key.
const Token = "forma-test-token"

var (
	once     sync.Once
	endpoint string
	setupErr error
)

func start() {
	ctx := context.Background()
	container, err := testcontainers.Run(ctx, Image,
		testcontainers.WithCmd("serve", "--grpc-preshared-key="+Token, "--datastore-engine=memory", "--http-enabled=false", "--telemetry-endpoint="),
		testcontainers.WithExposedPorts("50051/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("grpc server started serving").WithStartupTimeout(90*time.Second)),
	)
	if err != nil {
		setupErr = fmt.Errorf("start spicedb: %w", err)
		return
	}
	endpoint, setupErr = container.PortEndpoint(ctx, "50051/tcp", "")
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

// Empty starts a SpiceDB of the test's own with no schema, terminated when the test ends, for tests that need to observe the empty state.
func Empty(t testing.TB) config.AuthzConfig {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	ctx := context.Background()
	container, err := testcontainers.Run(ctx, Image,
		testcontainers.WithCmd("serve", "--grpc-preshared-key="+Token, "--datastore-engine=memory", "--http-enabled=false", "--telemetry-endpoint="),
		testcontainers.WithExposedPorts("50051/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("grpc server started serving").WithStartupTimeout(90*time.Second)),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("spicedbtest: start spicedb: %v", err)
	}
	addr, err := container.PortEndpoint(ctx, "50051/tcp", "")
	if err != nil {
		t.Fatalf("spicedbtest: %v", err)
	}
	return config.AuthzConfig{Enabled: true, Endpoint: addr, Token: Token, Insecure: true, Consistency: "fully_consistent"}
}
