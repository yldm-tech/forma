package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/response"
)

// DisplayContact is the contact a display was shown to, with its email and userId attributes.
type DisplayContact struct {
	ID         uuid.UUID         `json:"id"`
	Attributes map[string]string `json:"attributes"`
}

// DisplayWithContact is forma2's TDisplayWithContact, a row of the summary's Impressions tab.
type DisplayWithContact struct {
	ID        uuid.UUID       `json:"id"`
	CreatedAt time.Time       `json:"createdAt"`
	SurveyID  uuid.UUID       `json:"surveyId"`
	Contact   *DisplayContact `json:"contact"`
}

// DisplaysWithContact is forma2's getDisplaysWithContactAction: the survey's displays shown to a contact, newest first, a page of limit (1 to 100) after offset.
func (s *Service) DisplaysWithContact(ctx context.Context, userID, surveyID uuid.UUID, limit, offset int) ([]DisplayWithContact, error) {
	if _, err := s.readableSurvey(ctx, userID, surveyID, authz.SurveyResponseRead); err != nil {
		return nil, err
	}
	var rows []struct {
		ID         uuid.UUID
		CreatedAt  time.Time
		SurveyID   uuid.UUID
		ContactID  uuid.UUID
		Attributes []byte
	}
	err := s.db.WithContext(ctx).Raw(`SELECT d.id, d.created_at, d.survey_id, d.contact_id,
		(SELECT COALESCE(jsonb_object_agg(k.key, a.value_text), '{}'::jsonb) FROM contact_attributes a JOIN contact_attribute_keys k ON k.id = a.attribute_key_id
			WHERE a.contact_id = d.contact_id AND k.key IN ('email', 'userId')) AS attributes
		FROM displays d WHERE d.survey_id = ? AND d.contact_id IS NOT NULL ORDER BY d.created_at DESC, d.id DESC LIMIT ? OFFSET ?`, surveyID, limit, offset).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list displays: %w", err)
	}
	out := make([]DisplayWithContact, len(rows))
	for i, r := range rows {
		c := &DisplayContact{ID: r.ContactID, Attributes: map[string]string{}}
		var attributes map[string]*string
		if err := json.Unmarshal(r.Attributes, &attributes); err != nil {
			return nil, fmt.Errorf("decode display contact attributes: %w", err)
		}
		for k, v := range attributes {
			if v != nil {
				c.Attributes[k] = *v
			}
		}
		out[i] = DisplayWithContact{ID: r.ID, CreatedAt: r.CreatedAt, SurveyID: r.SurveyID, Contact: c}
	}
	return out, nil
}

// ResetResult is what a survey reset removed.
type ResetResult struct {
	Success               bool `json:"success"`
	DeletedResponsesCount int  `json:"deletedResponsesCount"`
	DeletedDisplaysCount  int  `json:"deletedDisplaysCount"`
}

// storageDeleteChunk is forma2's STORAGE_DELETE_CHUNK_SIZE: how many uploaded files one cleanup job deletes.
const storageDeleteChunk = 100

// responseFileScanPage is forma2's RESPONSE_FILE_SCAN_PAGE_SIZE: how many responses a reset reads at once, so resetting a large survey never holds every response's data in memory. Only the URLs accumulate, and storageDeleteChunk bounds them on the way out.
const responseFileScanPage = 500

// responseFileURLs is forma2's collectSurveyResponseFileUrls: the storage URLs the survey's file upload answers point at, read in pages of responseFileScanPage in (created_at, id) order, which the (survey_id, created_at) index serves and id makes total. Each page locks its rows, so a respondent cannot change an answer between the scan and the delete.
func responseFileURLs(ctx context.Context, db *gorm.DB, surveyID uuid.UUID, fileUploads map[string]bool) ([]string, error) {
	if len(fileUploads) == 0 {
		return nil, nil
	}
	var urls []string
	// The scan starts before every row; each page starts after the last row of the one before.
	query := db.WithContext(ctx).Raw(`SELECT id, created_at, data FROM responses WHERE survey_id = ? ORDER BY created_at, id LIMIT ? FOR UPDATE`, surveyID, responseFileScanPage)
	for {
		var page []struct {
			ID        uuid.UUID
			CreatedAt time.Time
			Data      []byte
		}
		if err := query.Scan(&page).Error; err != nil {
			return nil, fmt.Errorf("read response files: %w", err)
		}
		for _, r := range page {
			found, err := response.FileURLs(r.Data, fileUploads)
			if err != nil {
				return nil, err
			}
			urls = append(urls, found...)
		}
		// A short page is the last one.
		if len(page) < responseFileScanPage {
			return urls, nil
		}
		last := page[len(page)-1]
		query = db.WithContext(ctx).Raw(`SELECT id, created_at, data FROM responses WHERE survey_id = ? AND (created_at, id) > (?, ?) ORDER BY created_at, id LIMIT ? FOR UPDATE`, surveyID, last.CreatedAt, last.ID, responseFileScanPage)
	}
}

// Reset is forma2's resetSurveyAction: every response and display of the survey goes in one transaction, which also enqueues the deletion of the files the survey's file upload answers point at, a hundred per job. An archived survey keeps its history.
func (s *Service) Reset(ctx context.Context, userID, surveyID uuid.UUID) (result *ResetResult, err error) {
	var organizationID *uuid.UUID
	defer func() {
		e := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetSurvey, TargetID: surveyID.String(), ActorType: audit.ActorUser, Status: audit.StatusSuccess}
		if organizationID != nil {
			e.OrganizationID = organizationID.String()
		}
		if err != nil {
			e.Status = audit.StatusFailure
		} else {
			e.NewObject = map[string]int{"deletedResponsesCount": result.DeletedResponsesCount, "deletedDisplaysCount": result.DeletedDisplaysCount}
		}
		s.audit.Log(ctx, audit.FromRequest(ctx, e))
	}()
	if err := s.authz.Require(ctx, authz.User(userID), authz.SurveyWrite, authz.Survey(surveyID)); err != nil {
		return nil, err
	}
	sv, err := loadSurveyDoc(ctx, s.db, surveyID)
	if err != nil {
		return nil, err
	}
	if sv == nil {
		return nil, apperr.Forbidden("Not authorized")
	}
	organizationID = &sv.OrganizationID
	if sv.ArchivedAt != nil {
		return nil, apperr.OperationNotAllowed("Cannot reset an archived survey.")
	}
	fileUploads := sv.FileUploadElementIDs()
	result = &ResetResult{Success: true}
	err = s.jobs.Transaction(ctx, s.db, func(tx *jobs.Tx) error {
		urls, err := responseFileURLs(ctx, tx.DB, surveyID, fileUploads)
		if err != nil {
			return err
		}
		deleted := tx.WithContext(ctx).Exec(`DELETE FROM responses WHERE survey_id = ?`, surveyID)
		if deleted.Error != nil {
			return fmt.Errorf("delete responses: %w", deleted.Error)
		}
		result.DeletedResponsesCount = int(deleted.RowsAffected)
		deleted = tx.WithContext(ctx).Exec(`DELETE FROM displays WHERE survey_id = ?`, surveyID)
		if deleted.Error != nil {
			return fmt.Errorf("delete displays: %w", deleted.Error)
		}
		result.DeletedDisplaysCount = int(deleted.RowsAffected)
		for i := 0; i < len(urls); i += storageDeleteChunk {
			chunk := urls[i:min(i+storageDeleteChunk, len(urls))]
			if err := tx.Enqueue(ctx, response.DeleteFilesArgs{WorkspaceID: sv.WorkspaceID, FileURLs: chunk}, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
