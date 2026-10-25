package brevo

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
)

// call is one request the Brevo stub received.
type call struct {
	method, uri, apiKey, contentType, accept, body string
}

// stub is Brevo's API answering every request with status, recording what it received.
func stub(t *testing.T, status int, answer string) (*Client, func() []call) {
	t.Helper()
	var mu sync.Mutex
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, call{r.Method, r.URL.RequestURI(), r.Header.Get("api-key"), r.Header.Get("Content-Type"), r.Header.Get("Accept"), string(body)})
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(config.Config{Brevo: config.BrevoConfig{APIKey: "xkeysib-test", ListID: "7"}})
	c.baseURL = srv.URL + "/v3"
	return c, func() []call {
		mu.Lock()
		defer mu.Unlock()
		return append([]call(nil), calls...)
	}
}

var userID = uuid.MustParse("0190a000-0000-7000-8000-0000000000aa")

// The three jobs send forma2's requests: the bodies JSON.stringify produced, at the same paths, with the API key header.
func TestContactRequests(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status int
		work   func(c *Client) error
		want   call
	}{
		"create": {http.StatusCreated, func(c *Client) error {
			return (&createWorker{client: c}).Work(context.Background(), &river.Job[CreateContactArgs]{Args: CreateContactArgs{UserID: userID, Email: "ada@example.com"}})
		}, call{"POST", "/v3/contacts", "xkeysib-test", "application/json", "application/json", `{"email":"ada@example.com","ext_id":"0190a000-0000-7000-8000-0000000000aa","updateEnabled":false,"listIds":[7]}`}},
		"update": {http.StatusNoContent, func(c *Client) error {
			return (&updateWorker{client: c}).Work(context.Background(), &river.Job[UpdateContactArgs]{Args: UpdateContactArgs{UserID: userID, OldEmail: "Ada@Example.com", Email: "ada@new.example"}})
		}, call{"PUT", "/v3/contacts/0190a000-0000-7000-8000-0000000000aa?identifierType=ext_id", "xkeysib-test", "application/json", "application/json", `{"attributes":{"EMAIL":"ada@new.example"}}`}},
		"delete": {http.StatusNoContent, func(c *Client) error {
			return (&deleteWorker{client: c}).Work(context.Background(), &river.Job[DeleteContactArgs]{Args: DeleteContactArgs{Email: "Ada+forma@Example.com"}})
		}, call{"DELETE", "/v3/contacts/ada%2Bforma%40example.com?identifierType=email_id", "xkeysib-test", "", "application/json", ""}},
	} {
		c, calls := stub(t, tc.status, "")
		if err := tc.work(c); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if got := calls(); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s sent %+v, want %+v", name, got, tc.want)
		}
	}
}

// Without BREVO_LIST_ID a new contact joins no list, and forma2 left listIds out of the body.
func TestCreateContactWithoutList(t *testing.T) {
	t.Parallel()
	c, calls := stub(t, http.StatusCreated, "")
	c.listID = 0
	if err := c.createContact(context.Background(), userID, "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if got := calls()[0].body; got != `{"email":"ada@example.com","ext_id":"0190a000-0000-7000-8000-0000000000aa","updateEnabled":false}` {
		t.Errorf("body %s", got)
	}
}

// A contact created under another ext_id is not found by the user's id, so after the ext_id lookup finds nothing the contact is found by the address it had before the change.
func TestUpdateContactCreatedElsewhere(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		byEmail int
		cancel  bool
	}{
		"moved":             {http.StatusNoContent, false},
		"unknown address":   {http.StatusNotFound, true},
		"brevo unavailable": {http.StatusBadGateway, false},
	} {
		var mu sync.Mutex
		var uris []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			uris = append(uris, r.Method+" "+r.URL.RequestURI()+" "+string(body))
			mu.Unlock()
			if strings.Contains(r.URL.RawQuery, "identifierType=ext_id") {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"code":"document_not_found","message":"Contact does not exist"}`)
				return
			}
			w.WriteHeader(tc.byEmail)
		}))
		c := NewClient(config.Config{Brevo: config.BrevoConfig{APIKey: "xkeysib-test"}})
		c.baseURL = srv.URL + "/v3"
		err := (&updateWorker{client: c}).Work(context.Background(), &river.Job[UpdateContactArgs]{Args: UpdateContactArgs{UserID: userID, OldEmail: "Ada+forma@Example.com", Email: "ada@new.example"}})
		srv.Close()
		var cancel *river.JobCancelError
		if (tc.byEmail == http.StatusNoContent) != (err == nil) || errors.As(err, &cancel) != tc.cancel {
			t.Errorf("%s: %v, want cancelled %v", name, err, tc.cancel)
		}
		want := []string{
			`PUT /v3/contacts/0190a000-0000-7000-8000-0000000000aa?identifierType=ext_id {"attributes":{"EMAIL":"ada@new.example"}}`,
			`PUT /v3/contacts/ada%2Bforma%40example.com?identifierType=email_id {"attributes":{"EMAIL":"ada@new.example"}}`,
		}
		if len(uris) != 2 || uris[0] != want[0] || uris[1] != want[1] {
			t.Errorf("%s sent %q, want %q", name, uris, want)
		}
	}
}

// A job queued before the old address was recorded with it only looks the contact up by ext_id.
func TestUpdateContactWithoutOldEmail(t *testing.T) {
	t.Parallel()
	c, calls := stub(t, http.StatusNotFound, `{"code":"document_not_found"}`)
	err := c.updateContact(context.Background(), userID, "", "ada@new.example")
	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) {
		t.Errorf("%v, want cancelled", err)
	}
	if n := len(calls()); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

// Brevo being unavailable is retried; a refused request (an existing contact, an unknown one) is not, and the job is cancelled with Brevo's error code.
func TestContactFailures(t *testing.T) {
	t.Parallel()
	for status, cancelled := range map[int]bool{http.StatusBadRequest: true, http.StatusNotFound: true, http.StatusUnauthorized: true, http.StatusTooManyRequests: false, http.StatusBadGateway: false} {
		c, _ := stub(t, status, `{"code":"duplicate_parameter","message":"Contact already exist"}`)
		err := c.createContact(context.Background(), userID, "ada@example.com")
		var cancel *river.JobCancelError
		if err == nil || errors.As(err, &cancel) != cancelled {
			t.Errorf("%d: %v, want cancelled %v", status, err, cancelled)
		}
	}
}

// A job still queued after the key was removed completes without calling Brevo.
func TestWorkersWithoutKey(t *testing.T) {
	t.Parallel()
	c, calls := stub(t, http.StatusCreated, "")
	c.apiKey = ""
	if err := (&createWorker{client: c}).Work(context.Background(), &river.Job[CreateContactArgs]{Args: CreateContactArgs{UserID: userID, Email: "ada@example.com"}}); err != nil {
		t.Fatal(err)
	}
	if n := len(calls()); n != 0 {
		t.Errorf("%d requests without a key", n)
	}
}

// The syncs are enqueued in the transaction of the change, and only when BREVO_API_KEY is set.
func TestContactsEnqueueOnlyWhenConfigured(t *testing.T) {
	t.Parallel()
	db := pgtest.GORM(t)
	enq, err := jobs.NewEnqueuer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	enqueueAll := func(c *Contacts) {
		t.Helper()
		err := enq.Transaction(ctx, db, func(tx *jobs.Tx) error {
			return errors.Join(c.Verified(ctx, tx, userID, "ada@example.com"), c.EmailChanged(ctx, tx, userID, "ada@example.com", "ada@new.example"), c.Deleted(ctx, tx, "ada@new.example"))
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	count := func() map[string]int {
		t.Helper()
		var rows []struct {
			Kind  string
			Email string
		}
		if err := db.Raw(`SELECT kind, args->>'email' AS email FROM river_job WHERE kind LIKE 'brevo.%'`).Scan(&rows).Error; err != nil {
			t.Fatal(err)
		}
		out := map[string]int{}
		for _, r := range rows {
			out[r.Kind+" "+r.Email]++
		}
		return out
	}

	enqueueAll(NewContacts(config.Config{}))
	if got := count(); len(got) != 0 {
		t.Fatalf("enqueued without a key: %v", got)
	}
	enqueueAll(NewContacts(config.Config{Brevo: config.BrevoConfig{APIKey: "xkeysib-test"}}))
	got := count()
	if len(got) != 3 || got["brevo.create-contact ada@example.com"] != 1 || got["brevo.update-contact ada@new.example"] != 1 || got["brevo.delete-contact ada@new.example"] != 1 {
		t.Fatalf("enqueued %v", got)
	}
}
