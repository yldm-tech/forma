package forma2import

import (
	"strings"
	"testing"
)

// --source takes forma2's DATABASE_URL as it is, as MIGRATE_DATABASE_URL does: Prisma's parameters are dropped before pgx connects.
func TestParseFlagsAcceptsForma2SourceURL(t *testing.T) {
	f, err := parseFlags([]string{"--dry-run", "--source=postgresql://forma:pw@pgbouncer.postgres.svc.cluster.local:5432/forma?schema=public&pgbouncer=true&connection_limit=10&pool_timeout=20"})
	if err != nil || f.source != "postgresql://forma:pw@pgbouncer.postgres.svc.cluster.local:5432/forma" || !f.dryRun {
		t.Fatalf("parseFlags = %+v, %v", f, err)
	}
	if _, err := parseFlags([]string{"--source=postgresql://forma:pw@db/forma?schema=tenant"}); err == nil || !strings.Contains(err.Error(), "--source") {
		t.Fatalf("parseFlags accepted another schema: %v", err)
	}
	if f, err := parseFlags(nil); err != nil || f.source != "" {
		t.Fatalf("parseFlags without --source = %+v, %v", f, err)
	}
}

// --overwrite-changes is a refresh's, and a discard takes nothing but its reason.
func TestParseFlagsOverwriteChanges(t *testing.T) {
	if f, err := parseFlags([]string{"--refresh", "--overwrite-changes"}); err != nil || !f.refresh || !f.overwrite {
		t.Fatalf("parseFlags = %+v, %v", f, err)
	}
	for _, args := range [][]string{{"--overwrite-changes"}, {"--discard-forma2-data", "--reason=x", "--refresh", "--overwrite-changes"}} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%v) accepted", args)
		}
	}
}
