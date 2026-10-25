package mgmtapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/contact"
)

// Repository reads and writes the rows the management APIs serve as forma2's raw Prisma rows: contacts, attribute values and keys, workspaces, teams, memberships and workspace grants. Every query names the rows it is scoped to; the handlers authorize first.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func uuidArray(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ---- workspaces ----

// workspaceRow is what the APIs read of a workspace.
type workspaceRow struct {
	ID                uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Name              string
	OrganizationID    uuid.UUID
	AppSetupCompleted bool
}

func (r *Repository) workspaces(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]workspaceRow, error) {
	var rows []workspaceRow
	if err := r.db.WithContext(ctx).Raw(`SELECT id, created_at, updated_at, name, organization_id, app_setup_completed FROM workspaces WHERE id = ANY(?::uuid[])`,
		uuidArray(ids)).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load workspaces: %w", err)
	}
	out := make(map[uuid.UUID]workspaceRow, len(rows))
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}

// environmentIDs is the environmentId of each workspace that exists: its own id.
func (r *Repository) environmentIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	found, err := r.workspaces(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]string, len(found))
	for id := range found {
		out[id] = id.String()
	}
	return out, nil
}

// organizationExists reports errNotFound when the workspace has no organization.
func (r *Repository) organizationExists(ctx context.Context, workspaceID uuid.UUID) error {
	var n int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM workspaces w JOIN organizations o ON o.id = w.organization_id WHERE w.id = ?`, workspaceID).Scan(&n).Error; err != nil {
		return fmt.Errorf("load workspace organization: %w", err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

// ---- contacts ----

// contactRow is forma2's raw Contact row.
type contactRow struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
}

// contacts lists the contacts of the workspaces, newest first, limit of them after offset.
func (r *Repository) contacts(ctx context.Context, workspaceIDs []uuid.UUID, limit, offset int) ([]contactRow, error) {
	out := []contactRow{}
	if err := r.db.WithContext(ctx).Raw(`SELECT id, created_at, updated_at, workspace_id FROM contacts WHERE workspace_id = ANY(?::uuid[]) ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		uuidArray(workspaceIDs), limit, offset).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	return out, nil
}

func (r *Repository) contact(ctx context.Context, id uuid.UUID) (*contactRow, error) {
	var rows []contactRow
	if err := r.db.WithContext(ctx).Raw(`SELECT id, created_at, updated_at, workspace_id FROM contacts WHERE id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load contact: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// deleteContact deletes a contact with its attribute values and reports whether there was one.
func (r *Repository) deleteContact(ctx context.Context, id uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM contacts WHERE id = ?`, id)
	if res.Error != nil {
		return false, fmt.Errorf("delete contact: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// attributeRow is forma2's raw ContactAttribute row: the value as text (the column forma2 always wrote), with the typed copies of a number or date key beside it.
type attributeRow struct {
	ID             uuid.UUID  `json:"id"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	AttributeKeyID uuid.UUID  `json:"attributeKeyId"`
	ContactID      uuid.UUID  `json:"contactId"`
	Value          string     `json:"value"`
	ValueNumber    *float64   `json:"valueNumber"`
	ValueDate      *time.Time `json:"valueDate"`
}

// contactAttributes lists the attribute values whose keys belong to the workspaces, contact by contact in the order the contacts and their values were created.
func (r *Repository) contactAttributes(ctx context.Context, workspaceIDs []uuid.UUID) ([]attributeRow, error) {
	var rows []struct {
		ID             uuid.UUID
		CreatedAt      time.Time
		UpdatedAt      time.Time
		AttributeKeyID uuid.UUID
		ContactID      uuid.UUID
		ValueText      *string
		ValueNumber    *float64
		ValueDate      *time.Time
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT a.id, a.created_at, a.updated_at, a.attribute_key_id, a.contact_id, a.value_text, a.value_number, a.value_date
		FROM contact_attributes a JOIN contact_attribute_keys k ON k.id = a.attribute_key_id JOIN contacts c ON c.id = a.contact_id
		WHERE k.workspace_id = ANY(?::uuid[]) ORDER BY c.created_at, c.id, a.created_at, k.created_at, a.id`, uuidArray(workspaceIDs)).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list contact attributes: %w", err)
	}
	out := make([]attributeRow, len(rows))
	for i, row := range rows {
		out[i] = attributeRow{
			ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, AttributeKeyID: row.AttributeKeyID, ContactID: row.ContactID,
			Value: contact.Columns{Text: row.ValueText, Number: row.ValueNumber, Date: row.ValueDate}.String(), ValueNumber: row.ValueNumber, ValueDate: row.ValueDate,
		}
	}
	return out, nil
}

// ---- attribute keys ----

const keyColumns = `id, created_at, updated_at, key, name, description, is_unique, type, data_type, workspace_id`

// keys lists the attribute keys of the workspaces in the order they were created.
func (r *Repository) keys(ctx context.Context, workspaceIDs []uuid.UUID) ([]contact.Key, error) {
	out := []contact.Key{}
	if err := r.db.WithContext(ctx).Raw(`SELECT `+keyColumns+` FROM contact_attribute_keys WHERE workspace_id = ANY(?::uuid[]) ORDER BY created_at, id`, uuidArray(workspaceIDs)).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("list contact attribute keys: %w", err)
	}
	return out, nil
}

func (r *Repository) key(ctx context.Context, id uuid.UUID) (*contact.Key, error) {
	var rows []contact.Key
	if err := r.db.WithContext(ctx).Raw(`SELECT `+keyColumns+` FROM contact_attribute_keys WHERE id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load contact attribute key: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// errKeyExists is a key the workspace already has.
var errKeyExists = errors.New("contact attribute key exists")

// createKey inserts a custom key.
func (r *Repository) createKey(ctx context.Context, workspaceID uuid.UUID, key, name string, description *string, dataType string) (*contact.Key, error) {
	var k contact.Key
	err := r.db.WithContext(ctx).Raw(`INSERT INTO contact_attribute_keys (workspace_id, key, name, description, type, data_type) VALUES (?, ?, ?, ?, 'custom', ?) RETURNING `+keyColumns,
		workspaceID, key, name, description, dataType).Scan(&k).Error
	if isUniqueViolation(err) {
		return nil, errKeyExists
	}
	if err != nil {
		return nil, fmt.Errorf("create contact attribute key: %w", err)
	}
	return &k, nil
}

// keyUpdate is the columns an update may change; nil leaves one as it is.
type keyUpdate struct {
	Name, Description, Key, DataType *string
	// SetName and SetDescription write Name and Description even when nil, as v2's nullable fields do.
	SetName, SetDescription bool
}

func (r *Repository) updateKey(ctx context.Context, id uuid.UUID, u keyUpdate) (*contact.Key, error) {
	sets := []string{"updated_at = now()"}
	var args []any
	add := func(column string, v *string, always bool) {
		if v != nil || always {
			sets = append(sets, column+" = ?")
			args = append(args, v)
		}
	}
	add("name", u.Name, u.SetName)
	add("description", u.Description, u.SetDescription)
	add("key", u.Key, false)
	add("data_type", u.DataType, false)
	var rows []contact.Key
	err := r.db.WithContext(ctx).Raw(`UPDATE contact_attribute_keys SET `+strings.Join(sets, ", ")+` WHERE id = ? RETURNING `+keyColumns, append(args, id)...).Scan(&rows).Error
	if isUniqueViolation(err) {
		return nil, errKeyExists
	}
	if err != nil {
		return nil, fmt.Errorf("update contact attribute key: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *Repository) deleteKey(ctx context.Context, id uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM contact_attribute_keys WHERE id = ?`, id)
	if res.Error != nil {
		return false, fmt.Errorf("delete contact attribute key: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// keyCount is how many attribute keys the workspace has.
func (r *Repository) keyCount(ctx context.Context, workspaceID uuid.UUID) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM contact_attribute_keys WHERE workspace_id = ?`, workspaceID).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count contact attribute keys: %w", err)
	}
	return n, nil
}

// ---- users ----

// meUser is the signed-in user as forma2's v1 /me answered it. The rewrite keeps whether the address is verified, not when, so emailVerified is a flag; identityProvider is the provider of the user's first external identity, "email" for a password-only account.
type meUser struct {
	ID                   uuid.UUID       `json:"id"`
	Name                 string          `json:"name"`
	Email                string          `json:"email"`
	EmailVerified        bool            `json:"emailVerified"`
	CreatedAt            time.Time       `json:"createdAt"`
	UpdatedAt            time.Time       `json:"updatedAt"`
	TwoFactorEnabled     bool            `json:"twoFactorEnabled"`
	IdentityProvider     string          `json:"identityProvider"`
	NotificationSettings json.RawMessage `json:"notificationSettings" gorm:"-"`
	Settings             []byte          `json:"-" gorm:"column:notification_settings"`
	Locale               string          `json:"locale"`
	LastLoginAt          *time.Time      `json:"lastLoginAt"`
	IsActive             bool            `json:"isActive"`
}

func (r *Repository) meUser(ctx context.Context, id uuid.UUID) (*meUser, error) {
	var rows []meUser
	err := r.db.WithContext(ctx).Raw(`SELECT u.id, u.name, u.email, u.email_verified, u.created_at, u.updated_at, u.notification_settings, u.locale, u.last_login_at, u.is_active,
		EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.verified) AS two_factor_enabled,
		COALESCE((SELECT i.provider FROM user_identities i WHERE i.user_id = u.id AND i.provider <> 'credential' ORDER BY i.created_at LIMIT 1), 'email') AS identity_provider
		FROM users u WHERE u.id = ?`, id).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	u := rows[0]
	u.NotificationSettings = u.Settings
	return &u, nil
}

// keyPage reads a page of the attribute keys of the workspaces under the common filter, and how many it selects in all.
func (r *Repository) keyPage(ctx context.Context, workspaceIDs []uuid.UUID, f listFilter) ([]contact.Key, int64, error) {
	where, args := filterWhere(`workspace_id = ANY(?::uuid[])`, []any{uuidArray(workspaceIDs)}, f)
	var total int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM contact_attribute_keys WHERE `+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count contact attribute keys: %w", err)
	}
	out := []contact.Key{}
	query, pageArgs := pageClause(f)
	if err := r.db.WithContext(ctx).Raw(`SELECT `+keyColumns+` FROM contact_attribute_keys WHERE `+where+query, append(args, pageArgs...)...).Scan(&out).Error; err != nil {
		return nil, 0, fmt.Errorf("list contact attribute keys: %w", err)
	}
	return out, total, nil
}

// filterWhere adds the common filter's date bounds to a condition on a table with created_at and updated_at.
func filterWhere(where string, args []any, f listFilter) (string, []any) {
	if f.Start != nil {
		where += ` AND ` + f.DateField + ` >= ?`
		args = append(args, *f.Start)
	}
	if f.End != nil {
		where += ` AND ` + f.DateField + ` <= ?`
		args = append(args, *f.End)
	}
	return where, args
}

// pageClause is the common filter's order and page: sortBy in order, the primary key breaking ties the same way, then take and skip when they are set.
func pageClause(f listFilter, tieBreakers ...string) (string, []any) {
	direction := " DESC"
	if f.Ascending {
		direction = " ASC"
	}
	if len(tieBreakers) == 0 {
		tieBreakers = []string{"id"}
	}
	order := ` ORDER BY ` + f.orderColumn() + direction
	for _, t := range tieBreakers {
		order += `, ` + t + direction
	}
	var args []any
	if f.Limit > 0 {
		order += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	if f.Skip > 0 {
		order += ` OFFSET ?`
		args = append(args, f.Skip)
	}
	return order, args
}

// hasResponded reports whether the contact has a response to the survey.
func (r *Repository) hasResponded(ctx context.Context, contactID, surveyID uuid.UUID) (bool, error) {
	var exists bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM responses WHERE contact_id = ? AND survey_id = ?)`, contactID, surveyID).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("find contact response: %w", err)
	}
	return exists, nil
}

// segmentWorkspace is the workspace of a segment, nil when there is none.
func (r *Repository) segmentWorkspace(ctx context.Context, id uuid.UUID) (*uuid.UUID, error) {
	var ids []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT workspace_id FROM segments WHERE id = ?`, id).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("load segment: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return &ids[0], nil
}

// attributeValues reads the contacts' values under the given keys as text, by contact and key.
func (r *Repository) attributeValues(ctx context.Context, contactIDs []uuid.UUID, keys []string) (map[uuid.UUID]map[string]string, error) {
	out := map[uuid.UUID]map[string]string{}
	if len(contactIDs) == 0 || len(keys) == 0 {
		return out, nil
	}
	var rows []struct {
		ContactID   uuid.UUID
		Key         string
		ValueText   *string
		ValueNumber *float64
		ValueDate   *time.Time
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT a.contact_id, k.key, a.value_text, a.value_number, a.value_date FROM contact_attributes a JOIN contact_attribute_keys k ON k.id = a.attribute_key_id
		WHERE a.contact_id = ANY(?::uuid[]) AND k.key IN ?`, uuidArray(contactIDs), keys).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load contact attributes: %w", err)
	}
	for _, row := range rows {
		if out[row.ContactID] == nil {
			out[row.ContactID] = map[string]string{}
		}
		out[row.ContactID][row.Key] = contact.Columns{Text: row.ValueText, Number: row.ValueNumber, Date: row.ValueDate}.String()
	}
	return out, nil
}
