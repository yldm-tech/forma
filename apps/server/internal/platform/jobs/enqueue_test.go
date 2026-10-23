package jobs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
)

func countJobs(t *testing.T, db *gorm.DB, kind string) (n int64) {
	t.Helper()
	if err := db.Raw("SELECT count(*) FROM river_job WHERE kind = ?", kind).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnqueueCommitsAndRollsBackWithTheTransaction(t *testing.T) {
	dsn := pgtest.DSN(t)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	enq, err := jobs.NewEnqueuer(discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.Exec("CREATE TABLE notes (body text NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}

	err = enq.Transaction(ctx, db, func(tx *jobs.Tx) error {
		if err := tx.Exec("INSERT INTO notes (body) VALUES ('kept')").Error; err != nil {
			return err
		}
		return tx.Enqueue(ctx, recordArgs{Note: "kept"}, nil)
	})
	if err != nil {
		t.Fatalf("committed transaction: %v", err)
	}
	if n := countJobs(t, db, "test.record"); n != 1 {
		t.Fatalf("%d jobs after commit, want 1", n)
	}

	boom := errors.New("boom")
	err = enq.Transaction(ctx, db, func(tx *jobs.Tx) error {
		if err := tx.Exec("INSERT INTO notes (body) VALUES ('dropped')").Error; err != nil {
			return err
		}
		if err := tx.Enqueue(ctx, recordArgs{Note: "dropped"}, &river.InsertOpts{Priority: 2}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("rolled back transaction returned %v, want boom", err)
	}
	if n := countJobs(t, db, "test.record"); n != 1 {
		t.Fatalf("%d jobs after rollback, want still 1", n)
	}
	var notes int64
	if err := db.Raw("SELECT count(*) FROM notes").Scan(&notes).Error; err != nil {
		t.Fatal(err)
	}
	if notes != 1 {
		t.Fatalf("%d notes, want only the committed one", notes)
	}
	var note string
	if err := db.Raw("SELECT args->>'note' FROM river_job WHERE kind = 'test.record'").Scan(&note).Error; err != nil {
		t.Fatal(err)
	}
	if note != "kept" {
		t.Fatalf("job args note %q, want kept", note)
	}
}
