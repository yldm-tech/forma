package response

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/contact"
	"github.com/yldm-tech/forma/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/server/internal/tag"
)

// Unique constraints of responses the create path maps to forma2's errors.
const (
	constraintSingleUse = "responses_survey_id_single_use_id_key"
	constraintDisplay   = "responses_display_id_key"
)

func uniqueViolation(err error) (constraint string, ok bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName, true
	}
	return "", false
}

// jsonParam is a JSON column value.
func jsonParam(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode response column: %w", err)
	}
	return string(b), nil
}

// contactRef is a contact a response or display is attached to, with every attribute value as forma2's text column held it.
type contactRef struct {
	ID         uuid.UUID
	Attributes map[string]string
}

func (c *contactRef) apiContact() *Contact {
	if c == nil {
		return nil
	}
	out := &Contact{ID: c.ID}
	if v, ok := c.Attributes["userId"]; ok {
		out.UserID = &v
	}
	return out
}

// contactAttributes reads every attribute of a contact as text: a number as String(n), a date as its ISO string, forma2's readAttributeValue.
func contactAttributes(ctx context.Context, db *gorm.DB, contactID uuid.UUID) (map[string]string, error) {
	var rows []struct {
		Key         string
		ValueText   *string
		ValueNumber *float64
		ValueDate   *time.Time
	}
	err := db.WithContext(ctx).Raw(`SELECT k.key, a.value_text, a.value_number, a.value_date
		FROM contact_attributes a JOIN contact_attribute_keys k ON k.id = a.attribute_key_id
		WHERE a.contact_id = ?`, contactID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load contact attributes: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Key] = contact.Columns{Text: r.ValueText, Number: r.ValueNumber, Date: r.ValueDate}.String()
	}
	return out, nil
}

// contactIDByUserID is forma2's getContactByUserId: the contact of the workspace whose userId attribute holds userID, nil when none does.
func contactIDByUserID(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID, userID string) (*uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.WithContext(ctx).Raw(`SELECT a.contact_id FROM contact_attributes a
		JOIN contact_attribute_keys k ON k.id = a.attribute_key_id
		WHERE k.workspace_id = ? AND k.key = 'userId' AND a.value_text = ?
		ORDER BY a.created_at, a.contact_id LIMIT 1`, workspaceID, userID).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("find contact by userId: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return &ids[0], nil
}

// contactInWorkspace reports whether the contact exists in the workspace.
func contactInWorkspace(ctx context.Context, db *gorm.DB, workspaceID, contactID uuid.UUID) (bool, error) {
	var n int64
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM contacts WHERE id = ? AND workspace_id = ?`, contactID, workspaceID).Scan(&n).Error; err != nil {
		return false, fmt.Errorf("find contact: %w", err)
	}
	return n > 0, nil
}

// displayForResponse is forma2's getDisplayForResponseValidation; nil when the display does not exist.
type displayForResponse struct {
	SurveyID    uuid.UUID
	WorkspaceID uuid.UUID
	ResponseID  *uuid.UUID
	ContactID   *uuid.UUID
}

func loadDisplayForResponse(ctx context.Context, db *gorm.DB, displayID uuid.UUID) (*displayForResponse, error) {
	var rows []displayForResponse
	err := db.WithContext(ctx).Raw(`SELECT d.survey_id, s.workspace_id, r.id AS response_id, d.contact_id
		FROM displays d JOIN surveys s ON s.id = d.survey_id LEFT JOIN responses r ON r.display_id = d.id
		WHERE d.id = ?`, displayID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load display: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// responseRow is a responses row with the userId of its contact.
type responseRow struct {
	ID                uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	SurveyID          uuid.UUID
	DisplayID         *uuid.UUID
	ContactID         *uuid.UUID
	ContactUserID     *string
	ContactAttributes []byte
	Finished          bool
	EndingID          *string
	Data              []byte
	Variables         []byte
	TTC               []byte
	Meta              []byte
	IngestFlags       []byte
	SingleUseID       *string
	Language          *string
}

// responseSelect reads responses with their contact's userId, forma2's responseSelection. Callers append the WHERE clause.
const responseSelect = `SELECT r.id, r.created_at, r.updated_at, r.survey_id, r.display_id, r.contact_id,
	(SELECT a.value_text FROM contact_attributes a JOIN contact_attribute_keys k ON k.id = a.attribute_key_id
		WHERE a.contact_id = r.contact_id AND k.key = 'userId') AS contact_user_id,
	r.contact_attributes, r.finished, r.ending_id, r.data, r.variables, r.ttc, r.meta, r.ingest_flags, r.single_use_id, r.language
	FROM responses r `

func rawOrNull(b []byte) json.RawMessage {
	if b == nil {
		return json.RawMessage("null")
	}
	return b
}

func (row responseRow) response() Response {
	r := Response{
		ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, SurveyID: row.SurveyID, DisplayID: row.DisplayID,
		ContactAttributes: rawOrNull(row.ContactAttributes), Finished: row.Finished, EndingID: row.EndingID,
		Data: row.Data, Variables: row.Variables, TTC: row.TTC, Tags: []tag.Tag{}, Meta: row.Meta, SingleUseID: row.SingleUseID, Language: row.Language,
	}
	if row.ContactID != nil {
		r.Contact = &Contact{ID: *row.ContactID, UserID: row.ContactUserID}
	}
	return r
}

// loadTags attaches each response's tags, by tag name as forma2's relation listed them.
func loadTags(ctx context.Context, db *gorm.DB, responses []Response) error {
	if len(responses) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(responses))
	index := make(map[uuid.UUID]int, len(responses))
	for i := range responses {
		ids[i] = responses[i].ID
		index[responses[i].ID] = i
	}
	var rows []struct {
		ResponseID uuid.UUID
		tag.Tag
	}
	err := db.WithContext(ctx).Raw(`SELECT rt.response_id, t.id, t.created_at, t.updated_at, t.name, t.workspace_id
		FROM response_tags rt JOIN tags t ON t.id = rt.tag_id WHERE rt.response_id IN ? ORDER BY t.name, t.id`, ids).Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("load response tags: %w", err)
	}
	for _, row := range rows {
		i := index[row.ResponseID]
		responses[i].Tags = append(responses[i].Tags, row.Tag)
	}
	return nil
}

// loadResponse reads one response with its contact and tags; nil when it does not exist. The ingest flags come back separately: they are never part of a response payload.
func loadResponse(ctx context.Context, db *gorm.DB, id uuid.UUID) (*Response, []embeddeddata.Flag, error) {
	var rows []responseRow
	if err := db.WithContext(ctx).Raw(responseSelect+`WHERE r.id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, nil, fmt.Errorf("load response: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}
	var flags []embeddeddata.Flag
	if rows[0].IngestFlags != nil {
		if err := json.Unmarshal(rows[0].IngestFlags, &flags); err != nil {
			return nil, nil, fmt.Errorf("decode ingest flags of response %s: %w", id, err)
		}
	}
	r := []Response{rows[0].response()}
	if err := loadTags(ctx, db, r); err != nil {
		return nil, nil, err
	}
	return &r[0], flags, nil
}

// ttcTotal is forma2's calculateTtcTotal: _total becomes the sum of every value, a _total already present included. Keys are summed in sorted order so the result does not depend on map order.
func ttcTotal(ttc map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(ttc)+1)
	keys := make([]string, 0, len(ttc))
	for k, v := range ttc {
		out[k] = v
		keys = append(keys, k)
	}
	sort.Strings(keys)
	total := 0.0
	for _, k := range keys {
		total += ttc[k]
	}
	out["_total"] = total
	return out
}
