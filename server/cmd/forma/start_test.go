package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthURL(t *testing.T) {
	cases := []struct{ addr, want string }{
		{":3000", "http://127.0.0.1:3000/health"},
		{"0.0.0.0:3000", "http://127.0.0.1:3000/health"},
		{"[::]:8080", "http://127.0.0.1:8080/health"},
		{"10.0.0.5:3000", "http://10.0.0.5:3000/health"},
		{"[::1]:3000", "http://[::1]:3000/health"},
		{"localhost:3000", "http://localhost:3000/health"},
	}
	for _, tc := range cases {
		got, err := healthURL(tc.addr)
		if err != nil || got != tc.want {
			t.Errorf("healthURL(%q) = %q, %v; want %q", tc.addr, got, err, tc.want)
		}
	}
	for _, addr := range []string{"3000", ":http", ":0", ":70000"} {
		if got, err := healthURL(addr); err == nil {
			t.Errorf("healthURL(%q) = %q, want an error", addr, got)
		}
	}
}

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("server address: %v", err)
	}
	t.Setenv("HTTP_ADDR", ":"+port)

	if err := healthcheck(context.Background()); err != nil {
		t.Fatalf("healthcheck against a healthy server: %v", err)
	}
	if path != "/health" {
		t.Fatalf("probed %q, want /health", path)
	}

	status = http.StatusServiceUnavailable
	if err := healthcheck(context.Background()); err == nil {
		t.Fatal("healthcheck passed on a 503")
	}

	srv.Close()
	if err := healthcheck(context.Background()); err == nil {
		t.Fatal("healthcheck passed with nothing listening")
	}
}

func TestWaitForDatabaseFlags(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://forma@127.0.0.1:1/forma")
	for _, args := range [][]string{
		{"--timeout-seconds", "0"},
		{"--interval-seconds", "-1"},
		{"--connection-timeout-seconds", "NaN"},
		{"--timeout-seconds"},
		{"--unknown", "1"},
		{"extra"},
	} {
		if err := waitForDatabase(context.Background(), args); err == nil {
			t.Errorf("waitForDatabase(%q) accepted invalid arguments", args)
		}
	}
}

func TestRunRejectsUnknownCommands(t *testing.T) {
	if code := run(nil); code != 2 {
		t.Errorf("run() = %d, want 2", code)
	}
	if code := run([]string{"nope"}); code != 2 {
		t.Errorf("run(nope) = %d, want 2", code)
	}
	if code := run([]string{"help"}); code != 0 {
		t.Errorf("run(help) = %d, want 0", code)
	}
}
