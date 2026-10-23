package forma2import

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
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

// verify reads back what the run wrote, in the import transaction: every written row is present under the id legacyid derives from its forma2 id, sample rows hold forma2's values, and the documents pass this server's validators.
func (im *importer) verify(ctx context.Context) error {
	if _, err := im.tx.Exec(ctx, `CREATE TEMP TABLE forma2_verify_keys (k0 uuid, k1 uuid) ON COMMIT DROP`); err != nil {
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

// verifyPresent checks that every row written is in the table, and that no two forma2 rows were written onto one.
func (im *importer) verifyPresent(ctx context.Context, w *writtenTable) error {
	if _, err := im.tx.Exec(ctx, `TRUNCATE forma2_verify_keys`); err != nil {
		return err
	}
	if _, err := im.tx.CopyFrom(ctx, pgx.Identifier{"forma2_verify_keys"}, []string{"k0", "k1"}, pgx.CopyFromSlice(len(w.keys), func(i int) ([]any, error) {
		k := w.keys[i]
		out := []any{pgtype.UUID{Bytes: k[0], Valid: true}, pgtype.UUID{}}
		if len(k) > 1 {
			out[1] = pgtype.UUID{Bytes: k[1], Valid: true}
		}
		return out, nil
	})); err != nil {
		return err
	}
	match := make([]string, len(w.spec.key))
	for i, k := range w.spec.key {
		match[i] = fmt.Sprintf("t.%s = v.k%d", pgx.Identifier{k}.Sanitize(), i)
	}
	var distinct, present int
	if err := im.tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(DISTINCT (v.k0, COALESCE(v.k1, v.k0))), count(*) FILTER (WHERE EXISTS (SELECT 1 FROM %s t WHERE %s)) FROM forma2_verify_keys v`,
		pgx.Identifier{w.spec.target}.Sanitize(), strings.Join(match, " AND "))).Scan(&distinct, &present); err != nil {
		return err
	}
	rep := &im.report.Tables[w.report]
	rep.Present = present
	if distinct != len(w.keys) {
		im.fail("%s from %s: %d rows were written onto %d keys", w.spec.target, w.spec.label(), len(w.keys), distinct)
	}
	if present != len(w.keys) {
		im.fail("%s from %s: %d of the %d rows written are not in the table", w.spec.target, w.spec.label(), len(w.keys)-present, len(w.keys))
	}
	return nil
}

// verifySamples compares the first rows written with what forma2 holds, column by column: a plain column holds forma2's value, a JSON column the same document, and an id column legacyid's id of forma2's.
func (im *importer) verifySamples(ctx context.Context, w *writtenTable) error {
	var compared []int
	for i, c := range w.spec.cols {
		if c.kind == plain || c.kind == jsonDoc || c.kind == id {
			compared = append(compared, i)
		}
	}
	names := make([]string, len(compared))
	for i, ci := range compared {
		names[i] = pgx.Identifier{w.spec.cols[ci].name}.Sanitize()
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
		for i, ci := range compared {
			col := w.spec.cols[ci]
			if !sameValue(col.kind, s.raw[ci], got[i]) {
				im.fail("%s: row %s holds a %s that differs from forma2's", w.spec.target, s.key, col.name)
			}
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

// sameValue compares a value read from forma2 with the value read back from the target.
func sameValue(k kind, src, tgt any) bool {
	if src == nil || tgt == nil {
		return src == nil && tgt == nil
	}
	switch k {
	case id:
		s, ok := src.(string)
		if !ok {
			return false
		}
		want, _ := legacyid.Resolve(s)
		got, ok := tgt.([16]byte)
		return ok && uuid.UUID(got) == want
	case jsonDoc:
		s, ok := src.(string)
		if !ok {
			return false
		}
		b, err := json.Marshal(tgt)
		return err == nil && sameJSON([]byte(s), b)
	}
	switch s := src.(type) {
	case time.Time:
		t, ok := tgt.(time.Time)
		return ok && s.Equal(t)
	case pgtype.Numeric:
		t, ok := tgt.(pgtype.Numeric)
		if !ok {
			return false
		}
		a, err1 := s.Float64Value()
		b, err2 := t.Float64Value()
		return err1 == nil && err2 == nil && a == b
	}
	return reflect.DeepEqual(src, tgt)
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
