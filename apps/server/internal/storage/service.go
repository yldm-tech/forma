package storage

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// AccessType is who may read a stored file: anyone (public), or whoever may read its workspace (private).
type AccessType string

const (
	AccessPublic  AccessType = "public"
	AccessPrivate AccessType = "private"
)

// safePathSegment is what a directory under the access type may be named, so a caller-chosen survey or element id can never add a path of its own.
var safePathSegment = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Service is forma2's apps/web/modules/storage service: the path layout of stored files. A file lives at <workspace id>/<access type>/<segments...>/<name>--fid--<uuid>.<ext> in the bucket and is served at /storage/ followed by the same path, its segments and name URL-encoded. Files uploaded before the workspace rename live under the environment id instead, and files migrated from forma2 under forma2's own ids, which is why reads and deletes also try the id the URL carried.
type Service struct {
	bucket *Bucket
	newID  func() uuid.UUID
}

func NewService(bucket *Bucket) *Service {
	return &Service{bucket: bucket, newID: uuid.New}
}

// Configured is forma2's IS_STORAGE_CONFIGURED.
func (s *Service) Configured() bool { return s.bucket.Configured() }

// Upload is a signed upload: the client POSTs PresignedFields and the file to SignedURL, then keeps FileURL, the relative URL the file is served at. When UploadMethod is "put" the client PUTs the file to UploadURL with UploadHeaders instead; the three are left out otherwise, so an answer without them is a POST upload, as every answer before them was.
type Upload struct {
	SignedURL       string            `json:"signedUrl"`
	PresignedFields PresignedFields   `json:"presignedFields"`
	FileURL         string            `json:"fileUrl"`
	UploadMethod    string            `json:"uploadMethod,omitempty"`
	UploadURL       string            `json:"uploadUrl,omitempty"`
	UploadHeaders   map[string]string `json:"uploadHeaders,omitempty"`
}

// SignUpload is forma2's getSignedUrlForUpload. The file name is sanitized and made unique with a --fid--<uuid> suffix before its extension; segments are the directories between the access type and the file (a respondent's upload goes under surveys/<survey id>/elements/<element id>). A name with nothing left after sanitizing, without an allowed extension, or a segment that is not letters, digits, hyphens and underscores, is invalid_input.
//
// fileSize is the size the client says the file has, nil when it did not say. A size over maxSize (when maxSize is not zero) is file_too_large, before anything is signed. With STORAGE_UPLOAD_METHOD=put and a size, the upload is also signed as a presigned PUT of exactly that size and the answer tells the client to use it; without a size only the POST is signed, which a client that predates fileSize knows.
//
// The file is stored with its extension's own content type, never the type the caller sent: forma2 signed the caller's string, so a respondent could store "text/plain;,text/html" on a .txt file, which passes the type check (it reads only what comes before the first semicolon) while browsers, which take the last type of a comma-separated list, render it as HTML on the admin app's origin.
func (s *Service) SignUpload(ctx context.Context, fileName, workspaceID string, access AccessType, maxSize int64, fileSize *int64, segments ...string) (*Upload, error) {
	safeName := SanitizeFileName(fileName)
	contentType, ok := mimeTypes[Extension(safeName)]
	if safeName == "" || !ok {
		return nil, fail(CodeInvalidInput)
	}
	for _, segment := range segments {
		if !safePathSegment.MatchString(segment) {
			return nil, fail(CodeInvalidInput)
		}
	}
	if fileSize != nil {
		if *fileSize < 0 {
			return nil, fail(CodeInvalidInput)
		}
		if maxSize != 0 && *fileSize > maxSize {
			return nil, fail(CodeFileTooLarge)
		}
	}
	base, extension := "", safeName
	if i := strings.LastIndex(safeName, "."); i >= 0 {
		base, extension = safeName[:i], safeName[i+1:]
	}
	storedName := base + "--fid--" + s.newID().String() + "." + extension

	filePath := strings.Join(append([]string{workspaceID, string(access)}, segments...), "/")
	post, err := s.bucket.SignUpload(ctx, storedName, contentType, filePath, maxSize)
	if err != nil {
		return nil, err
	}
	urlPath := make([]string, 0, len(segments)+1)
	for _, segment := range segments {
		urlPath = append(urlPath, encodeURIComponent(segment))
	}
	urlPath = append(urlPath, encodeURIComponent(storedName))
	upload := &Upload{
		SignedURL:       post.URL,
		PresignedFields: post.Fields,
		FileURL:         "/storage/" + workspaceID + "/" + string(access) + "/" + strings.Join(urlPath, "/"),
	}
	if fileSize != nil && s.bucket.PutUploads() {
		put, err := s.bucket.SignPut(ctx, storedName, contentType, filePath, *fileSize)
		if err != nil {
			return nil, err
		}
		upload.UploadMethod, upload.UploadURL, upload.UploadHeaders = config.UploadMethodPut, put.URL, put.Headers
	}
	return upload, nil
}

// Open is forma2's getFileStreamForDownload: the file at fileName (the path after the access type, decoded once, as the route received it) under workspaceID, else, when it is not there, under rawID, the id the URL carried. A path with a . or .. segment is invalid_input, so a caller authorized for one workspace's prefix can never name another's object.
func (s *Service) Open(ctx context.Context, fileName, workspaceID, rawID string, access AccessType) (*Object, error) {
	if hasTraversalSegment(fileName) {
		return nil, fail(CodeInvalidInput)
	}
	obj, err := s.bucket.Open(ctx, key(workspaceID, access, fileName))
	if CodeOf(err) == CodeFileNotFound && rawID != workspaceID {
		return s.bucket.Open(ctx, key(rawID, access, fileName))
	}
	return obj, err
}

// Delete is forma2's deleteFile, with the same traversal guard as Open. forma2 fell back to the URL's own id only when the first delete reported the file missing, which S3 never does, so a file stored under an environment id or a migrated forma2 id was never deleted; here both keys are deleted.
func (s *Service) Delete(ctx context.Context, fileName, workspaceID, rawID string, access AccessType) error {
	if hasTraversalSegment(fileName) {
		return fail(CodeInvalidInput)
	}
	if err := s.bucket.Delete(ctx, key(workspaceID, access, fileName)); err != nil {
		return err
	}
	if rawID != workspaceID {
		return s.bucket.Delete(ctx, key(rawID, access, fileName))
	}
	return nil
}

// DeleteWorkspaceFiles is forma2's deleteFilesByWorkspaceId: every file stored under each of ids, the deleted workspace's own id and the older ids its files may live under. Each prefix ends in a slash so another workspace whose id starts with the same characters is never touched. It does nothing when storage is not configured, since nothing can have been stored, and returns the first failure after trying every id.
func (s *Service) DeleteWorkspaceFiles(ctx context.Context, ids ...string) error {
	if !s.Configured() {
		return nil
	}
	var first error
	for _, id := range ids {
		if err := s.bucket.DeleteByPrefix(ctx, id+"/"); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func key(id string, access AccessType, fileName string) string {
	return id + "/" + string(access) + "/" + fileName
}

// hasTraversalSegment reports a . or .. segment, as given or after one more URL decoding: the route receives the path decoded once, so %252e%252e arrives as %2e%2e. A segment whose escapes are malformed (100%) is not a dot segment.
func hasTraversalSegment(fileName string) bool {
	for segment := range strings.SplitSeq(fileName, "/") {
		if segment == "." || segment == ".." {
			return true
		}
		if decoded, err := url.PathUnescape(segment); err == nil && (decoded == "." || decoded == "..") {
			return true
		}
	}
	return false
}

// encodeURIComponent is JavaScript's: everything but letters, digits and -_.!~*'() is percent-encoded as UTF-8.
func encodeURIComponent(s string) string {
	return strings.NewReplacer("+", "%20", "%21", "!", "%27", "'", "%28", "(", "%29", ")", "%2A", "*").Replace(url.QueryEscape(s))
}
