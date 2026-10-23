package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// WaitOptions bound WaitReachable: the whole wait, the pause between attempts and each connection attempt. forma2's migration job waited 900 s, retrying every 5 s with 5 s per attempt.
type WaitOptions struct {
	Timeout           time.Duration
	Interval          time.Duration
	ConnectionTimeout time.Duration
}

// WaitReachable waits until the PostgreSQL server of databaseURL accepts TCP connections, forma2's wait-for-database step of the migration job. It only opens and closes a socket, so it needs no credentials to be valid yet and never logs the URL; failures are reported by error class alone.
func WaitReachable(ctx context.Context, databaseURL string, opts WaitOptions, log *slog.Logger) error {
	address, err := tcpAddress(databaseURL)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(opts.Timeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	lastError := "UNKNOWN"
	for attempt := 1; ; attempt++ {
		dialer := net.Dialer{Timeout: opts.ConnectionTimeout}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			log.InfoContext(ctx, "PostgreSQL is reachable", "attempts", attempt)
			return nil
		}
		remaining := time.Until(deadline)
		// An attempt the overall deadline cut short says less than the one before it.
		if remaining > 0 || lastError == "UNKNOWN" {
			lastError = errorClass(err)
		}
		if remaining <= 0 {
			break
		}
		delay := min(opts.Interval, remaining)
		log.InfoContext(ctx, "PostgreSQL is not reachable yet", "attempt", attempt, "error_code", lastError, "retry_in", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if time.Until(deadline) <= 0 || ctx.Err() != nil {
			break
		}
	}
	return fmt.Errorf("PostgreSQL did not become reachable within %s (last error code: %s)", opts.Timeout, lastError)
}

// tcpAddress is the host and port of the first server in a PostgreSQL connection string, in URL or keyword form.
func tcpAddress(databaseURL string) (string, error) {
	cfg, err := pgconn.ParseConfig(databaseURL)
	if err != nil {
		// pgconn's message can quote the connection string, password included.
		return "", errors.New("the migration database URL is not a valid PostgreSQL connection string")
	}
	if cfg.Host == "" || cfg.Host[0] == '/' {
		return "", errors.New("the migration database URL must name a TCP host")
	}
	return net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))), nil
}

// errorClass names a dial failure without its addresses, like the Node error codes forma2 logged.
func errorClass(err error) string {
	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "ECONNREFUSED"
	case errors.Is(err, syscall.EHOSTUNREACH):
		return "EHOSTUNREACH"
	case errors.Is(err, syscall.ENETUNREACH):
		return "ENETUNREACH"
	case errors.As(err, &dnsErr):
		return "ENOTFOUND"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "ETIMEDOUT"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "ETIMEDOUT"
	}
	return "UNKNOWN"
}
