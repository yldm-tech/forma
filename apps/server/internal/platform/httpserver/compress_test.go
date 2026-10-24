package httpserver

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/gzhttp"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// big is a body well above compressMinSize; small is one below it.
var (
	big   = []byte(strings.Repeat("forma survey page ", 200))
	small = []byte(`{"data":"ok"}`)
)

func compressedEngine(t *testing.T) (*gin.Engine, http.Handler) {
	t.Helper()
	r := NewEngine(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return r, New(config.Config{}, r, slog.New(slog.NewTextHandler(io.Discard, nil))).srv.Handler
}

func serve(h http.Handler, path string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var acceptGzip = http.Header{"Accept-Encoding": {"gzip, deflate, br, zstd"}}

func gunzip(t *testing.T, body []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return out
}

// The text a browser downloads in bulk is gzipped for a client that accepts gzip, with the headers its handler set left as they were and Vary: Accept-Encoding kept or added.
func TestCompressText(t *testing.T) {
	r, h := compressedEngine(t)
	files := http.FS(fstest.MapFS{"surveys.umd.js": {Data: big}})
	r.GET("/js/surveys.umd.js", func(c *gin.Context) {
		// The SDK bundle's headers, as webui serves them.
		c.Header("Cache-Control", "public, max-age=3600, s-maxage=2592000, stale-while-revalidate=3600, stale-if-error=86400")
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Vary", "Accept-Encoding")
		c.Header("Content-Type", "application/javascript; charset=UTF-8")
		c.FileFromFS("/surveys.umd.js", files)
	})
	r.GET("/app/assets/survey.css", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Data(http.StatusOK, "text/css; charset=utf-8", big)
	})
	r.GET("/s/survey", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-cache, no-store, max-age=0, must-revalidate")
		c.Header("Vary", "rsc, next-router-state-tree, next-router-prefetch, next-router-segment-prefetch, Accept-Encoding")
		c.Data(http.StatusOK, "text/html; charset=utf-8", big)
	})
	r.GET("/api/v1/client/env", func(c *gin.Context) {
		c.Header("Cache-Control", "public, s-maxage=60")
		c.Header("Vary", "rsc, next-router-state-tree, next-router-prefetch, next-router-segment-prefetch")
		c.JSON(http.StatusOK, gin.H{"data": string(big)})
	})
	r.GET("/icon.svg", func(c *gin.Context) { c.Data(http.StatusOK, "image/svg+xml", big) })
	r.GET("/legacy.js", func(c *gin.Context) { c.Data(http.StatusOK, "text/javascript", big) })

	for _, tc := range []struct {
		path, cacheControl, vary string
	}{
		{"/js/surveys.umd.js", "public, max-age=3600, s-maxage=2592000, stale-while-revalidate=3600, stale-if-error=86400", "Accept-Encoding"},
		{"/app/assets/survey.css", "public, max-age=31536000, immutable", "Accept-Encoding"},
		// noTransformHTML still marks the page; the server's gzip is the only compression it gets behind Cloudflare.
		{"/s/survey", "private, no-cache, no-store, max-age=0, must-revalidate, no-transform", "rsc, next-router-state-tree, next-router-prefetch, next-router-segment-prefetch, Accept-Encoding"},
		{"/api/v1/client/env", "public, s-maxage=60", "rsc, next-router-state-tree, next-router-prefetch, next-router-segment-prefetch, Accept-Encoding"},
		{"/icon.svg", "", "Accept-Encoding"},
		{"/legacy.js", "", "Accept-Encoding"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := serve(h, tc.path, acceptGzip)
			if rec.Code != http.StatusOK {
				t.Fatalf("code = %d", rec.Code)
			}
			if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
				t.Fatalf("Content-Encoding = %q, want gzip", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.cacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, tc.cacheControl)
			}
			if got := rec.Header().Values("Vary"); len(got) != 1 || got[0] != tc.vary {
				t.Errorf("Vary = %q, want [%q]", got, tc.vary)
			}
			if rec.Header().Get("Content-Length") != "" || rec.Header().Get("Accept-Ranges") != "" {
				t.Errorf("Content-Length %q, Accept-Ranges %q on a compressed body", rec.Header().Get("Content-Length"), rec.Header().Get("Accept-Ranges"))
			}
			if len(rec.Header()[gzhttp.HeaderNoCompression]) != 0 {
				t.Errorf("%s leaked", gzhttp.HeaderNoCompression)
			}
			plain := gunzip(t, rec.Body.Bytes())
			if tc.path == "/api/v1/client/env" {
				if !bytes.Contains(plain, big) {
					t.Errorf("decompressed JSON does not carry the body")
				}
			} else if !bytes.Equal(plain, big) {
				t.Errorf("decompressed body differs")
			}
		})
	}

	t.Run("JavaScript keeps its type and cache headers", func(t *testing.T) {
		rec := serve(h, "/js/surveys.umd.js", acceptGzip)
		if got := rec.Header().Get("Content-Type"); got != "application/javascript; charset=UTF-8" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Access-Control-Allow-Origin = %q", got)
		}
	})
}

// What must reach the client as its handler wrote it is never compressed: streams, downloads, images, uploads, small bodies, bodies already encoded and ranges. The No-Gzip-Compression marker never leaves the server.
func TestCompressLeavesAlone(t *testing.T) {
	r, h := compressedEngine(t)
	r.GET("/zip", func(c *gin.Context) {
		c.Header("Content-Disposition", `attachment; filename="attachments.zip"`)
		c.Data(http.StatusOK, "application/zip", big)
	})
	r.GET("/csv", func(c *gin.Context) {
		c.Header("Content-Disposition", `attachment; filename="export.csv"`)
		c.Data(http.StatusOK, "text/csv", big)
	})
	r.GET("/xlsx", func(c *gin.Context) {
		c.Header("Content-Disposition", `attachment; filename="export.xlsx"`)
		c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", big)
	})
	r.GET("/json-download", func(c *gin.Context) {
		c.Header("Content-Disposition", `Attachment; filename="export.json"`)
		c.Data(http.StatusOK, "application/json", big)
	})
	r.GET("/png", func(c *gin.Context) { c.Data(http.StatusOK, "image/png", big) })
	r.GET("/storage/:workspaceId/public/*file", func(c *gin.Context) { c.Data(http.StatusOK, "image/svg+xml", big) })
	r.GET("/small", func(c *gin.Context) { c.Data(http.StatusOK, "application/json", small) })
	r.GET("/encoded", func(c *gin.Context) {
		c.Header("Content-Encoding", "br")
		c.Data(http.StatusOK, "application/javascript", big)
	})
	r.GET("/plain", func(c *gin.Context) { c.Data(http.StatusOK, "text/plain; charset=utf-8", big) })
	r.GET("/untyped", func(c *gin.Context) {
		c.Writer.Header()["Content-Type"] = nil
		c.Status(http.StatusOK)
		_, _ = c.Writer.Write(big)
	})
	files := http.FS(fstest.MapFS{"app.js": {Data: big}})
	r.GET("/range.js", func(c *gin.Context) { c.FileFromFS("/app.js", files) })
	r.GET("/page", func(c *gin.Context) { c.Data(http.StatusOK, "text/html; charset=utf-8", big) })

	for _, tc := range []struct {
		path   string
		header http.Header
		code   int
	}{
		{"/zip", acceptGzip, http.StatusOK},
		{"/csv", acceptGzip, http.StatusOK},
		{"/xlsx", acceptGzip, http.StatusOK},
		{"/json-download", acceptGzip, http.StatusOK},
		{"/png", acceptGzip, http.StatusOK},
		{"/storage/ws/public/logo.svg", acceptGzip, http.StatusOK},
		{"/small", acceptGzip, http.StatusOK},
		{"/encoded", acceptGzip, http.StatusOK},
		{"/plain", acceptGzip, http.StatusOK},
		{"/untyped", acceptGzip, http.StatusOK},
		{"/range.js", http.Header{"Accept-Encoding": {"gzip"}, "Range": {"bytes=0-99"}}, http.StatusPartialContent},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := serve(h, tc.path, tc.header)
			if rec.Code != tc.code {
				t.Fatalf("code = %d, want %d", rec.Code, tc.code)
			}
			want := ""
			if tc.path == "/encoded" {
				want = "br"
			}
			if got := rec.Header().Get("Content-Encoding"); got != want {
				t.Errorf("Content-Encoding = %q, want %q", got, want)
			}
			if len(rec.Header()[gzhttp.HeaderNoCompression]) != 0 {
				t.Errorf("%s leaked", gzhttp.HeaderNoCompression)
			}
			if tc.path == "/untyped" && rec.Header().Get("Content-Type") != "" {
				t.Errorf("Content-Type = %q, want none", rec.Header().Get("Content-Type"))
			}
			if tc.code == http.StatusOK && !bytes.Equal(rec.Body.Bytes(), big) && tc.path != "/small" {
				t.Errorf("body was changed")
			}
		})
	}

	t.Run("a client that does not accept gzip", func(t *testing.T) {
		for _, header := range []http.Header{{}, {"Accept-Encoding": {"identity"}}, {"Accept-Encoding": {"gzip;q=0"}}} {
			rec := serve(h, "/page", header)
			if rec.Header().Get("Content-Encoding") != "" || !bytes.Equal(rec.Body.Bytes(), big) {
				t.Errorf("Accept-Encoding %q: Content-Encoding %q", header.Get("Accept-Encoding"), rec.Header().Get("Content-Encoding"))
			}
			if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
				t.Errorf("Accept-Encoding %q: Vary = %q", header.Get("Accept-Encoding"), got)
			}
			if len(rec.Header()[gzhttp.HeaderNoCompression]) != 0 {
				t.Errorf("%s leaked", gzhttp.HeaderNoCompression)
			}
		}
	})
}

// A stream reaches the client as it is written: headers flushed before the first event arrive at once, and each event arrives when it is flushed, uncompressed, although the client accepts gzip.
func TestCompressStreams(t *testing.T) {
	for _, contentType := range []string{"text/event-stream", "application/x-ndjson"} {
		t.Run(contentType, func(t *testing.T) {
			r, h := compressedEngine(t)
			started, next := make(chan struct{}), make(chan struct{})
			r.GET("/stream", func(c *gin.Context) {
				c.Header("Content-Type", contentType)
				c.Header("Cache-Control", "no-cache, no-store, no-transform")
				c.Status(http.StatusOK)
				c.Writer.Flush()
				// A failing test closes the connection rather than releasing the handler, which would otherwise keep the server from closing.
				select {
				case <-started:
				case <-c.Request.Context().Done():
					return
				}
				_, _ = c.Writer.WriteString("data: one\n\n")
				c.Writer.Flush()
				select {
				case <-next:
				case <-c.Request.Context().Done():
					return
				}
				_, _ = c.Writer.WriteString("data: two\n\n")
			})
			srv := httptest.NewServer(h)
			defer srv.Close()

			client := &http.Client{Transport: &http.Transport{DisableCompression: true}, Timeout: 10 * time.Second}
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/stream", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			// The response's headers arrive before the handler writes anything.
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("headers did not arrive before the first event: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if got := resp.Header.Get("Content-Encoding"); got != "" {
				t.Fatalf("Content-Encoding = %q, want none", got)
			}
			if len(resp.Header[gzhttp.HeaderNoCompression]) != 0 {
				t.Errorf("%s leaked", gzhttp.HeaderNoCompression)
			}
			close(started)
			lines := bufio.NewReader(resp.Body)
			// The first event arrives while the handler still waits to write the second.
			if line, err := lines.ReadString('\n'); err != nil || line != "data: one\n" {
				t.Fatalf("first event = %q, %v", line, err)
			}
			close(next)
			rest, err := io.ReadAll(lines)
			if err != nil || string(rest) != "\ndata: two\n\n" {
				t.Fatalf("rest = %q, %v", rest, err)
			}
		})
	}
}

// An engine served without Compress, as every other package's tests serve it, answers exactly as before.
func TestEngineWithoutCompress(t *testing.T) {
	r := NewEngine(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.GET("/api", func(c *gin.Context) {
		c.Header("Vary", "rsc")
		c.Data(http.StatusOK, "application/json", big)
	})
	r.GET("/zip", func(c *gin.Context) { c.Data(http.StatusOK, "application/zip", big) })
	for path, vary := range map[string]string{"/api": "rsc", "/zip": ""} {
		rec := serve(r, path, acceptGzip)
		if rec.Header().Get("Content-Encoding") != "" || rec.Header().Get("Vary") != vary || len(rec.Header()[gzhttp.HeaderNoCompression]) != 0 {
			t.Errorf("%s: headers changed without Compress: %v", path, rec.Header())
		}
	}
}

func TestAddVary(t *testing.T) {
	for _, tc := range []struct {
		have []string
		want string
	}{
		{nil, "Accept-Encoding"},
		{[]string{"Origin"}, "Origin, Accept-Encoding"},
		{[]string{"rsc, accept-encoding"}, "rsc, accept-encoding"},
		{[]string{"Origin", "Accept"}, "Origin, Accept, Accept-Encoding"},
		{[]string{"*"}, "*"},
	} {
		h := http.Header{}
		for _, v := range tc.have {
			h.Add("Vary", v)
		}
		addVary(h, "Accept-Encoding")
		if got := strings.Join(h.Values("Vary"), " | "); got != tc.want {
			t.Errorf("addVary(%q) = %q, want %q", tc.have, got, tc.want)
		}
	}
}
