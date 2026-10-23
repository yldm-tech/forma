package storage

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// The checks on the file URLs a response's answers carry (forma2's validateClientFileUploads, apps/web/modules/storage/utils.ts) and on its "Other" texts, shared by the client API and the management APIs.

// MaxOtherOptionLength is forma2's MAX_OTHER_OPTION_LENGTH: the longest "Other" text a choice answer may carry.
const MaxOtherOptionLength = 250

// AnswerElement is a survey element as the upload and "Other" rules read it.
type AnswerElement struct {
	ID                    string    `json:"id"`
	Type                  string    `json:"type"`
	AllowedFileExtensions *[]string `json:"allowedFileExtensions"`
	MaxSizeInMB           *float64  `json:"maxSizeInMB"`
	Choices               []struct {
		Label map[string]string `json:"label"`
	} `json:"choices"`
}

// AnswerElements reads a survey's elements in document order.
func AnswerElements(blocks json.RawMessage) ([]AnswerElement, error) {
	var bs []struct {
		Elements []AnswerElement `json:"elements"`
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(blocks, &bs); err != nil {
		return nil, fmt.Errorf("decode survey blocks: %w", err)
	}
	var out []AnswerElement
	for _, b := range bs {
		out = append(out, b.Elements...)
	}
	return out, nil
}

// FileUploadElements is forma2's getSurveyFileUploadConfigs.
func FileUploadElements(elements []AnswerElement) []AnswerElement {
	var out []AnswerElement
	for _, e := range elements {
		if e.Type == surveydoc.ElementFileUpload {
			out = append(out, e)
		}
	}
	return out
}

// UploadScope is where the files of one response's answers must have been uploaded.
type UploadScope struct {
	WorkspaceID uuid.UUID
	SurveyID    uuid.UUID
	// AppOrigins are the origins an absolute file URL may be on: WEBAPP_URL's and the public domain's.
	AppOrigins []*url.URL
	// LegacyPrefixes are the storage prefixes the workspace owns for files uploaded before forma2 scoped uploads to a survey and element (its own id and its legacy environment id; the forma2 id the workspace was migrated from is owned too). Only the management APIs pass them, so a replayed old response validates; the client widget only ever emits the scoped shape.
	LegacyPrefixes []string
}

// ValidAnswerFiles is forma2's validateClientFileUploads: every answer to a file upload element is a list of URLs of files uploaded for exactly that workspace, survey and element (/storage/{workspace}/private/surveys/{survey}/elements/{element}/{file}, relative or on one of the app's own origins), or under a legacy prefix the workspace owns (/storage/{prefix}/private/{file}), with an extension the element allows. Data without such answers passes. Ids in the URL may be the rewrite's or forma2's.
func ValidAnswerFiles(data map[string]any, elements []AnswerElement, scope UploadScope) bool {
	for _, e := range FileUploadElements(elements) {
		answer, ok := data[e.ID]
		if !ok {
			continue
		}
		urls, ok := answer.([]any)
		if !ok {
			return false
		}
		for _, u := range urls {
			s, ok := u.(string)
			if !ok {
				return false
			}
			if !validSingleFile(s, e.AllowedFileExtensions) || !scope.scopedUploadURL(s, e.ID) {
				return false
			}
		}
	}
	return true
}

// validSingleFile is forma2's validateSingleFile: the file's original name has an extension, one of allowed when the element restricts them.
func validSingleFile(fileURL string, allowed *[]string) bool {
	name := surveydoc.OriginalFileName(fileURL)
	if name == "" {
		return false
	}
	ext := Extension(name)
	if ext == "" {
		return false
	}
	return allowed == nil || slices.Contains(*allowed, ext)
}

// scopedUploadURL is forma2's isScopedPrivateUploadUrl.
func (s UploadScope) scopedUploadURL(fileURL, elementID string) bool {
	path := fileURL
	if !strings.HasPrefix(fileURL, "/storage/") {
		u, err := url.Parse(fileURL)
		if err != nil || u.Scheme == "" || u.Host == "" || !s.appOrigin(u) {
			return false
		}
		path = u.EscapedPath()
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if !strings.HasPrefix(path, "/storage/") {
		return false
	}
	var segments []string
	for _, seg := range strings.Split(path, "/") {
		if seg != "" {
			segments = append(segments, seg)
		}
	}
	switch {
	case len(segments) == 8:
		return segments[0] == "storage" && legacyid.Matches(segments[1], s.WorkspaceID) && segments[2] == "private" &&
			segments[3] == "surveys" && legacyid.Matches(segments[4], s.SurveyID) && segments[5] == "elements" && segments[6] == elementID && segments[7] != ""
	case len(segments) == 4 && len(s.LegacyPrefixes) > 0:
		owned := slices.Contains(s.LegacyPrefixes, segments[1]) || legacyid.Matches(segments[1], s.WorkspaceID)
		return segments[0] == "storage" && segments[2] == "private" && owned && segments[3] != ""
	}
	return false
}

// appOrigin reports whether u is on one of the scope's app origins, the only absolute URLs a stored file may be read back under.
func (s UploadScope) appOrigin(u *url.URL) bool {
	origin := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
	for _, base := range s.AppOrigins {
		if base != nil && origin == strings.ToLower(base.Scheme)+"://"+strings.ToLower(base.Host) {
			return true
		}
	}
	return false
}

// OtherOptionTooLong is forma2's validateOtherOptionLengthForMultipleChoice: the id of the first choice element among keys whose answer carries a text that is no choice's label in the response's language and is longer than MaxOtherOptionLength, "" when there is none. A nil language is "default".
func OtherOptionTooLong(data map[string]any, keys []string, elements []AnswerElement, responseLanguage *string) string {
	language := "default"
	if responseLanguage != nil {
		language = *responseLanguage
	}
	for _, key := range keys {
		i := slices.IndexFunc(elements, func(e AnswerElement) bool { return e.ID == key })
		if i < 0 {
			continue
		}
		e := elements[i]
		if (e.Type != surveydoc.ElementMultipleChoiceMulti && e.Type != surveydoc.ElementMultipleChoiceSingle) || e.Choices == nil {
			continue
		}
		var answers []any
		switch v := data[key].(type) {
		case string:
			answers = []any{v}
		case []any:
			answers = v
		}
		for _, a := range answers {
			s, ok := a.(string)
			if !ok || len(utf16.Encode([]rune(s))) <= MaxOtherOptionLength {
				continue
			}
			labelled := slices.ContainsFunc(e.Choices, func(c struct {
				Label map[string]string `json:"label"`
			}) bool {
				return c.Label != nil && localizedLabel(c.Label, language) == s
			})
			if !labelled {
				return key
			}
		}
	}
	return ""
}

// localizedLabel is the server's getLocalizedValue (apps/web/lib/i18n/utils.ts): the label in exactly that language, "" when it has none. Unlike the runtime's it does not fall back to the default.
func localizedLabel(label map[string]string, language string) string {
	if _, ok := label["default"]; !ok {
		return ""
	}
	return label[language]
}
