package forma2import

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
)

// kind says how a column read from forma2 is written. Verification does not go by it: it reads each sampled row again from forma2 and checks every written column against what it computes on its own (expect.go).
type kind int

const (
	// plain is written as read.
	plain kind = iota
	// jsonDoc is JSON read as text and written as read.
	jsonDoc
	// id is a forma2 id read as text and written as legacyid.Resolve of it; NULL stays NULL.
	id
	// derived is computed by the query or the row hook from what forma2 holds.
	derived
	// extra is read for the row hook and not written.
	extra
)

type column struct {
	name string
	expr string
	kind kind
}

// tableSpec is one table of this schema filled from one forma2 source.
type tableSpec struct {
	target string
	// from is the forma2 table the rows come from. When oneToOne is set every row of it is read, and the count is checked against the table's.
	from     string
	oneToOne bool
	// derivedFrom names the forma2 columns rows that are not one per forma2 row come from.
	derivedFrom string
	key         []string
	cols        []column
	// query is the source query after its select list: FROM, joins, WHERE and ORDER BY.
	query string
	// prepare runs before the source is read, on the import transaction (the hook cannot use it while rows stream in).
	prepare func(ctx context.Context) error
	// hook adjusts a row before it is written, or drops it (false).
	hook       func(r *row) (bool, error)
	skipReason string
}

func (s *tableSpec) label() string {
	if s.derivedFrom != "" {
		return s.derivedFrom
	}
	return s.from
}

func (s *tableSpec) sql() string {
	parts := make([]string, len(s.cols))
	for i, c := range s.cols {
		parts[i] = c.expr + " AS " + pgx.Identifier{c.name}.Sanitize()
	}
	return "SELECT " + strings.Join(parts, ", ") + " " + s.query
}

// written are the target columns, in order.
func (s *tableSpec) written() []string {
	var out []string
	for _, c := range s.cols {
		if c.kind != extra {
			out = append(out, c.name)
		}
	}
	return out
}

// row is one source row on its way to the target.
type row struct {
	spec *tableSpec
	vals []any
}

func (r *row) index(name string) int {
	i := slices.IndexFunc(r.spec.cols, func(c column) bool { return c.name == name })
	if i < 0 {
		panic(fmt.Sprintf("forma2import: %s has no column %s", r.spec.target, name))
	}
	return i
}

func (r *row) get(name string) any    { return r.vals[r.index(name)] }
func (r *row) set(name string, v any) { r.vals[r.index(name)] = v }
func (r *row) str(name string) string { s, _ := r.get(name).(string); return s }
func (r *row) flag(name string) bool  { b, _ := r.get(name).(bool); return b }
func (r *row) newID(name string) uuid.UUID {
	resolved, _ := legacyid.Resolve(r.str(name))
	return resolved
}

// key is a written row's primary key, its uuid columns in order.
type key []uuid.UUID

// writtenTable is one table spec on its way through a run: staged, written, then verified.
type writtenTable struct {
	spec *tableSpec
	// report indexes Report.Tables.
	report int
	// stage is the temporary table the spec's rows wait in until they are written.
	stage string
	keys  []key
	// skipped are the keys of the rows the spec's hook dropped on purpose.
	skipped []key
	// samples are the rows verification reads again from forma2 and compares column by column.
	samples []sample
}

// sample is a written row: its key here, and the forma2 values that key was derived from, which find the row in forma2 again.
type sample struct {
	key    key
	forma2 []string
}

// samplesPerTable is how many rows of each table spec verification compares with forma2, spread over the table when its row count is known.
const samplesPerTable = 20

// stageTable streams the spec's source rows through its hook into a staging table on the import transaction. The target table is not touched yet: every spec is staged before anything is written, so a refresh knows the whole of what it writes before it deletes what it no longer does.
func (im *importer) stageTable(ctx context.Context, n int, spec *tableSpec) error {
	if err := assertTargetTable(spec.target); err != nil {
		return err
	}
	rep := TableReport{Table: spec.target, From: spec.label(), Forma2Rows: -1, SkipReason: spec.skipReason}
	stride := 1
	if spec.oneToOne {
		rep.Forma2Rows = im.schema.counts[spec.from]
		stride = max(1, (rep.Forma2Rows+samplesPerTable-1)/samplesPerTable)
	}
	if spec.prepare != nil {
		if err := spec.prepare(ctx); err != nil {
			return err
		}
	}
	w := &writtenTable{spec: spec, stage: fmt.Sprintf("forma2_stage_%d", n)}
	if _, err := im.tx.Exec(ctx, fmt.Sprintf(`CREATE TEMP TABLE %s (LIKE %s INCLUDING DEFAULTS) ON COMMIT DROP`, pgx.Identifier{w.stage}.Sanitize(), pgx.Identifier{spec.target}.Sanitize())); err != nil {
		return fmt.Errorf("create staging table: %w", err)
	}
	rows, err := im.src.Query(ctx, spec.sql())
	if err != nil {
		return fmt.Errorf("read forma2: %w", err)
	}
	defer rows.Close()
	cols := spec.written()
	keyIdx := make([]int, len(spec.key))
	keySrc := make([]int, len(spec.key))
	for i, k := range spec.key {
		keyIdx[i] = slices.Index(cols, k)
		keySrc[i] = slices.IndexFunc(spec.cols, func(c column) bool { return c.name == k })
	}
	if _, err := im.tx.CopyFrom(ctx, pgx.Identifier{w.stage}, cols, pgx.CopyFromFunc(func() ([]any, error) {
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				return nil, err
			}
			rep.Read++
			forma2Key := make([]string, len(keySrc))
			for i, ci := range keySrc {
				forma2Key[i] = idText(vals[ci])
			}
			r := &row{spec: spec, vals: vals}
			if spec.hook != nil {
				keep, err := spec.hook(r)
				if err != nil {
					return nil, err
				}
				if !keep {
					rep.Skipped++
					if k, ok := resolveKey(forma2Key); ok {
						w.skipped = append(w.skipped, k)
					}
					continue
				}
			}
			out, err := toTarget(spec, r)
			if err != nil {
				return nil, err
			}
			k := make(key, len(keyIdx))
			for i, idx := range keyIdx {
				u, ok := out[idx].(pgtype.UUID)
				if !ok || !u.Valid {
					return nil, fmt.Errorf("key column %s is not a uuid", spec.key[i])
				}
				k[i] = u.Bytes
			}
			if len(w.keys)%stride == 0 && len(w.samples) < samplesPerTable {
				w.samples = append(w.samples, sample{key: k, forma2: forma2Key})
			}
			w.keys = append(w.keys, k)
			return out, nil
		}
		return nil, rows.Err()
	})); err != nil {
		return err
	}
	im.report.Tables = append(im.report.Tables, rep)
	w.report = len(im.report.Tables) - 1
	im.written = append(im.written, w)
	if spec.oneToOne && rep.Read != rep.Forma2Rows {
		im.fail("%s: read %d rows of forma2's %s, which holds %d", spec.target, rep.Read, spec.from, rep.Forma2Rows)
	}
	return nil
}

// idText is a forma2 id as read, as text.
func idText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case uuid.UUID:
		return x.String()
	}
	return ""
}

// resolveKey is the key legacyid derives from forma2's key values; false when one is empty.
func resolveKey(forma2Key []string) (key, bool) {
	k := make(key, len(forma2Key))
	for i, s := range forma2Key {
		u, ok := legacyid.Resolve(s)
		if !ok {
			return nil, false
		}
		k[i] = u
	}
	return k, true
}

// prune is a refresh's first write: it deletes, from every table the run writes, the rows the run does not write. Those are rows forma2 no longer holds, or no longer imports (an older invite to an address that has a newer one), and with --overwrite-changes the rows created through this server. Deleting them first lets a row forma2 recreated under a new id take the unique values of the one it replaces. Children go before their parents; the authorization triggers queue the revocations.
func (im *importer) prune(ctx context.Context) error {
	var tables []string
	for _, w := range im.written {
		if !slices.Contains(tables, w.spec.target) {
			tables = append(tables, w.spec.target)
		}
	}
	slices.Reverse(tables)
	for _, table := range tables {
		var kept []string
		first := -1
		for _, w := range im.written {
			if w.spec.target != table {
				continue
			}
			if first < 0 {
				first = w.report
			}
			match := make([]string, len(w.spec.key))
			for i, k := range w.spec.key {
				col := pgx.Identifier{k}.Sanitize()
				match[i] = fmt.Sprintf("s.%s = t.%s", col, col)
			}
			kept = append(kept, fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s s WHERE %s)", pgx.Identifier{w.stage}.Sanitize(), strings.Join(match, " AND ")))
		}
		var deleted int
		if err := im.tx.QueryRow(ctx, fmt.Sprintf(`WITH gone AS (DELETE FROM %s t WHERE %s RETURNING 1) SELECT count(*) FROM gone`,
			pgx.Identifier{table}.Sanitize(), strings.Join(kept, " AND "))).Scan(&deleted); err != nil {
			return fmt.Errorf("%s: delete the rows forma2 does not hold: %w", table, err)
		}
		im.report.Tables[first].Deleted = deleted
	}
	return nil
}

// writeTable upserts a staged table spec into its target table.
func (im *importer) writeTable(ctx context.Context, w *writtenTable) error {
	rep := &im.report.Tables[w.report]
	var inserted, updated int
	if err := im.tx.QueryRow(ctx, upsertSQL(w.spec.target, pgx.Identifier{w.stage}.Sanitize(), w.spec.written(), w.spec.key)).Scan(&inserted, &updated); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	rep.Written = len(w.keys)
	rep.Inserted, rep.Updated = inserted, updated
	rep.Unchanged = rep.Written - inserted - updated
	if _, err := im.tx.Exec(ctx, "DROP TABLE "+pgx.Identifier{w.stage}.Sanitize()); err != nil {
		return err
	}
	im.opts.Log.InfoContext(ctx, "imported a table", "table", w.spec.target, "from", w.spec.label(), "read", rep.Read, "skipped", rep.Skipped,
		"inserted", rep.Inserted, "updated", rep.Updated, "unchanged", rep.Unchanged, "deleted", rep.Deleted)
	return nil
}

// upsertSQL inserts the staged rows, updates a row whose key exists and whose values differ, and leaves an identical row alone, so a re-run neither duplicates rows nor fires the authorization triggers for nothing. It returns how many rows were inserted and how many updated.
func upsertSQL(target, stage string, cols, keyCols []string) string {
	q := func(names []string, prefix string) string {
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = prefix + pgx.Identifier{n}.Sanitize()
		}
		return strings.Join(out, ", ")
	}
	var rest []string
	for _, c := range cols {
		if !slices.Contains(keyCols, c) {
			rest = append(rest, c)
		}
	}
	conflict := "DO NOTHING"
	if len(rest) > 0 {
		sets := make([]string, len(rest))
		for i, c := range rest {
			sets[i] = pgx.Identifier{c}.Sanitize() + " = EXCLUDED." + pgx.Identifier{c}.Sanitize()
		}
		conflict = fmt.Sprintf("DO UPDATE SET %s WHERE (%s) IS DISTINCT FROM (%s)", strings.Join(sets, ", "), q(rest, "t."), q(rest, "EXCLUDED."))
		if len(rest) == 1 {
			conflict = fmt.Sprintf("DO UPDATE SET %s WHERE %s IS DISTINCT FROM %s", sets[0], q(rest, "t."), q(rest, "EXCLUDED."))
		}
	}
	return fmt.Sprintf(`WITH up AS (INSERT INTO %s AS t (%s) SELECT %s FROM %s ON CONFLICT (%s) %s RETURNING (xmax = 0) AS inserted)
SELECT count(*) FILTER (WHERE inserted), count(*) FILTER (WHERE NOT inserted) FROM up`,
		pgx.Identifier{target}.Sanitize(), q(cols, ""), q(cols, ""), stage, q(keyCols, ""), conflict)
}

// toTarget turns a row into the values written: forma2 ids through legacyid, JSON as text.
func toTarget(spec *tableSpec, r *row) ([]any, error) {
	out := make([]any, 0, len(spec.cols))
	for i, c := range spec.cols {
		if c.kind == extra {
			continue
		}
		v := r.vals[i]
		if c.kind == id {
			switch x := v.(type) {
			case nil:
				out = append(out, pgtype.UUID{})
			case string:
				resolved, ok := legacyid.Resolve(x)
				if !ok {
					return nil, fmt.Errorf("column %s holds an empty id", c.name)
				}
				out = append(out, pgtype.UUID{Bytes: resolved, Valid: true})
			case uuid.UUID:
				out = append(out, pgtype.UUID{Bytes: x, Valid: true})
			default:
				return nil, fmt.Errorf("column %s: unexpected id %T", c.name, v)
			}
			continue
		}
		switch x := v.(type) {
		case uuid.UUID:
			v = pgtype.UUID{Bytes: x, Valid: true}
		case json.RawMessage:
			v = string(x)
		}
		out = append(out, v)
	}
	return out, nil
}

// targetTables are the only tables the import writes. Every one is this schema's; none is forma2's.
var targetTables = map[string]bool{
	"organizations": true, "organization_billing": true, "users": true, "user_identities": true, "user_totp": true, "workspaces": true,
	"memberships": true, "teams": true, "team_members": true, "workspace_teams": true, "invites": true, "invite_teams": true,
	"api_keys": true, "api_key_workspaces": true, "languages": true, "action_classes": true, "contact_attribute_keys": true,
	"contacts": true, "contact_attributes": true, "segments": true, "surveys": true, "survey_triggers": true,
	"survey_attribute_filters": true, "survey_quotas": true, "survey_follow_ups": true, "survey_languages": true,
	"embedded_data": true, "survey_embedded_data": true, "displays": true, "responses": true, "tags": true, "response_tags": true,
	"response_quota_links": true, "webhooks": true, "webhook_surveys": true, "integrations": true, "integration_credentials": true,
	"workflows": true, "workflow_versions": true, "workflow_runs": true, "workflow_run_logs": true,
}

// assertTargetTable refuses to write anything but this schema's tables: forma2's must stay as they are for a rollback.
func assertTargetTable(name string) error {
	if !targetTables[name] {
		return fmt.Errorf("refusing to write %q, which is not one of this schema's tables", name)
	}
	for _, t := range append(slices.Clone(forma2Tables), laterTables...) {
		if strings.EqualFold(t, name) {
			return errors.New("refusing to write forma2's table " + t)
		}
	}
	return nil
}
