// Package v1api writes forma2's v1 API responses (apps/web/lib/api/response.ts): every body is {"data": ...} or {"code", "message", "details"}, with Cache-Control "private, no-store" and, when the route asked for it, its CORS headers. The routes that answer this way are the ones external tools call with forma2's contract: /storage and /api/v1/webhooks.
package v1api

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CacheNoStore is the Cache-Control of every v1 response.
const CacheNoStore = "private, no-store"

// ErrorBody is forma2's ApiErrorResponse.
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// WriteJSON writes body as forma2's Response.json did: content type application/json, no HTML escaping, and the route's CORS headers when cors is set. It aborts the handler chain.
func WriteJSON(c *gin.Context, status int, body any, cors bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if cors {
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	}
	c.Header("Cache-Control", CacheNoStore)
	c.Data(status, "application/json", bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	c.Abort()
}

// Success is forma2's successResponse: 200 with {"data": data}.
func Success(c *gin.Context, data any) {
	WriteJSON(c, http.StatusOK, map[string]any{"data": data}, false)
}

// Error writes {"code", "message", "details"}; nil details are written as {}.
func Error(c *gin.Context, status int, code, message string, details map[string]any, cors bool) {
	if details == nil {
		details = map[string]any{}
	}
	WriteJSON(c, status, ErrorBody{Code: code, Message: message, Details: details}, cors)
}

// BadRequest is forma2's badRequestResponse.
func BadRequest(c *gin.Context, message string, details map[string]any, cors bool) {
	Error(c, http.StatusBadRequest, "bad_request", message, details, cors)
}

// NotFound is forma2's notFoundResponse: "<Type> not found" naming the resource the caller asked for.
func NotFound(c *gin.Context, resourceType, resourceID string, cors bool) {
	Error(c, http.StatusNotFound, "not_found", resourceType+" not found", map[string]any{"resource_id": resourceID, "resource_type": resourceType}, cors)
}

// NotAuthenticated is forma2's notAuthenticatedResponse, the answer to a request without a valid API key.
func NotAuthenticated(c *gin.Context) {
	Error(c, http.StatusUnauthorized, "not_authenticated", "Not authenticated", map[string]any{"x-Api-Key": "Header not provided or API Key invalid"}, false)
}

// Unauthorized is forma2's unauthorizedResponse: an authenticated caller refused access, which v1 answers with 401, not 403.
func Unauthorized(c *gin.Context) {
	Error(c, http.StatusUnauthorized, "unauthorized", "You are not authorized to access this resource", nil, false)
}

// TooManyRequests is forma2's tooManyRequestsResponse.
func TooManyRequests(c *gin.Context, message string) {
	Error(c, http.StatusTooManyRequests, "too_many_requests", message, nil, false)
}

// PayloadTooLarge is forma2's payloadTooLargeResponse.
func PayloadTooLarge(c *gin.Context, details map[string]any) {
	Error(c, http.StatusRequestEntityTooLarge, "payload_too_large", "Payload Too Large", details, false)
}

// InternalError is forma2's internalServerErrorResponse with the given message.
func InternalError(c *gin.Context, message string) {
	Error(c, http.StatusInternalServerError, "internal_server_error", message, nil, false)
}
