package storage

import (
	"math"
	"slices"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"
)

// Size limits, forma2's.
const (
	// MaxFileUploadSize is MAX_FILE_UPLOAD_SIZE_BYTES, the ceiling of any upload: 1 GiB.
	MaxFileUploadSize int64 = 1 << 30
	// PublicUploadMaxSize caps an admin upload to a workspace's public files (logos, survey images and backgrounds): 5 MiB.
	PublicUploadMaxSize int64 = 5 << 20
)

// RespondentUploadMaxSize is the limit a respondent's upload to a file-upload element is signed with: the size the element advertises when it is a positive number of MB, capped at MaxFileUploadSize, else MaxFileUploadSize itself. The advertised size is otherwise checked only in the respondent's browser, so the signature is what enforces it.
func RespondentUploadMaxSize(maxSizeInMB *float64) int64 {
	if maxSizeInMB == nil || !(*maxSizeInMB > 0) {
		return MaxFileUploadSize
	}
	size := math.Ceil(*maxSizeInMB * 1024 * 1024)
	if size >= float64(MaxFileUploadSize) {
		return MaxFileUploadSize
	}
	return int64(size)
}

// AllowedExtensions are the only file extensions that may be uploaded (ALLOWED_FILE_EXTENSIONS), in forma2's order.
var AllowedExtensions = []string{
	"heic", "png", "jpeg", "jpg", "webp", "ico", "pdf", "eml", "doc", "docx", "xls", "xlsx", "ppt", "pptx",
	"txt", "csv", "mp4", "mov", "avi", "mkv", "webm", "mp3", "zip", "rar", "7z", "tar",
}

// mimeTypes is the content type each allowed extension must be uploaded with (mimeTypes in packages/types/storage.ts); its keys are AllowedExtensions.
var mimeTypes = map[string]string{
	"heic": "image/heic",
	"png":  "image/png",
	"jpeg": "image/jpeg",
	"jpg":  "image/jpeg",
	"webp": "image/webp",
	"ico":  "image/x-icon",
	"pdf":  "application/pdf",
	"eml":  "message/rfc822",
	"doc":  "application/msword",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xls":  "application/vnd.ms-excel",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"ppt":  "application/vnd.ms-powerpoint",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"txt":  "text/plain",
	"csv":  "text/csv",
	"mp4":  "video/mp4",
	"mov":  "video/quicktime",
	"avi":  "video/x-msvideo",
	"mkv":  "video/x-matroska",
	"webm": "video/webm",
	"mp3":  "audio/mpeg",
	"zip":  "application/zip",
	"rar":  "application/vnd.rar",
	"7z":   "application/x-7z-compressed",
	"tar":  "application/x-tar",
}

// IsAllowedExtension reports whether extension (lowercase, without the dot) may be uploaded.
func IsAllowedExtension(extension string) bool {
	_, ok := mimeTypes[extension]
	return ok
}

// Extension is forma2's extractFileExtension: the lowercased text after the last dot, or "" when the name has no dot or nothing after it.
func Extension(fileName string) string {
	i := strings.LastIndex(fileName, ".")
	if i < 0 {
		return ""
	}
	return strings.ToLower(fileName[i+1:])
}

// UploadIssue is the first upload rule a file failed: the request field it is reported on and forma2's message.
type UploadIssue struct {
	Field   string
	Message string
}

// ValidateUploadInput is forma2's refineFileUploadInput, run on the trimmed file name and type: the name needs an extension, the extension must be allowed, the type (lowercased, without parameters) must be the extension's, and the extension must be one of allowedExtensions when that list is not empty. It returns the first rule that fails, or nil.
func ValidateUploadInput(fileName, fileType string, allowedExtensions []string) *UploadIssue {
	extension := Extension(fileName)
	if extension == "" || extension == strings.ToLower(fileName) {
		return &UploadIssue{Field: "fileName", Message: "File name must have an extension"}
	}
	if !IsAllowedExtension(extension) {
		return &UploadIssue{Field: "fileName", Message: "File extension is not allowed for security reasons"}
	}
	// "image/jpeg; charset=binary" is checked as "image/jpeg"; the type stored with the file stays as sent.
	normalized, _, _ := strings.Cut(strings.ToLower(fileType), ";")
	if normalized != mimeTypes[extension] {
		return &UploadIssue{Field: "fileType", Message: "File type doesn't match the file extension"}
	}
	if len(allowedExtensions) > 0 && !slices.Contains(allowedExtensions, extension) {
		return &UploadIssue{Field: "fileName", Message: "File extension is not allowed, allowed extensions are: " + strings.Join(allowedExtensions, ", ")}
	}
	return nil
}

const (
	maxBaseLength      = 200
	maxExtensionLength = 20
)

// SanitizeFileName is forma2's sanitizeFileName: the name in NFC with path separators turned into hyphens, the characters that break S3 form fields or URLs (# < > : " | ? * ` ') removed, whitespace collapsed and trimmed, the base cut to 200 and the extension to 20 UTF-16 units, and the extension reduced to ASCII letters and digits. It returns "" for a name with nothing left to keep. Non-ASCII letters and % are kept.
func SanitizeFileName(raw string) string {
	if raw == "" {
		return ""
	}
	name := norm.NFC.String(raw)
	name = strings.NewReplacer("/", "-", `\`, "-").Replace(name)
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune("#<>:\"|?*`'", r) {
			return -1
		}
		return r
	}, name)
	name = jsTrim(collapseSpace(name))

	parts := strings.Split(name, ".")
	extension := ""
	if len(parts) > 1 {
		extension = parts[len(parts)-1]
		parts = parts[:len(parts)-1]
	}
	base := jsTrim(strings.Join(parts, "."))
	if base == "" || only(base, '-') || only(base, '.') {
		return ""
	}
	base = truncateUTF16(base, maxBaseLength)
	extension = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, truncateUTF16(extension, maxExtensionLength))

	result := base
	if extension != "" {
		result += "." + extension
	}
	if result == "" || only(result, '.') || only(result, '-') {
		return ""
	}
	return result
}

// only reports whether s is one or more of c.
func only(s string, c rune) bool {
	return s != "" && strings.Trim(s, string(c)) == ""
}

// isJSSpace is JavaScript's \s, which is also what String.prototype.trim removes.
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// collapseSpace replaces every run of whitespace with one space.
func collapseSpace(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if isJSSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// truncateUTF16 keeps the first n UTF-16 code units of s, JavaScript's String.prototype.slice(0, n). A character that would be cut in half is dropped rather than leaving a lone surrogate.
func truncateUTF16(s string, n int) string {
	units := 0
	for i, r := range s {
		units += utf16.RuneLen(r)
		if units > n {
			return s[:i]
		}
	}
	return s
}
