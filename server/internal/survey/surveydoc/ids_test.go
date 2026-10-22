package surveydoc

import (
	"slices"
	"strings"
	"testing"
)

func codeOf(e *IDError) ValidateIDCode {
	if e == nil {
		return ""
	}
	return e.Code
}

func TestValidateIDLenient(t *testing.T) {
	for _, name := range []string{"Legacy-Field", "Q1", "UserID", "_legacy", "1foo", "user_name", "country", "browser", "language", "userid", "lang", "startat", "WelcomeCard"} {
		if err := ValidateID(name, nil, false); err != nil {
			t.Errorf("lenient ValidateID(%q) = %v, want accepted", name, err)
		}
	}
	if got := codeOf(ValidateID("userId", nil, false)); got != IDReserved {
		t.Errorf("lenient userId = %q, want reserved", got)
	}
}

func TestValidateIDStrict(t *testing.T) {
	cases := map[string]ValidateIDCode{
		"Legacy-Field": IDNotSafeIdentifier, "Q1": IDNotSafeIdentifier, "CustomerRef": IDNotSafeIdentifier,
		"_legacy": IDNotSafeIdentifier, "1foo": IDNotSafeIdentifier, "user:name": IDInvalidChars,
		"user_name": "", "attr123": "", "user_id_ref": "", "verified": "", "started_at": "", "country_code": "",
		"deviceType": IDReserved, "userid": IDReserved, "verifiedemail": IDReserved, "multilanguage": IDReserved, "welcomecard": IDReserved,
	}
	for name, want := range cases {
		if got := codeOf(ValidateID(name, nil, true)); got != want {
			t.Errorf("strict ValidateID(%q) = %q, want %q", name, got, want)
		}
	}
	for reserved := range reservedDeclaredFieldNames {
		for _, candidate := range []string{reserved, strings.ToUpper(reserved)} {
			if got := ValidateID(candidate, nil, true); got == nil || *got != (IDError{IDReserved, candidate}) {
				t.Errorf("strict ValidateID(%q) = %v, want reserved", candidate, got)
			}
		}
	}
	for reserved := range ReservedFieldNames {
		if got := codeOf(ValidateID(strings.ToUpper(reserved), nil, true)); got != IDReserved {
			t.Errorf("strict ValidateID(%q) = %q, want reserved", strings.ToUpper(reserved), got)
		}
	}
	for _, p := range linkSurveySystemParams {
		if got := codeOf(ValidateID(p, nil, true)); got != IDReserved {
			t.Errorf("strict system param %q = %q, want reserved", p, got)
		}
	}
}

func TestValidateIDSharedChecks(t *testing.T) {
	for _, strict := range []bool{false, true} {
		if got := ValidateID("", nil, strict); got == nil || *got != (IDError{IDEmpty, ""}) {
			t.Errorf("strict=%v empty = %v", strict, got)
		}
		if got := codeOf(ValidateID("   ", nil, strict)); got != IDEmpty {
			t.Errorf("strict=%v blank = %q", strict, got)
		}
		if got := codeOf(ValidateID("field", []string{"FIELD"}, strict)); got != IDDuplicate {
			t.Errorf("strict=%v duplicate = %q", strict, got)
		}
		if got := codeOf(ValidateID("verifiedEmail", nil, strict)); got != IDReserved {
			t.Errorf("strict=%v verifiedEmail = %q", strict, got)
		}
		if got := ValidateID("my field", nil, strict); got == nil || *got != (IDError{IDHasSpaces, "my field"}) {
			t.Errorf("strict=%v spaces = %v", strict, got)
		}
		for _, name := range []string{"field!", "email@domain"} {
			if got := codeOf(ValidateID(name, nil, strict)); got != IDInvalidChars {
				t.Errorf("strict=%v %q = %q", strict, name, got)
			}
		}
	}
}

func declared(variables, hidden []string) DeclaredFields {
	var d DeclaredFields
	if variables != nil {
		vs := make([]Variable, len(variables))
		for i, n := range variables {
			vs[i] = Variable{ID: "var" + n, Name: n, Type: "text"}
		}
		d.Variables = &vs
	}
	if hidden != nil {
		d.HiddenFields = &HiddenFields{FieldIDs: hidden}
	}
	return d
}

func fields(errs []IDError) []string {
	out := []string{}
	for _, e := range errs {
		out = append(out, e.Field)
	}
	return out
}

func TestValidateNewDeclaredFieldNames(t *testing.T) {
	refused := func(existing, incoming []string) []string {
		return fields(ValidateNewDeclaredFieldNames(declared(nil, existing), declared(nil, incoming)))
	}
	cases := []struct {
		name               string
		existing, incoming []string
		want               []string
	}{
		{"a reserved name already declared passes", []string{"country"}, []string{"country"}, []string{}},
		{"several grandfathered names pass", []string{"country", "url", "source", "browser"}, []string{"country", "url", "source", "browser"}, []string{}},
		{"the same name absent from existing is refused", []string{"other_field"}, []string{"country"}, []string{"country"}},
		{"a grandfathered survey may not add a different reserved name", []string{"country"}, []string{"country", "browser"}, []string{"browser"}},
		{"grandfathering is per name", []string{"country"}, []string{"country", "team_size", "url"}, []string{"url"}},
		{"re-adding a deleted reserved field is refused", []string{"team_size"}, []string{"team_size", "country"}, []string{"country"}},
		{"any casing is refused", nil, []string{"COUNTRY"}, []string{"COUNTRY"}},
		{"existing grandfathers across casing", []string{"Country"}, []string{"country"}, []string{}},
		{"a duplicated incoming name yields one error", nil, []string{"country", "Country"}, []string{"country"}},
		{"system params are refused", nil, []string{"userid", "lang", "suid"}, []string{"userid", "lang", "suid"}},
		{"ordinary names pass", nil, []string{"team_size", "plan", "signup_source"}, []string{}},
	}
	for _, tc := range cases {
		if got := refused(tc.existing, tc.incoming); !slices.Equal(got, tc.want) {
			t.Errorf("%s: refused %v, want %v", tc.name, got, tc.want)
		}
	}
	if errs := ValidateNewDeclaredFieldNames(DeclaredFields{}, declared(nil, []string{"Team Size"})); len(errs) != 1 || errs[0].Code == IDReserved {
		t.Errorf("a name that is not a safe identifier: %v", errs)
	}
	if got := len(ValidateNewDeclaredFieldNames(DeclaredFields{}, DeclaredFields{Variables: &[]Variable{}})); got != 0 {
		t.Errorf("an absent hidden-field carrier declared %d names", got)
	}
}

func TestValidateNewDeclaredFieldClashes(t *testing.T) {
	cases := []struct {
		name               string
		existing, incoming DeclaredFields
		want               []string
	}{
		{"a variable may not take a hidden field's name", declared(nil, []string{"plan"}), declared([]string{"plan"}, []string{"plan"}), []string{"plan"}},
		{"a hidden field may not take a variable's name", declared([]string{"score"}, nil), declared([]string{"score"}, []string{"score"}), []string{"score"}},
		{"a create may not declare both sides", DeclaredFields{}, declared([]string{"plan"}, []string{"plan"}), []string{"plan"}},
		{"matching is case-insensitive", declared(nil, []string{"Plan"}), declared([]string{"plan"}, []string{"Plan"}), []string{"plan"}},
		{"the error names the side the write introduces (hidden field)", declared([]string{"plan"}, nil), declared([]string{"plan"}, []string{"Plan"}), []string{"Plan"}},
		{"the error names the side the write introduces (variable)", declared(nil, []string{"plan"}), declared([]string{"PLAN"}, []string{"plan"}), []string{"PLAN"}},
		{"a held clash may be resent", declared([]string{"first_name", "score"}, []string{"first_name", "plan"}), declared([]string{"first_name", "score"}, []string{"first_name", "plan"}), []string{}},
		{"an omitted carrier falls back to the stored one", declared(nil, []string{"plan"}), declared([]string{"plan"}, nil), []string{"plan"}},
	}
	for _, tc := range cases {
		if got := fields(ValidateNewDeclaredFieldClashes(tc.existing, tc.incoming)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	errs := ValidateNewDeclaredFields(DeclaredFields{}, declared([]string{"country"}, []string{"country"}))
	if len(errs) != 1 || errs[0].Code != IDReserved {
		t.Errorf("a reserved clash is reported once, as reserved: %v", errs)
	}
}

func TestDescribeDeclaredFieldNameError(t *testing.T) {
	reasonFor := func(name string) string {
		return DescribeDeclaredFieldNameError(ValidateNewDeclaredFieldNames(DeclaredFields{}, declared(nil, []string{name}))[0])
	}
	cases := []struct {
		name, contains, excludes string
	}{
		{"country", "auto-captured system field", "URL contract"},
		{"lang", `"lang"`, "auto-captured"},
		{"userid", `"userId"`, "auto-captured"},
		{"source", "URL contract", "auto-captured system field"},
		{"UserRegion", "lowercase letter", "reserved"},
		{"team size", "spaces", "lowercase letter"},
		{"user:name", "letters, numbers, underscores and hyphens", "lowercase letter"},
	}
	for _, tc := range cases {
		reason := reasonFor(tc.name)
		if !strings.Contains(reason, tc.contains) || strings.Contains(reason, tc.excludes) {
			t.Errorf("%q: %q", tc.name, reason)
		}
	}
	empty := DescribeDeclaredFieldNameError(IDError{IDEmpty, ""})
	if empty != `Field name "" cannot be used: it must not be empty.` {
		t.Errorf("empty: %q", empty)
	}
	msg := DescribeDeclaredFieldNameErrors(ValidateNewDeclaredFieldNames(DeclaredFields{}, declared(nil, []string{"country", "url"})))
	for _, want := range []string{`"country"`, `"url"`, "newly added names only"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

func TestNewIDHasCUID2Shape(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := NewID()
		if len(id) != 24 || !IsCUID2(id) || id[0] < 'a' || id[0] > 'z' || seen[id] {
			t.Fatalf("NewID() = %q", id)
		}
		seen[id] = true
	}
}
