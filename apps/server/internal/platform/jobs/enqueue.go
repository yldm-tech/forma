package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"gorm.io/gorm"
)

// Enqueuer inserts jobs in the transaction of the change that causes them, from processes that do not work jobs (`forma serve`). It holds no pool of its own: River's insert-only client writes through the transaction it is handed.
type Enqueuer struct {
	client  *river.Client[pgx.Tx]
	metrics *jobMetrics
}

func NewEnqueuer(log *slog.Logger) (*Enqueuer, error) {
	client, err := river.NewClient(riverpgxv5.New(nil), &river.Config{Logger: log, MaxAttempts: maxAttempts})
	if err != nil {
		return nil, fmt.Errorf("create job enqueuer: %w", err)
	}
	return &Enqueuer{client: client, metrics: defaultJobMetrics()}, nil
}

// Tx is a GORM transaction that can also enqueue jobs: a job enqueued through it commits or rolls back with the transaction's writes.
type Tx struct {
	*gorm.DB
	conn    *sql.Conn
	client  *river.Client[pgx.Tx]
	metrics *jobMetrics
}

// Enqueue inserts a job inside the transaction.
func (t *Tx) Enqueue(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) error {
	_, err := t.EnqueueUnique(ctx, args, opts)
	return err
}

// EnqueueUnique is Enqueue for a unique job kind: inserted is false when River skipped the job as a duplicate of one its unique options match, still queued or running.
func (t *Tx) EnqueueUnique(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (inserted bool, err error) {
	inserted, err = t.enqueue(ctx, args, opts)
	t.metrics.recordEnqueue(ctx, args.Kind(), err)
	return inserted, err
}

func (t *Tx) enqueue(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (inserted bool, err error) {
	err = t.conn.Raw(func(driverConn any) error {
		c, ok := driverConn.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("enqueue %s: database driver is %T, not pgx", args.Kind(), driverConn)
		}
		res, err := t.client.InsertTx(ctx, connTx{c.Conn()}, args, opts)
		if err != nil {
			return fmt.Errorf("enqueue %s: %w", args.Kind(), err)
		}
		inserted = !res.UniqueSkippedAsDuplicate
		return nil
	})
	return inserted, err
}

// Transaction runs fn in a transaction on one dedicated connection of db's pool. GORM's transaction is a database/sql one and River inserts through pgx, so Enqueue reaches the pgx connection underneath the same session: both write inside the one open transaction, which fn's return commits or rolls back as GORM's Transaction does.
func (e *Enqueuer) Transaction(ctx context.Context, db *gorm.DB, fn func(tx *Tx) error) error {
	return db.WithContext(ctx).Connection(func(session *gorm.DB) error {
		conn, ok := session.Statement.ConnPool.(*sql.Conn)
		if !ok {
			return fmt.Errorf("job transaction: connection is %T, not *sql.Conn", session.Statement.ConnPool)
		}
		return session.Transaction(func(tx *gorm.DB) error {
			return fn(&Tx{DB: tx, conn: conn, client: e.client, metrics: e.metrics})
		})
	})
}

// errNotATransaction is what connTx answers to the transaction-control calls River's insert path never makes: the transaction belongs to database/sql, which alone may end it.
var errNotATransaction = errors.New("the job transaction is controlled by database/sql")

// connTx presents the pgx connection beneath an open database/sql transaction as the pgx.Tx River inserts through. Statements run on the connection and so inside that transaction.
type connTx struct {
	conn *pgx.Conn
}

func (connTx) Begin(context.Context) (pgx.Tx, error) { return nil, errNotATransaction }
func (connTx) Commit(context.Context) error          { return errNotATransaction }
func (connTx) Rollback(context.Context) error        { return errNotATransaction }
func (connTx) LargeObjects() pgx.LargeObjects        { return pgx.LargeObjects{} }
func (t connTx) Conn() *pgx.Conn                     { return t.conn }

func (t connTx) CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, rows pgx.CopyFromSource) (int64, error) {
	return t.conn.CopyFrom(ctx, table, columns, rows)
}

func (t connTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return t.conn.SendBatch(ctx, b)
}

func (t connTx) Prepare(ctx context.Context, name, query string) (*pgconn.StatementDescription, error) {
	return t.conn.Prepare(ctx, name, query)
}

func (t connTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return t.conn.Exec(ctx, query, args...)
}

func (t connTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	return t.conn.Query(ctx, query, args...)
}

func (t connTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return t.conn.QueryRow(ctx, query, args...)
}
