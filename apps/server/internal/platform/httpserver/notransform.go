package httpserver

import (
	"mime"
	"strings"

	"github.com/gin-gonic/gin"
)

// noTransformHTML adds no-transform to the Cache-Control of every text/html response, after whatever caching its handler chose, or alone when the handler chose none. Cloudflare's Web Analytics injects its beacon script into every proxied HTML response that does not say no-transform, and the Content-Security-Policy blocks that script, so each page logged a CSP violation in the browser. no-transform also stops Cloudflare from compressing a response, which is why nothing but HTML gets it. The header is added when the response's headers are flushed, so every HTML responder is covered, the SPA document, the public survey pages, redirects and error pages alike, including ones added later.
func noTransformHTML() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer = &noTransformWriter{ResponseWriter: c.Writer}
		c.Next()
	}
}

// noTransformWriter marks an HTML response on every path through which gin's writer flushes the headers. A response that writes no body at all is flushed by gin on its own writer and stays unmarked, which is harmless: there is nothing to inject into.
type noTransformWriter struct {
	gin.ResponseWriter
}

func (w *noTransformWriter) mark() {
	if w.Written() {
		return
	}
	h := w.Header()
	if mediaType, _, _ := mime.ParseMediaType(h.Get("Content-Type")); mediaType != "text/html" {
		return
	}
	cacheControl := h.Get("Cache-Control")
	for _, directive := range strings.Split(cacheControl, ",") {
		if strings.EqualFold(strings.TrimSpace(directive), "no-transform") {
			return
		}
	}
	if cacheControl == "" {
		h.Set("Cache-Control", "no-transform")
		return
	}
	h.Set("Cache-Control", cacheControl+", no-transform")
}

func (w *noTransformWriter) WriteHeader(code int) {
	w.mark()
	w.ResponseWriter.WriteHeader(code)
}

func (w *noTransformWriter) WriteHeaderNow() {
	w.mark()
	w.ResponseWriter.WriteHeaderNow()
}

func (w *noTransformWriter) Write(data []byte) (int, error) {
	w.mark()
	return w.ResponseWriter.Write(data)
}

func (w *noTransformWriter) WriteString(s string) (int, error) {
	w.mark()
	return w.ResponseWriter.WriteString(s)
}

func (w *noTransformWriter) Flush() {
	w.mark()
	w.ResponseWriter.Flush()
}
