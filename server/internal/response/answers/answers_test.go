package answers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// now is a Wednesday, so working-day bounds step over a weekend.
var now = time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)

func blocksOf(t *testing.T, elements ...string) json.RawMessage {
	t.Helper()
	raw := json.RawMessage(`[{"id":"b1","name":"Block","elements":[` + strings.Join(elements, ",") + `]}]`)
	var check any
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Fatalf("bad blocks %s: %v", raw, err)
	}
	return raw
}

func dataOf(t *testing.T, raw string) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// messagesFor validates data against one element and returns its messages, nil when it passes.
func messagesFor(t *testing.T, el, data, language string) []string {
	t.Helper()
	errs, err := Validate(blocksOf(t, el), dataOf(t, data), language, "en-US", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 {
		return nil
	}
	var out []string
	for _, e := range errs[0].Errors {
		out = append(out, e.Message)
	}
	return out
}

func expectValid(t *testing.T, el, data string) {
	t.Helper()
	if got := messagesFor(t, el, data, "en"); got != nil {
		t.Fatalf("%s with %s: %v, want valid", el, data, got)
	}
}

func expectMessages(t *testing.T, el, data string, want ...string) {
	t.Helper()
	got := messagesFor(t, el, data, "en")
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s with %s: %v, want %v", el, data, got, want)
	}
}

const (
	required = "Please fill out this field"
	invalid  = "Please enter a valid format"
)

func openText(extra string) string {
	return `{"id":"q","type":"openText","headline":{"default":"Q"},"required":false,"inputType":"text"` + extra + `}`
}

func withRules(logic string, rules ...string) string {
	l := ""
	if logic != "" {
		l = `,"logic":"` + logic + `"`
	}
	return `,"validation":{"rules":[` + strings.Join(rules, ",") + `]` + l + `}`
}

// Only elements whose key the data carries are validated: a required element left out entirely passes (forma2's validateResponseData).
func TestOnlyElementsPresentInDataAreValidated(t *testing.T) {
	el := `{"id":"q","type":"openText","headline":{"default":"Q"},"required":true,"inputType":"text"}`
	expectValid(t, el, `{"other":"x"}`)
	expectMessages(t, el, `{"q":""}`, required)
	errs, err := Validate(nil, dataOf(t, `{"q":""}`), "en", "en-US", now)
	if err != nil || errs != nil {
		t.Fatalf("a survey without blocks: %v %v", errs, err)
	}
	if errs, _ := Validate(blocksOf(t, el), nil, "en", "en-US", now); errs != nil {
		t.Fatalf("an update without data: %v", errs)
	}
}

func TestRequiredChecks(t *testing.T) {
	expectValid(t, `{"id":"q","type":"openText","required":true}`, `{"q":"x"}`)
	expectMessages(t, `{"id":"q","type":"openText","required":true}`, `{"q":[]}`, required)
	expectValid(t, `{"id":"q","type":"cta","required":true}`, `{"q":""}`)
	ranking := `{"id":"q","type":"ranking","required":true,"choices":[{"id":"a","label":{"default":"A"}}]}`
	expectValid(t, ranking, `{"q":["A"]}`)
	expectMessages(t, ranking, `{"q":[]}`, required)
	expectMessages(t, ranking, `{"q":"A"}`, required)
	matrix := `{"id":"q","type":"matrix","required":true,"rows":[{"id":"r1","label":{"default":"R1"}},{"id":"r2","label":{"default":"R2"}}],"columns":[]}`
	expectValid(t, matrix, `{"q":{"R1":"Good","R2":""}}`)
	expectMessages(t, matrix, `{"q":{"R1":"","R2":""}}`, required)
	expectMessages(t, matrix, `{"q":{}}`, required)
}

func TestOtherOptions(t *testing.T) {
	multi := `{"id":"q","type":"multipleChoiceMulti","required":false,"choices":[{"id":"a","label":{"default":"A"}},{"id":"other","label":{"default":"Other"}}]}`
	expectMessages(t, multi, `{"q":["A",""]}`, required)
	expectMessages(t, multi, `{"q":["",""]}`, required)
	expectValid(t, multi, `{"q":["A","","custom"]}`)
	expectValid(t, multi, `{"q":["A","custom"]}`)

	single := `{"id":"q","type":"multipleChoiceSingle","required":false,"choices":[{"id":"a","label":{"default":"A","de":"Ä"}},{"id":"other","label":{"default":"Other"}}]}`
	expectValid(t, single, `{"q":"A"}`)
	expectValid(t, single, `{"q":"a"}`)
	expectValid(t, single, `{"q":"anything at all"}`)
	expectMessages(t, single, `{"q":"   "}`, required)
	expectMessages(t, single, `{"q":""}`, required)
	requiredSingle := strings.Replace(single, `"required":false`, `"required":true`, 1)
	// A required element sent empty is the required check's, not reported twice.
	expectMessages(t, requiredSingle, `{"q":""}`, required)
}

func TestChoiceMembership(t *testing.T) {
	single := `{"id":"q","type":"multipleChoiceSingle","required":false,"choices":[{"id":"a","label":{"default":"Apple","de":"Apfel"}},{"id":"b","label":{"default":"Banana"}}]}`
	expectValid(t, single, `{"q":"a"}`)
	expectValid(t, single, `{"q":"Apple"}`)
	expectMessages(t, single, `{"q":"Cherry"}`, invalid)
	expectMessages(t, single, `{"q":5}`, invalid)
	if got := messagesFor(t, single, `{"q":"Apfel"}`, "de"); got != nil {
		t.Fatalf("a label in the response's language: %v", got)
	}
	multi := strings.Replace(single, "multipleChoiceSingle", "multipleChoiceMulti", 1)
	expectValid(t, multi, `{"q":["a","Banana",""]}`)
	expectMessages(t, multi, `{"q":["a","Cherry"]}`, invalid)
	// A blank translation falls back to the default label (ENG-2001).
	blank := `{"id":"q","type":"multipleChoiceSingle","required":false,"choices":[{"id":"a","label":{"default":"Apple","en-GB":""}}]}`
	if got := messagesFor(t, blank, `{"q":"Apple"}`, "en-GB"); got != nil {
		t.Fatalf("default label fallback: %v", got)
	}
	if got := messagesFor(t, blank, `{"q":""}`, "en-GB"); got != nil {
		t.Fatalf("an empty optional answer: %v", got)
	}
	// A malformed element without choices is not checked.
	expectValid(t, `{"id":"q","type":"multipleChoiceSingle","required":false}`, `{"q":"x"}`)
}

func TestRuleLogic(t *testing.T) {
	min10, max20, max3 := `{"id":"r1","type":"minLength","params":{"min":10}}`, `{"id":"r2","type":"maxLength","params":{"max":20}}`, `{"id":"r2","type":"maxLength","params":{"max":3}}`
	expectValid(t, openText(withRules("and", `{"id":"r1","type":"minLength","params":{"min":5}}`, max20)), `{"q":"hello"}`)
	expectMessages(t, openText(withRules("and", min10, max20)), `{"q":"hi"}`, "Please enter at least 10 characters")
	expectMessages(t, openText(withRules("", min10, max3)), `{"q":"hello"}`, "Please enter at least 10 characters", "Please enter no more than 3 characters")
	expectValid(t, openText(withRules("or", min10, max20)), `{"q":"hello"}`)
	expectMessages(t, openText(withRules("or", min10, max3)), `{"q":"hello"}`, "Please enter at least 10 characters", "Please enter no more than 3 characters")
	expectValid(t, openText(withRules("and", `{"id":"r9","type":"noSuchRule","params":{}}`)), `{"q":"x"}`)

	// Implicit type rules are ANDed with an author's OR group, never one of its alternatives.
	email := `{"id":"q","type":"openText","required":false,"inputType":"email"` + withRules("or", `{"id":"a","type":"equals","params":{"value":"alice@corp.com"}}`, `{"id":"b","type":"equals","params":{"value":"bob@corp.com"}}`) + `}`
	expectValid(t, email, `{"q":"alice@corp.com"}`)
	expectMessages(t, email, `{"q":"mallory@evil.com"}`, "Value must equal alice@corp.com", "Value must equal bob@corp.com")
	listed := `{"id":"q","type":"openText","required":false,"inputType":"email"` + withRules("or", `{"id":"a","type":"equals","params":{"value":"not an address"}}`) + `}`
	expectMessages(t, listed, `{"q":"not an address"}`, "Please enter a valid email address")
}

func TestImplicitRules(t *testing.T) {
	expectMessages(t, `{"id":"q","type":"openText","inputType":"email"}`, `{"q":"nope"}`, "Please enter a valid email address")
	expectMessages(t, `{"id":"q","type":"openText","inputType":"url"}`, `{"q":"nope"}`, "Please enter a valid URL")
	expectMessages(t, `{"id":"q","type":"openText","inputType":"phone"}`, `{"q":"call me"}`, "Please enter a valid phone number")
	// An explicit rule of the same type replaces the implicit one.
	expectMessages(t, `{"id":"q","type":"openText","inputType":"email"`+withRules("", `{"id":"e","type":"email","params":{}}`)+`}`, `{"q":"nope"}`, "Please enter a valid email address")
	contact := `{"id":"q","type":"contactInfo","required":false,"firstName":{"show":true,"required":false,"placeholder":{"default":"First name"}},"lastName":{"show":true,"required":false,"placeholder":{"default":"Last name"}},"email":{"show":true,"required":false,"placeholder":{"default":"Email"}},"phone":{"show":true,"required":false,"placeholder":{"default":"Phone"}},"company":{"show":false,"required":false,"placeholder":{"default":"Company"}}}`
	expectMessages(t, contact, `{"q":["Ada","Lovelace","nope","abc",""]}`, "Email: Please enter a valid email address", "Phone: Please enter a valid phone number")
	expectValid(t, contact, `{"q":["Ada","Lovelace","ada@example.com","+44 20 7946 0958",""]}`)
}

func TestFieldRules(t *testing.T) {
	address := `{"id":"q","type":"address","required":false,"addressLine1":{"show":true,"required":false,"placeholder":{"default":"Address Line 1"}},"addressLine2":{"show":true,"required":false,"placeholder":{"default":"Address Line 2"}},"city":{"show":true,"required":false,"placeholder":{"default":"City"}},"state":{"show":true,"required":false,"placeholder":{"default":"State"}},"zip":{"show":true,"required":false,"placeholder":{"default":"Zip"}},"country":{"show":true,"required":false,"placeholder":{"default":"Country"}}` +
		withRules("and", `{"id":"z","type":"pattern","field":"zip","params":{"pattern":"^[0-9]{5}$"}}`) + `}`
	expectMessages(t, address, `{"q":["1 Main St","","Springfield","IL","ABCDE","US"]}`, "Zip: Please enter a valid format")
	expectValid(t, address, `{"q":["1 Main St","","Springfield","IL","62701","US"]}`)
}

func TestTextRules(t *testing.T) {
	cases := []struct {
		rule, value string
		want        []string
	}{
		{`{"id":"r","type":"minLength","params":{"min":3}}`, `"ab"`, []string{"Please enter at least 3 characters"}},
		{`{"id":"r","type":"minLength","params":{"min":3}}`, `""`, nil},
		{`{"id":"r","type":"minLength","params":{"min":2}}`, `"😀"`, nil},
		{`{"id":"r","type":"maxLength","params":{"max":1}}`, `"😀"`, []string{"Please enter no more than 1 characters"}},
		{`{"id":"r","type":"pattern","params":{"pattern":"^a+$"}}`, `"aaa"`, nil},
		{`{"id":"r","type":"pattern","params":{"pattern":"^a+$"}}`, `"AAA"`, []string{invalid}},
		{`{"id":"r","type":"pattern","params":{"pattern":"^a+$","flags":"i"}}`, `"AAA"`, nil},
		{`{"id":"r","type":"pattern","params":{"pattern":"(unclosed"}}`, `"x"`, nil},
		{`{"id":"r","type":"pattern","params":{"pattern":"a","flags":"q"}}`, `"b"`, nil},
		{`{"id":"r","type":"pattern","params":{"pattern":"` + strings.Repeat("a", 513) + `"}}`, `"a"`, []string{invalid}},
		{`{"id":"r","type":"pattern","params":{"pattern":"a"}}`, `"` + strings.Repeat("a", 4097) + `"`, []string{invalid}},
		{`{"id":"r","type":"email","params":{}}`, `"a@b.co"`, nil},
		{`{"id":"r","type":"url","params":{}}`, `"https://example.com/a b"`, nil},
		{`{"id":"r","type":"url","params":{}}`, `"mailto:ada@example.com"`, nil},
		{`{"id":"r","type":"url","params":{}}`, `"https://"`, []string{"Please enter a valid URL"}},
		{`{"id":"r","type":"url","params":{}}`, `"example.com"`, []string{"Please enter a valid URL"}},
		{`{"id":"r","type":"phone","params":{}}`, `"+1 555-0100"`, nil},
		{`{"id":"r","type":"phone","params":{}}`, `"555-"`, []string{"Please enter a valid phone number"}},
		{`{"id":"r","type":"equals","params":{"value":"yes"}}`, `"no"`, []string{"Value must equal yes"}},
		{`{"id":"r","type":"doesNotEqual","params":{"value":"yes"}}`, `"yes"`, []string{"Value must not equal yes"}},
		{`{"id":"r","type":"contains","params":{"value":"@"}}`, `"ab"`, []string{"Value must contain @"}},
		{`{"id":"r","type":"doesNotContain","params":{"value":"@"}}`, `"a@b"`, []string{"Value must not contain @"}},
	}
	for _, c := range cases {
		got := messagesFor(t, openText(withRules("and", c.rule)), `{"q":`+c.value+`}`, "en")
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s with %s: %v, want %v", c.rule, c.value, got, c.want)
		}
	}
}

func TestNumberRules(t *testing.T) {
	cases := []struct {
		rule, value string
		want        []string
	}{
		{`{"id":"r","type":"minValue","params":{"min":5}}`, `4`, []string{"Please enter a value of at least 5"}},
		{`{"id":"r","type":"minValue","params":{"min":5}}`, `"5"`, nil},
		{`{"id":"r","type":"minValue","params":{"min":5}}`, `"4 apples"`, []string{"Please enter a value of at least 5"}},
		{`{"id":"r","type":"minValue","params":{"min":5}}`, `"apples"`, nil},
		{`{"id":"r","type":"maxValue","params":{"max":1.5}}`, `2`, []string{"Please enter a value no greater than 1.5"}},
		{`{"id":"r","type":"isGreaterThan","params":{"min":5}}`, `5`, []string{"Please enter a value greater than 5"}},
		{`{"id":"r","type":"isLessThan","params":{"max":5}}`, `" 4.9"`, nil},
		{`{"id":"r","type":"isLessThan","params":{"max":5}}`, `""`, nil},
	}
	for _, c := range cases {
		got := messagesFor(t, openText(withRules("and", c.rule)), `{"q":`+c.value+`}`, "en")
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s with %s: %v, want %v", c.rule, c.value, got, c.want)
		}
	}
}

func TestSelectionRankingAndMatrixRules(t *testing.T) {
	multi := func(rules ...string) string {
		return `{"id":"q","type":"multipleChoiceMulti","required":false,"choices":[{"id":"a","label":{"default":"A"}},{"id":"b","label":{"default":"B"}},{"id":"c","label":{"default":"C"}},{"id":"other","label":{"default":"Other"}}]` + withRules("and", rules...) + `}`
	}
	expectMessages(t, multi(`{"id":"r","type":"minSelections","params":{"min":2}}`), `{"q":["A"]}`, "Please select at least 2 options")
	expectValid(t, multi(`{"id":"r","type":"minSelections","params":{"min":2}}`), `{"q":["A","","mine"]}`)
	expectValid(t, multi(`{"id":"r","type":"minSelections","params":{"min":2}}`), `{"q":[]}`)
	expectMessages(t, multi(`{"id":"r","type":"maxSelections","params":{"max":2}}`), `{"q":["A","B","C"]}`, "Please select no more than 2 options")

	ranking := func(rule string) string {
		return `{"id":"q","type":"ranking","required":false,"choices":[{"id":"a","label":{"default":"A"}},{"id":"b","label":{"default":"B"}},{"id":"c","label":{"default":"C"}}]` + withRules("and", rule) + `}`
	}
	expectMessages(t, ranking(`{"id":"r","type":"minRanked","params":{"min":2}}`), `{"q":["A"]}`, "Please rank at least 2 options")
	expectMessages(t, ranking(`{"id":"r","type":"rankAll","params":{}}`), `{"q":["A","B"]}`, "Please rank all options")
	expectValid(t, ranking(`{"id":"r","type":"rankAll","params":{}}`), `{"q":["A","B","C"]}`)

	matrix := func(required bool, rule string) string {
		r := "false"
		if required {
			r = "true"
		}
		return `{"id":"q","type":"matrix","required":` + r + `,"rows":[{"id":"r1","label":{"default":"R1"}},{"id":"r2","label":{"default":"R2"}}],"columns":[{"id":"c1","label":{"default":"Good"}}]` + withRules("and", rule) + `}`
	}
	expectMessages(t, matrix(false, `{"id":"r","type":"answerAllRows","params":{}}`), `{"q":{"R1":"Good","R2":""}}`, "Please answer all rows")
	expectMessages(t, matrix(true, `{"id":"r","type":"minRowsAnswered","params":{"min":2}}`), `{"q":{"R1":"Good"}}`, "Please answer at least 2 rows")
}

func TestFileExtensionRules(t *testing.T) {
	upload := func(rule string) string {
		return `{"id":"q","type":"fileUpload","required":false` + withRules("and", rule) + `}`
	}
	is := `{"id":"r","type":"fileExtensionIs","params":{"extensions":["pdf",".PNG"]}}`
	expectValid(t, upload(is), `{"q":["/storage/w/private/a--fid--1.pdf","https://x.example/b.png?sig=1"]}`)
	expectMessages(t, upload(is), `{"q":["/storage/w/private/a.exe"]}`, "File extension must be .pdf, .PNG")
	expectMessages(t, upload(is), `{"q":["/storage/w/private/noext"]}`, "File extension must be .pdf, .PNG")
	isNot := `{"id":"r","type":"fileExtensionIsNot","params":{"extensions":["exe"]}}`
	expectMessages(t, upload(isNot), `{"q":["/storage/w/private/a.EXE"]}`, "File extension must not be .exe")
	expectValid(t, upload(isNot), `{"q":["/storage/w/private/noext"]}`)
}

func TestDateRules(t *testing.T) {
	date := func(rule string) string {
		return `{"id":"q","type":"date","required":false` + withRules("and", rule) + `}`
	}
	expectMessages(t, date(`{"id":"r","type":"isLaterThan","params":{"date":"2026-03-01"}}`), `{"q":"2026-03-01"}`, "Please select a date later than 2026-03-01")
	expectValid(t, date(`{"id":"r","type":"isLaterThan","params":{"date":"2026-03-01"}}`), `{"q":"2026-03-02"}`)
	expectMessages(t, date(`{"id":"r","type":"isEarlierThan","params":{"date":"2026-03-01"}}`), `{"q":"2026-03-01"}`, "Please select a date earlier than 2026-03-01")
	expectMessages(t, date(`{"id":"r","type":"isBetween","params":{"startDate":"2026-01-01","endDate":"2026-01-31"}}`), `{"q":"2026-01-31"}`, "Please select a date between 2026-01-01 and 2026-01-31")
	expectMessages(t, date(`{"id":"r","type":"isNotBetween","params":{"startDate":"2026-01-01","endDate":"2026-01-31"}}`), `{"q":"2026-01-15"}`, "Please select a date not between 2026-01-01 and 2026-01-31")

	// Relative bounds are inclusive, and the server forgives one day either way for the respondent's time zone.
	later := date(`{"id":"r","type":"isLaterThan","params":{"relative":{"amount":2,"unit":"calendarDays","direction":"before"}}}`)
	expectValid(t, later, `{"q":"2026-10-19"}`)
	expectValid(t, later, `{"q":"2026-10-18"}`)
	expectMessages(t, later, `{"q":"2026-10-17"}`, "Please select a date later than 2026-10-18")
	// Two working days after Wednesday is Friday; the grace starts from Thursday, giving Monday.
	earlier := date(`{"id":"r","type":"isEarlierThan","params":{"relative":{"amount":2,"unit":"workingDays","direction":"after"}}}`)
	expectValid(t, earlier, `{"q":"2026-10-26"}`)
	expectMessages(t, earlier, `{"q":"2026-10-27"}`, "Please select a date earlier than 2026-10-24")
	between := date(`{"id":"r","type":"isBetween","params":{"relativeStart":{"amount":0,"unit":"calendarDays","direction":"after"},"relativeEnd":{"amount":3,"unit":"calendarDays","direction":"after"}}}`)
	expectValid(t, between, `{"q":"2026-10-20"}`)
	expectMessages(t, between, `{"q":"2026-10-26"}`, "Please select a date between 2026-10-21 and 2026-10-24")
	notBetween := date(`{"id":"r","type":"isNotBetween","params":{"relativeStart":{"amount":0,"unit":"calendarDays","direction":"after"},"relativeEnd":{"amount":3,"unit":"calendarDays","direction":"after"}}}`)
	expectValid(t, notBetween, `{"q":"2026-10-21"}`)
	expectMessages(t, notBetween, `{"q":"2026-10-22"}`, "Please select a date not between 2026-10-21 and 2026-10-24")
}

// The messages are in the response's language; "default" is the survey's default language.
func TestMessagesFollowTheLanguage(t *testing.T) {
	el := `{"id":"q","type":"openText","required":true}`
	if got := messagesFor(t, el, `{"q":""}`, "de-DE"); len(got) != 1 || got[0] != "Bitte füllen Sie dieses Feld aus" {
		t.Fatalf("de-DE: %v", got)
	}
	errs, err := Validate(blocksOf(t, el), dataOf(t, `{"q":""}`), "default", "fr-FR", now)
	if err != nil || len(errs) != 1 || errs[0].Errors[0].Message != "Veuillez remplir ce champ" {
		t.Fatalf("default: %v %v", errs, err)
	}
}

func TestV1Details(t *testing.T) {
	blocks := blocksOf(t, `{"id":"a","type":"openText","required":true}`, `{"id":"b","type":"openText","required":false`+withRules("and", `{"id":"r1","type":"minLength","params":{"min":3}}`, `{"id":"r2","type":"contains","params":{"value":"@"}}`)+`}`)
	errs, err := Validate(blocks, dataOf(t, `{"b":"x","a":""}`), "en", "en-US", now)
	if err != nil {
		t.Fatal(err)
	}
	got := V1Details(errs)
	want := []Detail{{"response.data.a", required}, {"response.data.b", "Please enter at least 3 characters; Value must contain @"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("details %v, want %v", got, want)
	}
}
