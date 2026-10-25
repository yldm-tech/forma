package apikey

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func bindCreate(t *testing.T, schemas *jsonschema.Validator, body string) (CreateInput, error) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	var req createRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		return CreateInput{}, err
	}
	return req.input(schemas, time.Now())
}

func TestCreateRequestValidation(t *testing.T) {
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	ws1, ws2 := uuid.NewString(), uuid.NewString()
	access := `"organizationAccess":{"accessControl":{"read":false,"write":false}}`
	cases := []struct {
		name    string
		body    string
		field   string
		rule    string
		message string
	}{
		{"missing label", `{` + access + `,"workspacePermissions":[{"workspaceId":"` + ws1 + `","permission":"read"}]}`, "label", "required", ""},
		{"blank label", `{"label":"  \t",` + access + `,"workspacePermissions":[{"workspaceId":"` + ws1 + `","permission":"read"}]}`, "label", "required", ""},
		{"label of the wrong type", `{"label":7,` + access + `}`, "label", "type", ""},
		{"missing organization access", `{"label":"CI","workspacePermissions":[{"workspaceId":"` + ws1 + `","permission":"read"}]}`, "organizationAccess", "required", ""},
		{"null organization access", `{"label":"CI","organizationAccess":null}`, "organizationAccess", "required", ""},
		{"organization access without accessControl", `{"label":"CI","organizationAccess":{}}`, "organizationAccess", "schema", ""},
		{"organization access missing a flag", `{"label":"CI","organizationAccess":{"accessControl":{"read":true}}}`, "organizationAccess", "schema", ""},
		{"organization access with an extra flag", `{"label":"CI","organizationAccess":{"accessControl":{"read":true,"write":false,"manage":true}}}`, "organizationAccess", "schema", ""},
		{"a workspace id that is not a uuid", `{"label":"CI",` + access + `,"workspacePermissions":[{"workspaceId":"clxyz123","permission":"read"}]}`, "workspacePermissions[0].workspaceId", "uuid", ""},
		{"an unknown permission", `{"label":"CI",` + access + `,"workspacePermissions":[{"workspaceId":"` + ws1 + `","permission":"admin"}]}`, "workspacePermissions[0].permission", "oneof", ""},
		{"a missing permission", `{"label":"CI",` + access + `,"workspacePermissions":[{"workspaceId":"` + ws1 + `"}]}`, "workspacePermissions[0].permission", "required", ""},
		{"the same workspace twice", `{"label":"CI",` + access + `,"workspacePermissions":[{"workspaceId":"` + ws1 + `","permission":"read"},{"workspaceId":"` + ws1 + `","permission":"write"}]}`, "workspacePermissions", "unique", "Duplicate workspace permissions are not allowed"},
		{"no access at all", `{"label":"CI",` + access + `,"workspacePermissions":[]}`, "workspacePermissions", "required", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := bindCreate(t, schemas, tc.body)
			e, ok := apperr.As(err)
			if !ok || e.Code != apperr.CodeValidation || e.Status != http.StatusBadRequest {
				t.Fatalf("err = %v, want a validation error", err)
			}
			fields, _ := e.Details["fields"].([]apperr.FieldError)
			found := false
			for _, f := range fields {
				found = found || (f.Field == tc.field && f.Rule == tc.rule)
			}
			if !found {
				t.Fatalf("fields = %+v, want %s/%s", fields, tc.field, tc.rule)
			}
			if tc.message != "" && e.Message != tc.message {
				t.Fatalf("message = %q, want %q", e.Message, tc.message)
			}
		})
	}

	t.Run("a valid request keeps the label as typed and strips unknown access groups", func(t *testing.T) {
		in, err := bindCreate(t, schemas, `{"label":" CI ","organizationAccess":{"accessControl":{"read":true,"write":false},"billing":{"read":true}},"workspacePermissions":[{"workspaceId":"`+ws1+`","permission":"read"},{"workspaceId":"`+ws2+`","permission":"manage"}],"ignored":true}`)
		if err != nil {
			t.Fatal(err)
		}
		if in.Label != " CI " || in.OrganizationAccess != (OrganizationAccess{AccessControl: AccessFlags{Read: true}}) {
			t.Fatalf("input = %+v", in)
		}
		want := []WorkspacePermissionInput{{uuid.MustParse(ws1), PermissionRead}, {uuid.MustParse(ws2), PermissionManage}}
		if len(in.WorkspacePermissions) != 2 || in.WorkspacePermissions[0] != want[0] || in.WorkspacePermissions[1] != want[1] {
			t.Fatalf("workspace permissions = %+v, want %+v", in.WorkspacePermissions, want)
		}
	})

	t.Run("organization access alone is enough", func(t *testing.T) {
		in, err := bindCreate(t, schemas, `{"label":"CI","organizationAccess":{"accessControl":{"read":false,"write":true}}}`)
		if err != nil {
			t.Fatal(err)
		}
		if len(in.WorkspacePermissions) != 0 || !in.OrganizationAccess.AccessControl.Write {
			t.Fatalf("input = %+v", in)
		}
	})
}
