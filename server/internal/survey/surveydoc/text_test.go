package surveydoc

import "testing"

// The cases of forma2's html-text.test.ts.
func TestTextContent(t *testing.T) {
	cases := []struct {
		label, input, text string
		isHTML             bool
	}{
		{"plain text", "How satisfied are you?", "How satisfied are you?", false},
		{"a wrapped headline", "<p>How satisfied are you?</p>", "How satisfied are you?", true},
		{"nested markup", "<p>Rate <strong>our</strong> support</p>", "Rate our support", true},
		{"an entity", "<p>Tom &amp; Jerry</p>", "Tom & Jerry", true},
		{"a bare entity in plain text", "Tom &amp; Jerry", "Tom &amp; Jerry", false},
		{"surrounding whitespace", "  <p> padded </p>  ", "padded", true},
		{"an empty string", "", "", false},
		{"only whitespace", "   ", "", false},
		{"a line break", "<p>line<br/>break</p>", "line\nbreak", true},
		{"an attribute that looks like a tag", `<p title="<b>">x</p>`, "x", true},
		{"multiple siblings", "<p>a</p><p>b</p>", "ab", true},
		{"empty markup", "<p><br></p>", "", true},
	}
	for _, tc := range cases {
		if got := TextContent(tc.input); got != tc.text {
			t.Errorf("TextContent(%s) = %q, want %q", tc.label, got, tc.text)
		}
		if got := IsValidHTML(tc.input); got != tc.isHTML {
			t.Errorf("IsValidHTML(%s) = %v, want %v", tc.label, got, tc.isHTML)
		}
	}
}

func TestURLRules(t *testing.T) {
	storage := map[string]bool{
		"/storage/ws/public/a.png": true, "https://cdn.example.com/a.png": true, "http://x.io/a": true,
		"javascript:alert(1)": false, "data:image/png;base64,AA": false, "not a url": false, "http://": false,
	}
	for in, want := range storage {
		if got := IsStorageURL(in); got != want {
			t.Errorf("IsStorageURL(%q) = %v", in, got)
		}
	}
	safeLink := map[string]bool{
		"https://example.com": true, "http://example.com": true, "mailto:a@b.co": true, "tel:+123": true,
		"javascript:alert(1)": false, "java\tscript:alert(1)": false, "example.com": false, " https://example.com ": true,
	}
	for in, want := range safeLink {
		if got := IsSafeLinkURL(in); got != want {
			t.Errorf("IsSafeLinkURL(%q) = %v", in, got)
		}
	}
	safe := map[string]string{
		"https://example.com":                         "",
		"https://example.com/#recall:abc/fallback:x#": "",
		"http://localhost:3000/x":                     "",
		"mailto:hi@example.com":                       "",
		"https://[::1]/":                              "",
		"https://192.168.0.1/":                        "",
		"http://example.com":                          "URL must start with https:// or mailto:",
		"tel:+123":                                    "URL must start with https:// or mailto:",
		"https://exa mple.com":                        "URL must not contain spaces",
		"#recall:x":                                   "URL must not start with a recall value",
		"https://example":                             "URL is not valid",
		"https://www.com":                             "URL is not valid",
		"https://ex_ample.com":                        "URL is not valid",
		"https://example.c":                           "URL is not valid",
	}
	for in, want := range safe {
		if got := SafeURLIssue(in); got != want {
			t.Errorf("SafeURLIssue(%q) = %q, want %q", in, got, want)
		}
	}
	if EndingCardURLIssue(" https://x.com/#recall:a ") != "" || EndingCardURLIssue("ftp://x") == "" {
		t.Error("EndingCardURLIssue")
	}
	images := map[string]bool{
		"/storage/ws/public/photo--fid--abc.png": true, "https://cdn.example.com/a/b.JPG?x=1": true,
		"https://images.unsplash.com/photo-123?auto=format": false, "https://x.com/a.svg": false, "https://x.com/a.": false,
		"https://x.com/png": true,
	}
	for in, want := range images {
		if got := IsValidImageFile(in); got != want {
			t.Errorf("IsValidImageFile(%q) = %v", in, got)
		}
	}
	videos := map[string]bool{
		"https://www.youtube.com/watch?v=x": true, "https://youtu.be/x": true, "https://vimeo.com/1": true, "https://www.loom.com/share/x": true,
		"http://youtube.com/watch?v=x": false, "https://example.com/v.mp4": false,
	}
	for in, want := range videos {
		if got := IsValidVideoURL(in); got != want {
			t.Errorf("IsValidVideoURL(%q) = %v", in, got)
		}
	}
}
