package storage

import (
	"slices"
	"strings"
	"testing"
)

func TestSanitizeFileName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"photo.jpg", "photo.jpg"},
		{`a/b\c.txt`, "a-b-c.txt"},
		{"we<>:\"|?*`'#ird.pdf", "weird.pdf"},
		{"test#file.png", "testfile.png"},
		{"  my   file   name   .jpg  ", "my file name.jpg"},
		{"my.backup.file.pdf", "my.backup.file.pdf"},
		{"----.png", ""},
		{"....png", ""},
		{"file.pn#g", "file.png"},
		{strings.Repeat("a", 300) + ".txt", strings.Repeat("a", 200) + ".txt"},
		// Non-ASCII letters and % stay; the extension keeps ASCII letters and digits only, cut to 20.
		{"résumé été.pdf", "résumé été.pdf"},
		{"invoice 100%.pdf", "invoice 100%.pdf"},
		{"report.pdf-v2_final", "report.pdfv2final"},
		{"a." + strings.Repeat("x", 25), "a." + strings.Repeat("x", 20)},
		{"archive.tar.gz", "archive.tar.gz"},
		{"README", "README"},
		{"a.ñ", "a"},
		// NFC: e + combining acute is stored as é.
		{"cafe\u0301.png", "caf\u00e9.png"},
		// JavaScript's \s covers Unicode spaces.
		{"\u3000a\u00a0\u00a0b\ufeff.png\u2003", "a b.png"},
		{".png", ""},
		{" - .png", ""},
		{"a..png", "a..png"},
		{"...", ""},
		{"---", ""},
	}
	for _, tc := range cases {
		if got := SanitizeFileName(tc.in); got != tc.want {
			t.Errorf("SanitizeFileName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// The base is cut at 200 UTF-16 units, as String.prototype.slice: a character outside the BMP counts twice and is not split.
	astral := strings.Repeat("😀", 101) + ".png"
	if got := SanitizeFileName(astral); got != strings.Repeat("😀", 100)+".png" {
		t.Errorf("astral base cut to %d runes", len([]rune(got))-4)
	}
	if got := SanitizeFileName("a" + strings.Repeat("😀", 100) + ".png"); got != "a"+strings.Repeat("😀", 99)+".png" {
		t.Errorf("a surrogate pair straddling the limit was kept as %q", got)
	}
}

func TestValidateUploadInput(t *testing.T) {
	cases := []struct {
		name, fileName, fileType string
		allowed                  []string
		field, message           string
	}{
		{"valid", "photo.png", "image/png", nil, "", ""},
		{"extension compared lowercased", "PHOTO.PNG", "image/png", nil, "", ""},
		{"type parameters dropped", "photo.jpg", "image/JPEG; charset=binary", nil, "", ""},
		{"allowed by the element", "doc.pdf", "application/pdf", []string{"png", "pdf"}, "", ""},
		{"no extension", "README", "text/plain", nil, "fileName", "File name must have an extension"},
		{"trailing dot", "photo.", "image/png", nil, "fileName", "File name must have an extension"},
		{"not allowed", "page.html", "text/html", nil, "fileName", "File extension is not allowed for security reasons"},
		{"svg is not allowed", "logo.svg", "image/svg+xml", nil, "fileName", "File extension is not allowed for security reasons"},
		{"type mismatch", "photo.png", "image/jpeg", nil, "fileType", "File type doesn't match the file extension"},
		{"type spoofed as html", "photo.png", "text/html", nil, "fileType", "File type doesn't match the file extension"},
		{"space before parameters", "photo.png", "image/png ; x=y", nil, "fileType", "File type doesn't match the file extension"},
		{"not allowed by the element", "photo.png", "image/png", []string{"pdf", "jpg"}, "fileName", "File extension is not allowed, allowed extensions are: pdf, jpg"},
		{"type checked before the element's list", "photo.png", "image/gif", []string{"pdf"}, "fileType", "File type doesn't match the file extension"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := ValidateUploadInput(tc.fileName, tc.fileType, tc.allowed)
			if tc.field == "" {
				if issue != nil {
					t.Fatalf("refused: %+v", issue)
				}
				return
			}
			if issue == nil || issue.Field != tc.field || issue.Message != tc.message {
				t.Fatalf("issue = %+v, want %s: %q", issue, tc.field, tc.message)
			}
		})
	}
}

func TestAllowedExtensions(t *testing.T) {
	if len(AllowedExtensions) != 26 || len(mimeTypes) != len(AllowedExtensions) {
		t.Fatalf("%d extensions with %d MIME types, want forma2's 26", len(AllowedExtensions), len(mimeTypes))
	}
	for _, ext := range AllowedExtensions {
		if !IsAllowedExtension(ext) || mimeTypes[ext] == "" {
			t.Errorf("%s has no MIME type", ext)
		}
	}
	for _, ext := range []string{"svg", "html", "js", "exe", "PNG", ""} {
		if IsAllowedExtension(ext) {
			t.Errorf("%q is allowed", ext)
		}
	}
	if mimeTypes["jpg"] != "image/jpeg" || mimeTypes["7z"] != "application/x-7z-compressed" || mimeTypes["eml"] != "message/rfc822" {
		t.Error("MIME types differ from forma2's")
	}
	if !slices.Equal(AllowedExtensions[:5], []string{"heic", "png", "jpeg", "jpg", "webp"}) {
		t.Errorf("order %v differs from forma2's", AllowedExtensions[:5])
	}
}

func TestRespondentUploadMaxSize(t *testing.T) {
	mb := func(v float64) *float64 { return &v }
	cases := []struct {
		in   *float64
		want int64
	}{
		{mb(5), 5 * 1024 * 1024},
		{mb(25), 25 * 1024 * 1024},
		{mb(0.5), 512 * 1024},
		{mb(0.0000001), 1},
		{mb(4096), MaxFileUploadSize},
		{mb(1024), MaxFileUploadSize},
		{mb(0), MaxFileUploadSize},
		{mb(-3), MaxFileUploadSize},
		{nil, MaxFileUploadSize},
	}
	for _, tc := range cases {
		if got := RespondentUploadMaxSize(tc.in); got != tc.want {
			t.Errorf("RespondentUploadMaxSize(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
	if MaxFileUploadSize != 1024*1024*1024 || PublicUploadMaxSize != 5*1024*1024 {
		t.Error("limits differ from forma2's")
	}
}

func TestExtension(t *testing.T) {
	for in, want := range map[string]string{"a.PNG": "png", "a.b.c": "c", "a": "", "a.": "", ".env": "env"} {
		if got := Extension(in); got != want {
			t.Errorf("Extension(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncodeURIComponent(t *testing.T) {
	for in, want := range map[string]string{
		"invoice 100%.pdf":   "invoice%20100%25.pdf",
		"résumé.pdf":         "r%C3%A9sum%C3%A9.pdf",
		"a+b&c=d/e?f#g":      "a%2Bb%26c%3Dd%2Fe%3Ff%23g",
		"-_.!~*'()":          "-_.!~*'()",
		"report--fid--1.pdf": "report--fid--1.pdf",
	} {
		if got := encodeURIComponent(in); got != want {
			t.Errorf("encodeURIComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHasTraversalSegment(t *testing.T) {
	for name, want := range map[string]bool{
		"../../ws-victim/private/secret.pdf":         true,
		"sub/../../../ws-victim/private/secret.pdf":  true,
		"%2e%2e/%2e%2e/ws-victim/private/secret.pdf": true,
		"%2E%2E/secret.pdf":                          true,
		"./secret.pdf":                               true,
		"a/%2e/b.pdf":                                true,
		"survey-1/q-2/file.jpg":                      false,
		"invoice 100%.pdf":                           false,
		"..a/b.pdf":                                  false,
		"%252e%252e/x.pdf":                           false,
		"a..b.pdf":                                   false,
	} {
		if got := hasTraversalSegment(name); got != want {
			t.Errorf("hasTraversalSegment(%q) = %v, want %v", name, got, want)
		}
	}
}
