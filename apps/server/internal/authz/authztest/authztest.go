// Package authztest gives a feature module's tests real authorization: an authz.Authorizer over the test's PostgreSQL and the shared SpiceDB test container, and the projection delivery the worker would run, so a test can make what it wrote visible to SpiceDB before it asks.
//
// Tests that use it may run in parallel: they share one SpiceDB, but every relationship names rows of the test's own database, whose ids never collide.
package authztest

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/authz/spicedbtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Env is authorization for one test.
type Env struct {
	Authorizer *authz.Authorizer
	delivery   *authz.Delivery
}

// New builds the authorizer for db with cfg's UserManagementMinimumRole. Tests that need Docker are skipped under `go test -short`.
func New(t testing.TB, cfg config.Config, db *gorm.DB) *Env {
	t.Helper()
	acfg := spicedbtest.Config(t, schema(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := authz.NewClient(acfg, authz.RequestProfile, log)
	if err != nil {
		t.Fatalf("authztest: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Env{Authorizer: authz.NewAuthorizer(cfg, db, client), delivery: authz.NewDelivery(db, client, log)}
}

// Deliver projects every pending outbox event into SpiceDB, as `forma worker` does every five seconds.
func (e *Env) Deliver(t testing.TB) {
	t.Helper()
	res, err := e.delivery.Drain(context.Background(), authz.DefaultDrainBatches)
	if err != nil {
		t.Fatalf("authztest: deliver: %v", err)
	}
	if res.Status != "drained" {
		t.Fatalf("authztest: projection not drained: %+v", res)
	}
}

// Delivery is the projection delivery over the same SpiceDB, for the services that deliver their own writes before they answer.
func (e *Env) Delivery() *authz.Delivery { return e.delivery }

// schema reads authzed/schema.zed, the canonical schema the authz package embeds.
func schema(t testing.TB) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "authzed", "schema.zed"))
	if err != nil {
		t.Fatalf("authztest: read schema: %v", err)
	}
	return string(raw)
}
