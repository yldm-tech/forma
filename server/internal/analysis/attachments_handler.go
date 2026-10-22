package analysis

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/v3api"
)

// cuid2 is zod's cuid2 shape, every forma2 id.
var cuid2 = regexp.MustCompile(`^[0-9a-z]+$`)

// AttachmentsHandler serves GET /api/surveys/{surveyId}/attachments, forma2's internal attachment export route: outside /api/v3, since a streaming ZIP has no contract to publish, but with the v3 conventions (session only, the api:v3 limit, RFC 9457 problems, audit). The browser reaches it by navigating, so it asks first with dryRun=true and only downloads when the counts allow.
type AttachmentsHandler struct {
	svc      *Service
	exporter *AttachmentExporter
	v3       *v3api.Middleware
	auditor  *v3api.Auditor
	limiter  *ratelimit.Limiter
	log      *slog.Logger
	now      func() time.Time
}

func NewAttachmentsHandler(svc *Service, exporter *AttachmentExporter, v3 *v3api.Middleware, auditor *v3api.Auditor, limiter *ratelimit.Limiter, log *slog.Logger) *AttachmentsHandler {
	return &AttachmentsHandler{svc: svc, exporter: exporter, v3: v3, auditor: auditor, limiter: limiter, log: log, now: time.Now}
}

// Register mounts the route on a group that renders v3 problems and reads the session.
func (h *AttachmentsHandler) Register(r gin.IRouter) {
	r.GET("/surveys/:surveyId/attachments", h.v3.Auth(v3api.AuthSession), h.attachments)
}

// AttachmentCounts is the dry run's answer.
type AttachmentCounts struct {
	FileCount       int  `json:"fileCount"`
	ResponseCount   int  `json:"responseCount"`
	ExceedsMaxFiles bool `json:"exceedsMaxFiles"`
	MaxFiles        int  `json:"maxFiles"`
}

func (h *AttachmentsHandler) attachments(c *gin.Context) {
	e := audit.Event{Action: audit.ActionExported, TargetType: audit.TargetFile}
	err := h.export(c, &e)
	if err != nil {
		v3api.Fail(c, err)
	}
	h.auditor.Record(c, e, err)
}

func (h *AttachmentsHandler) export(c *gin.Context, e *audit.Event) error {
	ctx := c.Request.Context()
	raw := c.Param("surveyId")
	if _, err := uuid.Parse(raw); (err != nil || len(raw) != 36) && !cuid2.MatchString(raw) {
		return v3api.BadRequest("Invalid route parameters", v3api.InvalidParam{Name: "surveyId", Reason: "Invalid cuid2"})
	}
	surveyID, _ := legacyid.Resolve(raw)
	q := v3api.ReadQuery(c, "filters", "dryRun")
	filters, hasFilters := c.GetQuery("filters")
	dryRun := c.Query("dryRun") == "true"
	if v, ok := c.GetQuery("dryRun"); ok && v != "true" && v != "false" {
		return v3api.BadRequest("Invalid query parameters", v3api.InvalidParam{Name: "dryRun", Reason: `Invalid option: expected one of "true"|"false"`})
	}
	if err := q.Err(); err != nil {
		return err
	}
	userID := v3api.PrincipalFrom(ctx).UserID
	sv, err := h.svc.readableSurvey(ctx, userID, surveyID, authz.SurveyRead)
	if err != nil {
		if ae, ok := apperr.As(err); ok && ae.Status == http.StatusForbidden {
			return v3api.Forbidden(v3api.ForbiddenDetail)
		}
		return err
	}
	var criteria *response.Criteria
	if hasFilters {
		var parsed any
		if json.Unmarshal([]byte(filters), &parsed) != nil {
			return v3api.BadRequest("The `filters` parameter is not valid JSON.", v3api.InvalidParam{Name: "filters", Reason: "Expected a JSON-encoded response filter."})
		}
		criteria = &response.Criteria{}
		if err := json.Unmarshal([]byte(filters), criteria); err != nil {
			return v3api.BadRequest("The `filters` parameter is not a valid response filter.", v3api.InvalidParam{Name: "filters", Reason: err.Error()})
		}
	}
	clause, err := response.Where(sv.Survey, criteria)
	if err != nil {
		reason := "Invalid"
		if ae, ok := apperr.As(err); ok {
			reason = ae.Message
		}
		return v3api.BadRequest("The `filters` parameter is not a valid response filter.", v3api.InvalidParam{Name: "filters", Reason: reason})
	}
	zone := "UTC"
	if sv.DisplayTimeZone != nil && *sv.DisplayTimeZone != "" {
		zone = *sv.DisplayTimeZone
	}
	collected, err := h.exporter.Collect(ctx, sv, clause, zone)
	if err != nil {
		return err
	}
	e.OrganizationID, e.TargetID = sv.OrganizationID.String(), sv.ID.String()
	if dryRun {
		// A pre-flight streams nothing: it is recorded as a query, not as an export.
		e.Action = audit.ActionQueried
		v3api.OK(c, AttachmentCounts{FileCount: collected.FileCount, ResponseCount: collected.ResponseCount, ExceedsMaxFiles: collected.ExceedsMaxFiles, MaxFiles: MaxAttachmentFiles})
		return nil
	}
	if collected.ExceedsMaxFiles {
		return &v3api.Problem{Status: http.StatusUnprocessableEntity, Title: "Unprocessable Content", Code: "attachment_export_too_large",
			Detail: fmt.Sprintf("This export holds more than %d files. Narrow the response filter and try again.", MaxAttachmentFiles)}
	}
	if collected.FileCount == 0 {
		return &v3api.Problem{Status: http.StatusUnprocessableEntity, Title: "Unprocessable Content", Code: "attachment_export_empty", Detail: "There are no attachments to export."}
	}
	// Charged on the download only, so the pre-flight does not spend a download's allowance.
	if err := h.limiter.Apply(ctx, ratelimit.StorageAttachmentsExport, userID.String()); err != nil {
		if ae, ok := apperr.As(err); ok && ae.Status == http.StatusTooManyRequests {
			return &v3api.Problem{Status: http.StatusTooManyRequests, Title: "Too Many Requests", Code: "too_many_requests", Detail: "Too many attachment exports. Try again shortly.", RetryAfter: ae.RetryAfter}
		}
		return err
	}
	now := h.now()
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", `attachment; filename="`+AttachmentArchiveName(sv.ID, now)+`"`)
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	if err := h.exporter.StreamZip(ctx, collected.Entries, c.Writer, now); err != nil {
		// The answer is already committed; the archive the client holds is incomplete.
		if !errors.Is(err, ctx.Err()) {
			h.log.ErrorContext(ctx, "Attachment export failed mid-stream", "err", err, "survey_id", sv.ID)
		}
	}
	return nil
}
