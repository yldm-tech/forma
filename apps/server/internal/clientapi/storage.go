package clientapi

import (
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/storage"
)

// elementIDPattern is ZUploadPrivateFileRequest's rule for elementId.
var elementIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// allowedExtensionsMessage is zod's message for a value outside ZAllowedFileExtension.
var allowedExtensionsMessage = func() string {
	quoted := make([]string, len(storage.AllowedExtensions))
	for i, e := range storage.AllowedExtensions {
		quoted[i] = `"` + e + `"`
	}
	return "Invalid option: expected one of " + strings.Join(quoted, "|")
}()

// storageUpload is forma2's client storage route: a presigned upload of a respondent's file to a file upload element of one of the workspace's surveys, stored privately under surveys/{survey}/elements/{element}. Besides the per-address storage:upload limit, a workspace takes at most storage:upload:workspace uploads a minute.
func (h *Handler) storageUpload(c *gin.Context) {
	ctx := c.Request.Context()
	ws, ok := h.workspace(c, c.Param("workspaceId"), msgUnexpected)
	if !ok {
		return
	}
	body, _, err := parseBody(c.Request)
	if err != nil {
		h.bodyError(c, err, msgMalformedJSON)
		return
	}
	in := asObject(body)
	var is issues
	fileName, hasName := is.str("fileName", in.get("fileName"), required)
	fileName = jsTrim(fileName)
	if hasName && fileName == "" {
		is.add("fileName", "Too small: expected string to have >=1 characters")
	}
	fileType, hasType := is.str("fileType", in.get("fileType"), required)
	fileType = jsTrim(fileType)
	if hasType && fileType == "" {
		is.add("fileType", "Too small: expected string to have >=1 characters")
	}
	var allowed []string
	if raw := in.get("allowedFileExtensions"); !isAbsent(raw) {
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
	surveyID, _ := is.id("surveyId", in.get("surveyId"), required)
	elementID, hasElement := is.str("elementId", in.get("elementId"), required)
	elementID = jsTrim(elementID)
	if hasElement {
		if elementID == "" {
			is.add("elementId", "Too small: expected string to have >=1 characters")
		}
		if !elementIDPattern.MatchString(elementID) {
			is.add("elementId", "Element id must contain only alphanumeric characters, hyphens, or underscores")
		}
	}
	// The refinements run only when every field had the right type, as zod runs an object's refinements.
	if !is.aborted {
		if issue := storage.ValidateUploadInput(fileName, fileType, allowed); issue != nil {
			is.add(issue.Field, issue.Message)
		}
	}
	if is.failed() {
		h.log.ErrorContext(ctx, "Fields are missing or incorrectly formatted")
		writeError(c, badRequest(msgInvalidFields, is.details))
		return
	}

	sv, err := h.responses.Survey(ctx, surveyID)
	if err != nil {
		h.fail(c, err, msgUnexpected)
		return
	}
	if sv.WorkspaceID != ws.ID {
		writeError(c, badRequest("Survey does not belong to the workspace", fields{{"surveyId", surveyID}, {"workspaceId", ws.ID.String()}}))
		return
	}
	res, err := h.limiter.Check(ctx, ratelimit.StorageUploadPerWorkspace, ws.ID.String(), 1)
	if err != nil {
		h.fail(c, err, msgUnexpected)
		return
	}
	if !res.Allowed {
		writeError(c, tooManyRequests())
		return
	}

	elements, err := surveyElements(sv.Blocks)
	if err != nil {
		h.fail(c, err, msgUnexpected)
		return
	}
	uploads := fileUploadElements(elements)
	i := slices.IndexFunc(uploads, func(e fileUploadElement) bool { return e.ID == elementID })
	switch {
	case len(uploads) == 0:
		writeError(c, badRequest("Survey does not allow file uploads", nil))
		return
	case i < 0:
		writeError(c, badRequest("Element does not allow file uploads", nil))
		return
	}
	element := uploads[i]
	if ext := storage.Extension(fileName); !storage.IsAllowedExtension(ext) || (element.AllowedFileExtensions != nil && !slices.Contains(*element.AllowedFileExtensions, ext)) {
		writeError(c, badRequest("File extension is not allowed for this element", nil))
		return
	}

	upload, err := h.files.SignUpload(ctx, fileName, ws.ID.String(), storage.AccessPrivate, storage.RespondentUploadMaxSize(element.MaxSizeInMB), "surveys", sv.ID.String(), "elements", elementID)
	if err != nil {
		h.storageError(c, err, fileName)
		return
	}
	writeData(c, upload, noStore)
}

// storageError is forma2's getErrorResponseFromStorageError: invalid input 400 naming the file, a storage configuration error 500 with the code the survey runtime reads to say uploads are unavailable, anything else 500.
func (h *Handler) storageError(c *gin.Context, err error, fileName string) {
	h.log.ErrorContext(c.Request.Context(), "Error getting signed url for upload", "err", err)
	switch code := storage.CodeOf(err); code {
	case storage.CodeInvalidInput:
		writeError(c, badRequest("Invalid input", fields{{"fileName", fileName}}))
	case storage.CodeS3Client, storage.CodeS3Credentials:
		writeError(c, newError(http.StatusInternalServerError, codeInternal, "File storage is not configured correctly. Please check your file upload settings.", fields{{"storage_error_code", string(code)}}))
	case storage.CodeFileNotFound:
		writeError(c, notFound("file", &fileName))
	default:
		writeError(c, internal("Internal server error"))
	}
}
