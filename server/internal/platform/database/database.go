// Package database opens the PostgreSQL connection pool shared by every repository.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/telemetry"
)

// Open connects and verifies the connection with a ping, so a wrong DATABASE_URL fails at startup rather than on the first request. The returned cleanup closes the pool.
func Open(ctx context.Context, cfg config.Config, log *slog.Logger) (*gorm.DB, func(), error) {
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{
		Logger: logger.NewSlogLogger(log, logger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
		}),
		// GORM stamps CreatedAt/UpdatedAt from NowFunc; keep them UTC so no row depends on the host time zone.
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.DatabaseMaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DatabaseMaxOpenConns)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("ping database: %w", err)
	}

	stopObserving, err := telemetry.ObserveSQLPool("main", sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	cleanup := func() {
		stopObserving()
		if err := sqlDB.Close(); err != nil {
			log.Error("close database", "err", err)
		}
	}
	return db, cleanup, nil
}

// serializableAttempts is forma2's MAX_SERIALIZABLE_TRANSACTION_ATTEMPTS.
const serializableAttempts = 3

// Serializable runs fn in a Serializable transaction, retried on a serialization failure or deadlock up to three attempts. A guard that reads an invariant (an organization keeps an active owner) in the same transaction as the write it protects holds only against writers that are serializable too: PostgreSQL detects the conflict between two such transactions and aborts one of them, but not between a serializable transaction and one at a weaker isolation level, so every writer of the guarded rows must use this.
func Serializable(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	var err error
	for attempt := 1; attempt <= serializableAttempts; attempt++ {
		err = db.WithContext(ctx).Transaction(fn, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if !isTransactionConflict(err) {
			return err
		}
	}
	return err
}

func isTransactionConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}
