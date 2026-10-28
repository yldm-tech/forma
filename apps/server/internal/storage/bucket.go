// Package storage is forma2's file storage: the S3 bucket uploaded files live in (packages/storage), the naming and path layout of those files and the rules for what may be uploaded (apps/web/modules/storage, packages/types/storage.ts), and the /storage route that serves and deletes them.
package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Code is forma2's StorageErrorCode. Clients read the two configuration codes from storage_error_code to tell an unconfigured instance from a failed upload.
type Code string

const (
	CodeUnknown       Code = "unknown"
	CodeS3Credentials Code = "s3_credentials_error"
	CodeS3Client      Code = "s3_client_error"
	CodeFileNotFound  Code = "file_not_found_error"
	CodeInvalidInput  Code = "invalid_input"
	// CodeFileTooLarge is an upload whose declared size is over its limit.
	CodeFileTooLarge Code = "file_too_large"
)

const (
	uploadExpiry        = 2 * time.Minute
	downloadExpiry      = 30 * time.Minute
	deleteBatchSize     = 1000
	defaultContentType  = "application/octet-stream"
	successActionStatus = "201"
)

// Error is a storage operation's expected failure. An unknown failure keeps its cause for the log.
type Error struct {
	Code Code
	err  error
}

func (e *Error) Error() string {
	if e.err != nil {
		return string(e.Code) + ": " + e.err.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.err }

func fail(code Code) error { return &Error{Code: code} }

func unknown(op string, err error) error {
	return &Error{Code: CodeUnknown, err: fmt.Errorf("%s: %w", op, err)}
}

// CodeOf is the storage code of err: its own for an *Error, unknown for anything else.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeUnknown
}

// Bucket is the S3 bucket of forma2's S3_* settings (packages/storage). Without a bucket name there is no client, and every operation fails with s3_client_error, which is what forma2 answered on an instance without storage.
type Bucket struct {
	client    *minio.Client
	name      string
	pathStyle bool
	// putUploads is STORAGE_UPLOAD_METHOD=put: uploads whose size is known are also signed as a presigned PUT, and clients are told to use it.
	putUploads bool
	log        *slog.Logger
}

// NewBucket builds the client of cfg.S3. Like forma2's it passes the region only when one is set, and static keys only when both are set; otherwise the AWS default credential chain applies.
func NewBucket(cfg config.Config, log *slog.Logger) (*Bucket, error) {
	s3 := cfg.S3
	b := &Bucket{name: s3.BucketName, pathStyle: s3.ForcePathStyle, putUploads: s3.UploadMethod == config.UploadMethodPut, log: log}
	if !s3.Configured() {
		return b, nil
	}
	endpoint, secure := "s3.amazonaws.com", true
	if s3.EndpointURL != nil {
		endpoint, secure = s3.EndpointURL.Host, s3.EndpointURL.Scheme == "https"
	}
	creds := credentials.NewChainCredentials([]credentials.Provider{&credentials.EnvAWS{}, &credentials.FileAWSCredentials{}, &credentials.IAM{}})
	if s3.AccessKey != "" && s3.SecretKey != "" {
		creds = credentials.NewStaticV4(s3.AccessKey, s3.SecretKey, "")
	}
	lookup := minio.BucketLookupDNS
	if s3.ForcePathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{Creds: creds, Secure: secure, Region: s3.Region, BucketLookup: lookup})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}
	b.client = client
	return b, nil
}

// Configured is forma2's IS_STORAGE_CONFIGURED.
func (b *Bucket) Configured() bool { return b.client != nil }

// PresignedPost is a browser upload straight to the bucket: a multipart form POSTed to URL with every field of Fields, then the file.
type PresignedPost struct {
	URL    string
	Fields PresignedFields
}

// PresignedFields are the form fields of a presigned POST, kept in the order and with the names the AWS SDK's createPresignedPost returned them, so the client API's JSON matches forma2's.
type PresignedFields []Field

type Field struct{ Name, Value string }

// MarshalJSON writes the fields as one JSON object in their order.
func (f PresignedFields) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	buf.WriteByte('{')
	for i, field := range f {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := enc.Encode(field.Name); err != nil {
			return nil, err
		}
		buf.Truncate(buf.Len() - 1)
		buf.WriteByte(':')
		if err := enc.Encode(field.Value); err != nil {
			return nil, err
		}
		buf.Truncate(buf.Len() - 1)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// sdkFieldNames maps minio-go's form field names to the AWS SDK's, in the SDK's order: the caller's fields, then bucket and the signing fields, then key, policy and signature. S3 reads form field names case-insensitively, so only the JSON the client receives changes.
var sdkFieldNames = []struct{ minio, sdk string }{
	{"Content-Type", "Content-Type"},
	{"success_action_status", "success_action_status"},
	{"bucket", "bucket"},
	{"x-amz-algorithm", "X-Amz-Algorithm"},
	{"x-amz-credential", "X-Amz-Credential"},
	{"x-amz-date", "X-Amz-Date"},
	{"x-amz-security-token", "X-Amz-Security-Token"},
	{"key", "key"},
	{"policy", "Policy"},
	{"x-amz-signature", "X-Amz-Signature"},
}

// SignUpload is forma2's getSignedUploadUrl: a POST policy valid for two minutes that stores one object at filePath/fileName with contentType, answers 201, and, when maxSize is not zero, refuses a body over maxSize bytes (S3 answers EntityTooLarge, which the survey runtime reports as over the plan's limit).
func (b *Bucket) SignUpload(ctx context.Context, fileName, contentType, filePath string, maxSize int64) (*PresignedPost, error) {
	if b.client == nil {
		b.log.ErrorContext(ctx, "Failed to get signed upload URL: S3 client is not set")
		return nil, fail(CodeS3Client)
	}
	policy := minio.NewPostPolicy()
	setters := []func() error{
		func() error { return policy.SetExpires(time.Now().UTC().Add(uploadExpiry)) },
		func() error { return policy.SetBucket(b.name) },
		func() error { return policy.SetKey(filePath + "/" + fileName) },
		func() error { return policy.SetContentType(contentType) },
		func() error { return policy.SetSuccessStatusAction(successActionStatus) },
	}
	if maxSize != 0 {
		setters = append(setters, func() error { return policy.SetContentLengthRange(0, maxSize) })
	}
	for _, set := range setters {
		if err := set(); err != nil {
			return nil, unknown("build upload policy", err)
		}
	}
	u, form, err := b.client.PresignedPostPolicy(ctx, policy)
	if err != nil {
		return nil, unknown("sign upload policy", err)
	}
	fields := make(PresignedFields, 0, len(sdkFieldNames))
	for _, name := range sdkFieldNames {
		if v, ok := form[name.minio]; ok {
			fields = append(fields, Field{Name: name.sdk, Value: v})
		}
	}
	signedURL := u.String()
	// The AWS SDK addresses a path-style bucket as <endpoint>/<bucket>; minio-go adds a trailing slash.
	if b.pathStyle {
		signedURL = strings.TrimSuffix(signedURL, "/")
	}
	return &PresignedPost{URL: signedURL, Fields: fields}, nil
}

// PresignedPut is a browser upload straight to the bucket: one PUT to URL with every header of Headers and a body of exactly the size it was signed for.
type PresignedPut struct {
	URL string
	// Headers are the signed headers the client sets itself. Content-Length is signed too, but a browser sets it from the body and refuses to have it set, so it is not among them.
	Headers map[string]string
}

// SignPut is the presigned PUT of the same upload SignUpload signs as a POST policy, for stores that take no POST uploads (Cloudflare R2): valid for two minutes, for one object at filePath/fileName, with contentType and a Content-Length of exactly size bytes both signed, so the store refuses another type or another size (S3 answers 403 SignatureDoesNotMatch). Nothing grants public access: the object is private like every other, and is read through the /storage route.
func (b *Bucket) SignPut(ctx context.Context, fileName, contentType, filePath string, size int64) (*PresignedPut, error) {
	if b.client == nil {
		b.log.ErrorContext(ctx, "Failed to get signed upload URL: S3 client is not set")
		return nil, fail(CodeS3Client)
	}
	if size < 0 {
		return nil, fail(CodeInvalidInput)
	}
	signed := http.Header{}
	signed.Set("Content-Type", contentType)
	signed.Set("Content-Length", strconv.FormatInt(size, 10))
	u, err := b.client.PresignHeader(ctx, http.MethodPut, b.name, filePath+"/"+fileName, uploadExpiry, nil, signed)
	if err != nil {
		return nil, unknown("sign upload URL", err)
	}
	return &PresignedPut{URL: u.String(), Headers: map[string]string{"Content-Type": contentType}}, nil
}

// PutUploads reports STORAGE_UPLOAD_METHOD=put.
func (b *Bucket) PutUploads() bool { return b.putUploads }

// SignDownload is forma2's getSignedDownloadUrl: a GET URL valid for 30 minutes. A key that does not exist is file_not_found_error; any other failure of the existence check is logged and the URL signed anyway.
func (b *Bucket) SignDownload(ctx context.Context, key string) (string, error) {
	if b.client == nil {
		return "", fail(CodeS3Client)
	}
	if _, err := b.client.StatObject(ctx, b.name, key, minio.StatObjectOptions{}); err != nil {
		if notFound(err) {
			return "", fail(CodeFileNotFound)
		}
		b.log.WarnContext(ctx, "HeadObject check failed; proceeding to sign download URL", "err", err)
	}
	u, err := b.client.PresignedGetObject(ctx, b.name, key, downloadExpiry, nil)
	if err != nil {
		return "", unknown("sign download URL", err)
	}
	return u.String(), nil
}

// Object is a stored file being read.
type Object struct {
	Body io.ReadCloser
	// ContentType is the type the file was uploaded with, or application/octet-stream.
	ContentType string
	// ContentLength is the size in bytes, 0 when the store did not report one.
	ContentLength int64
}

// Open is forma2's getFileStream. The caller closes the body.
func (b *Bucket) Open(ctx context.Context, key string) (*Object, error) {
	if b.client == nil {
		return nil, fail(CodeS3Client)
	}
	obj, err := b.client.GetObject(ctx, b.name, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, unknown("get object", err)
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		if notFound(err) {
			return nil, fail(CodeFileNotFound)
		}
		return nil, unknown("get object", err)
	}
	contentType := info.ContentType
	if contentType == "" {
		contentType = defaultContentType
	}
	return &Object{Body: obj, ContentType: contentType, ContentLength: max(info.Size, 0)}, nil
}

// Delete is forma2's deleteFile. S3 deletes are idempotent, so a missing key is not an error.
func (b *Bucket) Delete(ctx context.Context, key string) error {
	if b.client == nil {
		return fail(CodeS3Client)
	}
	if err := b.client.RemoveObject(ctx, b.name, key, minio.RemoveObjectOptions{}); err != nil {
		return unknown("delete object", err)
	}
	return nil
}

// DeleteByPrefix is forma2's deleteFilesByPrefix: every object whose key starts with prefix, deleted in batches of 1000. An empty or root prefix is refused as invalid_input, since it would empty the bucket. Objects that fail to delete are logged and do not fail the call.
func (b *Bucket) DeleteByPrefix(ctx context.Context, prefix string) error {
	if b.client == nil {
		return fail(CodeS3Client)
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		b.log.ErrorContext(ctx, "Refusing to delete files with an empty or root prefix")
		return fail(CodeInvalidInput)
	}
	var keys []minio.ObjectInfo
	for obj := range b.client.ListObjects(ctx, b.name, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return unknown("list objects", obj.Err)
		}
		keys = append(keys, minio.ObjectInfo{Key: obj.Key})
	}
	var failed, deleted int
	for start := 0; start < len(keys); start += deleteBatchSize {
		batch := keys[start:min(start+deleteBatchSize, len(keys))]
		objects := make(chan minio.ObjectInfo, len(batch))
		for _, k := range batch {
			objects <- k
		}
		close(objects)
		batchFailed := 0
		for res := range b.client.RemoveObjects(ctx, b.name, objects, minio.RemoveObjectsOptions{}) {
			batchFailed++
			b.log.ErrorContext(ctx, "Some objects failed to delete", "key", res.ObjectName, "err", res.Err)
		}
		failed += batchFailed
		deleted += len(batch) - batchFailed
	}
	if failed > 0 {
		b.log.WarnContext(ctx, "Bulk delete completed with some failures", "totalErrors", failed, "totalDeleted", deleted)
	}
	return nil
}

// notFound is forma2's not-found test: the NoSuchKey or NotFound error, or any 404.
func notFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey" || resp.Code == "NotFound"
}
