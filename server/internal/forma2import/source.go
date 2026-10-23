package forma2import

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// forma2Tables are the tables of forma2's init migration (packages/database/migration/20260916140000_init at 3db326f), the shape the import requires. Production never ran the later migrations, so their additions are read when present and defaulted when not.
var forma2Tables = []string{
	"Account", "ActionClass", "ApiKey", "ApiKeyWorkspace", "AuthzedProjectionOutbox", "Contact", "ContactAttribute", "ContactAttributeKey",
	"DataMigration", "Display", "EmbeddedData", "Integration", "Invite", "Language", "Membership", "Organization", "OrganizationBilling",
	"PasswordResetToken", "Response", "ResponseQuotaLink", "Segment", "Session", "Survey", "SurveyAttributeFilter", "SurveyEmbeddedData",
	"SurveyFollowUp", "SurveyLanguage", "SurveyQuota", "SurveyTrigger", "Tag", "TagsOnResponses", "Team", "TeamUser", "TwoFactor", "User",
	"VerificationToken", "Webhook", "Workflow", "WorkflowRun", "WorkflowRunLog", "WorkflowVersion", "Workspace", "WorkspaceTeam",
	"jwks", "oauthAccessToken", "oauthClient", "oauthClientAssertion", "oauthClientResource", "oauthConsent", "oauthRefreshToken", "oauthResource",
}

// laterTables are created by forma2 migrations after the init one; a source without them is still a forma2 database.
var laterTables = []string{"ResponsePipelineOutbox", "_prisma_migrations"}

// laterColumns are the columns forma2's migrations after the init one added, the ones a production database may lack.
var laterColumns = map[string][]string{
	"Integration":  {"lastErrorAt", "lastErrorMessage", "consecutiveFailures"},
	"Organization": {"workflowRunRetentionDays", "displayRetentionDays"},
	"Workflow":     {"triggerSurveyId"},
}

// sourceSchema is what the forma2 database holds: its tables with their columns and row counts, and the Prisma migrations it ran.
type sourceSchema struct {
	columns    map[string]map[string]bool
	counts     map[string]int
	migrations []string
}

func readSourceSchema(ctx context.Context, src pgx.Tx) (*sourceSchema, error) {
	s := &sourceSchema{columns: map[string]map[string]bool{}, counts: map[string]int{}, migrations: []string{}}
	rows, err := src.Query(ctx, `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ANY($1)`,
		append(slices.Clone(forma2Tables), laterTables...))
	if err != nil {
		return nil, fmt.Errorf("inspect the forma2 schema: %w", err)
	}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return nil, err
		}
		if s.columns[table] == nil {
			s.columns[table] = map[string]bool{}
		}
		s.columns[table][column] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect the forma2 schema: %w", err)
	}
	var missing []string
	for _, t := range forma2Tables {
		if s.columns[t] == nil {
			missing = append(missing, t)
		}
	}
	if len(missing) == len(forma2Tables) {
		return nil, errors.New(`the source database has no forma2 tables (no "User", "Organization", ...); pass --source with forma2's database`)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("the source database lacks forma2 tables %s; it is not a database forma2's init migration created", strings.Join(missing, ", "))
	}
	for table := range s.columns {
		var n int
		if err := src.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, pgx.Identifier{table}.Sanitize())).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		s.counts[table] = n
	}
	if s.columns["_prisma_migrations"] != nil {
		rows, err := src.Query(ctx, `SELECT migration_name FROM _prisma_migrations WHERE finished_at IS NOT NULL AND rolled_back_at IS NULL ORDER BY migration_name`)
		if err != nil {
			return nil, fmt.Errorf("read _prisma_migrations: %w", err)
		}
		if s.migrations, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return nil, fmt.Errorf("read _prisma_migrations: %w", err)
		}
	}
	return s, nil
}

// has reports whether the source table has the column.
func (s *sourceSchema) has(table, column string) bool {
	return s.columns[table][column]
}

// opt is expr when the source table has the column, else fallback.
func (s *sourceSchema) opt(table, column, expr, fallback string) string {
	if s.has(table, column) {
		return expr
	}
	return fallback
}

// absentColumns lists the later columns the source lacks, as "Table.column".
func (s *sourceSchema) absentColumns() []string {
	var out []string
	for _, table := range []string{"Integration", "Organization", "Workflow"} {
		for _, c := range laterColumns[table] {
			if !s.has(table, c) {
				out = append(out, table+"."+c)
			}
		}
	}
	if s.columns["ResponsePipelineOutbox"] == nil {
		out = append(out, "ResponsePipelineOutbox")
	}
	return out
}
