// Package s3test gives tests a real S3-compatible object store: the RustFS release docker-compose.dev.yml runs. One container serves a whole test binary; each test gets a bucket of its own.
package s3test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// Image is the RustFS release the tests and docker-compose.dev.yml run, forma2's.
const Image = "rustfs/rustfs:1.0.0-rc.2@sha256:7d6d361c49c08d427250fb59aae5d78df83d644c3405d9ccf4b21cda0b0692d0"

const (
	accessKey = "forma-test"
	secretKey = "forma-test-secret"
	region    = "us-east-1"
)

var (
	once     sync.Once
	endpoint *url.URL
	admin    *minio.Client
	setupErr error
)

// start runs RustFS once per test binary and waits until it answers S3 requests. The container is reaped by testcontainers' Ryuk when the binary exits.
func start() {
	ctx := context.Background()
	container, err := testcontainers.Run(ctx, Image,
		testcontainers.WithEnv(map[string]string{"RUSTFS_ACCESS_KEY": accessKey, "RUSTFS_SECRET_KEY": secretKey, "RUSTFS_ADDRESS": ":9000"}),
		testcontainers.WithCmd("/data"),
		testcontainers.WithExposedPorts("9000/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("9000/tcp").WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		setupErr = fmt.Errorf("start rustfs: %w", err)
		return
	}
	host, err := container.PortEndpoint(ctx, "9000/tcp", "http")
	if err != nil {
		setupErr = err
		return
	}
	if endpoint, setupErr = url.Parse(host); setupErr != nil {
		return
	}
	admin, setupErr = minio.New(endpoint.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Region: region, BucketLookup: minio.BucketLookupPath})
	if setupErr != nil {
		return
	}
	// The port opens before the server takes requests.
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := admin.ListBuckets(ctx); err == nil {
			return
		} else if time.Now().After(deadline) {
			setupErr = fmt.Errorf("rustfs did not become ready: %w", err)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Config returns forma's S3 settings for a new, empty bucket on the shared server, addressed path-style as forma2's self-hosted setup does. Tests that need Docker are skipped under `go test -short`.
func Config(t testing.TB) config.S3Config {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	once.Do(start)
	if setupErr != nil {
		t.Fatalf("s3test: %v", setupErr)
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	bucket := "forma-" + hex.EncodeToString(b[:])
	if err := admin.MakeBucket(t.Context(), bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		t.Fatalf("s3test: create bucket: %v", err)
	}
	u := *endpoint
	return config.S3Config{AccessKey: accessKey, SecretKey: secretKey, Region: region, EndpointURL: &u, ForcePathStyle: true, BucketName: bucket}
}

// Client is a client with full access to the server, for a test to arrange or inspect objects directly.
func Client(t testing.TB) *minio.Client {
	t.Helper()
	once.Do(start)
	if setupErr != nil {
		t.Fatalf("s3test: %v", setupErr)
	}
	return admin
}
