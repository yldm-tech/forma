package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/storage/s3test"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newBucket is a Bucket on a fresh bucket of the test server.
func newBucket(t *testing.T) (*Bucket, config.S3Config) {
	t.Helper()
	s3 := s3test.Config(t)
	b, err := NewBucket(config.Config{S3: s3}, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	return b, s3
}

func put(t *testing.T, s3 config.S3Config, key, contentType, body string) {
	t.Helper()
	if _, err := s3test.Client(t).PutObject(t.Context(), s3.BucketName, key, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: contentType}); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
}

func exists(t *testing.T, s3 config.S3Config, key string) bool {
	t.Helper()
	_, err := s3test.Client(t).StatObject(t.Context(), s3.BucketName, key, minio.StatObjectOptions{})
	if err != nil && !notFound(err) {
		t.Fatalf("stat %s: %v", key, err)
	}
	return err == nil
}

// postUpload sends the form a browser sends: every presigned field, then the file.
func postUpload(t *testing.T, post *PresignedPost, contentType string, body []byte) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range post.Fields {
		value := f.Value
		if f.Name == "Content-Type" && contentType != "" {
			value = contentType
		}
		if err := w.WriteField(f.Name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := w.CreateFormFile("file", "upload.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(body)
	_ = w.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, post.URL, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func fieldValue(fields PresignedFields, name string) string {
	for _, f := range fields {
		if f.Name == name {
			return f.Value
		}
	}
	return ""
}

func TestSignUploadStoresTheFileWithinThePolicy(t *testing.T) {
	t.Parallel()
	b, s3 := newBucket(t)
	const key = "ws/private/surveys/s1/elements/e1/report--fid--1.pdf"

	post, err := b.SignUpload(t.Context(), "report--fid--1.pdf", "application/pdf", "ws/private/surveys/s1/elements/e1", 10)
	if err != nil {
		t.Fatal(err)
	}
	// A path-style bucket is addressed as <endpoint>/<bucket>, without the trailing slash minio-go adds.
	if want := s3.EndpointURL.String() + "/" + s3.BucketName; post.URL != want {
		t.Fatalf("URL = %q, want %q", post.URL, want)
	}
	if got := fieldValue(post.Fields, "key"); got != key {
		t.Fatalf("key field = %q, want %q", got, key)
	}

	if status, body := postUpload(t, post, "", []byte("12345678901")); status != http.StatusBadRequest || !strings.Contains(body, "EntityTooLarge") {
		t.Fatalf("an 11-byte upload against a 10-byte limit answered %d %s, want 400 EntityTooLarge", status, body)
	}
	// AWS answers 403 to a form outside the policy, RustFS 400.
	if status, body := postUpload(t, post, "text/html", []byte("12345")); status != http.StatusForbidden && status != http.StatusBadRequest {
		t.Fatalf("an upload with another content type answered %d %s, want it refused", status, body)
	}
	if exists(t, s3, key) {
		t.Fatal("a refused upload was stored")
	}
	if status, body := postUpload(t, post, "", []byte("12345")); status != http.StatusCreated {
		t.Fatalf("upload answered %d %s, want 201", status, body)
	}
	obj, err := b.Open(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = obj.Body.Close() }()
	content, _ := io.ReadAll(obj.Body)
	if string(content) != "12345" || obj.ContentType != "application/pdf" || obj.ContentLength != 5 {
		t.Fatalf("stored %q as %s (%d bytes)", content, obj.ContentType, obj.ContentLength)
	}
}

func TestSignUploadPolicy(t *testing.T) {
	t.Parallel()
	b, s3 := newBucket(t)
	before := time.Now()
	post, err := b.SignUpload(t.Context(), "a--fid--1.png", "image/PNG; charset=binary", "ws/public", 5<<20)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(fieldValue(post.Fields, "Policy"))
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Expiration time.Time `json:"expiration"`
		Conditions []any     `json:"conditions"`
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatalf("policy %s: %v", raw, err)
	}
	// Two minutes, as forma2's Expires: 120.
	if d := policy.Expiration.Sub(before); d < 119*time.Second || d > 125*time.Second {
		t.Errorf("policy expires after %v, want 2m", d)
	}
	conditions, _ := json.Marshal(policy.Conditions)
	for _, want := range []string{
		`["content-length-range",0,5242880]`,
		`["eq","$Content-Type","image/PNG; charset=binary"]`,
		`["eq","$success_action_status","201"]`,
		`["eq","$bucket","` + s3.BucketName + `"]`,
		`["eq","$key","ws/public/a--fid--1.png"]`,
	} {
		if !strings.Contains(string(conditions), want) {
			t.Errorf("policy conditions %s lack %s", conditions, want)
		}
	}
	// The type is stored as sent, parameters and case included.
	if got := fieldValue(post.Fields, "Content-Type"); got != "image/PNG; charset=binary" {
		t.Errorf("Content-Type field = %q", got)
	}

	// A zero limit signs no size condition at all.
	post, err = b.SignUpload(t.Context(), "b--fid--2.png", "image/png", "ws/public", 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = base64.StdEncoding.DecodeString(fieldValue(post.Fields, "Policy"))
	if strings.Contains(string(raw), "content-length-range") {
		t.Errorf("policy without a limit has a size condition: %s", raw)
	}
}

// recordedFieldNames reads the presignedFields keys of a recorded forma2 exchange in their order.
func recordedFieldNames(t *testing.T, fixture string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "clientapi", "testdata", "forma2", fixture))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Response struct {
			Body struct {
				Data struct {
					PresignedFields json.RawMessage `json:"presignedFields"`
				} `json:"data"`
			} `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return objectKeys(t, doc.Response.Body.Data.PresignedFields)
}

func objectKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	var keys []string
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.(string))
		if _, err := dec.Token(); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func TestPresignedFieldsMatchForma2(t *testing.T) {
	t.Parallel()
	b, _ := newBucket(t)
	post, err := b.SignUpload(t.Context(), "report--fid--1.pdf", "application/pdf", "ws/private", 5<<20)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(post.Fields)
	if err != nil {
		t.Fatal(err)
	}
	got := objectKeys(t, raw)
	for _, fixture := range []string{"v1-storage-upload-init.json", "v2-storage-upload-init.json"} {
		if want := recordedFieldNames(t, fixture); !slices.Equal(got, want) {
			t.Errorf("presignedFields keys %v, forma2 sent %v (%s)", got, want, fixture)
		}
	}
	if got := fieldValue(post.Fields, "X-Amz-Algorithm"); got != "AWS4-HMAC-SHA256" {
		t.Errorf("X-Amz-Algorithm = %q", got)
	}
	if cred := fieldValue(post.Fields, "X-Amz-Credential"); !strings.HasPrefix(cred, "forma-test/") || !strings.HasSuffix(cred, "/us-east-1/s3/aws4_request") {
		t.Errorf("X-Amz-Credential = %q", cred)
	}
}

func TestPresignedFieldsJSON(t *testing.T) {
	fields := PresignedFields{{"Content-Type", "text/plain; x=<a&b>"}, {"bucket", "b"}, {"key", `a"b\c`}}
	// Written as the response writer writes every body: without HTML escaping, as JSON.stringify.
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	writeJSON(c, http.StatusOK, map[string]any{"presignedFields": fields}, false)
	if want := `{"presignedFields":{"Content-Type":"text/plain; x=<a&b>","bucket":"b","key":"a\"b\\c"}}`; rec.Body.String() != want {
		t.Fatalf("JSON = %s, want %s", rec.Body.String(), want)
	}
	if raw, _ := json.Marshal(PresignedFields{}); string(raw) != "{}" {
		t.Fatalf("empty fields = %s", raw)
	}
}

func TestSignDownload(t *testing.T) {
	t.Parallel()
	b, s3 := newBucket(t)
	put(t, s3, "ws/private/a.txt", "text/plain", "hello")

	signed, err := b.SignDownload(t.Context(), "ws/private/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(signed, "X-Amz-Expires=1800") {
		t.Errorf("download URL %s does not expire after 30 minutes", signed)
	}
	resp, err := http.Get(signed) //nolint:gosec,noctx // the URL is the one just signed against the test server
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "hello" {
		t.Fatalf("signed URL answered %d %q", resp.StatusCode, body)
	}

	if _, err := b.SignDownload(t.Context(), "ws/private/missing.txt"); CodeOf(err) != CodeFileNotFound {
		t.Fatalf("missing file: %v, want file_not_found_error", err)
	}
}

func TestOpenAndDelete(t *testing.T) {
	t.Parallel()
	b, s3 := newBucket(t)
	put(t, s3, "ws/public/logo.png", "image/png", "png")

	obj, err := b.Open(t.Context(), "ws/public/logo.png")
	if err != nil {
		t.Fatal(err)
	}
	_ = obj.Body.Close()
	if obj.ContentType != "image/png" || obj.ContentLength != 3 {
		t.Fatalf("opened %+v", obj)
	}
	if _, err := b.Open(t.Context(), "ws/public/missing.png"); CodeOf(err) != CodeFileNotFound {
		t.Fatalf("missing file: %v, want file_not_found_error", err)
	}

	if err := b.Delete(t.Context(), "ws/public/logo.png"); err != nil {
		t.Fatal(err)
	}
	if exists(t, s3, "ws/public/logo.png") {
		t.Fatal("the file survived its deletion")
	}
	// S3 deletes are idempotent: a missing file is deleted successfully.
	if err := b.Delete(t.Context(), "ws/public/logo.png"); err != nil {
		t.Fatalf("deleting a missing file: %v", err)
	}
}

func TestDeleteByPrefix(t *testing.T) {
	t.Parallel()
	b, s3 := newBucket(t)
	// More than one batch of 1000.
	const n = 1003
	var wg sync.WaitGroup
	keys := make(chan string)
	for range 16 {
		wg.Go(func() {
			for k := range keys {
				put(t, s3, k, "text/plain", "x")
			}
		})
	}
	for i := range n {
		keys <- fmt.Sprintf("ws1/private/surveys/s/elements/e/f%04d.txt", i)
	}
	close(keys)
	wg.Wait()
	put(t, s3, "ws2/public/keep.png", "image/png", "x")

	for _, prefix := range []string{"", "   ", "/", " / "} {
		if err := b.DeleteByPrefix(t.Context(), prefix); CodeOf(err) != CodeInvalidInput {
			t.Errorf("DeleteByPrefix(%q) = %v, want invalid_input", prefix, err)
		}
	}
	if err := b.DeleteByPrefix(t.Context(), " ws1/ "); err != nil {
		t.Fatal(err)
	}
	remaining := 0
	for obj := range s3test.Client(t).ListObjects(context.Background(), s3.BucketName, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Err != nil {
			t.Fatal(obj.Err)
		}
		if obj.Key != "ws2/public/keep.png" {
			t.Errorf("%s survived", obj.Key)
		}
		remaining++
	}
	if remaining != 1 {
		t.Fatalf("%d objects remain, want only the other workspace's", remaining)
	}
	// Nothing left under the prefix is not an error.
	if err := b.DeleteByPrefix(t.Context(), "ws1/"); err != nil {
		t.Fatal(err)
	}
}

func TestUnconfiguredBucket(t *testing.T) {
	b, err := NewBucket(config.Config{}, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	if b.Configured() {
		t.Fatal("a bucket without a name reports storage configured")
	}
	ctx := t.Context()
	_, signErr := b.SignUpload(ctx, "a.png", "image/png", "ws/public", 1)
	_, downloadErr := b.SignDownload(ctx, "k")
	_, openErr := b.Open(ctx, "k")
	for name, err := range map[string]error{
		"SignUpload": signErr, "SignDownload": downloadErr, "Open": openErr,
		"Delete": b.Delete(ctx, "k"), "DeleteByPrefix": b.DeleteByPrefix(ctx, "ws/"),
	} {
		if CodeOf(err) != CodeS3Client {
			t.Errorf("%s = %v, want s3_client_error", name, err)
		}
	}
}

func TestUnreachableStoreIsUnknown(t *testing.T) {
	endpoint, _ := url.Parse("http://127.0.0.1:1")
	b, err := NewBucket(config.Config{S3: config.S3Config{AccessKey: "a", SecretKey: "b", Region: "us-east-1", EndpointURL: endpoint, ForcePathStyle: true, BucketName: "forma"}}, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	ctx := func() context.Context {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		t.Cleanup(cancel)
		return ctx
	}
	if _, err := b.Open(ctx(), "k"); CodeOf(err) != CodeUnknown {
		t.Errorf("Open = %v, want unknown", err)
	}
	if err := b.Delete(ctx(), "k"); CodeOf(err) != CodeUnknown {
		t.Errorf("Delete = %v, want unknown", err)
	}
	if err := b.DeleteByPrefix(ctx(), "ws/"); CodeOf(err) != CodeUnknown {
		t.Errorf("DeleteByPrefix = %v, want unknown", err)
	}
	// Signing needs no request when the region is known, so an upload can be signed while the store is down, as with the AWS SDK.
	if _, err := b.SignUpload(ctx(), "a.png", "image/png", "ws/public", 1); err != nil {
		t.Errorf("SignUpload = %v", err)
	}
}
