package contact

import (
	"encoding/csv"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// exportCSV exports a workspace's contacts as the contacts page does and parses the file.
func exportCSV(t *testing.T, e *env, user, ws uuid.UUID, search string) [][]string {
	t.Helper()
	rec := e.do(user, http.MethodPost, wsPath(ws, "/contacts/export"), map[string]any{"search": search})
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != ContentTypeCSV {
		t.Fatalf("content type %q", ct)
	}
	if !regexp.MustCompile(`^attachment; filename="contacts-\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2}-utc\.csv"$`).MatchString(rec.Header().Get("Content-Disposition")) {
		t.Fatalf("content disposition %q", rec.Header().Get("Content-Disposition"))
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\ufeff") {
		t.Fatal("no byte order mark")
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatalf("parse export: %v\n%s", err, body)
	}
	return records
}

func TestExportContacts(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	_, other := e.organization(outsider)
	e.contact(other, map[string]string{"email": "eve@other.example"})

	ada := e.contact(ws, map[string]string{"email": "ada@example.com", "firstName": "Ada", "plan": "=HYPERLINK(\"https://evil.example\")"})
	e.setNumber(ws, ada, "balance", -5)
	e.setDate(ws, ada, "renewal", time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC))
	e.contact(ws, map[string]string{"email": "grace@example.com", "plan": "+81 90 1234 5678"})

	var keys []Key
	e.ok(e.do(owner, http.MethodGet, wsPath(ws, "/contact-attribute-keys"), nil), http.StatusOK, &keys)
	header := make([]string, len(keys))
	for i, k := range keys {
		header[i] = k.Key
	}

	records := exportCSV(t, e, reader, ws, "")
	if len(records) != 3 || !slices.Equal(records[0], header) {
		t.Fatalf("export %v, want the header %v and two contacts", records, header)
	}
	row := func(r []string) map[string]string {
		out := map[string]string{}
		for i, k := range header {
			out[k] = r[i]
		}
		return out
	}
	// Newest first: Grace, then Ada. A text that starts like a formula is defanged; a negative number is not.
	if got := row(records[1]); got["email"] != "grace@example.com" || got["plan"] != "'+81 90 1234 5678" || got["balance"] != "" {
		t.Fatalf("grace %v", got)
	}
	if got := row(records[2]); got["email"] != "ada@example.com" || got["firstName"] != "Ada" || got["plan"] != `'=HYPERLINK("https://evil.example")` || got["balance"] != "-5" || got["renewal"] != "2027-01-02T03:04:05.000Z" {
		t.Fatalf("ada %v", got)
	}
	if !strings.Contains(e.logs.String(), `"action":"exported"`) {
		t.Fatal("no audit event")
	}

	// The list's search: case-insensitive, trimmed.
	if records := exportCSV(t, e, reader, ws, "  ADA@ "); len(records) != 2 || row(records[1])["email"] != "ada@example.com" {
		t.Fatalf("searched export %v", records)
	}
	if records := exportCSV(t, e, reader, ws, "nobody"); len(records) != 1 {
		t.Fatalf("an export that matches nothing is the header alone: %v", records)
	}

	e.fails(e.do(outsider, http.MethodPost, wsPath(ws, "/contacts/export"), map[string]any{}), http.StatusForbidden, "forbidden", "")
}

// The export reads the contacts in batches; contacts created in the same instant are neither skipped nor repeated at a batch's edge.
func TestExportContactsAcrossBatches(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	email := e.ensureKey(ws, "email", TypeString)
	total := exportBatchSize*2 + 7
	e.exec(`WITH c AS (INSERT INTO contacts (workspace_id, created_at) SELECT ?::uuid, '2026-10-01T00:00:00Z'::timestamptz FROM generate_series(1, ?::int) RETURNING id)
		INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) SELECT id, ?::uuid, id::text || '@example.com' FROM c`, ws, total, email)

	records := exportCSV(t, e, owner, ws, "")
	if len(records) != total+1 {
		t.Fatalf("%d rows, want %d contacts and the header", len(records)-1, total)
	}
	column := slices.Index(records[0], "email")
	seen := map[string]bool{}
	for _, r := range records[1:] {
		if seen[r[column]] {
			t.Fatalf("%s exported twice", r[column])
		}
		seen[r[column]] = true
	}
	if len(seen) != total {
		t.Fatalf("%d distinct contacts, want %d", len(seen), total)
	}
}

// What the export writes imports again: the header names the keys, and the values parse as their types.
func TestExportedFileImportsAgain(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	ada := e.contact(ws, map[string]string{"email": "ada@example.com", "firstName": "Ada"})
	e.setNumber(ws, ada, "balance", -5)
	e.setDate(ws, ada, "renewal", time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC))
	records := exportCSV(t, e, owner, ws, "")
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.WriteAll(records)

	e.exec(`DELETE FROM contacts WHERE workspace_id = ?`, ws)
	res := importResult(t, e, owner, ws, b.String(), DuplicateSkip, nil)
	if len(res.Contacts) != 1 {
		t.Fatalf("imported %+v", res.Contacts)
	}
	if got := res.Contacts[0].Attributes; got["email"] != "ada@example.com" || got["firstName"] != "Ada" || got["balance"] != "-5" || got["renewal"] != "2027-01-02T00:00:00.000Z" {
		t.Fatalf("re-imported values %v", got)
	}
}
