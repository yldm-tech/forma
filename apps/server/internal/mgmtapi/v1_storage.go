package mgmtapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v1api"
	"github.com/yldm-tech/forma/apps/server/internal/storage"
)

// maxPublicFileSize is forma2's MAX_PUBLIC_FILE_SIZE_MB for a public upload, in bytes.
const maxPublicFileSize = 5 * 1024 * 1024

// allowedExtensionsMessage is zod's message for a value outside ZAllowedFileExtension.
var allowedExtensionsMessage = "Invalid option: expected one of " + v1api.QuotedOptions(storage.AllowedExtensions)

// v1StorageUpload is forma2's POST /api/v1/management/storage: a presigned upload of a public file (a logo, a background image) for a workspace the caller may write, by API key or by the signed-in user. An API key may name the workspace by its legacy environment id; a session must name it by id.
func (h *Handler) v1StorageUpload(c *gin.Context) {
	body, ok := mgmtv1.ReadBody(c)
	if !ok {
		return
	}
	if mgmtv1.PrincipalOf(c) != nil {
		if _, ok := h.v1.ResolveWorkspace(c, body, http.MethodPost); !ok {
			return
		}
	} else if !v1api.Truthy(body["workspaceId"]) {
		v1api.BadRequest(c, "workspaceId must be provided", nil, false)
		return
	}
	o := object(body)
	var is issues
	fileName, hasName := is.str("fileName", o.get("fileName"), required)
	fileName = jsTrim(fileName)
	if hasName && fileName == "" {
		is.add("fileName", "Too small: expected string to have >=1 characters")
	}
	fileType, hasType := is.str("fileType", o.get("fileType"), required)
	fileType = jsTrim(fileType)
	if hasType && fileType == "" {
		is.add("fileType", "Too small: expected string to have >=1 characters")
	}
	rawWorkspace, _ := is.id("workspaceId", o.get("workspaceId"), required)
	var allowed []string
	if raw := o.get("allowedFileExtensions"); !isAbsent(raw) {
		list, ok := raw.([]any)
		if !ok {
			is.typeError("allowedFileExtensions", "array", raw)
		}
		for i, v := range list {
			s, isString := v.(string)
			if !isString || !storage.IsAllowedExtension(s) {
				is.add(join("allowedFileExtensions", strconv.Itoa(i)), allowedExtensionsMessage)
				is.aborted = true
				continue
			}
			allowed = append(allowed, s)
		}
	}
	// The refinement runs only when every field had the right type, as zod runs an object's refinements.
	if !is.aborted {
		if issue := storage.ValidateUploadInput(fileName, fileType, allowed); issue != nil {
			is.add(issue.Field, issue.Message)
		}
	}
	if is.failed() {
		v1api.BadRequest(c, messageInvalidFields, is.v1Details(), true)
		return
	}
	workspaceID, _ := legacyid.Resolve(rawWorkspace)
	if !h.v1.Authorize(c, http.MethodPost, workspaceID) {
		return
	}
	upload, err := h.files.SignUpload(c.Request.Context(), fileName, workspaceID.String(), storage.AccessPublic, maxPublicFileSize)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "Error getting signed url for upload", "err", err)
		switch code := storage.CodeOf(err); code {
		case storage.CodeInvalidInput:
			v1api.BadRequest(c, "Invalid input", map[string]any{"fileName": fileName}, true)
		case storage.CodeS3Client, storage.CodeS3Credentials:
			v1api.Error(c, http.StatusInternalServerError, "internal_server_error", "File storage is not configured correctly. Please check your file upload settings.", map[string]any{"storage_error_code": string(code)}, true)
		default:
			v1api.Error(c, http.StatusInternalServerError, "internal_server_error", "Internal server error", nil, true)
		}
		return
	}
	v1OK(c, upload)
}
