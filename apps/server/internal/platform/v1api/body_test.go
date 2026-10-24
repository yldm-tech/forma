package v1api

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// forma2's parseJsonBodyWithLimit (apps/web/lib/api/request-body.test.ts): a declared or actual body over 2 MiB is refused, an empty or broken one is not JSON, and members are read only from an object.
func TestReadJSONObject(t *testing.T) {
	big := `{"a":"` + strings.Repeat("x", MaxBodyBytes) + `"}`
	declared := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	declared.ContentLength = MaxBodyBytes + 1
	undeclared := httptest.NewRequest("POST", "/", strings.NewReader(big))
	undeclared.ContentLength = -1
	for name, body := range map[string]string{"empty": "", "broken": `{"a":`} {
		if _, err := ReadJSONObject(httptest.NewRequest("POST", "/", strings.NewReader(body))); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := ReadJSONObject(declared); !errors.Is(err, ErrBodyTooLarge) {
		t.Errorf("declared length: %v", err)
	}
	if _, err := ReadJSONObject(undeclared); !errors.Is(err, ErrBodyTooLarge) {
		t.Errorf("actual length: %v", err)
	}
	if ErrBodyTooLarge.Error() != "Request body must not exceed 2097152 bytes" {
		t.Errorf("message = %q", ErrBodyTooLarge)
	}
	obj, err := ReadJSONObject(httptest.NewRequest("POST", "/", strings.NewReader(`["a"]`)))
	if err != nil || len(obj) != 0 {
		t.Errorf("an array reads as an object without members: %v, %v", obj, err)
	}
	obj, err = ReadJSONObject(httptest.NewRequest("POST", "/", strings.NewReader(`{"workspaceId":"w"}`)))
	if err != nil || obj["workspaceId"] != "w" {
		t.Errorf("object = %v, %v", obj, err)
	}
}

// IsID is zod's cuid2 check (lowercase letters and digits) widened to the rewrite's UUIDs.
func TestIsID(t *testing.T) {
	for id, want := range map[string]bool{
		"clsurveyaaaaaaaaaaaaaaa1":             true,
		"01a16644-c52c-7b84-9640-b3613fb74a6d": true,
		"01A16644-C52C-7B84-9640-B3613FB74A6D": true,
		"01a16644c52c7b849640b3613fb74a6d":     true,
		"01a16644-c52c-7b84-9640":              false,
		"NOT-AN-ID":                            false,
		"not-a-cuid!":                          false,
		"":                                     false,
	} {
		if got := IsID(id); got != want {
			t.Errorf("IsID(%q) = %v, want %v", id, got, want)
		}
	}
}
