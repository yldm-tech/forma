package response

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
)

// ResponseForUpdate is the lookup of forma2's client response update (putResponseHandler): the response by the id in the path, 404 Response naming it when there is none; its survey, 404 Survey when that is gone; and 404 Response again when the survey belongs to another workspace than the route's, so a response id cannot be probed across workspaces. An id that is neither a UUID nor a cuid is 400.
func (s *Service) ResponseForUpdate(ctx context.Context, workspaceID uuid.UUID, rawID string) (*Response, *Survey, error) {
	id, err := ResolveID(rawID)
	if err != nil {
		return nil, nil, err
	}
	existing, _, err := loadResponse(ctx, s.db, id)
	if err != nil {
		return nil, nil, err
	}
	if existing == nil {
		return nil, nil, NotFound("Response", &rawID)
	}
	sv, err := LoadSurvey(ctx, s.db, existing.SurveyID)
	if err != nil {
		return nil, nil, err
	}
	if sv == nil {
		surveyID := existing.SurveyID.String()
		return nil, nil, NotFound("Survey", &surveyID)
	}
	if sv.WorkspaceID != workspaceID {
		return nil, nil, NotFound("Response", &rawID)
	}
	return existing, sv, nil
}

// CheckUpdate is the update's state check after the ingest: a survey that stopped accepting submissions refuses even the finishing of a response started while it was open (403), and a finished response takes no more updates (400 with details.code response_already_finished, which the runtime keys off to stop retrying).
func CheckUpdate(sv *Survey, existing *Response) error {
	if err := sv.CheckAccepting(); err != nil {
		return err
	}
	if existing.Finished {
		return badRequest("Response is already finished", details("code", CodeResponseAlreadyFinished))
	}
	return nil
}

// CheckVerifiedEmail is the update's check of a survey that requires email verification: email is the address the update's verification token proves, and it must be the one the create stamped on the response (403 otherwise). A link survey page sends the token it was opened with, so a respondent whose page went on with another verified respondent's progress in a shared browser cannot add answers to, or finish, a response recorded under the other's address. An update that proves no address (email "": no token, from the SDK and pages that predate it, or a token that no longer verifies, such as a link that expired while its respondent was answering) is not checked, as before: the token is an optional addition to the frozen client API, and refusing a token that proves nothing would protect nothing an update without one does not get anyway.
func CheckVerifiedEmail(sv *Survey, existing *Response, email string) error {
	if !sv.IsVerifyEmailEnabled || email == "" {
		return nil
	}
	stored, err := decodeObject(existing.Data)
	if err != nil {
		return err
	}
	// Addresses compare case-insensitively, as the one-response-per-email rule reads them (claimVerifiedEmail).
	if recorded, _ := stored[verifiedEmailKey].(string); recorded != "" && strings.EqualFold(recorded, email) {
		return nil
	}
	return forbidden("Response was verified with another email", details("surveyId", sv.ID.String()))
}

// UpdateInput is a response update as the client API validated it (ZResponseUpdateInput) and after its checks ran. A nil field leaves the stored value as it is.
type UpdateInput struct {
	Finished *bool
	// EndingIDSet says the payload carried endingId; EndingID nil then clears it.
	EndingIDSet bool
	EndingID    *string
	// Data is the data IngestUpdate returned, nil when the payload carried none. It is merged over the stored data.
	Data map[string]any
	// IngestFlags are IngestUpdate's flags; the ingest runs on every update, so they are always merged.
	IngestFlags []embeddeddata.Flag
	// Variables are merged over the stored variables.
	Variables map[string]any
	// TTC is merged over the stored ttc.
	TTC      map[string]float64
	Language *string
	Locale   string
	// Management is set by the v1 management API's PUT, which sent the pipeline the response as a quota's endSurvey action left it (finished, on the quota's ending); the client API's update sent the row as written.
	Management bool
}

// Update is the write of forma2's client response update (updateResponseWithQuotaEvaluation, then sendToPipeline), in one transaction: data and variables are merged over what is stored, ttc too with its total recomputed when the update finishes the response, the ingest flags are merged by key, the survey's quotas are evaluated against the merged response and the pipeline events enqueued. Meta and contact never change on update. A response that disappeared meanwhile is 404 Response.
func (s *Service) Update(ctx context.Context, sv *Survey, rawID string, in UpdateInput) (*Result, error) {
	id, err := ResolveID(rawID)
	if err != nil {
		return nil, err
	}
	var result *Result
	err = s.jobs.Transaction(ctx, s.db, func(tx *jobs.Tx) error {
		var locked []uuid.UUID
		if err := tx.WithContext(ctx).Raw(`SELECT id FROM responses WHERE id = ? FOR UPDATE`, id).Scan(&locked).Error; err != nil {
			return fmt.Errorf("lock response: %w", err)
		}
		current, currentFlags, err := loadResponse(ctx, tx.DB, id)
		if err != nil {
			return err
		}
		if len(locked) == 0 || current == nil {
			return NotFound("Response", &rawID)
		}
		// A respondent finishing a response of a survey that takes one response per verified email is refused when another response of the address finished first (claimVerifiedEmail). The address is the one the create stamped; an update cannot change it (IngestUpdate). The v1 management API's PUT is the workspace's own write and, like its create (CreateManaged), is not limited.
		if !in.Management && in.Finished != nil && *in.Finished && !current.Finished {
			stored, err := decodeObject(current.Data)
			if err != nil {
				return err
			}
			if email := sv.oneResponseEmail(stored); email != "" {
				if err := claimVerifiedEmail(ctx, tx, sv.ID, email); err != nil {
					return err
				}
			}
		}
		row, data, variables, ttc, meta, err := s.applyUpdate(ctx, tx, sv, current, currentFlags, in)
		if err != nil {
			return err
		}

		quotaLanguage := "default"
		if row.Language != nil && *row.Language != "" {
			quotaLanguage = *row.Language
		}
		full := s.evaluateQuotas(ctx, tx.DB, sv, screening{
			ResponseID: row.ID, Finished: row.Finished, Data: data, Variables: variables, Language: quotaLanguage,
			Row: valueResponse(row, data, variables, ttc, meta),
		})
		if in.Management && full.endedResponse() {
			refreshed, _, err := loadResponse(ctx, tx.DB, row.ID)
			if err != nil {
				return err
			}
			row = *refreshed
		}

		events := []string{EventResponseUpdated}
		if row.Finished {
			events = append(events, EventResponseFinished)
		}
		for _, event := range events {
			if err := tx.Enqueue(ctx, PipelineArgs{Event: event, WorkspaceID: sv.WorkspaceID, SurveyID: sv.ID, Response: row, Locale: in.Locale}, nil); err != nil {
				return err
			}
		}
		result = &Result{ID: row.ID, QuotaFull: full.full()}
		return nil
	})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return nil, ae
		}
		return nil, fmt.Errorf("update response: %w", err)
	}
	return result, nil
}

func decodeObject(raw json.RawMessage) (map[string]any, error) {
	out := map[string]any{}
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response column: %w", err)
	}
	return out, nil
}

// applyUpdate is forma2's updateResponse with the ingest flags: it writes the merged row and returns it with the decoded columns quotas read. A client update stores the total score the server recomputes from the merged response (recomputeScore); the management API's writes are the workspace's own and keep the variables they send.
func (s *Service) applyUpdate(ctx context.Context, tx *jobs.Tx, sv *Survey, current *Response, currentFlags []embeddeddata.Flag, in UpdateInput) (row Response, data, variables map[string]any, ttc map[string]float64, meta map[string]any, err error) {
	if data, err = decodeObject(current.Data); err != nil {
		return
	}
	maps.Copy(data, in.Data)
	if variables, err = decodeObject(current.Variables); err != nil {
		return
	}
	maps.Copy(variables, in.Variables)
	if ttc, err = decodeTTC(current.TTC); err != nil {
		return
	}
	if in.TTC != nil {
		maps.Copy(ttc, clampTTC(in.TTC))
	}
	if in.Finished != nil && *in.Finished {
		ttc = ttcTotal(ttc)
	}
	if meta, err = decodeObject(current.Meta); err != nil {
		return
	}
	flags := embeddeddata.MergeFlags(currentFlags, in.Data, in.IngestFlags)

	row = *current
	if in.Finished != nil {
		row.Finished = *in.Finished
	}
	if in.EndingIDSet {
		row.EndingID = in.EndingID
	}
	if language := normalizeLanguage(in.Language); language != nil {
		row.Language = language
	}
	if !in.Management {
		s.recomputeScore(ctx, sv, &row.ID, valueResponse(row, data, variables, ttc, meta), ttc, variables, in.Variables)
	}
	if row.Data, err = json.Marshal(data); err != nil {
		return
	}
	if row.Variables, err = json.Marshal(variables); err != nil {
		return
	}
	if row.TTC, err = json.Marshal(ttc); err != nil {
		return
	}
	flagsJSON, err := json.Marshal(flags)
	if err != nil {
		return
	}
	row.UpdatedAt = time.Now().UTC()
	err = tx.WithContext(ctx).Exec(`UPDATE responses SET finished = ?, ending_id = ?, data = ?::jsonb, variables = ?::jsonb, ttc = ?::jsonb, language = ?, ingest_flags = ?::jsonb, updated_at = ? WHERE id = ?`,
		row.Finished, row.EndingID, string(row.Data), string(row.Variables), string(row.TTC), row.Language, string(flagsJSON), row.UpdatedAt, row.ID).Error
	if err != nil {
		err = fmt.Errorf("update response: %w", err)
	}
	return
}
