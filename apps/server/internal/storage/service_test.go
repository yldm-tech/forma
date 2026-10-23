package storage

import (
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

var fixedID = uuid.MustParse("6f1a3c1e-8a63-4b8e-9a35-5d3c1d1f2a10")

func newService(t *testing.T) (*Service, config.S3Config) {
	t.Helper()
	b, s3 := newBucket(t)
	s := NewService(b)
	s.newID = func() uuid.UUID { return fixedID }
	return s, s3
}

func TestServiceSignUpload(t *testing.T) {
	t.Parallel()
	s, _ := newService(t)
	ctx := t.Context()
	cases := []struct {
		name, fileName string
		access         AccessType
		segments       []string
		key, fileURL   string
	}{
		{"public", "test-image.jpg", AccessPublic, nil, "ws-123/public/test-image--fid--" + fixedID.String() + ".jpg", "/storage/ws-123/public/test-image--fid--" + fixedID.String() + ".jpg"},
		{"scoped private", "test-doc.pdf", AccessPrivate, []string{"surveys", "survey-123", "elements", "element-123"},
			"ws-123/private/surveys/survey-123/elements/element-123/test-doc--fid--" + fixedID.String() + ".pdf",
			"/storage/ws-123/private/surveys/survey-123/elements/element-123/test-doc--fid--" + fixedID.String() + ".pdf"},
		{"sanitized", "test#file.txt", AccessPrivate, nil, "ws-123/private/testfile--fid--" + fixedID.String() + ".txt", "/storage/ws-123/private/testfile--fid--" + fixedID.String() + ".txt"},
		{"several dots", "my.backup.file.pdf", AccessPrivate, nil, "ws-123/private/my.backup.file--fid--" + fixedID.String() + ".pdf", "/storage/ws-123/private/my.backup.file--fid--" + fixedID.String() + ".pdf"},
		// The key holds the name as stored; the URL encodes it.
		{"encoded in the URL only", "invoice 100%.pdf", AccessPrivate, nil, "ws-123/private/invoice 100%--fid--" + fixedID.String() + ".pdf", "/storage/ws-123/private/invoice%20100%25--fid--" + fixedID.String() + ".pdf"},
	}
	for _, tc := range cases {
		up, err := s.SignUpload(ctx, tc.fileName, "ws-123", tc.access, 10<<20, tc.segments...)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := fieldValue(up.PresignedFields, "key"); got != tc.key {
			t.Errorf("%s: key %q, want %q", tc.name, got, tc.key)
		}
		if up.FileURL != tc.fileURL {
			t.Errorf("%s: fileUrl %q, want %q", tc.name, up.FileURL, tc.fileURL)
		}
		if up.SignedURL == "" {
			t.Errorf("%s: no signed URL", tc.name)
		}
		if got, want := fieldValue(up.PresignedFields, "Content-Type"), mimeTypes[Extension(tc.fileName)]; got != want {
			t.Errorf("%s: Content-Type %q, want %q", tc.name, got, want)
		}
	}

	for _, segment := range []string{"", ".", "..", "bad segment", "bad/segment", `bad\segment`, "bad?segment", "bad#segment"} {
		if _, err := s.SignUpload(ctx, "test-doc.pdf", "ws-123", AccessPrivate, 10<<20, "surveys", segment); CodeOf(err) != CodeInvalidInput {
			t.Errorf("segment %q: %v, want invalid_input", segment, err)
		}
	}
	for _, name := range []string{"", "----.png", "#?*.png", "   ", "no-extension", "page.html", "trailing-dot."} {
		if _, err := s.SignUpload(ctx, name, "ws-123", AccessPublic, PublicUploadMaxSize); CodeOf(err) != CodeInvalidInput {
			t.Errorf("name %q: %v, want invalid_input", name, err)
		}
	}
}

func TestServiceSignUploadUnconfigured(t *testing.T) {
	b, err := NewBucket(config.Config{}, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(b)
	if _, err := s.SignUpload(t.Context(), "a.png", "ws", AccessPublic, PublicUploadMaxSize); CodeOf(err) != CodeS3Client {
		t.Fatalf("SignUpload = %v, want s3_client_error", err)
	}
	// Nothing can be stored without a bucket, so there is nothing to clean up.
	if err := s.DeleteWorkspaceFiles(t.Context(), "ws"); err != nil {
		t.Fatalf("DeleteWorkspaceFiles = %v", err)
	}
}

// routeFileName is what the route receives for a stored URL: the path after the access type, each segment decoded once.
func routeFileName(t *testing.T, fileURL, prefix string) string {
	t.Helper()
	segments := strings.Split(strings.TrimPrefix(fileURL, prefix), "/")
	for i, s := range segments {
		decoded, err := url.PathUnescape(s)
		if err != nil {
			t.Fatal(err)
		}
		segments[i] = decoded
	}
	return strings.Join(segments, "/")
}

// reader reads a whole opened file: read(s.Open(...)).
func reader(t *testing.T) func(*Object, error) string {
	return func(obj *Object, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = obj.Body.Close() }()
		b, _ := io.ReadAll(obj.Body)
		return string(b)
	}
}

func TestServiceOpen(t *testing.T) {
	t.Parallel()
	s, s3 := newService(t)
	ctx := t.Context()
	read := reader(t)

	// An upload's URL leads back to the key it was stored under, a literal % included.
	up, err := s.SignUpload(ctx, "invoice 100%.pdf", "ws-123", AccessPrivate, 10<<20)
	if err != nil {
		t.Fatal(err)
	}
	put(t, s3, fieldValue(up.PresignedFields, "key"), "application/pdf", "invoice")
	name := routeFileName(t, up.FileURL, "/storage/ws-123/private/")
	if got := read(s.Open(ctx, name, "ws-123", "ws-123", AccessPrivate)); got != "invoice" {
		t.Fatalf("opened %q", got)
	}

	put(t, s3, "ws-123/public/survey-1/q-2/file.jpg", "image/jpeg", "nested")
	if got := read(s.Open(ctx, "survey-1/q-2/file.jpg", "ws-123", "ws-123", AccessPublic)); got != "nested" {
		t.Fatalf("nested path opened %q", got)
	}

	// A file uploaded under an environment id (or a migrated forma2 id) is found under the id the URL carried.
	put(t, s3, "clenvaaaaaaaaaaaaaaaaaaa1/public/logo.png", "image/png", "legacy")
	if got := read(s.Open(ctx, "logo.png", "ws-123", "clenvaaaaaaaaaaaaaaaaaaa1", AccessPublic)); got != "legacy" {
		t.Fatalf("legacy file opened %q", got)
	}
	// The workspace's own copy wins.
	put(t, s3, "ws-123/public/logo.png", "image/png", "current")
	if got := read(s.Open(ctx, "logo.png", "ws-123", "clenvaaaaaaaaaaaaaaaaaaa1", AccessPublic)); got != "current" {
		t.Fatalf("opened %q, want the workspace's own file", got)
	}
	if _, err := s.Open(ctx, "missing.png", "ws-123", "clenvaaaaaaaaaaaaaaaaaaa1", AccessPublic); CodeOf(err) != CodeFileNotFound {
		t.Fatalf("missing file: %v", err)
	}
	// The access type is part of the key: a private file is not served as public.
	if _, err := s.Open(ctx, name, "ws-123", "ws-123", AccessPublic); CodeOf(err) != CodeFileNotFound {
		t.Fatalf("private file opened as public: %v", err)
	}

	put(t, s3, "ws-victim/private/secret.pdf", "application/pdf", "secret")
	for _, name := range []string{"../../ws-victim/private/secret.pdf", "sub/../../../ws-victim/private/secret.pdf", "%2e%2e/%2e%2e/ws-victim/private/secret.pdf", "./secret.pdf"} {
		if _, err := s.Open(ctx, name, "ws-attacker", "ws-attacker", AccessPublic); CodeOf(err) != CodeInvalidInput {
			t.Errorf("Open(%q) = %v, want invalid_input", name, err)
		}
	}
}

func TestServiceDelete(t *testing.T) {
	t.Parallel()
	s, s3 := newService(t)
	ctx := t.Context()

	put(t, s3, "ws-456/private/invoice 100%.pdf", "application/pdf", "x")
	if err := s.Delete(ctx, "invoice 100%.pdf", "ws-456", "ws-456", AccessPrivate); err != nil {
		t.Fatal(err)
	}
	if exists(t, s3, "ws-456/private/invoice 100%.pdf") {
		t.Fatal("file survived")
	}

	// Both the workspace's key and the one under the URL's own id go.
	put(t, s3, "ws-456/public/survey-1/q-2/file.jpg", "image/jpeg", "x")
	put(t, s3, "clenvaaaaaaaaaaaaaaaaaaa1/public/survey-1/q-2/file.jpg", "image/jpeg", "x")
	if err := s.Delete(ctx, "survey-1/q-2/file.jpg", "ws-456", "clenvaaaaaaaaaaaaaaaaaaa1", AccessPublic); err != nil {
		t.Fatal(err)
	}
	if exists(t, s3, "ws-456/public/survey-1/q-2/file.jpg") || exists(t, s3, "clenvaaaaaaaaaaaaaaaaaaa1/public/survey-1/q-2/file.jpg") {
		t.Fatal("a copy survived")
	}
	// Deleting what is not there succeeds.
	if err := s.Delete(ctx, "missing.png", "ws-456", "ws-456", AccessPublic); err != nil {
		t.Fatal(err)
	}

	put(t, s3, "ws-victim/private/secret.pdf", "application/pdf", "secret")
	for _, name := range []string{"../../ws-victim/private/secret.pdf", "sub/../../../ws-victim/private/secret.pdf", "%2e%2e/%2e%2e/ws-victim/private/secret.pdf", "./file.jpg"} {
		if err := s.Delete(ctx, name, "ws-456", "ws-456", AccessPublic); CodeOf(err) != CodeInvalidInput {
			t.Errorf("Delete(%q) = %v, want invalid_input", name, err)
		}
	}
	if !exists(t, s3, "ws-victim/private/secret.pdf") {
		t.Fatal("a traversal deleted another workspace's file")
	}
}

func TestServiceDeleteWorkspaceFiles(t *testing.T) {
	t.Parallel()
	s, s3 := newService(t)
	for _, k := range []string{"ws-1/public/a.png", "ws-1/private/surveys/s/elements/e/b.pdf", "clenv1/private/c.pdf", "ws-10/public/keep.png", "ws-1"} {
		put(t, s3, k, "application/octet-stream", "x")
	}
	if err := s.DeleteWorkspaceFiles(t.Context(), "ws-1", "clenv1"); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]bool{
		"ws-1/public/a.png": false, "ws-1/private/surveys/s/elements/e/b.pdf": false, "clenv1/private/c.pdf": false,
		// Another workspace whose id shares the first characters, and a stray object named like the id, are not under its prefix.
		"ws-10/public/keep.png": true, "ws-1": true,
	} {
		if exists(t, s3, k) != want {
			t.Errorf("%s exists = %v, want %v", k, !want, want)
		}
	}
	if err := s.DeleteWorkspaceFiles(t.Context(), "", "/"); CodeOf(err) != CodeInvalidInput {
		t.Errorf("empty id: %v, want invalid_input", err)
	}
}
