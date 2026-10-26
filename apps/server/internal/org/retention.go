package org

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/retention"
)

// MaxRetentionDays bounds a retention window: ten years.
const MaxRetentionDays = 3650

// RetentionWindows are the organization's retention windows in days, applied by the daily retention sweep (internal/retention); nil keeps that data forever, which every organization does until its owner sets a window.
type RetentionWindows struct {
	// WorkflowRunRetentionDays is how long finished workflow runs (completed, failed, canceled) and their logs are kept.
	WorkflowRunRetentionDays *int `json:"workflowRunRetentionDays"`
	// DisplayRetentionDays is how long survey impressions (displays) that never became a response are kept.
	DisplayRetentionDays *int `json:"displayRetentionDays"`
}

func (w RetentionWindows) equal(o RetentionWindows) bool {
	same := func(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
	return same(w.WorkflowRunRetentionDays, o.WorkflowRunRetentionDays) && same(w.DisplayRetentionDays, o.DisplayRetentionDays)
}

// Retention is the organization's Data retention card: its windows, whether this instance's sweep only counts (RETENTION_SWEEP_DRY_RUN) or deletes, when it next runs, and what its last run over the organization counted or deleted.
type Retention struct {
	RetentionWindows
	// DryRun is the instance's RETENTION_SWEEP_DRY_RUN: the sweep counts what the windows would delete and deletes nothing. Only the instance's operator changes it.
	DryRun bool `json:"dryRun"`
	// NextSweepAt is the next daily run (midnight UTC).
	NextSweepAt time.Time `json:"nextSweepAt"`
	// LastSweep is the organization's last sweep, nil when none has run with the windows it has now: a sweep under other windows does not tell what these would delete.
	LastSweep *retention.Report `json:"lastSweep"`
}

// Retention reads the organization's retention settings. Owners only (organization.write): the windows decide what is deleted for good, like deleting the organization does.
func (s *Service) Retention(ctx context.Context, userID, organizationID uuid.UUID) (*Retention, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationWrite, authz.Organization(organizationID)); err != nil {
		return nil, err
	}
	windows, found, err := s.repo.retentionWindows(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, apperr.NotFound("Organization", organizationID.String())
	}
	return s.retention(ctx, organizationID, windows)
}

func (s *Service) retention(ctx context.Context, organizationID uuid.UUID, windows RetentionWindows) (*Retention, error) {
	out := &Retention{RetentionWindows: windows, DryRun: s.cfg.RetentionSweepDryRun, NextSweepAt: retention.Schedule.Next(time.Now())}
	report, err := retention.LastReport(ctx, s.repo.db, organizationID)
	if err != nil {
		return nil, err
	}
	if report != nil && windows.equal(RetentionWindows{WorkflowRunRetentionDays: report.WorkflowRunRetentionDays, DisplayRetentionDays: report.DisplayRetentionDays}) &&
		(windows.WorkflowRunRetentionDays != nil || windows.DisplayRetentionDays != nil) {
		out.LastSweep = report
	}
	return out, nil
}

// SetRetention replaces the organization's retention windows, owners only. The next sweep applies them: on an instance where the sweep deletes, rows older than a window then go for good.
func (s *Service) SetRetention(ctx context.Context, userID, organizationID uuid.UUID, windows RetentionWindows) (*Retention, error) {
	old, err := s.setRetention(ctx, userID, organizationID, windows)
	var oldObject, newObject any
	if old != nil {
		oldObject, newObject = old, windows
	}
	s.record(ctx, audit.ActionUpdated, audit.TargetOrganization, organizationID.String(), organizationID.String(), err, oldObject, newObject)
	if err != nil {
		return nil, err
	}
	return s.retention(ctx, organizationID, windows)
}

func (s *Service) setRetention(ctx context.Context, userID, organizationID uuid.UUID, windows RetentionWindows) (*RetentionWindows, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationWrite, authz.Organization(organizationID)); err != nil {
		return nil, err
	}
	var old RetentionWindows
	err := s.repo.transaction(ctx, func(tx *Repository) error {
		exists, err := tx.lock(ctx, organizationID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.NotFound("Organization", organizationID.String())
		}
		if old, _, err = tx.retentionWindows(ctx, organizationID); err != nil {
			return err
		}
		return tx.update(ctx, organizationID, []column{{"workflow_run_retention_days", windows.WorkflowRunRetentionDays}, {"display_retention_days", windows.DisplayRetentionDays}})
	})
	if err != nil {
		return nil, err
	}
	return &old, nil
}

// retentionWindows reads the organization's windows and whether it exists.
func (r *Repository) retentionWindows(ctx context.Context, organizationID uuid.UUID) (RetentionWindows, bool, error) {
	var rows []struct {
		WorkflowRunRetentionDays sql.NullInt32
		DisplayRetentionDays     sql.NullInt32
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT workflow_run_retention_days, display_retention_days FROM organizations WHERE id = ?`, organizationID).Scan(&rows).Error; err != nil {
		return RetentionWindows{}, false, fmt.Errorf("load retention windows: %w", err)
	}
	if len(rows) == 0 {
		return RetentionWindows{}, false, nil
	}
	days := func(n sql.NullInt32) *int {
		if !n.Valid {
			return nil
		}
		v := int(n.Int32)
		return &v
	}
	return RetentionWindows{WorkflowRunRetentionDays: days(rows[0].WorkflowRunRetentionDays), DisplayRetentionDays: days(rows[0].DisplayRetentionDays)}, true, nil
}

// nullableDays is a window in a request: a number of days, or null to keep forever. set tells an explicit null from a missing field, which the request refuses.
type nullableDays struct {
	set   bool
	value *int
}

func (n *nullableDays) UnmarshalJSON(b []byte) error {
	n.set = true
	if string(b) == "null" {
		return nil
	}
	var days int
	if err := json.Unmarshal(b, &days); err != nil {
		return err
	}
	n.value = &days
	return nil
}

// retentionRequest replaces both windows, each a number of days or null.
type retentionRequest struct {
	WorkflowRunRetentionDays nullableDays `json:"workflowRunRetentionDays"`
	DisplayRetentionDays     nullableDays `json:"displayRetentionDays"`
}

func (r *retentionRequest) validate() error {
	var fields []apperr.FieldError
	for _, f := range []struct {
		name string
		days nullableDays
	}{{"workflowRunRetentionDays", r.WorkflowRunRetentionDays}, {"displayRetentionDays", r.DisplayRetentionDays}} {
		switch {
		case !f.days.set:
			fields = append(fields, apperr.FieldError{Field: f.name, Rule: "required"})
		case f.days.value != nil && *f.days.value < 1:
			fields = append(fields, apperr.FieldError{Field: f.name, Rule: "min", Param: "1"})
		case f.days.value != nil && *f.days.value > MaxRetentionDays:
			fields = append(fields, apperr.FieldError{Field: f.name, Rule: "max", Param: strconv.Itoa(MaxRetentionDays)})
		}
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	return nil
}

func (h *Handler) retention(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	res, err := h.svc.Retention(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) setRetention(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	req := &retentionRequest{}
	if !bind(c, req) {
		return
	}
	res, err := h.svc.SetRetention(c.Request.Context(), userID(c), id, RetentionWindows{WorkflowRunRetentionDays: req.WorkflowRunRetentionDays.value, DisplayRetentionDays: req.DisplayRetentionDays.value})
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
