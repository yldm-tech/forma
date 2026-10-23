package embeddeddata

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
)

// The cases of forma2's embedded-data-reconcile and embedded-data-tenancy integration tests, against a real PostgreSQL.

type dbEnv struct {
	t   *testing.T
	ctx context.Context
	db  *gorm.DB
	ws  uuid.UUID
}

func newDBEnv(t *testing.T) *dbEnv {
	t.Parallel()
	e := &dbEnv{t: t, ctx: context.Background(), db: pgtest.GORM(t)}
	org := e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	e.ws = e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Main') RETURNING id`, org)
	return e
}

func (e *dbEnv) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

func (e *dbEnv) exec(query string, args ...any) error {
	return e.db.Exec(query, args...).Error
}

func (e *dbEnv) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Raw(query, args...).Row().Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *dbEnv) survey(ws uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Survey') RETURNING id`, ws)
}

// save reconciles inside a transaction, as a survey save does.
func (e *dbEnv) save(survey uuid.UUID, patch Patch) error {
	return e.db.Transaction(func(tx *gorm.DB) error {
		return Reconcile(e.ctx, tx, survey, e.ws, patch)
	})
}

func (e *dbEnv) mustSave(survey uuid.UUID, patch Patch) {
	e.t.Helper()
	if err := e.save(survey, patch); err != nil {
		e.t.Fatal(err)
	}
}

// stored is one of a survey's links with the definition it points at.
type stored struct {
	StorageKey string
	Source     string
	Position   int
	FieldID    uuid.UUID
	Name       string
	DataType   string
}

func (e *dbEnv) fields(survey uuid.UUID) []stored {
	e.t.Helper()
	var out []stored
	if err := e.db.Raw(`SELECT l.storage_key, e.source, l.position, e.id AS field_id, e.name, e.data_type FROM survey_embedded_data l JOIN embedded_data e ON e.id = l.embedded_data_id
		WHERE l.survey_id = ? ORDER BY l.position, l.storage_key`, survey).Scan(&out).Error; err != nil {
		e.t.Fatal(err)
	}
	return out
}

func keys(fields []stored) []string {
	var out []string
	for _, f := range fields {
		out = append(out, f.StorageKey)
	}
	return out
}

func vars(vs ...Variable) *[]Variable { return &vs }

func hidden(ids ...string) *[]string { return &ids }

func TestReconcileCreatesAndNumbersFields(t *testing.T) {
	e := newDBEnv(t)
	s := e.survey(e.ws)
	e.mustSave(s, Patch{Variables: vars(Variable{ID: "v_score", Name: "score", Type: "number", Value: float64(0)}, Variable{ID: "v_tier", Name: "tier", Type: "text", Value: "free"}), HiddenFieldIDs: hidden("utm", "Legacy-Plan")})
	got := e.fields(s)
	if !slices.Equal(keys(got), []string{"v_score", "v_tier", "utm", "Legacy-Plan"}) {
		t.Fatalf("fields %+v", got)
	}
	for i, f := range got {
		if f.Position != i {
			t.Fatalf("positions %+v", got)
		}
	}
	if got[0].DataType != TypeNumber || got[1].DataType != TypeString || got[2].Source != SourceIngested || got[3].Name != "Legacy-Plan" {
		t.Fatalf("definitions %+v", got)
	}
	if e.count(`SELECT count(*) FROM embedded_data WHERE survey_id = ? AND key IS NULL AND workspace_id = ?`, s, e.ws) != 4 {
		t.Fatal("rows are not local to the survey")
	}
	linked, err := SurveyFields(e.ctx, e.db, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 4 || linked[0].Link.StorageKey != "v_score" || linked[0].Field.DefaultValue != float64(0) || linked[1].Field.DefaultValue != "free" || linked[2].Field.DefaultValue != nil || linked[2].Field.Locked {
		t.Fatalf("read seam %+v", linked)
	}

	// Running it again changes nothing.
	e.mustSave(s, Patch{Variables: vars(Variable{ID: "v_score", Name: "score", Type: "number", Value: float64(0)}, Variable{ID: "v_tier", Name: "tier", Type: "text", Value: "free"}), HiddenFieldIDs: hidden("utm", "Legacy-Plan")})
	if again := e.fields(s); !slices.Equal(again, got) {
		t.Fatalf("not idempotent: %+v", again)
	}

	// A save that does not carry a group keeps it.
	e.mustSave(s, Patch{})
	e.mustSave(s, Patch{Variables: vars(Variable{ID: "v_score", Name: "score", Type: "number", Value: float64(0)}, Variable{ID: "v_tier", Name: "tier", Type: "text", Value: "free"})})
	if kept := e.fields(s); !slices.Equal(kept, got) {
		t.Fatalf("an omitted group changed: %+v", kept)
	}
}

func TestReconcileUpdatesRemovesAndReplaces(t *testing.T) {
	e := newDBEnv(t)
	s := e.survey(e.ws)
	e.mustSave(s, Patch{Variables: vars(Variable{ID: "v_score", Name: "score", Type: "number", Value: float64(0)}), HiddenFieldIDs: hidden("h1", "h2", "h3")})
	before := e.fields(s)

	// A changed default rewrites the same row.
	e.mustSave(s, Patch{Variables: vars(Variable{ID: "v_score", Name: "score", Type: "number", Value: float64(10)})})
	linked, _ := SurveyFields(e.ctx, e.db, s)
	if linked[0].Field.DefaultValue != float64(10) || e.fields(s)[0].FieldID != before[0].FieldID {
		t.Fatalf("update %+v", linked)
	}

	// Removing a middle field deletes its row and renumbers the tail.
	e.mustSave(s, Patch{HiddenFieldIDs: hidden("h1", "h3")})
	after := e.fields(s)
	if !slices.Equal(keys(after), []string{"v_score", "h1", "h3"}) || after[2].Position != 2 || after[2].FieldID != before[3].FieldID {
		t.Fatalf("remove %+v", after)
	}
	if e.count(`SELECT count(*) FROM embedded_data WHERE id = ?`, before[2].FieldID) != 0 {
		t.Fatal("the removed field's row survived")
	}

	// A storage key whose source changes is a different field: replaced, not mutated.
	e.mustSave(s, Patch{Variables: vars(), HiddenFieldIDs: hidden("v_score", "h1", "h3")})
	replaced := e.fields(s)
	if replaced[0].StorageKey != "v_score" || replaced[0].Source != SourceIngested || replaced[0].FieldID == before[0].FieldID {
		t.Fatalf("replace %+v", replaced)
	}

	// Positions something else left wrong are repaired by the next save.
	if err := e.exec(`UPDATE survey_embedded_data SET position = 0 WHERE survey_id = ?`, s); err != nil {
		t.Fatal(err)
	}
	e.mustSave(s, Patch{})
	for i, f := range e.fields(s) {
		if f.Position != i {
			t.Fatalf("not repaired %+v", e.fields(s))
		}
	}

	err := e.save(s, Patch{HiddenFieldIDs: hidden("plan", "plan")})
	if e, ok := apperr.As(err); !ok || e.Message != "Duplicate embedded data field: plan" {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestReconcileLeavesSharedAndForeignDefinitions(t *testing.T) {
	e := newDBEnv(t)
	s, other := e.survey(e.ws), e.survey(e.ws)
	// Two surveys of one workspace may each hold a field named plan.
	e.mustSave(s, Patch{HiddenFieldIDs: hidden("plan")})
	e.mustSave(other, Patch{HiddenFieldIDs: hidden("plan")})

	shared := e.insert(`INSERT INTO embedded_data (workspace_id, key, name, source) VALUES (?, 'tier', 'Tier', 'ingested') RETURNING id`, e.ws)
	if err := e.exec(`INSERT INTO survey_embedded_data (workspace_id, survey_id, embedded_data_id, storage_key, position) VALUES (?, ?, ?, 'tier', 1)`, e.ws, s, shared); err != nil {
		t.Fatal(err)
	}
	// Another survey's local definition, linked from this one (nothing creates this today).
	foreign := e.fields(other)[0].FieldID
	if err := e.exec(`INSERT INTO survey_embedded_data (workspace_id, survey_id, embedded_data_id, storage_key, position) VALUES (?, ?, ?, 'borrowed', 2)`, e.ws, s, foreign); err != nil {
		t.Fatal(err)
	}

	// An edit to the shared field is ignored; dropping every hidden field unlinks both without deleting either definition.
	e.mustSave(s, Patch{HiddenFieldIDs: hidden("plan", "tier", "borrowed")})
	if e.count(`SELECT count(*) FROM embedded_data WHERE id = ? AND name = 'Tier'`, shared) != 1 {
		t.Fatal("the shared definition was edited")
	}
	e.mustSave(s, Patch{HiddenFieldIDs: hidden()})
	if len(e.fields(s)) != 0 || e.count(`SELECT count(*) FROM embedded_data WHERE id IN (?, ?)`, shared, foreign) != 2 || len(e.fields(other)) != 1 {
		t.Fatal("a definition this survey does not own was deleted")
	}

	// Deleting a survey takes its local fields and links, never the library.
	if err := e.exec(`DELETE FROM surveys WHERE id = ?`, other); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT count(*) FROM embedded_data WHERE survey_id = ?`, other) != 0 || e.count(`SELECT count(*) FROM embedded_data WHERE id = ?`, shared) != 1 {
		t.Fatal("survey deletion did not cascade as expected")
	}
}

func TestReconcileNeverTouchesResponses(t *testing.T) {
	e := newDBEnv(t)
	s := e.survey(e.ws)
	e.mustSave(s, Patch{HiddenFieldIDs: hidden("plan")})
	r := e.insert(`INSERT INTO responses (survey_id, data) VALUES (?, '{"plan":"pro"}') RETURNING id`, s)
	e.mustSave(s, Patch{HiddenFieldIDs: hidden()})
	if e.count(`SELECT count(*) FROM responses WHERE id = ? AND data = '{"plan":"pro"}'`, r) != 1 {
		t.Fatal("a reconcile changed a response")
	}
}

func TestEmbeddedDataTenancy(t *testing.T) {
	e := newDBEnv(t)
	s := e.survey(e.ws)
	org := e.insert(`INSERT INTO organizations (name) VALUES ('Other') RETURNING id`)
	otherWS := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Theirs') RETURNING id`, org)
	theirSurvey := e.survey(otherWS)

	// A reconcile told the wrong workspace fails rather than writing across tenants.
	err := e.db.Transaction(func(tx *gorm.DB) error {
		return Reconcile(e.ctx, tx, theirSurvey, e.ws, Patch{HiddenFieldIDs: hidden("plan")})
	})
	if err == nil || e.count(`SELECT count(*) FROM embedded_data`) != 0 {
		t.Fatalf("cross-workspace reconcile: %v", err)
	}

	if err := e.exec(`INSERT INTO embedded_data (workspace_id, survey_id, name, source) VALUES (?, ?, 'x', 'ingested')`, e.ws, theirSurvey); err == nil {
		t.Fatal("a local field was owned by another workspace's survey")
	}
	theirs := e.insert(`INSERT INTO embedded_data (workspace_id, key, name, source) VALUES (?, 'plan', 'Plan', 'ingested') RETURNING id`, otherWS)
	if err := e.exec(`INSERT INTO survey_embedded_data (workspace_id, survey_id, embedded_data_id, storage_key, position) VALUES (?, ?, ?, 'plan', 0)`, e.ws, s, theirs); err == nil {
		t.Fatal("a survey linked another workspace's field")
	}
	if err := e.exec(`INSERT INTO survey_embedded_data (workspace_id, survey_id, embedded_data_id, storage_key, position) VALUES (?, ?, ?, 'plan', 0)`, otherWS, s, theirs); err == nil {
		t.Fatal("a link named a survey of another workspace")
	}
	if err := e.exec(`INSERT INTO embedded_data (workspace_id, survey_id, key, name, source) VALUES (?, ?, 'both', 'x', 'ingested')`, e.ws, s); err == nil {
		t.Fatal("a field was both local and shared")
	}
}
