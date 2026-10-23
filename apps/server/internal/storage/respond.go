package storage

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/v1api"
)

// WriteStorageError is forma2's getErrorResponseFromStorageError, with its CORS headers: a missing file is 404, invalid input 400 with the file name, a storage configuration error 500 with the code clients read to tell an unconfigured instance from a failed upload, and anything else 500. fileName is the name the request asked for. An unknown failure is logged here.
func WriteStorageError(c *gin.Context, log *slog.Logger, err error, fileName string) {
	switch code := CodeOf(err); code {
	case CodeFileNotFound:
		v1api.NotFound(c, "file", fileName, true)
	case CodeInvalidInput:
		v1api.BadRequest(c, "Invalid input", map[string]any{"fileName": fileName}, true)
	case CodeS3Client, CodeS3Credentials:
		v1api.Error(c, http.StatusInternalServerError, "internal_server_error", "File storage is not configured correctly. Please check your file upload settings.", map[string]any{"storage_error_code": string(code)}, true)
	default:
		log.ErrorContext(c.Request.Context(), "Storage operation failed", "err", err)
		v1api.Error(c, http.StatusInternalServerError, "internal_server_error", "Internal server error", nil, true)
	}
}
