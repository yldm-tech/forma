package telemetry

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// poolStats is one connection pool's state at a collection.
type poolStats struct {
	idle, inUse, max int64
	// waits is how many acquisitions had to wait for a free connection, and waited the seconds they spent waiting, both since the pool opened. A rising wait rate is the pool exhaustion forma2's jobs retried on (apps/web/lib/jobs/pool-exhaustion.ts), visible before acquisitions start timing out.
	waits  int64
	waited float64
}

// ObserveSQLPool reports a database/sql pool (the one GORM queries through) as the forma_db_pool_* instruments, labelled pool=<name>. The returned function stops the reporting; call it before closing the pool.
func ObserveSQLPool(name string, db *sql.DB) (func(), error) {
	return observePool(otel.GetMeterProvider(), name, func() poolStats {
		s := db.Stats()
		return poolStats{idle: int64(s.Idle), inUse: int64(s.InUse), max: int64(s.MaxOpenConnections), waits: s.WaitCount, waited: s.WaitDuration.Seconds()}
	})
}

// ObservePgxPool reports a pgx pool (the one River works jobs through) like ObserveSQLPool.
func ObservePgxPool(name string, pool *pgxpool.Pool) (func(), error) {
	return observePool(otel.GetMeterProvider(), name, func() poolStats {
		s := pool.Stat()
		return poolStats{idle: int64(s.IdleConns()), inUse: int64(s.AcquiredConns()), max: int64(s.MaxConns()), waits: s.EmptyAcquireCount(), waited: s.EmptyAcquireWaitTime().Seconds()}
	})
}

func observePool(mp metric.MeterProvider, name string, stats func() poolStats) (func(), error) {
	meter := mp.Meter("forma.database")
	connections, err := meter.Int64ObservableGauge("forma_db_pool_connections", metric.WithDescription("Open connections of a database connection pool, by state"), metric.WithUnit("{connection}"))
	if err != nil {
		return nil, fmt.Errorf("create pool connections gauge: %w", err)
	}
	maxConnections, err := meter.Int64ObservableGauge("forma_db_pool_max_connections", metric.WithDescription("Connections a database connection pool may open"), metric.WithUnit("{connection}"))
	if err != nil {
		return nil, fmt.Errorf("create pool size gauge: %w", err)
	}
	waits, err := meter.Int64ObservableCounter("forma_db_pool_waits_total", metric.WithDescription("Connection acquisitions that waited for a free connection"))
	if err != nil {
		return nil, fmt.Errorf("create pool waits counter: %w", err)
	}
	waited, err := meter.Float64ObservableCounter("forma_db_pool_wait_duration_seconds_total", metric.WithDescription("Time spent waiting for a free connection"), metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("create pool wait duration counter: %w", err)
	}
	pool := attribute.String("pool", name)
	registration, err := meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := stats()
		o.ObserveInt64(connections, s.idle, metric.WithAttributes(pool, attribute.String("state", "idle")))
		o.ObserveInt64(connections, s.inUse, metric.WithAttributes(pool, attribute.String("state", "in_use")))
		o.ObserveInt64(maxConnections, s.max, metric.WithAttributes(pool))
		o.ObserveInt64(waits, s.waits, metric.WithAttributes(pool))
		o.ObserveFloat64(waited, s.waited, metric.WithAttributes(pool))
		return nil
	}, connections, maxConnections, waits, waited)
	if err != nil {
		return nil, fmt.Errorf("register pool metrics: %w", err)
	}
	return func() { _ = registration.Unregister() }, nil
}
