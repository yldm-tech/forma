package forma2import

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// maxReported caps the examples one check lists; the count says how many there were.
const maxReported = 10

// precheck refuses what this schema cannot hold before anything is written.
func (im *importer) precheck(ctx context.Context) error {
	rows, err := im.src.Query(ctx, `SELECT string_agg(id, ', ' ORDER BY id) FROM "User" GROUP BY lower(email) HAVING count(*) > 1 ORDER BY 1`)
	if err != nil {
		return fmt.Errorf("check user addresses: %w", err)
	}
	groups, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("check user addresses: %w", err)
	}
	for _, g := range groups {
		im.fail("users %s have addresses that differ only in case; this server keeps one account per address, so merge them in forma2 first", g)
	}
	if len(groups) > 0 {
		return ErrVerification
	}
	return nil
}

// notMigrated reports the forma2 tables the import leaves behind and how many rows they hold.
func (im *importer) notMigrated() {
	im.report.NotMigrated = []NotMigrated{}
	for _, t := range notMigratedTables {
		if n, ok := im.schema.counts[t.Table]; ok {
			t.Rows = n
			im.report.NotMigrated = append(im.report.NotMigrated, t)
		}
	}
}

// verify checks what the run wrote against forma2 read again on its own terms, in the import transaction: every forma2 row is in the table under the id legacyid derives from its own key, sampled rows hold what expect.go computes from forma2's stored columns, and the documents pass this server's validators.
func (im *importer) verify(ctx context.Context) error {
	if _, err := im.tx.Exec(ctx, `CREATE TEMP TABLE forma2_verify_keys (k0 uuid, k1 uuid, skipped boolean) ON COMMIT DROP;
		CREATE TEMP TABLE forma2_verify_expected (k0 uuid, k1 uuid) ON COMMIT DROP`); err != nil {
		return err
	}
	for _, w := range im.written {
		if err := im.verifyPresent(ctx, w); err != nil {
			return fmt.Errorf("verify %s: %w", w.spec.target, err)
		}
		if err := im.verifySamples(ctx, w); err != nil {
			return fmt.Errorf("verify %s: %w", w.spec.target, err)
		}
	}
	if err := im.verifySurveys(ctx); err != nil {
		return fmt.Errorf("verify surveys: %w", err)
	}
	return im.verifyDocuments(ctx)
}

func keyRow(k key, extra ...any) []any {
	out := []any{pgtype.UUID{Bytes: k[0], Valid: true}, pgtype.UUID{}}
	if len(k) > 1 {
		out[1] = pgtype.UUID{Bytes: k[1], Valid: true}
	}
	return append(out, extra...)
}

// verifyPresent checks the keys written. For a table read row by row, forma2's table is read again by its own key columns, apart from the mapping's query: the keys legacyid derives from them must be exactly the rows written and the rows skipped on purpose, and every one written must be in the table. Rows derived from other columns have no forma2 table of their own; they must be in the table under distinct keys, and their values are checked in the samples. Either way, no two rows may be written onto one key.
func (im *importer) verifyPresent(ctx context.Context, w *writtenTable) error {
	if _, err := im.tx.Exec(ctx, `TRUNCATE forma2_verify_keys, forma2_verify_expected`); err != nil {
		return err
	}
	all := len(w.keys) + len(w.skipped)
	if _, err := im.tx.CopyFrom(ctx, pgx.Identifier{"forma2_verify_keys"}, []string{"k0", "k1", "skipped"}, pgx.CopyFromSlice(all, func(i int) ([]any, error) {
		if i < len(w.keys) {
			return keyRow(w.keys[i], false), nil
		}
		return keyRow(w.skipped[i-len(w.keys)], true), nil
	})); err != nil {
		return err
	}
	match := func(a, b string) string {
		return fmt.Sprintf("%s.k0 = %s.k0 AND %s.k1 IS NOT DISTINCT FROM %s.k1", a, b, a, b)
	}
	inTarget := make([]string, len(w.spec.key))
	for i, k := range w.spec.key {
		inTarget[i] = fmt.Sprintf("t.%s = x.k%d", pgx.Identifier{k}.Sanitize(), i)
	}
	present := fmt.Sprintf("EXISTS (SELECT 1 FROM %s t WHERE %s)", pgx.Identifier{w.spec.target}.Sanitize(), strings.Join(inTarget, " AND "))
	var distinct int
	if err := im.tx.QueryRow(ctx, `SELECT count(DISTINCT (k0, COALESCE(k1, k0))) FROM forma2_verify_keys WHERE NOT skipped`).Scan(&distinct); err != nil {
		return err
	}
	if distinct != len(w.keys) {
		im.fail("%s from %s: %d rows were written onto %d keys", w.spec.target, w.spec.label(), len(w.keys), distinct)
	}
	rep := &im.report.Tables[w.report]
	if !w.spec.oneToOne {
		if err := im.tx.QueryRow(ctx, `SELECT count(*) FROM forma2_verify_keys x WHERE NOT skipped AND `+present).Scan(&rep.Present); err != nil {
			return err
		}
		if rep.Present != len(w.keys) {
			im.fail("%s from %s: %d of the %d rows written are not in the table", w.spec.target, w.spec.label(), len(w.keys)-rep.Present, len(w.keys))
		}
		return nil
	}

	exp := expectations[specName(w.spec)]
	cols := make([]string, len(w.spec.key))
	for i, k := range w.spec.key {
		c, ok := im.forma2Column(w.spec.from, k, exp.rename)
		if !ok {
			im.fail("%s: no column of forma2's %s holds its key %s", specName(w.spec), w.spec.from, k)
			return nil
		}
		cols[i] = "t." + pgx.Identifier{c}.Sanitize() + "::text"
	}
	rows, err := im.src.Query(ctx, fmt.Sprintf(`SELECT %s FROM public.%s t`, strings.Join(cols, ", "), pgx.Identifier{w.spec.from}.Sanitize()))
	if err != nil {
		return fmt.Errorf("read forma2's keys again: %w", err)
	}
	defer rows.Close()
	if _, err := im.tx.CopyFrom(ctx, pgx.Identifier{"forma2_verify_expected"}, []string{"k0", "k1"}, pgx.CopyFromFunc(func() ([]any, error) {
		if !rows.Next() {
			return nil, rows.Err()
		}
		values := make([]string, len(cols))
		targets := make([]any, len(cols))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		k, ok := resolveKey(values)
		if !ok {
			return nil, fmt.Errorf("forma2's %s has a row with an empty key", w.spec.from)
		}
		return keyRow(k), nil
	})); err != nil {
		return err
	}
	var unwritten, foreign, wanted int
	if err := im.tx.QueryRow(ctx, fmt.Sprintf(`SELECT
		(SELECT count(*) FROM forma2_verify_expected e WHERE NOT EXISTS (SELECT 1 FROM forma2_verify_keys v WHERE %[1]s)),
		(SELECT count(*) FROM forma2_verify_keys v WHERE NOT v.skipped AND NOT EXISTS (SELECT 1 FROM forma2_verify_expected e WHERE %[1]s)),
		(SELECT count(*) FROM forma2_verify_expected e WHERE NOT EXISTS (SELECT 1 FROM forma2_verify_keys v WHERE v.skipped AND %[1]s)),
		(SELECT count(*) FROM forma2_verify_expected x WHERE NOT EXISTS (SELECT 1 FROM forma2_verify_keys v WHERE v.skipped AND %[2]s) AND %[3]s)`,
		match("v", "e"), match("v", "x"), present)).Scan(&unwritten, &foreign, &wanted, &rep.Present); err != nil {
		return err
	}
	if unwritten > 0 {
		im.fail("%s from %s: %d of forma2's rows were neither written nor skipped", w.spec.target, w.spec.label(), unwritten)
	}
	if foreign > 0 {
		im.fail("%s from %s: %d rows written have a key no row of forma2's %s derives", w.spec.target, w.spec.label(), foreign, w.spec.from)
	}
	if rep.Present != wanted {
		im.fail("%s from %s: %d of forma2's %d rows are not in the table", w.spec.target, w.spec.label(), wanted-rep.Present, wanted)
	}
	return nil
}

// verifySamples compares the sampled rows with their forma2 rows, read again, column by column (checkSample).
func (im *importer) verifySamples(ctx context.Context, w *writtenTable) error {
	cols := w.spec.written()
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = pgx.Identifier{c}.Sanitize()
	}
	match := make([]string, len(w.spec.key))
	for i, k := range w.spec.key {
		match[i] = fmt.Sprintf("%s = $%d", pgx.Identifier{k}.Sanitize(), i+1)
	}
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE %s`, strings.Join(names, ", "), pgx.Identifier{w.spec.target}.Sanitize(), strings.Join(match, " AND "))
	for _, s := range w.samples {
		args := make([]any, len(s.key))
		for i, k := range s.key {
			args[i] = pgtype.UUID{Bytes: k, Valid: true}
		}
		rows, err := im.tx.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		var got []any
		if rows.Next() {
			got, err = rows.Values()
		}
		rows.Close()
		if err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		im.report.SamplesCompared++
		if got == nil {
			im.fail("%s: sample row %s is missing", w.spec.target, s.key)
			continue
		}
		tgt := make(map[string]any, len(cols))
		for i, c := range cols {
			tgt[c] = got[i]
		}
		if err := im.checkSample(ctx, w, s, tgt); err != nil {
			return err
		}
	}
	return nil
}

func (k key) String() string {
	parts := make([]string, len(k))
	for i, u := range k {
		parts[i] = u.String()
	}
	return strings.Join(parts, "/")
}

// surveyBatch is how many surveys are read back for validation at a time.
const surveyBatch = 200

// verifySurveys runs every imported survey document through this server's validators. A survey that is not a draft must pass the editor's full validation, as forma2 required on every save of such a survey; a draft must have the shape the editor's draft save requires, and what full validation still finds in it is reported, as the editor will ask for it before publishing.
func (im *importer) verifySurveys(ctx context.Context) error {
	var ids []string
	for _, w := range im.written {
		if w.spec.target == "surveys" {
			for _, k := range w.keys {
				ids = append(ids, k[0].String())
			}
		}
	}
	for start := 0; start < len(ids); start += surveyBatch {
		batch := ids[start:min(start+surveyBatch, len(ids))]
		langs, err := im.surveyLanguages(ctx, batch)
		if err != nil {
			return err
		}
		followUps, err := im.surveyFollowUps(ctx, batch)
		if err != nil {
			return err
		}
		rows, err := im.tx.Query(ctx, `SELECT id, status, welcome_card, blocks, endings, hidden_fields, variables, metadata,
			styling, survey_closed_message, single_use, recaptcha, workspace_overwrites, inline_triggers
			FROM surveys WHERE id = ANY($1::text[]::uuid[]) ORDER BY id`, batch)
		if err != nil {
			return err
		}
		type stored struct {
			id     uuid.UUID
			status string
			doc    surveydoc.Document
		}
		var surveys []stored
		for rows.Next() {
			var s stored
			d := &s.doc
			if err := rows.Scan(&s.id, &s.status, &d.WelcomeCard, &d.Blocks, &d.Endings, &d.HiddenFields, &d.Variables, &d.Metadata,
				&d.Styling, &d.SurveyClosedMessage, &d.SingleUse, &d.Recaptcha, &d.WorkspaceOverwrites, &d.InlineTriggers); err != nil {
				rows.Close()
				return err
			}
			surveys = append(surveys, s)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, s := range surveys {
			im.report.SurveysValidated++
			issues, _, err := im.docs.Check(s.doc, langs[s.id], followUps[s.id])
			if err != nil {
				return fmt.Errorf("validate survey %s: %w", s.id, err)
			}
			if s.status != "draft" {
				if len(issues) > 0 {
					im.fail("survey %s (%s) does not pass this server's validation: %s", s.id, s.status, describeIssues(issues))
				}
				continue
			}
			if shape := draftShapeIssues(s.doc); len(shape) > 0 {
				im.fail("draft survey %s does not have the shape a draft needs: %s", s.id, strings.Join(shape, ", "))
			} else if len(issues) > 0 {
				im.warn("draft survey %s will need changes before it can be published: %s", s.id, describeIssues(issues))
			}
		}
	}
	return nil
}

func (im *importer) surveyLanguages(ctx context.Context, ids []string) (map[uuid.UUID][]surveydoc.Language, error) {
	rows, err := im.tx.Query(ctx, `SELECT sl.survey_id, l.code, sl.is_default, sl.enabled FROM survey_languages sl JOIN languages l ON l.id = sl.language_id
		WHERE sl.survey_id = ANY($1::text[]::uuid[]) ORDER BY sl.survey_id, l.code`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]surveydoc.Language{}
	for rows.Next() {
		var surveyID uuid.UUID
		var l surveydoc.Language
		if err := rows.Scan(&surveyID, &l.Code, &l.Default, &l.Enabled); err != nil {
			return nil, err
		}
		out[surveyID] = append(out[surveyID], l)
	}
	return out, rows.Err()
}

func (im *importer) surveyFollowUps(ctx context.Context, ids []string) (map[uuid.UUID][]surveydoc.FollowUpInput, error) {
	rows, err := im.tx.Query(ctx, `SELECT survey_id, id, name, trigger, action FROM survey_follow_ups WHERE survey_id = ANY($1::text[]::uuid[]) ORDER BY survey_id, created_at, id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]surveydoc.FollowUpInput{}
	for rows.Next() {
		var surveyID, followUpID uuid.UUID
		var f surveydoc.FollowUpInput
		if err := rows.Scan(&surveyID, &followUpID, &f.Name, &f.Trigger, &f.Action); err != nil {
			return nil, err
		}
		f.ID = followUpID.String()
		out[surveyID] = append(out[surveyID], f)
	}
	return out, rows.Err()
}

// draftShapeIssues is the part of the editor's draft save rule (survey.draftShapeIssues) that concerns the document: the lists are lists and the cards are objects.
func draftShapeIssues(d surveydoc.Document) []string {
	var out []string
	for _, p := range []struct {
		name  string
		raw   json.RawMessage
		array bool
	}{
		{"welcomeCard", d.WelcomeCard, false}, {"blocks", d.Blocks, true}, {"endings", d.Endings, true},
		{"hiddenFields", d.HiddenFields, false}, {"variables", d.Variables, true}, {"metadata", d.Metadata, false},
	} {
		t := strings.TrimSpace(string(p.raw))
		if p.array && !strings.HasPrefix(t, "[") || !p.array && !strings.HasPrefix(t, "{") {
			out = append(out, p.name)
		}
	}
	return out
}

func describeIssues(issues []surveydoc.Issue) string {
	parts := make([]string, 0, min(len(issues), maxReported))
	for _, is := range issues[:min(len(issues), maxReported)] {
		path := make([]string, len(is.Path))
		for i, p := range is.Path {
			path[i] = fmt.Sprint(p)
		}
		parts = append(parts, strings.Join(path, ".")+": "+is.Message)
	}
	if len(issues) > maxReported {
		parts = append(parts, fmt.Sprintf("and %d more", len(issues)-maxReported))
	}
	return strings.Join(parts, "; ")
}

// documentChecks are the other JSON columns with an exported schema. A segment's filters must conform, since this server evaluates them; the rest were validated by forma2 when written and are reported when they do not conform, without failing the run.
var documentChecks = []struct {
	table, column, schema string
	required              bool
}{
	{"segments", "filters", "segment.filters", true},
	{"workspaces", "styling", "workspace.styling", false},
	{"workspaces", "config", "workspace.config", false},
	{"workspaces", "logo", "workspace.logo", false},
	{"api_keys", "organization_access", "api_key.organization_access", false},
	{"survey_quotas", "logic", "survey_quota.logic", false},
	{"responses", "data", "response.data", false},
	{"responses", "meta", "response.meta", false},
	{"responses", "ttc", "response.ttc", false},
	{"responses", "variables", "response.variables", false},
}

// verifyDocuments validates the imported rows' JSON columns. Only the path of a violation is reported: the values may be respondents' answers.
func (im *importer) verifyDocuments(ctx context.Context) error {
	for _, check := range documentChecks {
		var keys []key
		for _, w := range im.written {
			if w.spec.target == check.table {
				keys = append(keys, w.keys...)
			}
		}
		bad, examples := 0, []string{}
		for start := 0; start < len(keys); start += surveyBatch {
			batch := make([]string, 0, surveyBatch)
			for _, k := range keys[start:min(start+surveyBatch, len(keys))] {
				batch = append(batch, k[0].String())
			}
			rows, err := im.tx.Query(ctx, fmt.Sprintf(`SELECT id, %s FROM %s WHERE id = ANY($1::text[]::uuid[]) AND %s IS NOT NULL ORDER BY id`,
				pgx.Identifier{check.column}.Sanitize(), pgx.Identifier{check.table}.Sanitize(), pgx.Identifier{check.column}.Sanitize()), batch)
			if err != nil {
				return err
			}
			for rows.Next() {
				var rowID uuid.UUID
				var doc []byte
				if err := rows.Scan(&rowID, &doc); err != nil {
					rows.Close()
					return err
				}
				// An API key's {} is forma2's column default, which grants nothing; this server reads it so.
				if check.column == "organization_access" && sameJSON(doc, []byte("{}")) {
					continue
				}
				violations, err := im.schemas.Violations(check.schema, doc)
				if err != nil {
					rows.Close()
					return err
				}
				if len(violations) == 0 {
					continue
				}
				bad++
				if len(examples) < maxReported {
					locations := make([]string, 0, len(violations))
					for _, v := range violations {
						locations = append(locations, "/"+strings.Join(v.Location, "/"))
					}
					slices.Sort(locations)
					examples = append(examples, fmt.Sprintf("%s at %s", rowID, strings.Join(slices.Compact(locations), ", ")))
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		if bad == 0 {
			continue
		}
		report := im.warn
		if check.required {
			report = im.fail
		}
		report("%d %s.%s documents do not match %s: %s", bad, check.table, check.column, check.schema, strings.Join(examples, "; "))
	}
	return nil
}
