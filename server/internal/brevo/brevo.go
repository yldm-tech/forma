// Package brevo syncs users to Brevo, Forma Cloud's CRM, as forma2's apps/web/modules/auth/lib/brevo.ts did: a user becomes a contact when they verify their address (or sign up through an identity provider), the contact's address follows an email change, and the contact is deleted with the account. Each sync is a River job enqueued in the transaction of the change that causes it, so it runs once that change commits and is retried when Brevo is unavailable. Nothing is enqueued or sent without BREVO_API_KEY.
package brevo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// apiURL is Brevo's API, which forma2 called directly.
const apiURL = "https://api.brevo.com/v3"

// requestTimeout bounds one call to Brevo; jobTimeout one attempt of a job.
const (
	requestTimeout = 10 * time.Second
	jobTimeout     = 30 * time.Second
)

// CreateContactArgs is the brevo.create-contact job: forma2's createBrevoCustomer.
type CreateContactArgs struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
}

func (CreateContactArgs) Kind() string { return "brevo.create-contact" }

// UpdateContactArgs is the brevo.update-contact job: forma2's updateBrevoCustomer, the contact found by the user id it was created with or, for a contact forma2 created under the user's old cuid, by the address it had before the change.
type UpdateContactArgs struct {
	UserID   uuid.UUID `json:"userId"`
	OldEmail string    `json:"oldEmail,omitempty"`
	Email    string    `json:"email"`
}

func (UpdateContactArgs) Kind() string { return "brevo.update-contact" }

// DeleteContactArgs is the brevo.delete-contact job: forma2's deleteBrevoCustomerByEmail.
type DeleteContactArgs struct {
	Email string `json:"email"`
}

func (DeleteContactArgs) Kind() string { return "brevo.delete-contact" }

// Contacts enqueues the syncs, each inside the transaction of the change it follows. Without BREVO_API_KEY it enqueues nothing.
type Contacts struct {
	configured bool
}

func NewContacts(cfg config.Config) *Contacts {
	return &Contacts{configured: cfg.Brevo.Configured()}
}

// Verified adds a user to Brevo once their address is verified, or when an identity provider created their account.
func (c *Contacts) Verified(ctx context.Context, tx *jobs.Tx, userID uuid.UUID, email string) error {
	return c.enqueue(ctx, tx, CreateContactArgs{UserID: userID, Email: email})
}

// EmailChanged moves the user's contact from oldEmail to their new address.
func (c *Contacts) EmailChanged(ctx context.Context, tx *jobs.Tx, userID uuid.UUID, oldEmail, email string) error {
	return c.enqueue(ctx, tx, UpdateContactArgs{UserID: userID, OldEmail: oldEmail, Email: email})
}

// Deleted removes the contact of a deleted account, found by its address.
func (c *Contacts) Deleted(ctx context.Context, tx *jobs.Tx, email string) error {
	return c.enqueue(ctx, tx, DeleteContactArgs{Email: email})
}

func (c *Contacts) enqueue(ctx context.Context, tx *jobs.Tx, args river.JobArgs) error {
	if !c.configured {
		return nil
	}
	return tx.Enqueue(ctx, args, nil)
}

// Client calls Brevo's contacts API with forma2's requests.
type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	listID  int64
}

func NewClient(cfg config.Config) *Client {
	return &Client{http: &http.Client{Timeout: requestTimeout}, baseURL: apiURL, apiKey: string(cfg.Brevo.APIKey), listID: cfg.Brevo.List()}
}

// createContactRequest is forma2's BrevoCreateContact as it sent it: the address, the user id as the external id, no update of an existing contact, and the configured list.
type createContactRequest struct {
	Email         string  `json:"email"`
	ExtID         string  `json:"ext_id"`
	UpdateEnabled bool    `json:"updateEnabled"`
	ListIDs       []int64 `json:"listIds,omitempty"`
}

// updateContactRequest is forma2's BrevoUpdateContact: the EMAIL attribute.
type updateContactRequest struct {
	Attributes map[string]string `json:"attributes"`
}

func (c *Client) createContact(ctx context.Context, userID uuid.UUID, email string) error {
	body := createContactRequest{Email: email, ExtID: userID.String()}
	if c.listID > 0 {
		body.ListIDs = []int64{c.listID}
	}
	return c.do(ctx, http.MethodPost, "/contacts", body, http.StatusCreated)
}

// updateContact finds the contact by the user id it was created with, as forma2 did. A contact forma2 created carries forma2's cuid as its ext_id, which the migrated user's id (a one-way uuidv5 of it) cannot match, so when Brevo knows no such ext_id the contact is found by the address it had before the change.
func (c *Client) updateContact(ctx context.Context, userID uuid.UUID, oldEmail, email string) error {
	body := updateContactRequest{Attributes: map[string]string{"EMAIL": email}}
	err := c.do(ctx, http.MethodPut, "/contacts/"+userID.String()+"?identifierType=ext_id", body, http.StatusNoContent)
	var answer *answerError
	if oldEmail == "" || !errors.As(err, &answer) || answer.status != http.StatusNotFound {
		return err
	}
	return c.do(ctx, http.MethodPut, "/contacts/"+url.QueryEscape(strings.ToLower(oldEmail))+"?identifierType=email_id", body, http.StatusNoContent)
}

func (c *Client) deleteContact(ctx context.Context, email string) error {
	// QueryEscape encodes an address as forma2's encodeURIComponent did ("@" as %40).
	return c.do(ctx, http.MethodDelete, "/contacts/"+url.QueryEscape(strings.ToLower(email))+"?identifierType=email_id", nil, http.StatusNoContent)
}

// answerError is Brevo answering a request with a status other than the one forma2 treated as success, with Brevo's error code.
type answerError struct {
	method string
	status int
	code   string
}

func (e *answerError) Error() string {
	return fmt.Sprintf("brevo %s contacts answered %d %s", e.method, e.status, e.code)
}

// do sends one request and checks for the status forma2 treated as success. Brevo being unavailable (a network error, 429 or 5xx) is an error the job retries; any other answer means the request itself was refused (an existing contact, an unknown one, a bad key), which no retry changes, so the job is cancelled with Brevo's error code.
func (c *Client) do(ctx context.Context, method, path string, body any, want int) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode brevo request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build brevo request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("api-key", c.apiKey)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("brevo %s contacts: %w", method, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == want {
		return nil
	}
	var answer struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&answer)
	err = &answerError{method: method, status: res.StatusCode, code: answer.Code}
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= http.StatusInternalServerError {
		return err
	}
	return river.JobCancel(err)
}

type createWorker struct {
	river.WorkerDefaults[CreateContactArgs]
	client *Client
}

func (w *createWorker) Timeout(*river.Job[CreateContactArgs]) time.Duration { return jobTimeout }

func (w *createWorker) Work(ctx context.Context, job *river.Job[CreateContactArgs]) error {
	if w.client.apiKey == "" {
		return nil
	}
	return w.client.createContact(ctx, job.Args.UserID, job.Args.Email)
}

type updateWorker struct {
	river.WorkerDefaults[UpdateContactArgs]
	client *Client
}

func (w *updateWorker) Timeout(*river.Job[UpdateContactArgs]) time.Duration { return jobTimeout }

func (w *updateWorker) Work(ctx context.Context, job *river.Job[UpdateContactArgs]) error {
	if w.client.apiKey == "" {
		return nil
	}
	return w.client.updateContact(ctx, job.Args.UserID, job.Args.OldEmail, job.Args.Email)
}

type deleteWorker struct {
	river.WorkerDefaults[DeleteContactArgs]
	client *Client
}

func (w *deleteWorker) Timeout(*river.Job[DeleteContactArgs]) time.Duration { return jobTimeout }

func (w *deleteWorker) Work(ctx context.Context, job *river.Job[DeleteContactArgs]) error {
	if w.client.apiKey == "" {
		return nil
	}
	return w.client.deleteContact(ctx, job.Args.Email)
}

// RegisterJobs adds the contact syncs to `forma worker`. A job left in the queue after BREVO_API_KEY was removed completes without calling Brevo, as forma2's functions returned early without the key.
func RegisterJobs(reg *jobs.Registry, client *Client) {
	jobs.Add(reg, &createWorker{client: client})
	jobs.Add(reg, &updateWorker{client: client})
	jobs.Add(reg, &deleteWorker{client: client})
}
