package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// airtableScope is what the Airtable integration asks for, fixed by the Airtable app registration.
const airtableScope = "data.records:read data.records:write schema.bases:read schema.bases:write user.email:read"

// airtableAuthorizeURL is forma2's Airtable authorize URL: PKCE with S256, no client secret.
func (p *providers) airtableAuthorizeURL(state, verifier string) string {
	q := []string{
		"client_id=" + url.QueryEscape(p.cfg.AirtableClientID),
		"redirect_uri=" + url.QueryEscape(p.airtableRedirectURI()),
		"state=" + url.QueryEscape(state),
		"scope=" + url.QueryEscape(airtableScope),
		"response_type=code",
		"code_challenge_method=S256",
		"code_challenge=" + url.QueryEscape(oauth2.S256ChallengeFromVerifier(verifier)),
	}
	return p.airtable + "/oauth2/v1/authorize?" + strings.Join(q, "&")
}

// airtableToken is forma2's fetchAirtableAuthToken: the token endpoint with form, its answer read as {access_token, refresh_token, expires_in}, kept with expires_in turned into an ISO expiry_date. An answer without them is an error carrying the shape's complaint.
func (p *providers) airtableToken(ctx context.Context, form url.Values, now time.Time) (*AirtableKey, error) {
	res, err := p.do(ctx, http.MethodPost, p.airtable+"/oauth2/v1/token", http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, []byte(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("airtable token request: %w", err)
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return nil, fmt.Errorf("airtable token answer %d is not JSON", res.Status)
	}
	access, ok1 := body["access_token"].(string)
	refresh, ok2 := body["refresh_token"].(string)
	// zod's coerce.number(): an absent expires_in is NaN and refused, anything else is Number() of it.
	expiresIn := math.NaN()
	if raw, present := body["expires_in"]; present {
		expiresIn = jsvalue.Number(raw)
	}
	if !ok1 || !ok2 || math.IsNaN(expiresIn) {
		return nil, fmt.Errorf("airtable token answer %d has no access_token, refresh_token and expires_in", res.Status)
	}
	expiry := now.Add(time.Duration(expiresIn) * time.Second)
	return &AirtableKey{AccessToken: access, RefreshToken: refresh, ExpiryDate: jsvalue.ISOString(expiry)}, nil
}

// airtableEmail is the connected account's address, from whoami.
func (p *providers) airtableEmail(ctx context.Context, accessToken string) (string, error) {
	res, err := p.do(ctx, http.MethodGet, p.airtableAPI+"/v0/meta/whoami", http.Header{"Authorization": {bearer(accessToken)}}, nil)
	if err != nil {
		return "", fmt.Errorf("airtable whoami: %w", err)
	}
	var body struct {
		Email *string `json:"email"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil || body.Email == nil {
		return "", fmt.Errorf("airtable whoami answer %d has no email", res.Status)
	}
	return *body.Email, nil
}

// Item is one base, table or channel the pages offer.
type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// airtableBases is forma2's getBases.
func (p *providers) airtableBases(ctx context.Context, accessToken string) ([]Item, error) {
	res, err := p.do(ctx, http.MethodGet, p.airtableAPI+"/v0/meta/bases", http.Header{"Authorization": {bearer(accessToken)}}, nil)
	if err != nil {
		return nil, fmt.Errorf("airtable bases: %w", err)
	}
	if !res.ok() {
		return nil, fmt.Errorf("Airtable API error fetching bases: %d %s %s", res.Status, res.StatusText, res.Body) //nolint:staticcheck // forma2's message
	}
	var body struct {
		Bases []Item `json:"bases"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil || body.Bases == nil {
		return nil, fmt.Errorf("airtable bases answer is not a base list")
	}
	return body.Bases, nil
}

// airtableTable is a table of a base with its fields' names.
type airtableTable struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Fields []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"fields"`
}

// airtableTables is forma2's tableFetcher.
func (p *providers) airtableTables(ctx context.Context, accessToken, baseID string) ([]airtableTable, error) {
	res, err := p.do(ctx, http.MethodGet, p.airtableAPI+"/v0/meta/bases/"+url.PathEscape(baseID)+"/tables", http.Header{"Authorization": {bearer(accessToken)}}, nil)
	if err != nil {
		return nil, fmt.Errorf("airtable tables: %w", err)
	}
	if !res.ok() {
		return nil, fmt.Errorf("Airtable API error fetching tables: %d %s %s", res.Status, res.StatusText, res.Body) //nolint:staticcheck // forma2's message
	}
	var body struct {
		Tables []airtableTable `json:"tables"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil || body.Tables == nil {
		return nil, fmt.Errorf("airtable tables answer is not a table list")
	}
	return body.Tables, nil
}

// existingFields is forma2's getExistingFields: the names of the table's fields.
func (p *providers) existingFields(ctx context.Context, accessToken, baseID, tableID string) (map[string]bool, error) {
	tables, err := p.airtableTables(ctx, accessToken, baseID)
	if err != nil {
		return nil, err
	}
	for _, t := range tables {
		if t.ID == tableID {
			names := map[string]bool{}
			for _, f := range t.Fields {
				names[f.Name] = true
			}
			return names, nil
		}
	}
	return nil, fmt.Errorf("Table with ID %s not found", tableID) //nolint:staticcheck // forma2's message
}

// airtableTiming is how the Airtable writer paces itself: between field creations (Airtable allows five requests a second per base), and while it waits for created fields to appear.
type airtableTiming struct {
	fieldCreateGap   time.Duration
	fieldWaitRetries int
	fieldWaitGap     time.Duration
}

var defaultAirtableTiming = airtableTiming{fieldCreateGap: 250 * time.Millisecond, fieldWaitRetries: 5, fieldWaitGap: 2 * time.Second}

// sleep waits d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// writeAirtable is forma2's airtable writeData: one record of {label: value} (a repeated label keeps its last value), each value truncated to Airtable's limit, after creating the fields the table does not have yet as single line text, one at a time, and waiting for them to appear.
func (p *providers) writeAirtable(ctx context.Context, timing airtableTiming, accessToken string, link AirtableLink, values, labels []string) error {
	if len(values) != len(labels) {
		return fmt.Errorf("Array length mismatch: responses (%d) and elements (%d) must be equal", len(values), len(labels)) //nolint:staticcheck // forma2's message
	}
	fields := map[string]string{}
	for i, l := range labels {
		fields[l] = truncate(values[i], airtableMessageLimit)
	}
	existing, err := p.existingFields(ctx, accessToken, link.BaseID, link.TableID)
	if err != nil {
		return err
	}
	// forma2 created a repeated missing label twice, and Airtable refused the second; it is created once here.
	var missing []string
	seen := map[string]bool{}
	for _, l := range labels {
		if !existing[l] && !seen[l] {
			missing = append(missing, l)
			seen[l] = true
		}
	}
	fieldsURL := p.airtableAPI + "/v0/meta/bases/" + url.PathEscape(link.BaseID) + "/tables/" + url.PathEscape(link.TableID) + "/fields"
	header := http.Header{"Authorization": {bearer(accessToken)}, "Content-Type": {"application/json"}}
	for i, name := range missing {
		body, err := encodeJS(map[string]string{"name": name, "type": "singleLineText"})
		if err != nil {
			return err
		}
		res, err := p.do(ctx, http.MethodPost, fieldsURL, header, body)
		if err != nil {
			return fmt.Errorf("airtable create field: %w", err)
		}
		var answer map[string]any
		if json.Unmarshal(res.Body, &answer) == nil && answer["error"] != nil {
			return fmt.Errorf("Failed to create field %q: %s", name, res.Body) //nolint:staticcheck // forma2's message
		}
		if i < len(missing)-1 {
			if err := sleep(ctx, timing.fieldCreateGap); err != nil {
				return err
			}
		}
	}
	if len(missing) > 0 {
		if err := p.waitForFields(ctx, timing, accessToken, link, missing); err != nil {
			return err
		}
	}
	body, err := encodeJS(map[string]any{"fields": fields, "typecast": true})
	if err != nil {
		return err
	}
	res, err := p.do(ctx, http.MethodPost, p.airtableAPI+"/v0/"+url.PathEscape(link.BaseID)+"/"+url.PathEscape(link.TableID), header, body)
	if err != nil {
		return fmt.Errorf("airtable create record: %w", err)
	}
	if !res.ok() {
		return fmt.Errorf("Airtable API error creating record: %d %s %s", res.Status, res.StatusText, res.Body) //nolint:staticcheck // forma2's message
	}
	return nil
}

// waitForFields is forma2's waitForFieldsToExist.
func (p *providers) waitForFields(ctx context.Context, timing airtableTiming, accessToken string, link AirtableLink, names []string) error {
	var missing []string
	var existing map[string]bool
	for attempt := 1; attempt <= timing.fieldWaitRetries; attempt++ {
		var err error
		if existing, err = p.existingFields(ctx, accessToken, link.BaseID, link.TableID); err != nil {
			return err
		}
		missing = missing[:0]
		for _, n := range names {
			if !existing[n] {
				missing = append(missing, n)
			}
		}
		if len(missing) == 0 {
			return nil
		}
		if attempt < timing.fieldWaitRetries {
			if err := sleep(ctx, timing.fieldWaitGap); err != nil {
				return err
			}
		}
	}
	available := make([]string, 0, len(existing))
	for n := range existing {
		available = append(available, n)
	}
	return errors.New("Timed out waiting for " + strconv.Itoa(len(missing)) + " field(s) [" + strings.Join(missing, ", ") + "] to become available. Available fields: [" + strings.Join(available, ", ") + "]")
}
