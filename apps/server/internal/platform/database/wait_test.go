package database

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestWaitReachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port

	opts := WaitOptions{Timeout: 5 * time.Second, Interval: 10 * time.Millisecond, ConnectionTimeout: time.Second}
	for _, url := range []string{
		"postgresql://forma:secret@127.0.0.1:" + strconv.Itoa(port) + "/forma?sslmode=disable",
		"host=127.0.0.1 port=" + strconv.Itoa(port) + " user=forma dbname=forma",
	} {
		if err := WaitReachable(context.Background(), url, opts, discardLog()); err != nil {
			t.Fatalf("WaitReachable(%q): %v", url, err)
		}
	}
}

func TestWaitReachableGivesUpWithTheErrorClass(t *testing.T) {
	// A port that was just free: connections to it are refused at once.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	url := "postgresql://forma:do-not-log-me@127.0.0.1:" + strconv.Itoa(port) + "/forma"
	opts := WaitOptions{Timeout: 200 * time.Millisecond, Interval: 20 * time.Millisecond, ConnectionTimeout: 100 * time.Millisecond}
	err = WaitReachable(context.Background(), url, opts, discardLog())
	if err == nil {
		t.Fatal("WaitReachable succeeded against a closed port")
	}
	if !strings.Contains(err.Error(), "ECONNREFUSED") || strings.Contains(err.Error(), "do-not-log-me") {
		t.Fatalf("error = %q, want the refusal class and no credentials", err)
	}
}

func TestWaitReachableRejectsAnUnusableURL(t *testing.T) {
	opts := WaitOptions{Timeout: time.Second, Interval: time.Millisecond, ConnectionTimeout: time.Millisecond}
	for _, url := range []string{"postgresql://forma:do-not-log-me@[::1/forma", "host=/var/run/postgresql user=forma"} {
		err := WaitReachable(context.Background(), url, opts, discardLog())
		if err == nil || strings.Contains(err.Error(), "do-not-log-me") {
			t.Fatalf("WaitReachable(%q) = %v, want an error without credentials", url, err)
		}
	}
}
