package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// The cleanup a response delete or a survey reset enqueues is worked by `forma worker`: the files of the survey's workspace go, whichever of its ids they are stored under, and a URL naming anything else is left alone, since respondents write response data.
func TestWorkerDeletesResponseFiles(t *testing.T) {
	t.Parallel()
	dsn := pgtest.DSN(t)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DatabaseURL: dsn, DatabaseMaxOpenConns: 5, ShutdownTimeout: 5 * time.Second, WorkerConcurrency: 2}
	pool, closePool, err := jobs.OpenPool(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closePool)
	bucket, s3 := newBucket(t)
	reg := jobs.NewRegistry(discardLog())
	RegisterJobs(reg, NewService(bucket), workspace.NewRepository(db, discardLog()), discardLog())
	worker, err := jobs.NewWorker(cfg, pool, reg, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = worker.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	var org, ws, other uuid.UUID
	if err := db.Raw(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`).Row().Scan(&org); err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Main') RETURNING id`, org).Row().Scan(&ws); err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Other') RETURNING id`, org).Row().Scan(&other); err != nil {
		t.Fatal(err)
	}
	const dir = "/private/surveys/s1/elements/f/"
	keys := map[string]bool{
		ws.String() + dir + "a b--fid--1.pdf":          false,
		ws.String() + "/private/x--fid--3.pdf":         false,
		other.String() + dir + "theirs--fid--4.pdf":    true,
		ws.String() + dir + "unreferenced--fid--5.pdf": true,
	}
	for key := range keys {
		put(t, s3, key, "application/pdf", "file")
	}

	inserter, err := jobs.NewInserter(pool, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	res, err := inserter.Insert(t.Context(), response.DeleteFilesArgs{WorkspaceID: ws, FileURLs: []string{
		"/storage/" + ws.String() + dir + "a%20b--fid--1.pdf",
		"https://app.example.com/storage/" + ws.String() + "/private/x--fid--3.pdf?download=1",
		// Another workspace's file, named directly and through a path that climbs out of this workspace's directory.
		"/storage/" + other.String() + dir + "theirs--fid--4.pdf",
		"/storage/" + ws.String() + "/private/../../" + other.String() + dir + "theirs--fid--4.pdf",
		"/storage/clunknownaaaaaaaaaaaaaaa1/private/x.pdf",
		"/storage/" + ws.String() + "/secret/x.pdf",
		"not a url",
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		var state string
		if err := pool.QueryRow(t.Context(), `SELECT state FROM river_job WHERE id = $1`, res.Job.ID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cleanup job is %s", state)
		}
		time.Sleep(50 * time.Millisecond)
	}
	for key, kept := range keys {
		if got := exists(t, s3, key); got != kept {
			t.Errorf("%s: exists %v, want %v", key, got, kept)
		}
	}
}

func TestParseFileURL(t *testing.T) {
	for in, want := range map[string]storedFile{
		"/storage/ws/private/a.pdf":                            {"ws", AccessPrivate, "a.pdf"},
		"/storage/ws/public/surveys/s/elements/e/a.pdf":        {"ws", AccessPublic, "surveys/s/elements/e/a.pdf"},
		"https://x.example/storage/ws/private/a%20b.pdf?x=1#y": {"ws", AccessPrivate, "a%20b.pdf"},
		"/storage//ws//private//a.pdf":                         {"ws", AccessPrivate, "a.pdf"},
		"/storage/ws/private/":                                 {},
		"/storage/ws/other/a.pdf":                              {},
		"/files/ws/private/a.pdf":                              {},
		"relative/storage/ws/private/a.pdf":                    {},
	} {
		got, ok := parseFileURL(in)
		if got != want || ok != (want != storedFile{}) {
			t.Errorf("%q: %+v %v, want %+v", in, got, ok, want)
		}
	}
}
