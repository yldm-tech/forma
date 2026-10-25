package storage

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
)

// forma2's validateClientFileUploads (apps/web/modules/storage/utils.test.ts): a file upload answer lists files uploaded for exactly that workspace, survey and element, on the app's own origins; a management caller may also replay files stored under a prefix its workspace owns from before uploads were scoped.
func TestValidAnswerFiles(t *testing.T) {
	ws, sv := uuid.New(), uuid.New()
	app, _ := url.Parse("https://app.example.com")
	var elements []AnswerElement
	if err := json.Unmarshal([]byte(`[{"id":"doc","type":"fileUpload","allowedFileExtensions":["pdf"]},{"id":"text","type":"openText"}]`), &elements); err != nil {
		t.Fatal(err)
	}
	scoped := "/storage/" + ws.String() + "/private/surveys/" + sv.String() + "/elements/doc/cv--fid--" + uuid.NewString() + ".pdf"
	client := UploadScope{WorkspaceID: ws, SurveyID: sv, AppOrigins: []*url.URL{app}}

	for _, c := range []struct {
		name   string
		answer any
		scope  UploadScope
		want   bool
	}{
		{"scoped", []any{scoped}, client, true},
		{"scoped on the app's origin", []any{"https://app.example.com" + scoped}, client, true},
		{"on another origin", []any{"https://evil.example" + scoped}, client, false},
		{"another element", []any{"/storage/" + ws.String() + "/private/surveys/" + sv.String() + "/elements/other/cv.pdf"}, client, false},
		{"an extension the element refuses", []any{"/storage/" + ws.String() + "/private/surveys/" + sv.String() + "/elements/doc/cv.exe"}, client, false},
		{"not a list", scoped, client, false},
		{"another workspace", []any{"/storage/" + uuid.NewString() + "/private/surveys/" + sv.String() + "/elements/doc/cv.pdf"}, client, false},
		{"another survey", []any{"/storage/" + ws.String() + "/private/surveys/" + uuid.NewString() + "/elements/doc/cv.pdf"}, client, false},
		{"unscoped under the workspace", []any{"/storage/" + ws.String() + "/private/cv.pdf"}, client, false},
		{"a public file", []any{"/storage/" + ws.String() + "/public/cv.pdf"}, client, false},
		{"a cuid2 for the workspace", []any{"/storage/clworkspaceaaaaaaaaaaaa1/private/surveys/" + sv.String() + "/elements/doc/cv.pdf"}, client, false},
	} {
		if got := ValidAnswerFiles(map[string]any{"doc": c.answer, "text": "https://evil.example/x.pdf"}, elements, c.scope); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
