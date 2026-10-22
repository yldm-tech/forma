package storage

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// forma2's v1 API responses (apps/web/lib/api/response.ts): every body is {"data": ...} or {"code", "message", "details"}, with Cache-Control "private, no-store" and, when the route asked for it, its CORS headers.

const noStore = "private, no-store"

// errorBody is forma2's ApiErrorResponse.
type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// writeJSON writes body as forma2's Response.json did: content type application/json, no HTML escaping.
func writeJSON(c *gin.Context, status int, body any, cors bool) {
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
	c.Header("Cache-Control", noStore)
	c.Data(status, "application/json", bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	c.Abort()
}

func writeError(c *gin.Context, status int, code, message string, details map[string]any, cors bool) {
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(c, status, errorBody{Code: code, Message: message, Details: details}, cors)
}

func badRequest(c *gin.Context, message string, details map[string]any, cors bool) {
	writeError(c, http.StatusBadRequest, "bad_request", message, details, cors)
}

func notFoundResponse(c *gin.Context, resourceType, resourceID string, cors bool) {
	writeError(c, http.StatusNotFound, "not_found", resourceType+" not found", map[string]any{"resource_id": resourceID, "resource_type": resourceType}, cors)
}

func notAuthenticated(c *gin.Context) {
	writeError(c, http.StatusUnauthorized, "not_authenticated", "Not authenticated", map[string]any{"x-Api-Key": "Header not provided or API Key invalid"}, false)
}

func unauthorized(c *gin.Context) {
	writeError(c, http.StatusUnauthorized, "unauthorized", "You are not authorized to access this resource", nil, false)
}

// WriteStorageError is forma2's getErrorResponseFromStorageError, with its CORS headers: a missing file is 404, invalid input 400 with the file name, a storage configuration error 500 with the code clients read to tell an unconfigured instance from a failed upload, and anything else 500. fileName is the name the request asked for. An unknown failure is logged here.
func WriteStorageError(c *gin.Context, log *slog.Logger, err error, fileName string) {
	switch code := CodeOf(err); code {
	case CodeFileNotFound:
		notFoundResponse(c, "file", fileName, true)
	case CodeInvalidInput:
		badRequest(c, "Invalid input", map[string]any{"fileName": fileName}, true)
	case CodeS3Client, CodeS3Credentials:
		writeError(c, http.StatusInternalServerError, "internal_server_error", "File storage is not configured correctly. Please check your file upload settings.", map[string]any{"storage_error_code": string(code)}, true)
	default:
		log.ErrorContext(c.Request.Context(), "Storage operation failed", "err", err)
		writeError(c, http.StatusInternalServerError, "internal_server_error", "Internal server error", nil, true)
	}
}
