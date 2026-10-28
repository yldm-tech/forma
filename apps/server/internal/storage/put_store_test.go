package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// putFrom sends a presigned PUT as a browser on origin does (no Origin header when origin is empty): the signed headers, with contentType in place of the signed type when it is not empty, and body, whose length Go sets as Content-Length as a browser does.
func putFrom(t *testing.T, put *PresignedPut, contentType string, body []byte, origin string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, put.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range put.Headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(out)
}

// allows reports whether a CORS answer's Access-Control-Allow-Origin lets origin read it.
func allows(h http.Header, origin string) bool {
	got := h.Get("Access-Control-Allow-Origin")
	return got == "*" || got == origin
}

// listHas reports whether the comma-separated header value lists want, or is the wildcard.
func listHas(value, want string) bool {
	for item := range strings.SplitSeq(value, ",") {
		item = strings.TrimSpace(item)
		if item == "*" || strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

// putStoreCheck is what a store must do for STORAGE_UPLOAD_METHOD=put to be safe on it, signed through b exactly as the server signs an upload: the exact file is stored with its signed type, while a body one byte longer or one byte shorter, another Content-Type, or a URL past its expiry is refused and leaves nothing behind. The size limit of put uploads rests on the store refusing a body whose length is not the signed Content-Length; nothing else checks it. The file name is one that needs URL encoding in the signed path (letters outside ASCII, a space, %, +, & and =), which a POST upload never put in a URL. For each of origins the store must also answer a browser's CORS preflight for the PUT with its Content-Type, and the PUT itself must allow the first origin. Nothing is left in the bucket afterwards.
func putStoreCheck(t *testing.T, b *Bucket, prefix string, origins []string) {
	t.Helper()
	ctx := t.Context()
	name := "請求書 100% a+b&c=d--fid--" + uuid.NewString() + ".pdf"
	key := prefix + "/" + name
	t.Cleanup(func() {
		// t.Context is already canceled when cleanups run.
		if err := b.Delete(context.Background(), key); err != nil {
			t.Errorf("delete %s: %v", key, err)
		}
	})
	stored := func() bool {
		t.Helper()
		obj, err := b.Open(ctx, key)
		if CodeOf(err) == CodeFileNotFound {
			return false
		}
		if err != nil {
			t.Fatalf("open %s: %v", key, err)
		}
		_ = obj.Body.Close()
		return true
	}
	const file = "%PDF-"

	put, err := b.SignPut(ctx, name, "application/pdf", prefix, int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	expired, err := b.signPut(ctx, name, "application/pdf", prefix, int64(len(file)), time.Second)
	if err != nil {
		t.Fatal(err)
	}

	for _, refused := range []struct {
		name, contentType, body string
	}{
		{"a body one byte longer than signed", "", file + "x"},
		{"a body one byte shorter than signed", "", file[:len(file)-1]},
		{"another content type", "text/html", file},
	} {
		if status, _, body := putFrom(t, put, refused.contentType, []byte(refused.body), ""); status/100 == 2 {
			t.Errorf("%s answered %d %s, want it refused", refused.name, status, body)
		}
	}
	if stored() {
		t.Fatal("a refused upload was stored")
	}
	// X-Amz-Date has whole seconds, so a one-second URL is past its expiry after three.
	time.Sleep(3 * time.Second)
	if status, _, body := putFrom(t, expired, "", []byte(file), ""); status/100 == 2 {
		t.Errorf("an expired URL answered %d %s, want it refused", status, body)
	}
	if stored() {
		t.Fatal("an upload with an expired URL was stored")
	}

	for _, origin := range origins {
		req, err := http.NewRequestWithContext(ctx, http.MethodOptions, put.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPut)
		req.Header.Set("Access-Control-Request-Headers", "content-type")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode/100 != 2 || !allows(resp.Header, origin) || !listHas(resp.Header.Get("Access-Control-Allow-Methods"), http.MethodPut) || !listHas(resp.Header.Get("Access-Control-Allow-Headers"), "content-type") {
			t.Errorf("the CORS preflight of a PUT from %s answered %d %v, want it allowed", origin, resp.StatusCode, resp.Header)
		}
	}

	var origin string
	if len(origins) > 0 {
		origin = origins[0]
	}
	status, header, body := putFrom(t, put, "", []byte(file), origin)
	if status != http.StatusOK {
		t.Fatalf("the signed upload answered %d %s, want 200", status, body)
	}
	if origin != "" && !allows(header, origin) {
		t.Errorf("the upload from %s answered Access-Control-Allow-Origin %q, so a browser fails it", origin, header.Get("Access-Control-Allow-Origin"))
	}
	obj, err := b.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(obj.Body)
	_ = obj.Body.Close()
	if string(content) != file || obj.ContentType != "application/pdf" || obj.ContentLength != int64(len(file)) {
		t.Fatalf("stored %q as %s (%d bytes)", content, obj.ContentType, obj.ContentLength)
	}

	// Nothing reads the bucket without a signature: files are served through /storage.
	unsigned, err := url.Parse(put.URL)
	if err != nil {
		t.Fatal(err)
	}
	unsigned.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, unsigned.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		t.Fatalf("an unsigned GET of the uploaded file answered %d", resp.StatusCode)
	}
}

func TestPutStoreCheck(t *testing.T) {
	t.Parallel()
	b, _ := newBucket(t)
	putStoreCheck(t, b, "ws/private/surveys/s1/elements/e1", nil)
}

// TestPutCanary runs putStoreCheck against a real store, such as the Cloudflare R2 bucket production moves to, before STORAGE_UPLOAD_METHOD=put is set for it. It reads the store's S3_* settings from the environment, the same production uses (the bucket's own token is enough: it writes, reads and deletes one object under forma-put-canary/), and is skipped unless PUT_CANARY_ORIGINS is set: the comma-separated origins the bucket's CORS rule must let upload, the app's own and a customer site's.
//
//	PUT_CANARY_ORIGINS=https://forma.yldm.ai,https://customer.example S3_ENDPOINT_URL=https://<account id>.r2.cloudflarestorage.com S3_REGION=auto S3_BUCKET_NAME=<bucket> S3_ACCESS_KEY=<key id> S3_SECRET_KEY=<secret> go test ./internal/storage/ -run '^TestPutCanary$' -v
func TestPutCanary(t *testing.T) {
	raw := os.Getenv("PUT_CANARY_ORIGINS")
	if raw == "" {
		t.Skip("PUT_CANARY_ORIGINS is not set")
	}
	origins := slices.DeleteFunc(strings.Split(raw, ","), func(s string) bool { return strings.TrimSpace(s) == "" })
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}
	s3, err := env.ParseAs[config.S3Config]()
	if err != nil {
		t.Fatal(err)
	}
	if !s3.Configured() {
		t.Fatal("S3_BUCKET_NAME is not set")
	}
	b, err := NewBucket(config.Config{S3: s3}, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	putStoreCheck(t, b, "forma-put-canary/"+uuid.NewString(), origins)
}
