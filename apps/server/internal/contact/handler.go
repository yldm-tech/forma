package contact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Handler serves the contact routes of /api/app. Every route needs a session.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth) *Handler {
	return &Handler{svc: svc, sessions: sessions}
}

// Register mounts the routes on the /api/app group, which already applies the error envelope and the CSRF check to every mutation.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("", h.sessions.Require())
	g.GET("/workspaces/:workspaceId/contact-attribute-keys", h.keys)
	g.POST("/workspaces/:workspaceId/contact-attribute-keys", h.createKey)
	g.PATCH("/contact-attribute-keys/:keyId", h.updateKey)
	g.DELETE("/contact-attribute-keys/:keyId", h.deleteKey)
	g.GET("/contact-attribute-keys/:keyId/values", h.distinctValues)

	g.GET("/workspaces/:workspaceId/contacts", h.list)
	g.POST("/workspaces/:workspaceId/contacts/import", h.importCSV)
	g.POST("/workspaces/:workspaceId/contacts/export", h.exportCSV)
	g.GET("/workspaces/:workspaceId/contacts/:contactId", h.get)
	g.DELETE("/contacts/:contactId", h.delete)
	g.PUT("/contacts/:contactId/attributes", h.updateAttributes)

	g.GET("/workspaces/:workspaceId/segments", h.segments)
	g.POST("/workspaces/:workspaceId/segments", h.createSegment)
	g.GET("/workspaces/:workspaceId/segments/contact-counts", h.segmentContactCounts)
	g.POST("/workspaces/:workspaceId/segments/preview-count", h.previewSegmentCount)
	g.GET("/workspaces/:workspaceId/segment-surveys", h.surveyRefs)
	g.GET("/segments/:segmentId", h.segment)
	g.PATCH("/segments/:segmentId", h.updateSegment)
	g.DELETE("/segments/:segmentId", h.deleteSegment)
	g.GET("/segments/:segmentId/result-shares", h.segmentResultShares)
	g.POST("/segments/:segmentId/clone", h.cloneSegment)
	g.POST("/surveys/:surveyId/segment/reset", h.resetSegment)
}

func userID(c *gin.Context) uuid.UUID {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID
}

func pathID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := httpapi.ParamUUID(c, name)
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, false
	}
	return id, true
}

// respond writes v, or fails the request with err.
func respond(c *gin.Context, status int, v any, err error) {
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(status, v)
}

func (h *Handler) keys(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	keys, err := h.svc.Keys(c.Request.Context(), userID(c), ws)
	respond(c, http.StatusOK, keys, err)
}

type createKeyRequest struct {
	Key         *string `json:"key" binding:"required"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	DataType    *string `json:"dataType" binding:"omitempty,oneof=string number date"`
}

func (h *Handler) createKey(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	var req createKeyRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	k, err := h.svc.CreateKey(c.Request.Context(), userID(c), ws, NewKeyInput{Key: *req.Key, Name: req.Name, Description: req.Description, DataType: req.DataType})
	respond(c, http.StatusCreated, k, err)
}

type updateKeyRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (h *Handler) updateKey(c *gin.Context) {
	id, ok := pathID(c, "keyId")
	if !ok {
		return
	}
	var req updateKeyRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	k, err := h.svc.UpdateKey(c.Request.Context(), userID(c), id, KeyUpdate(req))
	respond(c, http.StatusOK, k, err)
}

func (h *Handler) deleteKey(c *gin.Context) {
	id, ok := pathID(c, "keyId")
	if !ok {
		return
	}
	k, err := h.svc.DeleteKey(c.Request.Context(), userID(c), id)
	respond(c, http.StatusOK, k, err)
}

func (h *Handler) distinctValues(c *gin.Context) {
	id, ok := pathID(c, "keyId")
	if !ok {
		return
	}
	values, err := h.svc.DistinctValues(c.Request.Context(), userID(c), id)
	respond(c, http.StatusOK, values, err)
}

type listQuery struct {
	Offset int    `form:"offset" binding:"min=0"`
	Search string `form:"search"`
}

func (h *Handler) list(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	var q listQuery
	if err := httpapi.BindQuery(c, &q); err != nil {
		httpapi.Fail(c, err)
		return
	}
	contacts, err := h.svc.List(c.Request.Context(), userID(c), ws, q.Offset, q.Search)
	respond(c, http.StatusOK, contacts, err)
}

func (h *Handler) get(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	id, ok := pathID(c, "contactId")
	if !ok {
		return
	}
	d, err := h.svc.Get(c.Request.Context(), userID(c), ws, id)
	respond(c, http.StatusOK, d, err)
}

func (h *Handler) delete(c *gin.Context) {
	id, ok := pathID(c, "contactId")
	if !ok {
		return
	}
	old, err := h.svc.Delete(c.Request.Context(), userID(c), id)
	respond(c, http.StatusOK, old, err)
}

type updateAttributesRequest struct {
	Attributes json.RawMessage `json:"attributes" binding:"required"`
}

func (h *Handler) updateAttributes(c *gin.Context) {
	id, ok := pathID(c, "contactId")
	if !ok {
		return
	}
	var req updateAttributesRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	attrs, err := decodeAttributes(req.Attributes)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	res, err := h.svc.UpdateAttributes(c.Request.Context(), userID(c), id, attrs)
	respond(c, http.StatusOK, res, err)
}

// decodeAttributes reads forma2's ZContactAttributesInput, an object of strings, numbers and booleans, keeping the order the keys were sent in. Booleans become "true" and "false", as forma2 coerced them.
func decodeAttributes(raw json.RawMessage) ([]AttributeInput, error) {
	invalid := func(key string) error {
		return apperr.Validation([]apperr.FieldError{{Field: "attributes" + key, Rule: "type"}})
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, invalid("")
	}
	var out []AttributeInput
	seen := map[string]int{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, invalid("")
		}
		key, _ := tok.(string)
		var v Value
		tok, err = dec.Token()
		if err != nil {
			return nil, invalid("." + key)
		}
		switch t := tok.(type) {
		case string:
			v = Value{String: t}
		case bool:
			v = Value{String: map[bool]string{true: "true", false: "false"}[t]}
		case json.Number:
			f, err := t.Float64()
			if err != nil {
				return nil, invalid("." + key)
			}
			v = Value{Number: f, IsNumber: true}
		default:
			return nil, invalid("." + key)
		}
		// A repeated key keeps its first position and its last value, as a JavaScript object does.
		if i, ok := seen[key]; ok {
			out[i].Value = v
			continue
		}
		seen[key] = len(out)
		out = append(out, AttributeInput{Key: key, Value: v})
	}
	return out, nil
}

// importFormLimit bounds the whole multipart body: the file plus the two small fields and the part headers.
const importFormLimit = MaxImportBytes + 64*1024

func (h *Handler) importCSV(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	in, err := readImportForm(c)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	res, err := h.svc.Import(c.Request.Context(), userID(c), ws, *in)
	respond(c, http.StatusOK, res, err)
}

// readImportForm reads the multipart import form: `file`, the CSV of at most MaxImportBytes, `duplicateContactsAction`, and optionally `attributeMap`, a JSON object from column to key id or new key name. Like forma2's dialog it takes a file that is named .csv or declared as CSV.
func readImportForm(c *gin.Context) (*ImportInput, error) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, apperr.BadRequest("Content-Type must be multipart/form-data")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, importFormLimit)
	mr, err := c.Request.MultipartReader()
	if err != nil {
		return nil, apperr.BadRequest("Malformed multipart body")
	}
	in := &ImportInput{AttributeMap: map[string]string{}}
	var file []byte
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, formError(err)
		}
		data, err := io.ReadAll(io.LimitReader(part, MaxImportBytes+1))
		_ = part.Close()
		if err != nil {
			return nil, formError(err)
		}
		switch part.FormName() {
		case "file":
			if len(data) > MaxImportBytes {
				return nil, &apperr.Error{Status: http.StatusRequestEntityTooLarge, Code: "upload_contacts_error_file_too_large", Message: "The file is too large. The maximum size is 800 KB."}
			}
			declared := part.Header.Get("Content-Type")
			if (declared == "" && !strings.HasSuffix(part.FileName(), ".csv")) || (declared != "" && !strings.Contains(declared, "csv")) {
				return nil, apperr.InvalidInput("upload_contacts_error_invalid_file_type", "Please upload a CSV file")
			}
			file = data
		case "duplicateContactsAction":
			in.DuplicateAction = string(data)
		case "attributeMap":
			if err := json.Unmarshal(data, &in.AttributeMap); err != nil {
				return nil, apperr.Validation([]apperr.FieldError{{Field: "attributeMap", Rule: "type"}})
			}
		}
	}
	if file == nil {
		return nil, apperr.Validation([]apperr.FieldError{{Field: "file", Rule: "required"}})
	}
	switch in.DuplicateAction {
	case DuplicateSkip, DuplicateUpdate, DuplicateOverwrite:
	default:
		return nil, apperr.Validation([]apperr.FieldError{{Field: "duplicateContactsAction", Rule: "oneof", Param: "skip update overwrite"}})
	}
	in.File = bytes.NewReader(file)
	return in, nil
}

func formError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return apperr.RequestTooLarge(importFormLimit)
	}
	return apperr.BadRequest("Malformed multipart body")
}

func (h *Handler) segments(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	segs, err := h.svc.Segments(c.Request.Context(), userID(c), ws)
	respond(c, http.StatusOK, segs, err)
}

func (h *Handler) segmentContactCounts(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	counts, err := h.svc.SegmentContactCounts(c.Request.Context(), userID(c), ws)
	respond(c, http.StatusOK, counts, err)
}

// previewSegmentCountRequest is a filter tree being edited, not yet saved.
type previewSegmentCountRequest struct {
	Filters json.RawMessage `json:"filters" binding:"required"`
}

func (h *Handler) previewSegmentCount(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	var req previewSegmentCountRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	count, err := h.svc.PreviewSegmentCount(c.Request.Context(), userID(c), ws, req.Filters)
	respond(c, http.StatusOK, count, err)
}

func (h *Handler) surveyRefs(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	refs, err := h.svc.SurveyFilterRefs(c.Request.Context(), userID(c), ws)
	respond(c, http.StatusOK, refs, err)
}

func (h *Handler) segment(c *gin.Context) {
	id, ok := pathID(c, "segmentId")
	if !ok {
		return
	}
	seg, err := h.svc.Segment(c.Request.Context(), userID(c), id)
	respond(c, http.StatusOK, seg, err)
}

// createSegmentRequest is forma2's ZSegmentCreateInput. An empty surveyId, as the segments page sends, links no survey.
type createSegmentRequest struct {
	Title       *string         `json:"title" binding:"required"`
	Description *string         `json:"description"`
	IsPrivate   *bool           `json:"isPrivate"`
	Filters     json.RawMessage `json:"filters" binding:"required"`
	SurveyID    *string         `json:"surveyId" binding:"required"`
}

func (h *Handler) createSegment(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	var req createSegmentRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	in := SegmentInput{Title: *req.Title, Description: req.Description, IsPrivate: req.IsPrivate == nil || *req.IsPrivate, Filters: req.Filters}
	if *req.SurveyID != "" {
		id, err := uuid.Parse(*req.SurveyID)
		if err != nil {
			httpapi.Fail(c, apperr.Validation([]apperr.FieldError{{Field: "surveyId", Rule: "uuid"}}))
			return
		}
		in.SurveyID = &id
	}
	seg, err := h.svc.CreateSegment(c.Request.Context(), userID(c), ws, in)
	respond(c, http.StatusCreated, seg, err)
}

// updateSegmentRequest is forma2's ZSegmentUpdateInput; description distinguishes null from absent.
type updateSegmentRequest struct {
	Title       *string         `json:"title"`
	Description json.RawMessage `json:"description"`
	IsPrivate   *bool           `json:"isPrivate"`
	Filters     json.RawMessage `json:"filters"`
	Surveys     *[]uuid.UUID    `json:"surveys" binding:"omitempty,max=500"`
}

func (h *Handler) updateSegment(c *gin.Context) {
	id, ok := pathID(c, "segmentId")
	if !ok {
		return
	}
	var req updateSegmentRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	in := SegmentUpdate{Title: req.Title, IsPrivate: req.IsPrivate, Filters: req.Filters}
	if req.Description != nil {
		in.DescriptionSet = true
		if string(bytes.TrimSpace(req.Description)) != "null" {
			var d string
			if err := json.Unmarshal(req.Description, &d); err != nil {
				httpapi.Fail(c, apperr.Validation([]apperr.FieldError{{Field: "description", Rule: "type"}}))
				return
			}
			in.Description = &d
		}
	}
	if req.Surveys != nil {
		in.Surveys, in.SurveysSet = *req.Surveys, true
	}
	seg, err := h.svc.UpdateSegment(c.Request.Context(), userID(c), id, in)
	respond(c, http.StatusOK, seg, err)
}

func (h *Handler) deleteSegment(c *gin.Context) {
	id, ok := pathID(c, "segmentId")
	if !ok {
		return
	}
	old, err := h.svc.DeleteSegment(c.Request.Context(), userID(c), id)
	respond(c, http.StatusOK, old, err)
}

func (h *Handler) segmentResultShares(c *gin.Context) {
	id, ok := pathID(c, "segmentId")
	if !ok {
		return
	}
	shares, err := h.svc.SegmentResultShares(c.Request.Context(), userID(c), id)
	respond(c, http.StatusOK, shares, err)
}

type cloneSegmentRequest struct {
	SurveyID uuid.UUID `json:"surveyId" binding:"required"`
}

func (h *Handler) cloneSegment(c *gin.Context) {
	id, ok := pathID(c, "segmentId")
	if !ok {
		return
	}
	var req cloneSegmentRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	seg, err := h.svc.CloneSegment(c.Request.Context(), userID(c), id, req.SurveyID)
	respond(c, http.StatusCreated, seg, err)
}

func (h *Handler) resetSegment(c *gin.Context) {
	id, ok := pathID(c, "surveyId")
	if !ok {
		return
	}
	seg, err := h.svc.ResetSurveySegment(c.Request.Context(), userID(c), id)
	respond(c, http.StatusOK, seg, err)
}

// exportRequest names the contacts to export: the contacts list's search, empty for all of them.
type exportRequest struct {
	Search string `json:"search"`
}

// exportCSV answers the CSV itself, named by Content-Disposition. It is written to a temporary file first, so a failure halfway is still an error envelope rather than a truncated download.
func (h *Handler) exportCSV(c *gin.Context) {
	ws, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	var req exportRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	file, err := os.CreateTemp("", "forma-contacts-export-*")
	if err != nil {
		httpapi.Fail(c, fmt.Errorf("create contacts export file: %w", err))
		return
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	name, err := h.svc.Export(c.Request.Context(), userID(c), ws, req.Search, file)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	size, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		httpapi.Fail(c, fmt.Errorf("measure contacts export file: %w", err))
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		httpapi.Fail(c, fmt.Errorf("rewind contacts export file: %w", err))
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.DataFromReader(http.StatusOK, size, ContentTypeCSV, file, map[string]string{"Content-Disposition": `attachment; filename="` + name + `"`})
}
