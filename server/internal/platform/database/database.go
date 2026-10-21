// Package database opens the PostgreSQL connection pool shared by every repository.
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/yldm-tech/forma/server/internal/platform/config"
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

	cleanup := func() {
		if err := sqlDB.Close(); err != nil {
			log.Error("close database", "err", err)
		}
	}
	return db, cleanup, nil
}
