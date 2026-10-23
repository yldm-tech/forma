package embeddeddata

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// The cases of forma2's reconcile.test.ts.

var (
	surveyID      = uuid.MustParse("00000000-0000-7000-8000-000000000001")
	otherSurveyID = uuid.MustParse("00000000-0000-7000-8000-000000000002")
	desiredPlan   = Field{StorageKey: "plan", Name: "plan", Source: SourceIngested, DataType: TypeString}
	desiredScore  = Field{StorageKey: "var_score", Name: "score", Source: SourceComputed, DataType: TypeNumber, DefaultValue: float64(0)}
)

func ingested(name string) Field {
	return Field{StorageKey: name, Name: name, Source: SourceIngested, DataType: TypeString}
}

// ids derives stable link and field ids from a name.
func ids(name string) (link, field uuid.UUID) {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("link_"+name)), uuid.NewSHA1(uuid.NameSpaceOID, []byte(name))
}

// local is a link to a definition this survey owns, the only kind a survey save may edit or delete.
func local(f Field, name string, position int) current {
	link, field := ids(name)
	owner := surveyID
	return current{LinkID: link, StorageKey: f.StorageKey, Position: position, FieldID: field, SurveyID: &owner, Field: f}
}

func ptr[T any](v T) *T { return &v }

func TestResolveDesired(t *testing.T) {
	cur := []Field{desiredScore, desiredPlan}
	if got := resolveDesired(cur, Patch{}); !reflect.DeepEqual(got, cur) {
		t.Fatalf("an omitted group must carry over: %v", got)
	}
	if got := resolveDesired(cur, Patch{Variables: &[]Variable{}}); !reflect.DeepEqual(got, []Field{desiredPlan}) {
		t.Fatalf("an empty group clears: %v", got)
	}
	renamed := desiredScore
	renamed.Name = "total_score"
	got := resolveDesired(cur, Patch{Variables: &[]Variable{{ID: "var_score", Name: "total_score", Type: "number", Value: float64(0)}}})
	if !reflect.DeepEqual(got, []Field{renamed, desiredPlan}) {
		t.Fatalf("only the carried group is replaced: %v", got)
	}
	got = resolveDesired(cur, Patch{HiddenFieldIDs: &[]string{"plan", "tier"}})
	if len(got) != 3 || got[0].StorageKey != "var_score" || got[1].StorageKey != "plan" || got[2].StorageKey != "tier" {
		t.Fatalf("the ingested group is independent: %v", got)
	}
	got = resolveDesired([]Field{desiredPlan, desiredScore}, Patch{HiddenFieldIDs: &[]string{"plan"}})
	if got[0].Source != SourceComputed || got[1].Source != SourceIngested {
		t.Fatalf("computed fields come first: %v", got)
	}
	// A text variable is a string field; its value is the default.
	got = DesiredFields([]Variable{{ID: "v1", Name: "greeting", Type: "text", Value: "hi"}}, []string{"utm"})
	if !reflect.DeepEqual(got, []Field{{StorageKey: "v1", Name: "greeting", Source: SourceComputed, DataType: TypeString, DefaultValue: "hi"}, ingested("utm")}) {
		t.Fatalf("mapping %v", got)
	}
}

func TestPlanReconcile(t *testing.T) {
	if p := planReconcile(surveyID, []current{local(desiredPlan, "ed_plan", 0), local(desiredScore, "ed_score", 1)}, []Field{desiredPlan, desiredScore}); !reflect.DeepEqual(p, plan{}) {
		t.Fatalf("a matching survey changes nothing: %+v", p)
	}
	if p := planReconcile(surveyID, nil, []Field{desiredPlan}); !reflect.DeepEqual(p, plan{create: []positioned{{Field: desiredPlan, Position: 0}}}) {
		t.Fatalf("create: %+v", p)
	}
	planLink, planField := ids("ed_plan")
	if p := planReconcile(surveyID, []current{local(desiredPlan, "ed_plan", 0)}, nil); !reflect.DeepEqual(p, plan{unlink: []unlinked{{LinkID: planLink, DeleteField: &planField}}}) {
		t.Fatalf("remove: %+v", p)
	}
	// A rename of a hidden field moves its address: a delete plus a create.
	renamed := Field{StorageKey: "plan_tier", Name: "plan_tier", Source: SourceIngested, DataType: TypeString}
	p := planReconcile(surveyID, []current{local(desiredPlan, "ed_plan", 0)}, []Field{renamed})
	if !reflect.DeepEqual(p, plan{unlink: []unlinked{{LinkID: planLink, DeleteField: &planField}}, create: []positioned{{Field: renamed, Position: 0}}}) {
		t.Fatalf("rename: %+v", p)
	}

	_, scoreField := ids("ed_score")
	for label, updated := range map[string]Field{
		"name":         {StorageKey: "var_score", Name: "total_score", Source: SourceComputed, DataType: TypeNumber, DefaultValue: float64(0)},
		"dataType":     {StorageKey: "var_score", Name: "score", Source: SourceComputed, DataType: TypeString, DefaultValue: "0"},
		"defaultValue": {StorageKey: "var_score", Name: "score", Source: SourceComputed, DataType: TypeNumber, DefaultValue: float64(10)},
	} {
		p := planReconcile(surveyID, []current{local(desiredScore, "ed_score", 0)}, []Field{updated})
		if !reflect.DeepEqual(p, plan{update: []Field{updated}, updateIDs: []uuid.UUID{scoreField}}) {
			t.Fatalf("%s: %+v", label, p)
		}
	}

	// A field whose source changed is replaced, not mutated.
	nowIngested := desiredScore
	nowIngested.Source = SourceIngested
	scoreLink, _ := ids("ed_score")
	p = planReconcile(surveyID, []current{local(desiredScore, "ed_score", 0)}, []Field{nowIngested})
	if !reflect.DeepEqual(p, plan{unlink: []unlinked{{LinkID: scoreLink, DeleteField: &scoreField}}, create: []positioned{{Field: nowIngested, Position: 0}}}) {
		t.Fatalf("source change: %+v", p)
	}
}

func TestPlanReconcileOrder(t *testing.T) {
	h1, h2, h3 := ingested("h1"), ingested("h2"), ingested("h3")
	h2Link, h2Field := ids("ed_h2")
	h1Link, _ := ids("ed_h1")
	h3Link, _ := ids("ed_h3")

	// Removing a middle field moves only the tail, and the link on its way out is not reordered.
	p := planReconcile(surveyID, []current{local(h1, "ed_h1", 0), local(h2, "ed_h2", 1), local(h3, "ed_h3", 2)}, []Field{h1, h3})
	if !reflect.DeepEqual(p, plan{unlink: []unlinked{{LinkID: h2Link, DeleteField: &h2Field}}, reorder: []positionedLink{{LinkID: h3Link, Position: 1}}}) {
		t.Fatalf("remove middle: %+v", p)
	}
	p = planReconcile(surveyID, []current{local(h1, "ed_h1", 0), local(h2, "ed_h2", 1)}, []Field{desiredScore, h1, h2})
	if !reflect.DeepEqual(p, plan{create: []positioned{{Field: desiredScore, Position: 0}}, reorder: []positionedLink{{LinkID: h1Link, Position: 1}, {LinkID: h2Link, Position: 2}}}) {
		t.Fatalf("insert: %+v", p)
	}
	// Links left at the wrong position are repaired by the next save.
	p = planReconcile(surveyID, []current{local(h1, "ed_h1", 0), local(h2, "ed_h2", 0), local(h3, "ed_h3", 0)}, []Field{h1, h2, h3})
	if !reflect.DeepEqual(p, plan{reorder: []positionedLink{{LinkID: h2Link, Position: 1}, {LinkID: h3Link, Position: 2}}}) {
		t.Fatalf("self-healing: %+v", p)
	}
}

func TestPlanReconcileSharedAndForeign(t *testing.T) {
	link, field := ids("ed_shared")
	shared := func(position int) current {
		return current{LinkID: link, StorageKey: "plan", Position: position, FieldID: field, Field: desiredPlan}
	}
	if p := planReconcile(surveyID, []current{shared(0)}, nil); !reflect.DeepEqual(p, plan{unlink: []unlinked{{LinkID: link}}}) {
		t.Fatalf("a shared field is unlinked, never deleted: %+v", p)
	}
	edited := Field{StorageKey: "plan", Name: "Plan tier", Source: SourceIngested, DataType: TypeNumber, DefaultValue: float64(1)}
	if p := planReconcile(surveyID, []current{shared(0)}, []Field{edited}); !reflect.DeepEqual(p, plan{}) {
		t.Fatalf("a shared definition is never edited: %+v", p)
	}
	if p := planReconcile(surveyID, []current{shared(3)}, []Field{desiredPlan}); !reflect.DeepEqual(p, plan{reorder: []positionedLink{{LinkID: link, Position: 0}}}) {
		t.Fatalf("a shared field still moves: %+v", p)
	}
	foreign := current{LinkID: link, StorageKey: "plan", FieldID: field, SurveyID: ptr(otherSurveyID), Field: desiredPlan}
	if p := planReconcile(surveyID, []current{foreign}, nil); !reflect.DeepEqual(p, plan{unlink: []unlinked{{LinkID: link}}}) {
		t.Fatalf("another survey's definition is never deleted: %+v", p)
	}
}

func TestValidateDesired(t *testing.T) {
	ok := []Field{desiredScore, desiredPlan, {StorageKey: "Legacy-Name_1", Name: "Legacy", Source: SourceIngested, DataType: TypeString},
		{StorageKey: "flag", Name: "flag", Source: SourceIngested, DataType: TypeBoolean, DefaultValue: true},
		{StorageKey: "day", Name: "day", Source: SourceIngested, DataType: TypeDate, DefaultValue: "2026-02-28"},
		{StorageKey: "at", Name: "at", Source: SourceIngested, DataType: TypeDate, DefaultValue: "2026-02-28T10:00:00.5Z"}}
	if err := validateDesired(ok); err != nil {
		t.Fatal(err)
	}
	for message, fields := range map[string][]Field{
		"Duplicate embedded data field: plan": {desiredPlan, desiredPlan},
		`Invalid embedded data field " plan": the key must contain only letters, digits, hyphens and underscores`: {ingested(" plan")},
		`Invalid embedded data field "x": the name must not be blank`:                                             {{StorageKey: "x", Name: " ", Source: SourceIngested, DataType: TypeString}},
		`Invalid embedded data field "x": the default value does not match its number type`:                       {{StorageKey: "x", Name: "x", Source: SourceComputed, DataType: TypeNumber, DefaultValue: "1"}},
		`Invalid embedded data field "x": the default value does not match its date type`:                         {{StorageKey: "x", Name: "x", Source: SourceIngested, DataType: TypeDate, DefaultValue: "2026-02-30"}},
		`Invalid embedded data field "y": the default value does not match its date type`:                         {{StorageKey: "y", Name: "y", Source: SourceIngested, DataType: TypeDate, DefaultValue: "2026-02-01T10:00:00+02:00"}},
	} {
		if err := validateDesired(fields); err == nil || err.Error() != "validation_error: "+message {
			t.Fatalf("%v: %v, want %s", fields, err, message)
		}
	}
}
