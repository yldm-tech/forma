package httpserver

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/gzhttp"
)

// compressedTypes are the media types the server gzips: the text a browser downloads in bulk (the SPA's and the survey page's documents, scripts and styles, the SDK bundles under /js, API JSON) and SVG. Everything else is left as its handler wrote it: images, fonts, ZIP and XLSX are compressed already, CSV is only ever a download, and the streams (text/event-stream, the survey generation's NDJSON) must reach the client as they are written.
var compressedTypes = []string{"text/html", "text/css", "application/javascript", "text/javascript", "application/json", "image/svg+xml"}

// compressMinSize is the smallest body worth compressing: below about a kilobyte the gzip framing and the CPU cost outweigh the bytes saved.
const compressMinSize = 1024

// compressingKey marks, on the request context, a request served through Compress, so compressionPolicy knows its markers are read by the compressor.
type compressingKey struct{}

// Compress gzips the engine's responses for a client that accepts gzip, as compressionPolicy allows: a response of one of compressedTypes, at least compressMinSize long, that is not an attachment, not a file served from /storage and not encoded already. Every compressible response says Vary: Accept-Encoding. A ranged (206) response is never compressed and a compressed one drops Accept-Ranges. Cache-Control, ETag and the other headers a handler set are left as they were. Only gzip is offered: zstd would be preferred by browsers that announce it, and Cloudflare in front of production fetches with gzip.
//
// HTML matters most here: noTransformHTML marks it no-transform so Cloudflare does not inject its beacon, which also keeps Cloudflare from compressing it, so the server's gzip is the only compression the survey pages and the SPA document get.
func Compress(engine http.Handler) http.Handler {
	wrap, err := gzhttp.NewWrapper(
		gzhttp.MinSize(compressMinSize),
		gzhttp.ContentTypes(compressedTypes),
		gzhttp.EnableZstd(false),
		// A response without a Content-Type keeps having none; gzhttp would otherwise add the type it sniffed.
		gzhttp.SetContentType(false),
	)
	if err != nil {
		panic(fmt.Sprintf("gzip wrapper: %v", err))
	}
	return wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		engine.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), compressingKey{}, true)))
	}))
}

// compressionPolicy tells Compress, when the response's headers are flushed, what it may compress: a response it must leave alone gets gzhttp's No-Gzip-Compression marker (which gzhttp removes before the response leaves), and one it may compress gets Accept-Encoding added to whatever Vary its handler set, since a handler that sets Vary replaces the one gzhttp added. Outside Compress (an engine served directly, as the tests do) it does nothing.
func compressionPolicy() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Context().Value(compressingKey{}) != nil {
			c.Writer = &compressionPolicyWriter{ResponseWriter: c.Writer, storage: strings.HasPrefix(c.Request.URL.Path, "/storage/")}
		}
		c.Next()
	}
}

// compressionPolicyWriter decides on every path through which gin's writer flushes the headers. gin's WriteHeader only records the status, before handlers such as c.JSON set the Content-Type, so it does not decide. A response that writes no body is flushed by gin on its own writer and stays undecided, which is harmless: gzhttp compresses no empty body.
type compressionPolicyWriter struct {
	gin.ResponseWriter
	// storage is a file served from /storage: an upload, whatever its type, is sent as it was stored.
	storage bool
}

func (w *compressionPolicyWriter) mark() {
	if !w.Written() {
		w.decide()
	}
}

// decide marks the response and reports whether it may be compressed.
func (w *compressionPolicyWriter) decide() bool {
	h := w.Header()
	if w.storage || !compressible(h) {
		h.Set(gzhttp.HeaderNoCompression, "1")
		return false
	}
	addVary(h, "Accept-Encoding")
	return true
}

// compressible reports whether headers describe a response Compress may gzip: one of compressedTypes, not an attachment (a download is saved as it was sent, and a JSON export is a file, not an API answer), not encoded by its handler already.
func compressible(h http.Header) bool {
	if h.Get("Content-Encoding") != "" {
		return false
	}
	disposition, _, _ := strings.Cut(h.Get("Content-Disposition"), ";")
	if strings.EqualFold(strings.TrimSpace(disposition), "attachment") {
		return false
	}
	mediaType, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
	return slices.Contains(compressedTypes, mediaType)
}

// addVary adds token to the response's Vary unless it, or *, is there already, keeping one header line.
func addVary(h http.Header, token string) {
	values := h.Values("Vary")
	for _, value := range values {
		for field := range strings.SplitSeq(value, ",") {
			if field = strings.TrimSpace(field); field == "*" || strings.EqualFold(field, token) {
				return
			}
		}
	}
	h.Set("Vary", strings.Join(append(values, token), ", "))
}

func (w *compressionPolicyWriter) WriteHeaderNow() {
	w.mark()
	w.ResponseWriter.WriteHeaderNow()
}

func (w *compressionPolicyWriter) Write(data []byte) (int, error) {
	w.mark()
	return w.ResponseWriter.Write(data)
}

func (w *compressionPolicyWriter) WriteString(s string) (int, error) {
	w.mark()
	return w.ResponseWriter.WriteString(s)
}

// Flush sends what was written so far. gzhttp holds a response's headers until it has seen enough of the body to decide, and a stream that flushes its headers before its first event (the survey generation does, so the client learns at once that the stream started) would wait for that event; an empty write of a response it must not compress makes gzhttp send them now.
func (w *compressionPolicyWriter) Flush() {
	if !w.Written() && !w.decide() {
		_, _ = w.ResponseWriter.Write(nil)
	}
	w.ResponseWriter.Flush()
}
