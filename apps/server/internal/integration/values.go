package integration

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/analysis"
	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// storageURL matches a stored file's relative URL, forma2's STORAGE_URL_PATTERN.
var storageURL = regexp.MustCompile(`^/storage/[^/]+/(public|private)/.+`)

// fileURLs resolves relative storage URLs the way forma2's resolveStorageUrlAuto did: a public file against the public domain, a private one against the app.
type fileURLs struct {
	app    string
	public string
}

func newFileURLs(cfg config.Config) fileURLs {
	u := fileURLs{}
	if cfg.WebAppURL != nil {
		u.app = trimSlash(cfg.WebAppURL.String())
	}
	u.public = u.app
	if cfg.PublicURL != nil {
		u.public = trimSlash(cfg.PublicURL.String())
	}
	return u
}

func (u fileURLs) resolve(s string) string {
	m := storageURL.FindStringSubmatch(s)
	switch {
	case m == nil:
		return s
	case m[1] == "private":
		return u.app + s
	}
	return u.public + s
}

// row is one response as the integrations write it: its answers and their column labels, side by side.
type row struct {
	values []string
	labels []string
}

func (r *row) add(value, label string) {
	r.values = append(r.values, value)
	r.labels = append(r.labels, label)
}

// selection is forma2's TIntegrationFieldSelection: which columns a link writes, every flag a plain boolean.
type selection struct {
	elementIDs               []string
	includeCreatedAt         bool
	includeHiddenFields      bool
	includeMetadata          bool
	includeVariables         bool
	includeContactAttributes bool
}

func flag(b *bool) bool { return b != nil && *b }

func selectionOf(l LinkBase, e Elements) selection {
	return selection{
		elementIDs: e.ElementIDs, includeCreatedAt: flag(l.IncludeCreatedAt), includeHiddenFields: flag(l.IncludeHiddenFields),
		includeMetadata: flag(l.IncludeMetadata), includeVariables: flag(l.IncludeVariables), includeContactAttributes: flag(l.IncludeContactAttributes),
	}
}

// submission is a finished response as the writers read it: its decoded answers, its stored JSON in the order PostgreSQL returns it, and the survey it answers.
type submission struct {
	answers *responsemail.Answers
	survey  *responsemail.Survey
	// data keeps each answer's stored JSON, for the Notion values that are written as JSON.
	data map[string]json.RawMessage
	meta map[string]any
	// contactAttributes are the response's contact attributes in their stored order; nil when it has none.
	contactAttributes []jsvalue.Member
	files             fileURLs
	timeZone          string
}

func newSubmission(answers *responsemail.Answers, sv *responsemail.Survey, files fileURLs, timeZone string) (*submission, error) {
	r := &submission{answers: answers, survey: sv, data: map[string]json.RawMessage{}, meta: map[string]any{}, files: files, timeZone: timeZone}
	res := answers.Response
	if len(res.Data) > 0 && string(res.Data) != "null" {
		if err := json.Unmarshal(res.Data, &r.data); err != nil {
			return nil, fmt.Errorf("decode response data: %w", err)
		}
	}
	if len(res.Meta) > 0 && string(res.Meta) != "null" {
		if err := json.Unmarshal(res.Meta, &r.meta); err != nil {
			return nil, fmt.Errorf("decode response meta: %w", err)
		}
	}
	if len(res.ContactAttributes) > 0 && string(res.ContactAttributes) != "null" {
		members, err := jsvalue.DecodeObject(res.ContactAttributes)
		if err != nil {
			return nil, fmt.Errorf("decode response contact attributes: %w", err)
		}
		if members == nil {
			members = []jsvalue.Member{}
		}
		r.contactAttributes = members
	}
	return r, nil
}

// metadata is forma2's convertMetaObjectToString: the response's metadata that is set, one "Name: value" line each.
func (r *submission) metadata() string {
	var lines []string
	add := func(label string, v any) {
		if truthy(v) {
			lines = append(lines, label+": "+jsvalue.String(v))
		}
	}
	add("Source", r.meta["source"])
	add("URL", r.meta["url"])
	ua, _ := r.meta["userAgent"].(map[string]any)
	add("Browser", ua["browser"])
	add("OS", ua["os"])
	add("Device", ua["device"])
	add("Country", r.meta["country"])
	add("Action", r.meta["action"])
	add("IP Address", r.meta["ipAddress"])
	return strings.Join(lines, "\n")
}

// truthy is JavaScript's truthiness of a JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0 && !math.IsNaN(x)
	}
	return v != jsvalue.Undefined
}

// build is forma2's processDataForIntegration: the selected elements' answers (and, with hidden fields, the survey's ingested fields) in order, then the metadata, the computed variables the response captured, the creation time in the organization's zone and the contact attributes, as the link asks. Slack's labels expand recall against the response; every other integration's labels are column headers, the same for every response, so recall shows its fallbacks.
func (r *submission) build(sel selection, slack bool) (*row, error) {
	ingested := ingestedKeys(r.survey)
	ids := sel.elementIDs
	if sel.includeHiddenFields {
		ids = append(slices.Clone(ids), ingested...)
	}
	values, variables := map[string]any{}, map[string]any{}
	if slack {
		var err error
		if values, err = r.answers.ServerValues(r.survey); err != nil {
			return nil, err
		}
		variables = r.answers.Variables
	} else {
		for k := range r.answers.Data {
			values[k] = ""
		}
	}
	elements := r.survey.Elements()
	out := &row{}
	for _, id := range ids {
		if slices.Contains(ingested, id) {
			out.add(r.answers.Text(id), id)
			continue
		}
		i := slices.IndexFunc(elements, func(e surveydoc.Element) bool { return e.ID == id })
		if i < 0 {
			continue
		}
		el := elements[i]
		label := responsemail.ParseRecall(surveydoc.TextContent(el.Headline.Default()), values, variables, mailer.DefaultLocale, false)
		out.add(r.answer(el), label)
	}
	if sel.includeMetadata {
		out.add(r.metadata(), "Metadata")
	}
	if sel.includeVariables {
		for _, f := range embeddeddata.OfSource(r.survey.Fields, embeddeddata.SourceComputed) {
			if v, ok := r.answers.Variables[f.StorageKey]; ok {
				out.add(jsvalue.String(v), f.Name)
			}
		}
	}
	if sel.includeCreatedAt {
		out.add(analysis.FormatTimestamp(r.answers.Response.CreatedAt, r.timeZone), "Created At")
	}
	if sel.includeContactAttributes && r.contactAttributes != nil {
		for _, m := range r.contactAttributes {
			out.add(jsvalue.String(m.Value), "person."+m.Key)
		}
	}
	return out, nil
}

// ingestedKeys is forma2's getIngestedStorageKeys.
func ingestedKeys(sv *responsemail.Survey) []string {
	var keys []string
	for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceIngested) {
		keys = append(keys, f.StorageKey)
	}
	return keys
}

// answer is forma2's processElementResponse: a picture selection as the chosen pictures' URLs, one a line; a file upload list as its URLs joined by "; "; anything else as processResponseData's text. Relative storage URLs are made absolute.
func (r *submission) answer(el surveydoc.Element) string {
	v, present := r.answers.Data[el.ID]
	if !present {
		return ""
	}
	switch el.Type {
	case surveydoc.ElementPictureSelection:
		var urls []string
		for _, c := range el.Choices {
			if chosen(v, c.ID) {
				urls = append(urls, r.files.resolve(c.ImageURL))
			}
		}
		return strings.Join(urls, "\n")
	case surveydoc.ElementFileUpload:
		if list, ok := v.([]any); ok {
			parts := make([]string, len(list))
			for i, u := range list {
				switch x := u.(type) {
				case string:
					parts[i] = r.files.resolve(x)
				case nil:
				default:
					parts[i] = jsvalue.String(x)
				}
			}
			return strings.Join(parts, "; ")
		}
	}
	return r.answers.Text(el.ID)
}

// chosen is `selectedChoiceIds.includes(id)` on whatever the answer holds: membership for a list, a substring for a string.
func chosen(answer any, id string) bool {
	switch x := answer.(type) {
	case []any:
		return slices.ContainsFunc(x, func(v any) bool { return v == id })
	case string:
		return strings.Contains(x, id)
	}
	return false
}
