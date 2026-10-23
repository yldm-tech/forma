package contact

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
)

// Repository reads and writes contacts, attribute keys, attribute values and segments. Every query names the workspace, contact or key it is scoped to.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) withTx(tx *gorm.DB) *Repository {
	return &Repository{db: tx}
}

func (r *Repository) transaction(ctx context.Context, fn func(tx *Repository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(r.withTx(tx))
	})
}

// retrying runs fn in a transaction retried on a deadlock or serialization failure, forma2's retryOnDeadlock around its attribute writes.
func (r *Repository) retrying(ctx context.Context, fn func(tx *Repository) error) error {
	return database.Serializable(ctx, r.db, func(tx *gorm.DB) error {
		return fn(r.withTx(tx))
	})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// optionalID reads the single uuid a query returns, or nil when it returns no row.
func (r *Repository) optionalID(ctx context.Context, what, query string, args ...any) (*uuid.UUID, error) {
	var id *uuid.UUID
	err := r.db.WithContext(ctx).Raw(query, args...).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return id, nil
}

// returningID runs a statement that returns one id.
func (r *Repository) returningID(ctx context.Context, query string, args ...any) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.WithContext(ctx).Raw(query, args...).Row().Scan(&id)
	return id, err
}

func (r *Repository) workspaceOrganization(ctx context.Context, workspaceID uuid.UUID) (*uuid.UUID, error) {
	return r.optionalID(ctx, "read workspace organization", `SELECT organization_id FROM workspaces WHERE id = ?`, workspaceID)
}

// ---- attribute keys ----

const keyColumns = `id, created_at, updated_at, key, name, description, is_unique, type, data_type, workspace_id`

// keys lists a workspace's keys, oldest first.
func (r *Repository) keys(ctx context.Context, workspaceID uuid.UUID) ([]Key, error) {
	keys := []Key{}
	if err := r.db.WithContext(ctx).Raw(`SELECT `+keyColumns+` FROM contact_attribute_keys WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID).Scan(&keys).Error; err != nil {
		return nil, fmt.Errorf("list contact attribute keys: %w", err)
	}
	return keys, nil
}

func (r *Repository) keyByID(ctx context.Context, id uuid.UUID) (*Key, error) {
	var k Key
	res := r.db.WithContext(ctx).Raw(`SELECT `+keyColumns+` FROM contact_attribute_keys WHERE id = ?`, id).Scan(&k)
	if res.Error != nil {
		return nil, fmt.Errorf("read contact attribute key: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &k, nil
}

var errKeyExists = errors.New("contact attribute key exists")

func (r *Repository) insertKey(ctx context.Context, workspaceID uuid.UUID, key, name string, description *string, dataType string) (*Key, error) {
	var k Key
	err := r.db.WithContext(ctx).Raw(`INSERT INTO contact_attribute_keys (workspace_id, key, name, description, type, data_type) VALUES (?, ?, ?, ?, 'custom', ?) RETURNING `+keyColumns, workspaceID, key, name, description, dataType).Scan(&k).Error
	if isUniqueViolation(err) {
		return nil, errKeyExists
	}
	if err != nil {
		return nil, fmt.Errorf("create contact attribute key: %w", err)
	}
	return &k, nil
}

// updateKey changes the name and description; a nil field is left as it is.
func (r *Repository) updateKey(ctx context.Context, id uuid.UUID, name, description *string) (*Key, error) {
	var k Key
	res := r.db.WithContext(ctx).Raw(`UPDATE contact_attribute_keys SET name = COALESCE(?, name), description = COALESCE(?, description), updated_at = now() WHERE id = ? RETURNING `+keyColumns, name, description, id).Scan(&k)
	if res.Error != nil {
		return nil, fmt.Errorf("update contact attribute key: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &k, nil
}

func (r *Repository) deleteKey(ctx context.Context, id uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM contact_attribute_keys WHERE id = ?`, id)
	if res.Error != nil {
		return false, fmt.Errorf("delete contact attribute key: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// newKey is a key an attribute update or an import creates.
type newKey struct {
	key, dataType string
}

// createKeys inserts keys of type custom, named after their key, skipping any the workspace already has: forma2's createMany with skipDuplicates. They are inserted in key order, so concurrent writers lock in the same order.
func (r *Repository) createKeys(ctx context.Context, workspaceID uuid.UUID, keys []newKey) error {
	if len(keys) == 0 {
		return nil
	}
	sorted := append([]newKey(nil), keys...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].key < sorted[j].key })
	for batch := range slices.Chunk(sorted, maxBindParameters/4) {
		values := make([]string, len(batch))
		args := make([]any, 0, len(batch)*4)
		for i, k := range batch {
			values[i] = "(?, ?, ?, 'custom', ?)"
			args = append(args, workspaceID, k.key, TitleCase(k.key), k.dataType)
		}
		err := r.db.WithContext(ctx).Exec(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, data_type) VALUES `+strings.Join(values, ", ")+` ON CONFLICT (workspace_id, key) DO NOTHING`, args...).Error
		if err != nil {
			return fmt.Errorf("create contact attribute keys: %w", err)
		}
	}
	return nil
}

// maxBindParameters is the most parameters one statement may carry in PostgreSQL's extended protocol; a multi-row insert is split into statements that stay within it.
const maxBindParameters = 65535

// ---- contacts ----

func (r *Repository) contactWorkspace(ctx context.Context, contactID uuid.UUID) (*uuid.UUID, error) {
	return r.optionalID(ctx, "read contact workspace", `SELECT workspace_id FROM contacts WHERE id = ?`, contactID)
}

type contactRow struct {
	ID          uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	WorkspaceID uuid.UUID
}

type attributeRow struct {
	ContactID   uuid.UUID
	AttributeID uuid.UUID
	KeyID       uuid.UUID
	Key         string
	Name        *string
	Type        string
	DataType    string
	ValueText   *string
	ValueNumber *float64
	ValueDate   *time.Time
}

func (a attributeRow) columns() Columns {
	return Columns{Text: a.ValueText, Number: a.ValueNumber, Date: a.ValueDate}
}

const attributeSelect = `SELECT ca.contact_id, ca.id AS attribute_id, k.id AS key_id, k.key, k.name, k.type, k.data_type, ca.value_text, ca.value_number, ca.value_date FROM contact_attributes ca JOIN contact_attribute_keys k ON k.id = ca.attribute_key_id`

// listContacts is forma2's getContacts: newest first, a page at offset. A search matches a contact with any value containing it, case-insensitively, or whose id it is exactly.
func (r *Repository) listContacts(ctx context.Context, workspaceID uuid.UUID, offset int, search string) ([]Contact, error) {
	query := `SELECT c.id, c.created_at, c.updated_at, c.workspace_id FROM contacts c WHERE c.workspace_id = ?`
	args := []any{workspaceID}
	if search != "" {
		// The key is pinned to the workspace too: it does not change the result, but it lets the planner start from this workspace's attribute rows.
		match := `EXISTS (SELECT 1 FROM contact_attributes ca JOIN contact_attribute_keys k ON k.id = ca.attribute_key_id WHERE ca.contact_id = c.id AND k.workspace_id = ? AND ` + attributeText + ` ILIKE ?)`
		args = append(args, workspaceID, "%"+escapeLike(search)+"%")
		if id, err := uuid.Parse(search); err == nil {
			match += ` OR c.id = ?`
			args = append(args, id)
		}
		query += ` AND (` + match + `)`
	}
	query += ` ORDER BY c.created_at DESC, c.id DESC LIMIT ? OFFSET ?`
	args = append(args, ItemsPerPage, offset)
	var rows []contactRow
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	return r.withAttributes(ctx, rows)
}

func (r *Repository) withAttributes(ctx context.Context, rows []contactRow) ([]Contact, error) {
	out := make([]Contact, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i, c := range rows {
		ids[i] = c.ID
		index[c.ID] = i
		out[i] = Contact{ID: c.ID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, WorkspaceID: c.WorkspaceID, Attributes: map[string]string{}}
	}
	var attrs []attributeRow
	if err := r.db.WithContext(ctx).Raw(attributeSelect+` WHERE ca.contact_id IN ?`, ids).Scan(&attrs).Error; err != nil {
		return nil, fmt.Errorf("read contact attributes: %w", err)
	}
	for _, a := range attrs {
		out[index[a.ContactID]].Attributes[a.Key] = a.columns().String()
	}
	return out, nil
}

// contact reads a contact; with a workspace it must belong to it, forma2's getContactInWorkspace.
func (r *Repository) contact(ctx context.Context, id uuid.UUID, workspaceID *uuid.UUID) (*Contact, error) {
	query := `SELECT id, created_at, updated_at, workspace_id FROM contacts WHERE id = ?`
	args := []any{id}
	if workspaceID != nil {
		query += ` AND workspace_id = ?`
		args = append(args, *workspaceID)
	}
	var rows []contactRow
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read contact: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out, err := r.withAttributes(ctx, rows)
	if err != nil {
		return nil, err
	}
	return &out[0], nil
}

// attributeDetails is forma2's getContactAttributesWithKeyInfo, scoped through the contact's workspace.
func (r *Repository) attributeDetails(ctx context.Context, contactID, workspaceID uuid.UUID) ([]AttributeDetail, error) {
	var attrs []attributeRow
	if err := r.db.WithContext(ctx).Raw(attributeSelect+` JOIN contacts c ON c.id = ca.contact_id WHERE ca.contact_id = ? AND c.workspace_id = ? ORDER BY k.created_at, k.id`, contactID, workspaceID).Scan(&attrs).Error; err != nil {
		return nil, fmt.Errorf("read contact attributes: %w", err)
	}
	out := make([]AttributeDetail, len(attrs))
	for i, a := range attrs {
		out[i] = AttributeDetail{Key: a.Key, Name: a.Name, Type: a.Type, Value: a.columns().String(), DataType: a.DataType}
	}
	return out, nil
}

// attributes is a contact's values by key.
func (r *Repository) attributes(ctx context.Context, contactID uuid.UUID) (map[string]attributeRow, error) {
	var attrs []attributeRow
	if err := r.db.WithContext(ctx).Raw(attributeSelect+` WHERE ca.contact_id = ?`, contactID).Scan(&attrs).Error; err != nil {
		return nil, fmt.Errorf("read contact attributes: %w", err)
	}
	out := make(map[string]attributeRow, len(attrs))
	for _, a := range attrs {
		out[a.Key] = a
	}
	return out, nil
}

func (r *Repository) deleteContact(ctx context.Context, id uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM contacts WHERE id = ?`, id)
	if res.Error != nil {
		return false, fmt.Errorf("delete contact: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// valueOnOtherContact is forma2's hasEmailAttribute / hasUserIdAttribute: whether another contact of the workspace already has value for key.
func (r *Repository) valueOnOtherContact(ctx context.Context, workspaceID uuid.UUID, key, value string, contactID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM contact_attributes ca JOIN contact_attribute_keys k ON k.id = ca.attribute_key_id WHERE k.workspace_id = ? AND k.key = ? AND ca.value_text = ? AND ca.contact_id <> ?)`, workspaceID, key, value, contactID).Scan(&exists).Error
	if err != nil {
		return false, fmt.Errorf("check %s uniqueness: %w", key, err)
	}
	return exists, nil
}

// keyedColumns is a value to store under a key.
type keyedColumns struct {
	keyID   uuid.UUID
	columns Columns
}

// upsertAttributes writes a contact's values in key id order, so concurrent writes to one contact lock its rows in the same order (forma2's ENG-2252).
func (r *Repository) upsertAttributes(ctx context.Context, contactID uuid.UUID, values []keyedColumns) error {
	if len(values) == 0 {
		return nil
	}
	sorted := append([]keyedColumns(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].keyID.String() < sorted[j].keyID.String() })
	for batch := range slices.Chunk(sorted, maxBindParameters/5) {
		rows := make([]string, len(batch))
		args := make([]any, 0, len(batch)*5)
		for i, v := range batch {
			rows[i] = "(?, ?, ?, ?, ?)"
			args = append(args, contactID, v.keyID, v.columns.Text, v.columns.Number, v.columns.Date)
		}
		err := r.db.WithContext(ctx).Exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text, value_number, value_date) VALUES `+strings.Join(rows, ", ")+`
			ON CONFLICT (contact_id, attribute_key_id) DO UPDATE SET value_text = EXCLUDED.value_text, value_number = EXCLUDED.value_number, value_date = EXCLUDED.value_date, updated_at = now()`, args...).Error
		if err != nil {
			return fmt.Errorf("write contact attributes: %w", err)
		}
	}
	return nil
}

func (r *Repository) deleteAttributes(ctx context.Context, contactID uuid.UUID, keyIDs []uuid.UUID) error {
	if len(keyIDs) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM contact_attributes WHERE contact_id = ? AND attribute_key_id IN ?`, contactID, keyIDs).Error; err != nil {
		return fmt.Errorf("delete contact attributes: %w", err)
	}
	return nil
}

func (r *Repository) touchContact(ctx context.Context, contactID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE contacts SET updated_at = now() WHERE id = ?`, contactID).Error; err != nil {
		return fmt.Errorf("touch contact: %w", err)
	}
	return nil
}

// distinctValues is forma2's getDistinctAttributeValues: up to 100 distinct non-empty values of a key, ascending.
func (r *Repository) distinctValues(ctx context.Context, keyID uuid.UUID) ([]string, error) {
	values := []string{}
	err := r.db.WithContext(ctx).Raw(`SELECT v FROM (SELECT DISTINCT `+attributeText+` AS v FROM contact_attributes ca WHERE ca.attribute_key_id = ?) s WHERE v <> '' ORDER BY v LIMIT 100`, keyID).Scan(&values).Error
	if err != nil {
		return nil, fmt.Errorf("read distinct attribute values: %w", err)
	}
	return values, nil
}

// ---- import ----

// contactsByEmail finds the workspace's contacts by their email value, exactly as written, with their userId.
func (r *Repository) contactsByEmail(ctx context.Context, workspaceID uuid.UUID, emails []string) (map[string]existingContact, error) {
	out := map[string]existingContact{}
	if len(emails) == 0 {
		return out, nil
	}
	var rows []struct {
		ID     uuid.UUID
		Email  string
		UserID *string
	}
	err := r.db.WithContext(ctx).Raw(`SELECT c.id, e.value_text AS email, u.value_text AS user_id FROM contacts c
		JOIN contact_attributes e ON e.contact_id = c.id JOIN contact_attribute_keys ek ON ek.id = e.attribute_key_id AND ek.key = 'email'
		LEFT JOIN LATERAL (SELECT ca.value_text FROM contact_attributes ca JOIN contact_attribute_keys uk ON uk.id = ca.attribute_key_id AND uk.key = 'userId' WHERE ca.contact_id = c.id) u ON true
		WHERE c.workspace_id = ? AND e.value_text IN ? ORDER BY c.created_at, c.id`, workspaceID, emails).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("find contacts by email: %w", err)
	}
	for _, row := range rows {
		c := existingContact{id: row.ID}
		if row.UserID != nil {
			c.userID = *row.UserID
		}
		out[row.Email] = c
	}
	return out, nil
}

// userIDOwners maps each of the userId values to the workspace's contacts that hold it.
func (r *Repository) userIDOwners(ctx context.Context, workspaceID uuid.UUID, userIDs []string) (map[string][]uuid.UUID, error) {
	out := map[string][]uuid.UUID{}
	if len(userIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		Value     string
		ContactID uuid.UUID
	}
	err := r.db.WithContext(ctx).Raw(`SELECT ca.value_text AS value, ca.contact_id FROM contact_attributes ca JOIN contact_attribute_keys k ON k.id = ca.attribute_key_id WHERE k.workspace_id = ? AND k.key = 'userId' AND ca.value_text IN ?`, workspaceID, userIDs).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("find userId owners: %w", err)
	}
	for _, row := range rows {
		out[row.Value] = append(out[row.Value], row.ContactID)
	}
	return out, nil
}

func (r *Repository) insertContact(ctx context.Context, workspaceID uuid.UUID) (uuid.UUID, error) {
	id, err := r.returningID(ctx, `INSERT INTO contacts (workspace_id) VALUES (?) RETURNING id`, workspaceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create contact: %w", err)
	}
	return id, nil
}

func (r *Repository) deleteAllAttributes(ctx context.Context, contactID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM contact_attributes WHERE contact_id = ?`, contactID).Error; err != nil {
		return fmt.Errorf("clear contact attributes: %w", err)
	}
	return nil
}

// contactsByID reads contacts with their values, in the order of ids.
func (r *Repository) contactsByID(ctx context.Context, ids []uuid.UUID) ([]Contact, error) {
	if len(ids) == 0 {
		return []Contact{}, nil
	}
	var rows []contactRow
	if err := r.db.WithContext(ctx).Raw(`SELECT id, created_at, updated_at, workspace_id FROM contacts WHERE id IN ?`, ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read contacts: %w", err)
	}
	position := make(map[uuid.UUID]int, len(ids))
	for i, id := range ids {
		position[id] = i
	}
	sort.Slice(rows, func(i, j int) bool { return position[rows[i].ID] < position[rows[j].ID] })
	return r.withAttributes(ctx, rows)
}

// ---- segments ----

var errSegmentTitleTaken = errors.New("segment title taken")

type segmentRow struct {
	ID          uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Title       string
	Description *string
	IsPrivate   bool
	Filters     []byte
	WorkspaceID uuid.UUID
}

const segmentColumns = `id, created_at, updated_at, title, description, is_private, filters, workspace_id`

// segments reads a workspace's segments, or the one with the given id, with the surveys that target them.
func (r *Repository) segments(ctx context.Context, workspaceID uuid.UUID, id *uuid.UUID) ([]Segment, error) {
	query := `SELECT ` + segmentColumns + ` FROM segments WHERE workspace_id = ? ORDER BY created_at, id`
	args := []any{workspaceID}
	if id != nil {
		query = `SELECT ` + segmentColumns + ` FROM segments WHERE id = ?`
		args = []any{*id}
	}
	var rows []segmentRow
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read segments: %w", err)
	}
	out := make([]Segment, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i, s := range rows {
		ids[i] = s.ID
		index[s.ID] = i
		out[i] = Segment{ID: s.ID, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, Title: s.Title, Description: s.Description, IsPrivate: s.IsPrivate, Filters: s.Filters, WorkspaceID: s.WorkspaceID, Surveys: []uuid.UUID{}, ActiveSurveys: []SurveyRef{}, InactiveSurveys: []SurveyRef{}}
	}
	var surveys []struct {
		ID        uuid.UUID
		Name      string
		Status    string
		SegmentID uuid.UUID
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT id, name, status, segment_id FROM surveys WHERE segment_id IN ? ORDER BY created_at, id`, ids).Scan(&surveys).Error; err != nil {
		return nil, fmt.Errorf("read segment surveys: %w", err)
	}
	for _, sv := range surveys {
		seg := &out[index[sv.SegmentID]]
		seg.Surveys = append(seg.Surveys, sv.ID)
		ref := SurveyRef{ID: sv.ID, Name: sv.Name}
		if sv.Status == surveyStatusInProgress {
			seg.ActiveSurveys = append(seg.ActiveSurveys, ref)
		} else {
			seg.InactiveSurveys = append(seg.InactiveSurveys, ref)
		}
	}
	return out, nil
}

func (r *Repository) segment(ctx context.Context, id uuid.UUID) (*Segment, error) {
	segs, err := r.segments(ctx, uuid.Nil, &id)
	if err != nil || len(segs) == 0 {
		return nil, err
	}
	return &segs[0], nil
}

func (r *Repository) insertSegment(ctx context.Context, workspaceID uuid.UUID, title string, description *string, isPrivate bool, filters json.RawMessage) (uuid.UUID, error) {
	id, err := r.returningID(ctx, `INSERT INTO segments (workspace_id, title, description, is_private, filters) VALUES (?, ?, ?, ?, ?) RETURNING id`, workspaceID, title, description, isPrivate, string(filters))
	if isUniqueViolation(err) {
		return uuid.Nil, errSegmentTitleTaken
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("create segment: %w", err)
	}
	return id, nil
}

// upsertPrivateSegment is forma2's private-segment upsert on (workspace, title): an existing segment of that title gets the filters, and the description when one is given.
func (r *Repository) upsertPrivateSegment(ctx context.Context, workspaceID uuid.UUID, title string, description *string, filters json.RawMessage) (uuid.UUID, error) {
	id, err := r.returningID(ctx, `INSERT INTO segments (workspace_id, title, description, is_private, filters) VALUES (?, ?, ?, true, ?)
		ON CONFLICT (workspace_id, title) DO UPDATE SET filters = EXCLUDED.filters, description = CASE WHEN ? THEN EXCLUDED.description ELSE segments.description END, updated_at = now()
		RETURNING id`, workspaceID, title, description, string(filters), description != nil)
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert segment: %w", err)
	}
	return id, nil
}

func (r *Repository) updateSegment(ctx context.Context, id uuid.UUID, in SegmentUpdate) error {
	sets := []string{"updated_at = now()"}
	var args []any
	if in.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *in.Title)
	}
	if in.DescriptionSet {
		sets = append(sets, "description = ?")
		args = append(args, in.Description)
	}
	if in.IsPrivate != nil {
		sets = append(sets, "is_private = ?")
		args = append(args, *in.IsPrivate)
	}
	if in.Filters != nil {
		sets = append(sets, "filters = ?")
		args = append(args, string(in.Filters))
	}
	err := r.db.WithContext(ctx).Exec(`UPDATE segments SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, id)...).Error
	if isUniqueViolation(err) {
		return errSegmentTitleTaken
	}
	if err != nil {
		return fmt.Errorf("update segment: %w", err)
	}
	return nil
}

// linkSurveys makes the workspace's surveys target the segment.
func (r *Repository) linkSurveys(ctx context.Context, segmentID, workspaceID uuid.UUID, surveyIDs []uuid.UUID) error {
	if len(surveyIDs) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Exec(`UPDATE surveys SET segment_id = ? WHERE id IN ? AND workspace_id = ?`, segmentID, surveyIDs, workspaceID).Error; err != nil {
		return fmt.Errorf("link surveys to segment: %w", err)
	}
	return nil
}

func (r *Repository) deleteSegment(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM segments WHERE id = ?`, id).Error; err != nil {
		return fmt.Errorf("delete segment: %w", err)
	}
	return nil
}

// surveyWorkspaces maps the surveys that exist to their workspaces, looked up in bounded batches.
func (r *Repository) surveyWorkspaces(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	out := map[uuid.UUID]uuid.UUID{}
	unique := slices.Compact(slices.SortedFunc(slices.Values(ids), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }))
	for start := 0; start < len(unique); start += surveyWorkspaceLookupBatchSize {
		batch := unique[start:min(start+surveyWorkspaceLookupBatchSize, len(unique))]
		var rows []struct {
			ID          uuid.UUID
			WorkspaceID uuid.UUID
		}
		if err := r.db.WithContext(ctx).Raw(`SELECT id, workspace_id FROM surveys WHERE id IN ?`, batch).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("read survey workspaces: %w", err)
		}
		for _, row := range rows {
			out[row.ID] = row.WorkspaceID
		}
	}
	return out, nil
}

// surveysInWorkspace reports which of the ids, as written in a filter, are surveys of the workspace.
func (r *Repository) surveysInWorkspace(ctx context.Context, workspaceID uuid.UUID, ids []string) (map[string]bool, error) {
	var found []string
	if err := r.db.WithContext(ctx).Raw(`SELECT id::text FROM surveys WHERE workspace_id = ? AND id::text IN ?`, workspaceID, ids).Scan(&found).Error; err != nil {
		return nil, fmt.Errorf("check filter surveys: %w", err)
	}
	out := make(map[string]bool, len(found))
	for _, id := range found {
		out[id] = true
	}
	return out, nil
}

func (r *Repository) surveyRefs(ctx context.Context, workspaceID uuid.UUID) ([]SurveyFilterRef, error) {
	refs := []SurveyFilterRef{}
	if err := r.db.WithContext(ctx).Raw(`SELECT id, name, status FROM surveys WHERE workspace_id = ? ORDER BY updated_at DESC, id LIMIT ?`, workspaceID, MaxSurveyFilterRefs).Scan(&refs).Error; err != nil {
		return nil, fmt.Errorf("list surveys: %w", err)
	}
	return refs, nil
}

func (r *Repository) segmentTitles(ctx context.Context, workspaceID uuid.UUID) ([]string, error) {
	var titles []string
	if err := r.db.WithContext(ctx).Raw(`SELECT title FROM segments WHERE workspace_id = ?`, workspaceID).Scan(&titles).Error; err != nil {
		return nil, fmt.Errorf("list segment titles: %w", err)
	}
	return titles, nil
}

func (r *Repository) privateSegmentID(ctx context.Context, workspaceID uuid.UUID, title string) (*uuid.UUID, error) {
	return r.optionalID(ctx, "find private segment", `SELECT id FROM segments WHERE workspace_id = ? AND title = ? AND is_private ORDER BY created_at LIMIT 1 FOR UPDATE`, workspaceID, title)
}

func (r *Repository) clearFilters(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE segments SET filters = '[]', updated_at = now() WHERE id = ?`, id).Error; err != nil {
		return fmt.Errorf("reset segment filters: %w", err)
	}
	return nil
}
