package contact

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Ported from forma2's contacts.test.ts (createContactsFromCSV), parse-contacts-csv.test.ts and types/contact.test.ts.

func TestParseCSV(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		headers string
		rows    int
	}{
		{"comma", "email,plan\na@example.com,pro\n", "email|plan", 1},
		{"semicolon", "email;plan\na@example.com;pro\n", "email|plan", 1},
		{"tab", "email\tplan\na@example.com\tpro\n", "email|plan", 1},
		{"byte order mark", "\xEF\xBB\xBFemail,plan\na@example.com,pro\n", "email|plan", 1},
		{"empty lines and CRLF", "email,plan\r\n\r\na@example.com,pro\r\n\r\nb@example.com,free\r\n", "email|plan", 2},
		{"quoted delimiter in the header", "\"name, full\";email\nAda;a@example.com\n", "name, full|email", 1},
		{"trimmed headers", " email , plan \na@example.com,pro\n", "email|plan", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			table, err := parseCSV(strings.NewReader(c.in))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(table.headers, "|") != c.headers || len(table.rows) != c.rows {
				t.Fatalf("headers %q rows %d", table.headers, len(table.rows))
			}
		})
	}
	if _, err := parseCSV(strings.NewReader("email,plan\na@example.com\n")); err == nil {
		t.Fatal("a row with fewer fields than the header was accepted")
	}
	var big strings.Builder
	big.WriteString("email\n")
	for range MaxImportRecords + 1 {
		big.WriteString("a@example.com\n")
	}
	if _, err := parseCSV(strings.NewReader(big.String())); err == nil || !strings.Contains(err.Error(), "Maximum 10000 records allowed at a time.") {
		t.Fatalf("record limit: %v", err)
	}
}

func importResult(t *testing.T, e *env, user, ws uuid.UUID, csv, action string, attributeMap map[string]string) ImportResult {
	t.Helper()
	var res ImportResult
	e.ok(e.upload(user, ws, csv, action, attributeMap), http.StatusOK, &res)
	return res
}

func TestImportCreatesContactsAndKeys(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	e.key(ws, "years", TypeNumber)

	res := importResult(t, e, owner, ws, "email,First Name,years,plan,signup,score\nada@example.com,Ada,36,pro,2024-01-15,1.5\ngrace@example.com,Grace,,free,15-01-2024,n/a\n", DuplicateSkip, nil)
	if len(res.Contacts) != 2 {
		t.Fatalf("contacts %+v", res.Contacts)
	}
	ada := res.Contacts[0].Attributes
	if ada["email"] != "ada@example.com" || ada["firstName"] != "Ada" || ada["years"] != "36" || ada["plan"] != "pro" || ada["signup"] != "2024-01-15T00:00:00.000Z" || ada["score"] != "1.5" {
		t.Fatalf("ada %v", ada)
	}
	grace := res.Contacts[1].Attributes
	if grace["years"] != "" || grace["signup"] != "2024-01-15T00:00:00.000Z" || grace["score"] != "n/a" {
		t.Fatalf("grace %v", grace)
	}
	var keys []Key
	e.ok(e.do(owner, http.MethodGet, wsPath(ws, "/contact-attribute-keys"), nil), http.StatusOK, &keys)
	types := map[string]Key{}
	for _, k := range keys {
		types[k.Key] = k
	}
	if types["plan"].DataType != TypeString || types["signup"].DataType != TypeDate || types["score"].DataType != TypeString || *types["signup"].Name != "Signup" || types["plan"].Type != KeyTypeCustom {
		t.Fatalf("new keys %+v", types)
	}
	if e.count(`SELECT count(*) FROM contact_attributes WHERE attribute_key_id = ? AND value_number IS NOT NULL`, e.keyID(ws, "years")) != 1 {
		t.Fatal("an existing number key stores parsed numbers")
	}
	if !strings.Contains(e.logs.String(), `"action":"createdFromCSV"`) {
		t.Fatal("no audit event")
	}
}

func TestImportDuplicates(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	existing := e.contact(ws, map[string]string{"email": "ada@example.com", "userId": "ada", "plan": "free", "lastName": "Lovelace"})
	other := e.contact(ws, map[string]string{"email": "grace@example.com", "userId": "grace"})
	const csv = "email,userId,plan\nada@example.com,grace,pro\nnew@example.com,,free\n"

	res := importResult(t, e, owner, ws, csv, DuplicateSkip, nil)
	if len(res.Contacts) != 1 || res.Contacts[0].Attributes["email"] != "new@example.com" {
		t.Fatalf("skip: %+v", res.Contacts)
	}
	if got := e.values(existing); got["plan"] != "free" {
		t.Fatalf("a skipped duplicate is untouched: %v", got)
	}

	res = importResult(t, e, owner, ws, "email,userId,plan\nada@example.com,grace,pro\n", DuplicateUpdate, nil)
	if len(res.Contacts) != 1 || res.Contacts[0].ID != existing {
		t.Fatalf("update: %+v", res.Contacts)
	}
	if got := e.values(existing); got["plan"] != "pro" || got["userId"] != "ada" || got["lastName"] != "Lovelace" {
		t.Fatalf("update merges, and a userId another contact holds gives way to the contact's own: %v", got)
	}
	if got := e.values(other); got["userId"] != "grace" {
		t.Fatalf("the other contact keeps its userId: %v", got)
	}

	importResult(t, e, owner, ws, "email,plan\nada@example.com,team\n", DuplicateOverwrite, nil)
	if got := e.values(existing); len(got) != 2 || got["plan"] != "team" || got["email"] != "ada@example.com" {
		t.Fatalf("overwrite replaces every value: %v", got)
	}
}

// forma2's column suggestion is a substring test against key names, so "age" would fill the default "Language" key; the tests name their columns around it.
func TestImportMapping(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	plan := e.key(ws, "plan", TypeString)

	res := importResult(t, e, owner, ws, "Mail,Tier,Email\nignored,pro,ada@example.com\n", DuplicateSkip, map[string]string{"Tier": plan.String(), "Mail": "mail_raw"})
	if got := res.Contacts[0].Attributes; got["plan"] != "pro" || got["email"] != "ada@example.com" || got["mail_raw"] != "ignored" {
		t.Fatalf("mapped %v", got)
	}

	rec := e.upload(owner, ws, "email,EMAIL\na@example.com,b@example.com\n", DuplicateSkip, nil)
	body := e.fails(rec, http.StatusBadRequest, CodeDuplicateMappings, "Attribute map contains duplicate values")
	if body.Details["attributes"] != "email, EMAIL" {
		t.Fatalf("details %v", body.Details)
	}
	e.fails(e.upload(owner, ws, "email,Favourite Colour\na@example.com,red\n", DuplicateSkip, nil), http.StatusBadRequest, CodeInvalidAttributeKeys, "Invalid attribute key(s): Favourite Colour. Keys must only contain lowercase letters, numbers, and underscores, and must start with a letter.")
	// "user_id" would be suggested for the userId key; mapped to a new key of its own name, it is reserved.
	e.fails(e.upload(owner, ws, "email,user_id\na@example.com,1\n", DuplicateSkip, map[string]string{"user_id": "user_id"}), http.StatusBadRequest, CodeReservedAttributeKeys, "Reserved attribute key(s): user_id. These keys are reserved for the v5.1 safe-identifier default attribute migration and cannot be created as custom attributes.")
}

func TestImportValidation(t *testing.T) {
	e := newEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	e.key(ws, "years", TypeNumber)

	e.fails(e.upload(owner, ws, "plan\npro\n", DuplicateSkip, nil), http.StatusBadRequest, CodeInvalidCSVRecords, "Missing email field for one or more records")
	e.fails(e.upload(owner, ws, "email,plan\n,pro\n", DuplicateSkip, nil), http.StatusBadRequest, CodeInvalidCSVRecords, "Email field is empty for one or more records")
	e.fails(e.upload(owner, ws, "email\na@example.com\na@example.com\n", DuplicateSkip, nil), http.StatusBadRequest, CodeInvalidCSVRecords, "Duplicate emails found in the records")
	e.fails(e.upload(owner, ws, "email,userId\na@example.com,1\nb@example.com,1\n", DuplicateSkip, nil), http.StatusBadRequest, CodeInvalidCSVRecords, "Duplicate userIds found in the records")
	e.fails(e.upload(owner, ws, "email\n", DuplicateSkip, nil), http.StatusBadRequest, CodeNoValidContacts, "")
	e.fails(e.upload(owner, ws, "email,plan\na@example.com\n", DuplicateSkip, nil), http.StatusBadRequest, CodeInvalidCSV, "")
	e.fails(e.upload(owner, ws, "email\na@example.com\n", "merge", nil), http.StatusBadRequest, "validation_error", "")
	e.fails(e.upload(reader, ws, "email\na@example.com\n", DuplicateSkip, nil), http.StatusForbidden, "forbidden", "")

	body := e.fails(e.upload(owner, ws, "email,years\na@example.com,12\nb@example.com,twelve\nc@example.com,1 2\n", DuplicateSkip, nil), http.StatusBadRequest, CodeAttributeTypeMismatch, "")
	raw, _ := json.Marshal(body.Details["validationErrors"])
	var mismatches []TypeMismatch
	if err := json.Unmarshal(raw, &mismatches); err != nil || len(mismatches) != 1 || mismatches[0].Key != "years" || mismatches[0].DataType != TypeNumber || strings.Join(mismatches[0].InvalidValues, "|") != "twelve|1 2" {
		t.Fatalf("type errors %s", raw)
	}
	if e.count(`SELECT count(*) FROM contacts WHERE workspace_id = ?`, ws) != 0 {
		t.Fatal("a failed import wrote contacts")
	}

	e.fails(e.uploadFile(owner, ws, "email\na@example.com\n", "contacts.csv", "application/octet-stream", DuplicateSkip, nil), http.StatusBadRequest, "upload_contacts_error_invalid_file_type", "")
	e.fails(e.uploadFile(owner, ws, "email\na@example.com\n", "contacts.txt", "", DuplicateSkip, nil), http.StatusBadRequest, "upload_contacts_error_invalid_file_type", "")
	if rec := e.uploadFile(owner, ws, "email\na@example.com\n", "contacts.csv", "", DuplicateSkip, nil); rec.Code != http.StatusOK {
		t.Fatalf("an undeclared .csv file is accepted: %d %s", rec.Code, rec.Body.String())
	}

	big := "email\n" + strings.Repeat("a", MaxImportBytes) + "@example.com\n"
	e.fails(e.upload(owner, ws, big, DuplicateSkip, nil), http.StatusRequestEntityTooLarge, "upload_contacts_error_file_too_large", "")
}
