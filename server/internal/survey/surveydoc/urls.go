package surveydoc

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// parseAbsolute parses an absolute URL the way the WHATWG parser accepts one closely enough for these checks: a scheme is required, and http and https need a host.
func parseAbsolute(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return nil, false
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme == "http" || scheme == "https") && u.Host == "" {
		return nil, false
	}
	u.Scheme = scheme
	return u, true
}

// IsStorageURL is forma2's ZStorageUrl: a relative storage path or an http(s) URL. Other schemes (javascript:, data:) are refused because these values are rendered into src and href attributes.
func IsStorageURL(s string) bool {
	if strings.HasPrefix(s, "/storage/") {
		return true
	}
	u, ok := parseAbsolute(s)
	return ok && (u.Scheme == "http" || u.Scheme == "https")
}

// StorageURLMessage is ZStorageUrl's error.
const StorageURLMessage = "Must be a valid http(s) URL or a relative storage path (/storage/...)"

// IsSafeLinkURL is forma2's isSafeLinkUrl: an http(s), mailto or tel link, safe to hand to window.open.
func IsSafeLinkURL(s string) bool {
	u, ok := parseAbsolute(trimJS(s))
	if !ok {
		return false
	}
	switch u.Scheme {
	case "http", "https", "mailto", "tel":
		return true
	}
	return false
}

// EndingCardURLIssue is forma2's ZEndingCardUrl refinement: the trimmed URL must start with http:// or https://. Recall tokens may follow the scheme.
func EndingCardURLIssue(s string) string {
	t := trimJS(s)
	if !strings.HasPrefix(t, "http://") && !strings.HasPrefix(t, "https://") {
		return "URL must start with http:// or https://"
	}
	return ""
}

var domainPart = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
var ipv6Literal = regexp.MustCompile(`^[0-9a-fA-F:]+$`)

// SafeURLIssue is the first issue forma2's safeUrlRefinement (getZSafeUrl) reports for a CTA button URL, or "" when it passes: https, http://localhost or mailto, no spaces, no recall token in the host, and a plausible host.
func SafeURLIssue(raw string) string {
	if strings.Contains(raw, " ") {
		return "URL must not contain spaces"
	}
	if strings.HasPrefix(raw, "#recall:") {
		return "URL must not start with a recall value"
	}
	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://localhost") && !strings.HasPrefix(raw, "mailto:") {
		return "URL must start with https:// or mailto:"
	}
	if strings.HasPrefix(raw, "mailto:") {
		u, ok := parseAbsolute(raw)
		if !ok || u.Scheme != "mailto" {
			return "Invalid mailto URL format"
		}
		return ""
	}
	u, ok := parseAbsolute(raw)
	if !ok {
		return "URL is not valid"
	}
	hostname := strings.ToLower(u.Hostname())
	if strings.Contains(u.Host, "#recall:") || strings.Contains(hostname, "#recall:") {
		return "Recall information must appear after the domain, not within it"
	}
	if hostname == "" || hostname == "localhost" {
		return ""
	}
	if strings.HasPrefix(u.Host, "[") {
		if hostname == "::1" || hostname == "::" || ipv6Literal.MatchString(hostname) {
			return ""
		}
	}
	const invalid = "URL is not valid"
	if !strings.Contains(hostname, ".") {
		return invalid
	}
	parts := strings.Split(hostname, ".")
	if len(parts) == 4 && isIPv4(parts) {
		return ""
	}
	tld := parts[len(parts)-1]
	if len(tld) < 2 || len(tld) > 63 || !domainPart.MatchString(tld) || strings.HasPrefix(tld, "-") || strings.HasSuffix(tld, "-") {
		return invalid
	}
	if strings.EqualFold(parts[0], "www") && len(parts) < 3 {
		return invalid
	}
	for _, p := range parts {
		if p == "" || !domainPart.MatchString(p) || strings.HasPrefix(p, "-") || strings.HasSuffix(p, "-") {
			return invalid
		}
	}
	return ""
}

func isIPv4(parts []string) bool {
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 || strconv.Itoa(n) != p {
			return false
		}
	}
	return true
}

// ImageFileExtensions are forma2's IMAGE_FILE_EXTENSIONS; svg is excluded because it is not sanitized.
var ImageFileExtensions = []string{"png", "jpeg", "jpg", "webp", "heic"}

// originalFileName is forma2's getOriginalFileNameFromUrl: the last path segment, with the "--fid--" storage suffix removed.
func originalFileName(fileURL string) string {
	var last string
	if strings.HasPrefix(fileURL, "/storage/") {
		segments := strings.Split(fileURL, "/")
		last = segments[len(segments)-1]
	} else {
		u, ok := parseAbsolute(fileURL)
		if !ok {
			return ""
		}
		segments := strings.Split(u.EscapedPath(), "/")
		last = segments[len(segments)-1]
	}
	if i := strings.IndexAny(last, "?#"); i >= 0 {
		last = last[:i]
	}
	name, fid, found := strings.Cut(last, "--fid--")
	if !found || fid == "" {
		decoded, err := url.PathUnescape(name)
		if err != nil {
			return ""
		}
		return decoded
	}
	dot := strings.LastIndex(last, ".")
	ext := ""
	if dot > strings.Index(last, "--fid--") {
		ext = last[dot+1:]
	}
	full := name
	if ext != "" {
		full = name + "." + ext
	}
	decoded, err := url.PathUnescape(full)
	if err != nil {
		return ""
	}
	return decoded
}

// IsValidImageFile is forma2's isValidImageFile: the file name ends in a supported image extension.
func IsValidImageFile(fileURL string) bool {
	name := originalFileName(fileURL)
	if name == "" || strings.HasSuffix(name, ".") {
		return false
	}
	// Like forma2's split(".").pop(), a name without a dot is its own extension.
	ext := name[strings.LastIndex(name, ".")+1:]
	return slices.Contains(ImageFileExtensions, strings.ToLower(ext))
}

// IsValidVideoURL is forma2's isValidVideoUrl: an https YouTube, Vimeo or Loom URL.
func IsValidVideoURL(raw string) bool {
	u, ok := parseAbsolute(raw)
	if !ok || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return slices.Contains([]string{
		"www.youtube.com", "www.youtu.be", "www.youtube-nocookie.com", "youtube.com", "youtu.be", "youtube-nocookie.com",
		"www.vimeo.com", "vimeo.com", "player.vimeo.com",
		"www.loom.com", "loom.com",
	}, host)
}
