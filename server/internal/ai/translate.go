package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TranslationField is one text to translate, forma2's ZAITranslationField. Path is the caller's own key for it and never reaches the model.
type TranslationField struct {
	Path        string `json:"path"`
	DefaultText string `json:"defaultText"`
	IsRichText  bool   `json:"isRichText"`
}

// TranslationResult is forma2's TAITranslationResult: what came back, and the paths that did not, so a caller can keep a partial result.
type TranslationResult struct {
	Translations map[string]string `json:"translations"`
	FailedPaths  []string          `json:"failedPaths"`
}

// forma2's translation budget: tokens per field, bounded per call, and the chunk size that keeps a chunk's budget under the cap.
const (
	translationTimeout         = 45 * time.Second
	translationMinOutputTokens = 1024
	translationMaxOutputTokens = 8192
	translationTokensPerField  = 160
	translationMaxFieldsChunk  = translationMaxOutputTokens / translationTokensPerField
)

// errIncompleteTranslation is forma2's "AI translation returned incomplete result".
var errIncompleteTranslation = errors.New("AI translation returned incomplete result")

type translationItem struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	RichText bool   `json:"richText"`
}

// TranslateFields is forma2's translateFields: empty texts pass through untranslated, the rest go to the model in chunks one after another, each item under an index id so paths never reach the model. A chunk that fails, or an item that comes back missing or empty, lands in FailedPaths; only a run where nothing at all was translated fails, with the first chunk's error.
func (s *Service) TranslateFields(ctx context.Context, organizationID uuid.UUID, fields []TranslationField, sourceLanguage, targetLanguage string) (*TranslationResult, error) {
	out := &TranslationResult{Translations: map[string]string{}, FailedPaths: []string{}}
	if len(fields) == 0 {
		return out, nil
	}
	passthrough := map[string]string{}
	var translatable []TranslationField
	for _, f := range fields {
		if f.DefaultText == "" {
			passthrough[f.Path] = ""
		} else {
			translatable = append(translatable, f)
		}
	}
	if len(translatable) == 0 {
		out.Translations = passthrough
		return out, nil
	}
	system := fmt.Sprintf(`You are a professional translator for survey content. Translate each item from %s to %s.
Rules:
- For rich text items (richText: true), preserve all HTML tags exactly. Only translate the text content within the tags.
- Preserve any {{variable}} patterns exactly — do not translate text inside double curly braces.
- Translate every item. Do not omit any keys.`, sourceLanguage, targetLanguage)

	var firstErr error
	for offset := 0; offset < len(translatable); offset += translationMaxFieldsChunk {
		chunk := translatable[offset:min(offset+translationMaxFieldsChunk, len(translatable))]
		items := make([]translationItem, len(chunk))
		properties := map[string]any{}
		required := make([]string, len(chunk))
		for i, f := range chunk {
			id := fmt.Sprintf("t%d", i)
			items[i] = translationItem{ID: id, Text: f.DefaultText, RichText: f.IsRichText}
			properties[id] = map[string]any{"type": "string"}
			required[i] = id
		}
		// A schema naming exactly these ids makes the provider return exactly this set, the shape zod's object schema had.
		schema, _ := json.Marshal(map[string]any{"$schema": "http://json-schema.org/draft-07/schema#", "type": "object", "properties": properties, "required": required, "additionalProperties": false})
		prompt, _ := json.Marshal(items)
		temperature := 0.0
		res, err := s.GenerateObject(ctx, organizationID, FeatureTranslation, ObjectRequest{
			Request: Request{
				System: system, Prompt: string(prompt), Temperature: &temperature, Timeout: translationTimeout,
				MaxOutputTokens: min(translationMaxOutputTokens, max(translationMinOutputTokens, len(chunk)*translationTokensPerField)),
			},
			Schema: schema,
		})
		if err != nil {
			if IsAbort(ctx, err) {
				return nil, err
			}
			if firstErr == nil {
				firstErr = err
			}
			for _, f := range chunk {
				out.FailedPaths = append(out.FailedPaths, f.Path)
			}
			s.log.ErrorContext(ctx, "AI translation chunk failed", "organizationId", organizationID.String(), "sourceLanguage", sourceLanguage, "targetLanguage", targetLanguage, "chunkSize", len(chunk), "err", err)
			continue
		}
		var byID map[string]any
		_ = json.Unmarshal(res.Object, &byID)
		var missing []string
		for i, f := range chunk {
			if v, ok := byID[items[i].ID].(string); ok && v != "" {
				out.Translations[f.Path] = v
			} else {
				missing = append(missing, items[i].ID)
				out.FailedPaths = append(out.FailedPaths, f.Path)
			}
		}
		if len(missing) > 0 {
			s.log.ErrorContext(ctx, "AI translation returned incomplete result", "organizationId", organizationID.String(), "sourceLanguage", sourceLanguage, "targetLanguage", targetLanguage,
				"requestedCount", len(chunk), "returnedCount", len(chunk)-len(missing), "missingIds", missing)
		}
	}
	if len(out.Translations) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, errIncompleteTranslation
	}
	for path, text := range passthrough {
		if _, ok := out.Translations[path]; !ok {
			out.Translations[path] = text
		}
	}
	return out, nil
}
