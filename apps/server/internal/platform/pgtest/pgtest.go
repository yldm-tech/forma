// Package pgtest gives tests a real, migrated PostgreSQL. One container serves a whole test binary; each test gets its own database cloned from a migrated template, so tests stay isolated without paying for a migration each.
package pgtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
)

const templateDB = "forma_template"

var (
	once      sync.Once
	adminDSN  string
	setupErr  error
	container *tcpostgres.PostgresContainer
)

// start runs PostgreSQL 18 once per test binary and migrates a template database exactly as `forma migrate` does: the goose schema, then River's tables. The container is reaped by testcontainers' Ryuk when the binary exits.
func start() {
	ctx := context.Background()
	container, setupErr = tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("forma"),
		tcpostgres.WithPassword("forma"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if setupErr != nil {
		setupErr = fmt.Errorf("start postgres: %w", setupErr)
		return
	}
	adminDSN, setupErr = container.ConnectionString(ctx, "sslmode=disable")
	if setupErr != nil {
		return
	}
	if setupErr = exec(adminDSN, "CREATE DATABASE "+templateDB); setupErr != nil {
		return
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if setupErr = database.MigrateUp(ctx, withDatabase(adminDSN, templateDB), log); setupErr != nil {
		return
	}
	setupErr = jobs.Migrate(ctx, withDatabase(adminDSN, templateDB), log)
}

func exec(dsn, statement string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(context.Background(), statement)
	return err
}

func withDatabase(dsn, name string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		panic(err)
	}
	u.Path = "/" + name
	return u.String()
}

// DSN returns the connection string of a fresh database with the full schema, dropped when the test ends. Tests that need Docker are skipped under `go test -short`.
func DSN(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	once.Do(start)
	if setupErr != nil {
		t.Fatalf("pgtest: %v", setupErr)
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	name := "t_" + hex.EncodeToString(b[:])
	if err := exec(adminDSN, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateDB)); err != nil {
		t.Fatalf("pgtest: create database: %v", err)
	}
	t.Cleanup(func() {
		_ = exec(adminDSN, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name))
	})
	return withDatabase(adminDSN, name)
}

// GORM opens a GORM handle on a fresh migrated database.
func GORM(t testing.TB) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(DSN(t)), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("pgtest: open gorm: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("pgtest: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
