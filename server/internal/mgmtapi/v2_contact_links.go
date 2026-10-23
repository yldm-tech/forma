package mgmtapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/linksurvey"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/survey"
)

// maxLinkAttributeKeys is how many attribute keys a segment's links may carry.
const maxLinkAttributeKeys = 20

// expirationDays is ZContactLinkQuery's expirationDays: Number(value), between 1 and 365 (an integer for a single contact's link). Zero when absent.
func (is *issues) expirationDays(q object, integer bool) int {
	v := q.get("expirationDays")
	if isAbsent(v) {
		return 0
	}
	n, ok := is.coerceNumber("expirationDays", v, 1, 365)
	if integer && !math.IsNaN(n) && n != math.Trunc(n) {
		is.add("expirationDays", "Invalid input: expected int, received number")
		return 0
	}
	if !ok {
		return 0
	}
	return int(n)
}

// contactLink is forma2's getContactSurveyLink: the personal link on the public domain, with fresh single-use parameters when the survey takes single-use links.
func (h *Handler) contactLink(sv *survey.Survey, contactID uuid.UUID, days int) (string, error) {
	link, err := linksurvey.NewContactToken(h.cfg, contactID, sv.ID, h.now(), time.Duration(days)*24*time.Hour)
	if err != nil {
		return "", err
	}
	out := h.surveys.PublicDomain() + "/c/" + link
	if settings := sv.SingleUseSettings(); settings.Enabled {
		su, err := h.surveys.NewSingleUseLink(sv.ID, settings.IsEncrypted)
		if err != nil {
			return "", err
		}
		q := url.Values{"suId": {su.SuID}}
		if su.SuToken != "" {
			q.Set("suToken", su.SuToken)
		}
		out += "?" + q.Encode()
	}
	return out, nil
}

// expiresAt is forma2's calculateExpirationDate: now plus the days, nil without them.
func (h *Handler) expiresAt(days int) *string {
	if days == 0 {
		return nil
	}
	s := jsvalue.ISOString(h.now().Add(time.Duration(days) * 24 * time.Hour))
	return &s
}

// linkSurvey loads the survey a contact link route names and authorizes read on its workspace, answering itself when it cannot: 404 survey, 401 when refused.
func (h *Handler) linkSurvey(c *gin.Context, id uuid.UUID) *survey.Survey {
	sv, err := h.surveys.Load(c.Request.Context(), id)
	if err != nil {
		h.v2Unexpected(c, err)
		return nil
	}
	if sv == nil {
		v2Error(c, http.StatusNotFound, detail("survey", "not found"))
		return nil
	}
	if !h.v2Authorize(c, http.MethodGet, sv.WorkspaceID) {
		return nil
	}
	return sv
}

// v2ContactLink is forma2's GET /api/v2/management/surveys/{surveyId}/contact-links/contacts/{contactId}: a link survey's personal link for a contact of its workspace who has not responded yet, expiring after expirationDays when given.
func (h *Handler) v2ContactLink(c *gin.Context) {
	var is issues
	days := is.expirationDays(v2Query(c), true)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	surveyID, ok := v2PathID(c, "surveyId")
	if !ok {
		return
	}
	contactID, ok := v2PathID(c, "contactId")
	if !ok || !h.v2Limit(c) {
		return
	}
	sv := h.linkSurvey(c, surveyID)
	if sv == nil {
		return
	}
	if sv.Type != survey.TypeLink {
		v2Error(c, http.StatusBadRequest, detail("surveyId", "Not a link survey"))
		return
	}
	ctx := c.Request.Context()
	found, err := h.repo.contact(ctx, contactID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if found == nil || found.WorkspaceID != sv.WorkspaceID {
		v2Error(c, http.StatusNotFound, detail("contact", "not found"))
		return
	}
	responded, err := h.repo.hasResponded(ctx, contactID, surveyID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if responded {
		v2Error(c, http.StatusBadRequest, detail("contactId", "Already responded"))
		return
	}
	link, err := h.contactLink(sv, contactID, days)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	v2OK(c, map[string]any{"surveyUrl": link, "expiresAt": h.expiresAt(days)}, nil)
}

// v2SegmentLinks is forma2's GET .../contact-links/segments/{segmentId}: personal links for a page of the contacts a segment of the survey's workspace matches, newest first, each with the requested attributes the workspace has keys for. Only a link survey in progress gives links (403 otherwise).
func (h *Handler) v2SegmentLinks(c *gin.Context) {
	q := v2Query(c)
	var is issues
	f := listFilter{Limit: 50}
	if v := q.get("limit"); !isAbsent(v) {
		if n, ok := is.coerceNumber("limit", v, 1, 250); ok {
			f.Limit = int(n)
		}
	}
	if v := q.get("skip"); !isAbsent(v) {
		if n, ok := is.coerceNumber("skip", v, 0, math.Inf(1)); ok {
			f.Skip = saturatingInt(n)
		}
	}
	days := is.expirationDays(q, false)
	attributeKeys, hasKeys := is.str("attributeKeys", q.get("attributeKeys"), optional)
	if hasKeys && len(strings.Split(attributeKeys, ",")) > maxLinkAttributeKeys {
		is.add("attributeKeys", "You can have max 20 keys.")
	}
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	surveyID, ok := v2PathID(c, "surveyId")
	if !ok {
		return
	}
	segmentID, ok := v2PathID(c, "segmentId")
	if !ok || !h.v2Limit(c) {
		return
	}
	sv := h.linkSurvey(c, surveyID)
	if sv == nil {
		return
	}
	if sv.Type != survey.TypeLink || sv.Status != survey.StatusInProgress {
		v2Error(c, http.StatusForbidden, nil)
		return
	}
	ctx := c.Request.Context()
	segmentWorkspace, err := h.repo.segmentWorkspace(ctx, segmentID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if segmentWorkspace == nil {
		v2Error(c, http.StatusNotFound, detail("segment", "not found"))
		return
	}
	if *segmentWorkspace != sv.WorkspaceID {
		v2Error(c, http.StatusBadRequest, detail("segmentId", "Environment mismatch"))
		return
	}
	ids, total, err := h.segments.SegmentContacts(ctx, segmentID, f.Limit, f.Skip)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	keys, err := h.includedKeys(ctx, sv.WorkspaceID, attributeKeys)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	values, err := h.repo.attributeValues(ctx, ids, keys)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	expires := h.expiresAt(days)
	out := []map[string]any{}
	for _, id := range ids {
		link, err := h.contactLink(sv, id, days)
		if err != nil {
			h.log.ErrorContext(ctx, "Failed to generate survey URL for contact", "err", err, "contactId", id.String(), "surveyId", sv.ID.String())
			continue
		}
		entry := map[string]any{"contactId": id, "surveyUrl": link, "expiresAt": expires}
		if attrs := values[id]; len(attrs) > 0 {
			entry["attributes"] = attrs
		}
		out = append(out, entry)
	}
	v2OK(c, out, f.meta(total))
}

// includedKeys are the requested attribute keys the workspace has, at most 20.
func (h *Handler) includedKeys(ctx context.Context, workspaceID uuid.UUID, requested string) ([]string, error) {
	keys, err := h.repo.keys(ctx, []uuid.UUID{workspaceID})
	if err != nil {
		return nil, fmt.Errorf("load attribute keys: %w", err)
	}
	var out []string
	for _, field := range strings.Split(requested, ",") {
		field = strings.TrimSpace(field)
		for _, k := range keys {
			if k.Key == field && !contains(out, field) {
				out = append(out, field)
				break
			}
		}
	}
	if len(out) > maxLinkAttributeKeys {
		out = out[:maxLinkAttributeKeys]
	}
	return out, nil
}
