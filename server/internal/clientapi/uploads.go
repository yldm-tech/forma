package clientapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/storage"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// maxOtherOptionLength is forma2's MAX_OTHER_OPTION_LENGTH: the longest "Other" text a choice answer may carry.
const maxOtherOptionLength = 250

// fileUploadElement is a survey's file upload element as the upload rules read it.
type fileUploadElement struct {
	ID                    string    `json:"id"`
	Type                  string    `json:"type"`
	AllowedFileExtensions *[]string `json:"allowedFileExtensions"`
	MaxSizeInMB           *float64  `json:"maxSizeInMB"`
	Choices               []struct {
		Label map[string]string `json:"label"`
	} `json:"choices"`
}

// surveyElements reads a survey's elements in document order.
func surveyElements(blocks json.RawMessage) ([]fileUploadElement, error) {
	var bs []struct {
		Elements []fileUploadElement `json:"elements"`
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(blocks, &bs); err != nil {
		return nil, fmt.Errorf("decode survey blocks: %w", err)
	}
	var out []fileUploadElement
	for _, b := range bs {
		out = append(out, b.Elements...)
	}
	return out, nil
}

// fileUploadElements is forma2's getSurveyFileUploadConfigs.
func fileUploadElements(elements []fileUploadElement) []fileUploadElement {
	var out []fileUploadElement
	for _, e := range elements {
		if e.Type == surveydoc.ElementFileUpload {
			out = append(out, e)
		}
	}
	return out
}

// validClientFileUploads is forma2's validateClientFileUploads: every answer to a file upload element is a list of URLs of files uploaded for exactly that workspace, survey and element (/storage/{workspace}/private/surveys/{survey}/elements/{element}/{file}, relative or on one of the app's own origins), with an extension the element allows. Data without such answers passes.
func (h *Handler) validClientFileUploads(data map[string]any, elements []fileUploadElement, workspaceID, surveyID uuid.UUID) bool {
	for _, e := range fileUploadElements(elements) {
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
			if !validSingleFile(s, e.AllowedFileExtensions) || !h.scopedUploadURL(s, workspaceID, surveyID, e.ID) {
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
	ext := storage.Extension(name)
	if ext == "" {
		return false
	}
	return allowed == nil || slices.Contains(*allowed, ext)
}

// scopedUploadURL is forma2's isScopedPrivateUploadUrl for the client widget, which only ever emits the fully scoped shape. Ids in the URL may be the rewrite's or forma2's.
func (h *Handler) scopedUploadURL(fileURL string, workspaceID, surveyID uuid.UUID, elementID string) bool {
	path := fileURL
	if !strings.HasPrefix(fileURL, "/storage/") {
		u, err := url.Parse(fileURL)
		if err != nil || u.Scheme == "" || u.Host == "" || !h.appOrigin(u) {
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
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	return len(segments) == 8 && segments[0] == "storage" && legacyid.Matches(segments[1], workspaceID) && segments[2] == "private" &&
		segments[3] == "surveys" && legacyid.Matches(segments[4], surveyID) && segments[5] == "elements" && segments[6] == elementID && segments[7] != ""
}

// appOrigin reports whether u is on WEBAPP_URL's or the public domain's origin, the only absolute URLs a stored file may be read back under.
func (h *Handler) appOrigin(u *url.URL) bool {
	origin := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
	for _, base := range []*url.URL{h.cfg.WebAppURL, h.cfg.PublicURL} {
		if base != nil && origin == strings.ToLower(base.Scheme)+"://"+strings.ToLower(base.Host) {
			return true
		}
	}
	return false
}

// otherOptionTooLong is forma2's validateOtherOptionLengthForMultipleChoice: the id of the first choice element whose answer carries a text that is no choice's label in the response's language and is longer than maxOtherOptionLength, "" when there is none.
func otherOptionTooLong(data map[string]any, keys []string, elements []fileUploadElement, responseLanguage *string) string {
	language := "default"
	if responseLanguage != nil {
		language = *responseLanguage
	}
	for _, key := range keys {
		i := slices.IndexFunc(elements, func(e fileUploadElement) bool { return e.ID == key })
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
			if !ok || len(utf16.Encode([]rune(s))) <= maxOtherOptionLength {
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
