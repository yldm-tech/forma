package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrUnreadForma2Data is RefuseUnreadForma2Data's refusal.
var ErrUnreadForma2Data = errors.New(`this database holds the data of the retired Next.js release (forma2): its "User" table has rows, and this server's users table has none. This server does not read forma2's tables, so it would serve an empty instance whose first sign-up becomes its administrator. To keep forma2's data, import it with release v0.2.48 or earlier (forma import-forma2) before moving to this one; to start empty on purpose, set FORMA_IGNORE_FORMA2_DATA=1`)

// RefuseUnreadForma2Data refuses a database where forma2's "User" table has rows while this server's users table is empty or missing: started there, the server would come up empty and admit its first sign-up as administrator. A database without forma2's tables, with an empty "User" table, or with a user of this server's own passes. It only reads, and it uses the simple query protocol on its own short connection, so it works through a transaction-pooling PgBouncer as well.
func RefuseUnreadForma2Data(ctx context.Context, dsn string) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("check for forma2's data: %w", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("check for forma2's data: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	var forma2, own bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('public."User"') IS NOT NULL, to_regclass('public.users') IS NOT NULL`).Scan(&forma2, &own); err != nil {
		return fmt.Errorf("check for forma2's data: %w", err)
	}
	if !forma2 {
		return nil
	}
	query := `SELECT EXISTS (SELECT 1 FROM "User"), false`
	if own {
		query = `SELECT EXISTS (SELECT 1 FROM "User"), EXISTS (SELECT 1 FROM users)`
	}
	var forma2Users, ownUsers bool
	if err := conn.QueryRow(ctx, query).Scan(&forma2Users, &ownUsers); err != nil {
		return fmt.Errorf("check for forma2's data: %w", err)
	}
	if forma2Users && !ownUsers {
		return ErrUnreadForma2Data
	}
	return nil
}
