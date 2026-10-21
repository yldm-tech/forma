// Package valkeytest gives tests a real Valkey. One container serves a whole test binary; each test holds a logical database of its own until it ends, flushed before use.
package valkeytest

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Image is the Valkey version the tests and docker-compose.dev.yml run.
const Image = "valkey/valkey:8.1"

// databases is how many logical databases the container is started with; database 0 is left alone. A test holds its database until it ends, so this bounds how many tests use Valkey at once; one more waits for a database to be released rather than share one, since a shared database would be flushed under a running test.
const databases = 64

var (
	once     sync.Once
	addr     string
	setupErr error
	free     chan int
)

// start runs Valkey once per test binary. The container is reaped by testcontainers' Ryuk when the binary exits.
func start() {
	ctx := context.Background()
	container, err := testcontainers.Run(ctx, Image,
		testcontainers.WithCmd("valkey-server", "--databases", strconv.Itoa(databases)),
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections").WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		setupErr = fmt.Errorf("start valkey: %w", err)
		return
	}
	addr, setupErr = container.PortEndpoint(ctx, "6379/tcp", "")
	free = make(chan int, databases-1)
	for db := 1; db < databases; db++ {
		free <- db
	}
}

// URL returns a redis:// URL for an empty database the test holds until it ends. Tests that need Docker are skipped under `go test -short`.
func URL(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	once.Do(start)
	if setupErr != nil {
		t.Fatalf("valkeytest: %v", setupErr)
	}
	db := <-free
	t.Cleanup(func() { free <- db })
	url := fmt.Sprintf("redis://%s/%d", addr, db)

	client := redis.NewClient(&redis.Options{Addr: addr, DB: db})
	defer func() { _ = client.Close() }()
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("valkeytest: flush database %d: %v", db, err)
	}
	return url
}

// Client returns a client on a fresh database, closed when the test ends.
func Client(t testing.TB) *redis.Client {
	t.Helper()
	opts, err := redis.ParseURL(URL(t))
	if err != nil {
		t.Fatalf("valkeytest: %v", err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	return client
}
