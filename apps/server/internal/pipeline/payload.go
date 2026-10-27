package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/apps/server/internal/tag"
)

// webhookTag is a response's tag with toISOString timestamps.
type webhookTag struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   string    `json:"createdAt"`
	UpdatedAt   string    `json:"updatedAt"`
	Name        string    `json:"name"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
}

type webhookSurvey struct {
	Title     string `json:"title"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// webhookData is the "data" of a webhook delivery, forma2's {...response, data, survey}: TResponse's fields in their zod order, timestamps as toISOString wrote them, then the survey. Fields, which forma2 did not send, follows the answers it lists.
type webhookData struct {
	ID                uuid.UUID         `json:"id"`
	CreatedAt         string            `json:"createdAt"`
	UpdatedAt         string            `json:"updatedAt"`
	SurveyID          uuid.UUID         `json:"surveyId"`
	DisplayID         *uuid.UUID        `json:"displayId"`
	Contact           *response.Contact `json:"contact"`
	ContactAttributes json.RawMessage   `json:"contactAttributes"`
	Finished          bool              `json:"finished"`
	EndingID          *string           `json:"endingId"`
	Data              json.RawMessage   `json:"data"`
	Fields            []webhookField    `json:"fields"`
	Variables         json.RawMessage   `json:"variables"`
	TTC               json.RawMessage   `json:"ttc,omitempty"`
	Tags              []webhookTag      `json:"tags"`
	Meta              json.RawMessage   `json:"meta"`
	SingleUseID       *string           `json:"singleUseId"`
	Language          *string           `json:"language"`
	Survey            webhookSurvey     `json:"survey"`
}

// Types of the fields that are not elements.
const (
	fieldTypeVariable    = "variable"
	fieldTypeHiddenField = "hiddenField"
)

// webhookField is one entry of a delivery's fields: what the answer under ID (a key of data, or of variables for a variable) answers, so an automation built in Zapier, Make or n8n can map answers by their headlines rather than by generated ids.
type webhookField struct {
	ID string `json:"id"`
	// Type is the element's type, or fieldTypeVariable or fieldTypeHiddenField.
	Type string `json:"type"`
	// Headline is the element's headline in the survey's default language as text, every recall token shown as its fallback so that it reads the same for every response; a variable's or hidden field's name.
	Headline string `json:"headline"`
	// Value is the stored answer as data or variables holds it, null when the response has none.
	Value json.RawMessage `json:"value"`
}

// webhookFields lists the survey's elements in document order, then its variables and its hidden fields in their stored order (the export's column order), each with the response's value. data is the response's data with its file URLs already made absolute.
func webhookFields(sv *Survey, data, variables json.RawMessage) ([]webhookField, error) {
	answers, err := members(data)
	if err != nil {
		return nil, fmt.Errorf("decode response data: %w", err)
	}
	values, err := members(variables)
	if err != nil {
		return nil, fmt.Errorf("decode response variables: %w", err)
	}
	elements := sv.Elements()
	fields := make([]webhookField, 0, len(elements)+len(sv.Fields))
	for _, el := range elements {
		headline := responsemail.ParseRecall(surveydoc.TextContent(el.Headline.Default()), nil, nil, mailer.DefaultLocale, false)
		fields = append(fields, webhookField{ID: el.ID, Type: el.Type, Headline: headline, Value: orNull(answers[el.ID])})
	}
	for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceComputed) {
		fields = append(fields, webhookField{ID: f.StorageKey, Type: fieldTypeVariable, Headline: f.Name, Value: orNull(values[f.StorageKey])})
	}
	for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceIngested) {
		fields = append(fields, webhookField{ID: f.StorageKey, Type: fieldTypeHiddenField, Headline: f.Name, Value: orNull(answers[f.StorageKey])})
	}
	return fields, nil
}

// members is a stored JSON object's members, each as its JSON; none for an absent or null object.
func members(raw json.RawMessage) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func orNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

func webhookTags(tags []tag.Tag) []webhookTag {
	out := make([]webhookTag, len(tags))
	for i, t := range tags {
		out[i] = webhookTag{ID: t.ID, CreatedAt: jsvalue.ISOString(t.CreatedAt), UpdatedAt: jsvalue.ISOString(t.UpdatedAt), Name: t.Name, WorkspaceID: t.WorkspaceID}
	}
	return out
}

// webhookPayloadData is the data every webhook of the event receives, with the answers' stored file URLs made absolute.
func webhookPayloadData(cfg config.Config, r response.Response, sv *Survey) (json.RawMessage, error) {
	data, err := resolveStorageURLs(cfg, orNull(r.Data))
	if err != nil {
		return nil, fmt.Errorf("resolve storage URLs in response %s: %w", r.ID, err)
	}
	fields, err := webhookFields(sv, data, r.Variables)
	if err != nil {
		return nil, fmt.Errorf("list the fields of response %s: %w", r.ID, err)
	}
	d := webhookData{
		ID: r.ID, CreatedAt: jsvalue.ISOString(r.CreatedAt), UpdatedAt: jsvalue.ISOString(r.UpdatedAt), SurveyID: r.SurveyID,
		DisplayID: r.DisplayID, Contact: r.Contact, ContactAttributes: orNull(r.ContactAttributes), Finished: r.Finished,
		EndingID: r.EndingID, Data: data, Fields: fields, Variables: orNull(r.Variables), TTC: r.TTC, Tags: webhookTags(r.Tags),
		Meta: orNull(r.Meta), SingleUseID: r.SingleUseID, Language: r.Language,
		Survey: webhookSurvey{Title: sv.Name, Type: sv.Type, Status: sv.Status, CreatedAt: jsvalue.ISOString(sv.CreatedAt), UpdatedAt: jsvalue.ISOString(sv.UpdatedAt)},
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(d); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// storageURL matches a stored file's relative URL, forma2's STORAGE_URL_PATTERN.
var storageURL = regexp.MustCompile(`^/storage/[^/]+/(public|private)/.+`)

// resolveStorageURLs is forma2's resolveStorageUrlsInObject on the stored JSON: every string that is a relative storage URL made absolute, a public file's against the public domain and a private one's against the app, with every other value and the members' order left as stored.
func resolveStorageURLs(cfg config.Config, raw json.RawMessage) (json.RawMessage, error) {
	app := strings.TrimRight(cfg.WebAppURL.String(), "/")
	public := app
	if cfg.PublicURL != nil {
		public = strings.TrimRight(cfg.PublicURL.String(), "/")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var buf bytes.Buffer
	err := rewriteStrings(dec, &buf, func(s string) string {
		m := storageURL.FindStringSubmatch(s)
		if m == nil {
			return s
		}
		if m[1] == "private" {
			return app + s
		}
		return public + s
	})
	return buf.Bytes(), err
}

// rewriteStrings copies one JSON value from dec to buf, passing every string value (not member names) through fn.
func rewriteStrings(dec *json.Decoder, buf *bytes.Buffer, fn func(string) string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch t := tok.(type) {
	case json.Delim:
		opening, closing := byte('['), byte(']')
		if t == '{' {
			opening, closing = '{', '}'
		}
		buf.WriteByte(opening)
		for first := true; dec.More(); first = false {
			if !first {
				buf.WriteByte(',')
			}
			if t == '{' {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				writeString(buf, key.(string))
				buf.WriteByte(':')
			}
			if err := rewriteStrings(dec, buf, fn); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
		buf.WriteByte(closing)
	case string:
		writeString(buf, fn(t))
	case json.Number:
		buf.WriteString(t.String())
	case bool:
		fmt.Fprint(buf, t)
	case nil:
		buf.WriteString("null")
	}
	return nil
}

func writeString(buf *bytes.Buffer, s string) {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	buf.Truncate(buf.Len() - 1)
}
