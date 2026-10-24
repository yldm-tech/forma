package integration

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// personPrefix marks a mapping of a contact attribute, forma2's NOTION_PERSON_ATTRIBUTE_PREFIX.
const personPrefix = "person."

// notionInput is a value forma2 handed its Notion getValue: decoded JSON (jsvalue.Undefined when absent) with the JSON it was stored as, a string forma2 built, or the response's creation time, which forma2 passed as a Date.
type notionInput struct {
	v    any
	raw  json.RawMessage
	date *time.Time
}

// json is the input as JSON.stringify wrote it: a Date as its ISO string, a stored value as stored.
func (in notionInput) json() any {
	switch {
	case in.date != nil:
		return jsvalue.ISOString(*in.date)
	case in.raw != nil:
		return compact(in.raw)
	}
	return in.v
}

// compact is stored JSON as JSON.stringify writes it, without the whitespace.
func compact(raw json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}

// falsy is JavaScript's falsiness of the input; a Date is truthy.
func (in notionInput) falsy() bool {
	return in.date == nil && !truthy(in.v)
}

// notionProperties is forma2's buildNotionPayloadProperties: each mapping entry's value under its column's name, as {<column type>: value}, null when getValue gave a falsy value (so a false checkbox and a 0 number are written as null, as forma2 wrote them). A picture selection's answer is the chosen pictures' URLs.
func (r *submission) notionProperties(mapping []NotionMapping) map[string]any {
	pictureElements := map[string]surveydoc.Element{}
	for _, el := range r.survey.Elements() {
		if el.Type == surveydoc.ElementPictureSelection {
			pictureElements[el.ID] = el
		}
	}
	pictureMapped := map[string]bool{}
	for _, m := range mapping {
		if m.Element.Type == surveydoc.ElementPictureSelection {
			pictureMapped[m.Element.ID] = true
		}
	}
	properties := map[string]any{}
	for _, m := range mapping {
		var in notionInput
		switch id := m.Element.ID; {
		case id == "metadata":
			in = notionInput{v: r.metadata()}
		case id == "createdAt":
			at := r.answers.Response.CreatedAt
			in = notionInput{date: &at}
		case strings.HasPrefix(id, personPrefix):
			in = notionInput{v: jsvalue.Undefined}
			for _, a := range r.contactAttributes {
				if a.Key == id[len(personPrefix):] {
					in = notionInput{v: a.Value}
				}
			}
		default:
			in = notionInput{v: jsvalue.Undefined}
			if v, ok := r.answers.Data[id]; ok {
				in = notionInput{v: v, raw: r.data[id]}
			}
			if el, ok := pictureElements[id]; ok && pictureMapped[id] {
				if chosenIDs, ok := in.v.([]any); ok {
					urls := []any{}
					for _, c := range el.Choices {
						if chosen(chosenIDs, c.ID) {
							if c.ImageURL == "" {
								urls = append(urls, nil)
							} else {
								urls = append(urls, r.files.resolve(c.ImageURL))
							}
						}
					}
					in = notionInput{v: urls}
				}
			}
		}
		properties[m.Column.Name] = map[string]any{m.Column.Type: notionValue(m.Column.Type, in)}
	}
	return properties
}

// notionValue is forma2's getValue followed by its `|| null`: the payload Notion expects for a property of the column type, nil for null.
func notionValue(columnType string, in notionInput) any {
	switch columnType {
	case "select":
		if s, ok := in.v.(string); ok && s != "" && in.date == nil {
			return map[string]any{"name": strings.ReplaceAll(s, ",", "")}
		}
		return nil
	case "multi_select":
		list, ok := in.v.([]any)
		if !ok || in.date != nil {
			return nil
		}
		out := []any{}
		for _, e := range list {
			switch x := e.(type) {
			case string:
				out = append(out, map[string]any{"name": strings.ReplaceAll(x, ",", "")})
			case map[string]any:
				if name, ok := x["name"].(string); ok {
					out = append(out, map[string]any{"name": strings.ReplaceAll(name, ",", "")})
				}
			}
		}
		return out
	case "title", "rich_text":
		content, ok := notionString(in, true)
		if !ok || content == "" {
			return nil
		}
		if columnType == "rich_text" {
			content = truncate(content, notionRichTextLimit)
		}
		return []any{map[string]any{"text": map[string]any{"content": content}}}
	case "status":
		if in.v == jsvalue.Undefined && in.date == nil {
			return map[string]any{}
		}
		return map[string]any{"name": in.json()}
	case "checkbox":
		if in.v == "accepted" || in.v == "clicked" {
			return true
		}
		return nil
	case "date":
		if in.v == jsvalue.Undefined && in.date == nil {
			return map[string]any{}
		}
		return map[string]any{"start": in.json()}
	case "email", "phone_number":
		if in.falsy() {
			return nil
		}
		return in.json()
	case "number":
		var n float64
		switch {
		case in.date != nil:
			n = float64(in.date.UnixMilli())
		default:
			n = jsvalue.Number(in.v)
		}
		if math.IsNaN(n) || math.IsInf(n, 0) || n == 0 {
			return nil
		}
		return n
	case "url":
		if list, ok := in.v.([]any); ok && in.date == nil {
			if len(list) != 1 {
				return nil
			}
			content, ok := notionString(notionInput{v: list[0]}, false)
			if !ok || content == "" {
				return nil
			}
			return content
		}
		content, ok := notionString(in, false)
		if !ok || content == "" {
			return nil
		}
		return content
	}
	return nil
}

// notionString is forma2's coerceToNotionString: nothing for null or undefined (and for a list unless lists are allowed, which are joined by newlines), a Date as its ISO string, an object as its JSON, anything else as String() of it.
func notionString(in notionInput, allowLists bool) (string, bool) {
	if in.date != nil {
		return jsvalue.ISOString(*in.date), true
	}
	switch x := in.v.(type) {
	case nil:
		return "", false
	case []any:
		if !allowLists {
			return "", false
		}
		parts := make([]string, len(x))
		for i, e := range x {
			if !jsvalue.IsNullish(e) {
				parts[i] = jsvalue.String(e)
			}
		}
		return strings.Join(parts, "\n"), true
	case map[string]any:
		if in.raw != nil {
			return string(compact(in.raw)), true
		}
		raw, err := encodeJS(x)
		if err != nil {
			return "", false
		}
		return string(raw), true
	}
	if in.v == jsvalue.Undefined {
		return "", false
	}
	return jsvalue.String(in.v), true
}
