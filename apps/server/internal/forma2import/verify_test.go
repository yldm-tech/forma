package forma2import_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/forma2import"
)

// Verification reads every sampled row again from forma2, by forma2's own columns and apart from the mapping's query, and computes what each column of this schema must hold on its own: a mapping mistake fails the run instead of being checked against itself.
func TestVerificationCatchesMappingMistakes(t *testing.T) {
	ctx := context.Background()
	dsn := forma2DB(t, 7)
	for _, tc := range []struct{ target, from, column, expr, failure string }{
		// A plain column read from the wrong forma2 column.
		{"organizations", "Organization", "display_retention_days", `t."workflowRunRetentionDays"`, "display_retention_days"},
		// A timestamp read in the wrong time zone.
		{"organizations", "Organization", "created_at", `t.created_at AT TIME ZONE 'Asia/Tokyo'`, "created_at"},
		// Derived columns: a transform fed the wrong input, a fallback, a status mapping, a password hash.
		{"users", "User", "notification_settings", `'{}'`, "notification_settings"},
		{"api_keys", "ApiKey", "lookup_hash", `t."hashedKey"`, "lookup_hash"},
		{"workflow_runs", "WorkflowRun", "status", `t.status::text`, "status"},
		{"user_identities", "Account", "password_hash", `NULL::text`, "password_hash"},
		// A sealed credential is opened and compared with what forma2 held.
		{"integration_credentials", "Integration.config.key", "x_email", `NULL::text`, "ciphertext"},
		// A key from the wrong column: the rows written are not forma2's rows.
		{"workflow_run_logs", "WorkflowRunLog", "id", `t.id || '-2'`, "workflow_run_logs"},
	} {
		t.Run(tc.target+"."+tc.column, func(t *testing.T) {
			r, err := forma2import.RunWithColumn(ctx, withDryRun(options(t, dsn)), tc.target, tc.from, tc.column, tc.expr)
			if !errors.Is(err, forma2import.ErrVerification) {
				t.Fatalf("a mapping mistake verified: %v\n%s", err, reportText(r))
			}
			if failures := strings.Join(r.Failures, "\n"); !strings.Contains(failures, tc.failure) {
				t.Errorf("the failures do not name %s:\n%s", tc.failure, failures)
			}
		})
	}
	r, err := forma2import.Run(ctx, withDryRun(options(t, dsn)))
	if err != nil || len(r.Failures) > 0 {
		t.Fatalf("the mapping as it is: %v\n%s", err, reportText(r))
	}
}
