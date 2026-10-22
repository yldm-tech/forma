package contact

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
)

// CSV import limits and choices (apps/web/modules/contacts/components/upload-contacts-button.tsx, types/contact.ts, lib/contacts.ts).
//
// MaxImportColumns is not forma2's: every column of a file becomes an attribute key, and the file size alone let one import create tens of thousands of them.
const (
	MaxImportBytes   = 800 * 1024
	MaxImportRecords = 10000
	MaxImportColumns = 1000
	importChunkSize  = 50
)

// Duplicate actions: what an import does with a row whose email an existing contact has.
const (
	DuplicateSkip      = "skip"
	DuplicateUpdate    = "update"
	DuplicateOverwrite = "overwrite"
)

// Stable codes of the import failures.
const (
	CodeInvalidCSV            = "invalid_csv"
	CodeNoValidContacts       = "upload_contacts_error_no_valid_contacts"
	CodeDuplicateMappings     = "upload_contacts_error_duplicate_mappings"
	CodeInvalidCSVRecords     = "invalid_csv_records"
	CodeInvalidAttributeKeys  = "invalid_attribute_keys"
	CodeReservedAttributeKeys = "reserved_attribute_keys"
	CodeAttributeTypeMismatch = "upload_contacts_error_attribute_type_mismatch"
)

// ImportInput is an import request: the file, what to do with existing contacts, and the column mapping. AttributeMap maps a column header to an existing key's id or to the name of a key to create; a column it leaves out is matched to a key as forma2's upload dialog suggested, or else creates a key named after it.
type ImportInput struct {
	File            io.Reader
	DuplicateAction string
	AttributeMap    map[string]string
}

// TypeMismatch is one column whose values do not fit its existing key's type; nothing is imported then.
type TypeMismatch struct {
	Key           string   `json:"key"`
	DataType      string   `json:"dataType"`
	InvalidValues []string `json:"invalidValues"`
}

// ImportResult lists the contacts an import created or changed; skipped duplicates are not in it.
type ImportResult struct {
	Contacts []Contact `json:"contacts"`
}

// csvTable is a parsed file: its trimmed headers and its rows.
type csvTable struct {
	headers []string
	rows    [][]string
}

// parseCSV reads a contacts file as forma2's upload dialog did (csv-parse with bom, columns, skip_empty_lines): a byte order mark is skipped, the first row names the columns and every row must have as many fields, empty lines are skipped. forma2 split on any of comma, semicolon and tab; encoding/csv takes one delimiter, chosen as the one the header row uses most. Rows stream from the reader; reading stops at the record limit.
func parseCSV(r io.Reader) (*csvTable, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	if bom, _ := br.Peek(3); bytes.Equal(bom, []byte{0xEF, 0xBB, 0xBF}) {
		_, _ = br.Discard(3)
	}
	head, _ := br.Peek(br.Size())
	if i := bytes.IndexAny(head, "\r\n"); i >= 0 {
		head = head[:i]
	}
	cr := csv.NewReader(br)
	cr.Comma = sniffDelimiter(head)
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return &csvTable{}, nil
	}
	if err != nil {
		return nil, apperr.InvalidInput(CodeInvalidCSV, "Invalid CSV: "+err.Error())
	}
	if len(header) > MaxImportColumns {
		return nil, apperr.InvalidInput(CodeInvalidCSV, fmt.Sprintf("Maximum %d columns allowed at a time.", MaxImportColumns))
	}
	t := &csvTable{headers: make([]string, len(header))}
	for i, h := range header {
		t.headers[i] = jsTrim(h)
	}
	for {
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return t, nil
		}
		if err != nil {
			return nil, apperr.InvalidInput(CodeInvalidCSV, "Invalid CSV: "+err.Error())
		}
		if len(t.rows) == MaxImportRecords {
			return nil, apperr.InvalidInput(CodeInvalidCSVRecords, "Maximum 10000 records allowed at a time.")
		}
		t.rows = append(t.rows, row)
	}
}

// sniffDelimiter picks the delimiter of a header line among comma, semicolon and tab, counting outside quotes.
func sniffDelimiter(line []byte) rune {
	counts := map[byte]int{}
	quoted := false
	for _, b := range line {
		switch {
		case b == '"':
			quoted = !quoted
		case !quoted && (b == ',' || b == ';' || b == '\t'):
			counts[b]++
		}
	}
	best := byte(',')
	for _, d := range []byte{';', '\t'} {
		if counts[d] > counts[best] {
			best = d
		}
	}
	return rune(best)
}

// mappedTable is the file with each column renamed to the key it fills.
type mappedTable struct {
	keys    []string
	records []map[string]string
}

// mapColumns applies the column mapping: an explicit target, else the first key whose name (or key) contains the column as forma2's isStringMatch has it, else the column's own name. Two columns may not fill one key.
func mapColumns(t *csvTable, keys []Key, attributeMap map[string]string) (*mappedTable, error) {
	byID := map[string]Key{}
	for _, k := range keys {
		byID[k.ID.String()] = k
	}
	targets := make([]string, len(t.headers))
	columnsByTarget := map[string][]string{}
	var targetOrder []string
	for i, h := range t.headers {
		target, ok := attributeMap[h]
		if !ok {
			target = h
			for _, k := range keys {
				label := k.Key
				if k.Name != nil {
					label = *k.Name
				}
				if isStringMatch(h, label) {
					target = k.ID.String()
					break
				}
			}
		}
		if k, ok := byID[target]; ok {
			target = k.Key
		}
		targets[i] = target
		lower := strings.ToLower(target)
		if _, seen := columnsByTarget[lower]; !seen {
			targetOrder = append(targetOrder, lower)
		}
		columnsByTarget[lower] = append(columnsByTarget[lower], h)
	}
	var duplicated []string
	for _, lower := range targetOrder {
		if cols := columnsByTarget[lower]; len(cols) > 1 {
			duplicated = append(duplicated, cols...)
		}
	}
	if len(duplicated) > 0 {
		e := apperr.InvalidInput(CodeDuplicateMappings, "Attribute map contains duplicate values")
		e.Details = map[string]any{"attributes": strings.Join(duplicated, ", ")}
		return nil, e
	}
	m := &mappedTable{keys: targets, records: make([]map[string]string, len(t.rows))}
	for r, row := range t.rows {
		rec := make(map[string]string, len(targets))
		for i, target := range targets {
			rec[target] = row[i]
		}
		m.records[r] = rec
	}
	return m, nil
}

// validateRecords is forma2's ZContactCSVUploadResponse on the mapped rows: at least one, every row with a non-empty email, no email twice, no non-empty userId twice.
func validateRecords(m *mappedTable) error {
	if len(m.records) == 0 {
		return apperr.InvalidInput(CodeNoValidContacts, "No valid contacts found in the CSV file")
	}
	invalid := func(msg string) error { return apperr.InvalidInput(CodeInvalidCSVRecords, msg) }
	if !slices.Contains(m.keys, keyEmail) {
		return invalid("Missing email field for one or more records")
	}
	emails := map[string]bool{}
	userIDs := map[string]bool{}
	for _, r := range m.records {
		if r[keyEmail] == "" {
			return invalid("Email field is empty for one or more records")
		}
	}
	for _, r := range m.records {
		if emails[r[keyEmail]] {
			return invalid("Duplicate emails found in the records")
		}
		emails[r[keyEmail]] = true
	}
	for _, r := range m.records {
		if id := r[keyUserID]; id != "" {
			if userIDs[id] {
				return invalid("Duplicate userIds found in the records")
			}
			userIDs[id] = true
		}
	}
	return nil
}

// Import is forma2's createContactsFromCSVAction. The file is parsed and mapped, then forma2's createContactsFromCSV runs: values for existing number and date keys must parse or nothing is imported; a new column becomes a key typed from its first value, or a string key when its values disagree; a row whose email no contact has creates a contact, and one whose email a contact has is skipped, merged into it (update) or replaces its values (overwrite). In a merge or overwrite, a userId another contact holds is replaced by the contact's own.
//
// As in forma2, the import does not apply MaxAttributeKeys to the keys it creates, and does not check a new contact's userId against other contacts; a file is limited to MaxImportColumns columns instead.
func (s *Service) Import(ctx context.Context, userID, workspaceID uuid.UUID, in ImportInput) (res *ImportResult, err error) {
	ev := audit.Event{Action: audit.ActionCreatedFromCSV, TargetType: audit.TargetContact}
	defer func() { s.record(ctx, ev, err) }()

	org, err := s.organizationOf(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	ev.OrganizationID = org.String()
	if err := s.write(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	table, err := parseCSV(in.File)
	if err != nil {
		return nil, err
	}
	keys, err := s.repo.keys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	mapped, err := mapColumns(table, keys, in.AttributeMap)
	if err != nil {
		return nil, err
	}
	if err := checkNewKeys(mapped.keys, keys); err != nil {
		return nil, err
	}
	if err := validateRecords(mapped); err != nil {
		return nil, err
	}
	contacts, err := s.importRecords(ctx, workspaceID, mapped, keys, in.DuplicateAction)
	if err != nil {
		return nil, err
	}
	res = &ImportResult{Contacts: contacts}
	ev.NewObject = map[string]any{"contacts": contacts}
	return res, nil
}

// checkNewKeys refuses columns that would create a key that is not a safe identifier, or is reserved, with forma2's messages.
func checkNewKeys(columns []string, keys []Key) error {
	existing := map[string]bool{}
	for _, k := range keys {
		existing[strings.ToLower(k.Key)] = true
	}
	var missing []string
	for _, c := range columns {
		if !existing[strings.ToLower(c)] {
			missing = append(missing, c)
		}
	}
	var unsafe []string
	for _, c := range missing {
		if !IsSafeIdentifier(c) {
			unsafe = append(unsafe, c)
		}
	}
	if len(unsafe) > 0 {
		return apperr.InvalidInput(CodeInvalidAttributeKeys, "Invalid attribute key(s): "+strings.Join(unsafe, ", ")+". Keys must only contain lowercase letters, numbers, and underscores, and must start with a letter.")
	}
	if len(reservedAmong(missing)) > 0 {
		return apperr.InvalidInput(CodeReservedAttributeKeys, reservedKeyIssue(missing))
	}
	return nil
}

type existingContact struct {
	id     uuid.UUID
	userID string
}

func (s *Service) importRecords(ctx context.Context, workspaceID uuid.UUID, m *mappedTable, keys []Key, action string) ([]Contact, error) {
	var emails, userIDs []string
	values := map[string][]string{}
	for _, r := range m.records {
		emails = append(emails, r[keyEmail])
		if id := r[keyUserID]; id != "" {
			userIDs = append(userIDs, id)
		}
		for _, k := range m.keys {
			if v := r[k]; jsTrim(v) != "" {
				values[k] = append(values[k], v)
			}
		}
	}
	byEmail, err := s.repo.contactsByEmail(ctx, workspaceID, emails)
	if err != nil {
		return nil, err
	}
	userIDOwners, err := s.repo.userIDOwners(ctx, workspaceID, userIDs)
	if err != nil {
		return nil, err
	}

	actual := map[string]Key{}
	for _, k := range keys {
		actual[strings.ToLower(k.Key)] = k
	}
	// The type of every column: its existing key's, or the one detected from its first value. forma2 kept this map by column name and looked it up by key name, so a column spelled differently from its key stored typed values as text; it is kept by lowercased name here.
	types := map[string]string{}
	var mismatches []TypeMismatch
	for _, col := range m.keys {
		lower := strings.ToLower(col)
		if k, ok := actual[lower]; ok {
			types[lower] = k.DataType
			if bad := unparsed(values[col], k.DataType); len(bad) > 0 {
				mismatches = append(mismatches, TypeMismatch{Key: col, DataType: k.DataType, InvalidValues: bad})
			}
			continue
		}
		if _, ok := types[lower]; ok {
			continue
		}
		dataType := TypeString
		if vs := values[col]; len(vs) > 0 {
			dataType = detectCSVType(vs[0])
		}
		if dataType != TypeString && len(unparsed(values[col], dataType)) > 0 {
			s.log.WarnContext(ctx, "Type validation warnings during CSV upload", "warnings", []string{fmt.Sprintf("Attribute %q has mixed or invalid values for type %q, treating as string type", col, dataType)})
			dataType = TypeString
		}
		types[lower] = dataType
	}
	if len(mismatches) > 0 {
		e := apperr.InvalidInput(CodeAttributeTypeMismatch, "Some values do not match their attribute's data type")
		e.Details = map[string]any{"validationErrors": mismatches}
		return nil, e
	}

	var created []newKey
	for _, col := range m.keys {
		lower := strings.ToLower(col)
		if _, ok := actual[lower]; ok || slices.ContainsFunc(created, func(k newKey) bool { return strings.ToLower(k.key) == lower }) {
			continue
		}
		created = append(created, newKey{key: col, dataType: types[lower]})
	}
	var ids []uuid.UUID
	for start := 0; start < len(m.records); start += importChunkSize {
		chunk := m.records[start:min(start+importChunkSize, len(m.records))]
		err := s.repo.transaction(ctx, func(tx *Repository) error {
			// The new keys are created with the first chunk, so a first chunk that fails leaves none of them behind.
			if start == 0 && len(created) > 0 {
				if err := tx.createKeys(ctx, workspaceID, created); err != nil {
					return err
				}
				keys, err := tx.keys(ctx, workspaceID)
				if err != nil {
					return err
				}
				for _, k := range keys {
					actual[strings.ToLower(k.Key)] = k
				}
			}
			for _, record := range chunk {
				id, err := importRecord(ctx, tx, workspaceID, m.keys, record, actual, byEmail, userIDOwners, action)
				if err != nil {
					return err
				}
				if id != uuid.Nil {
					ids = append(ids, id)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return s.repo.contactsByID(ctx, ids)
}

// unparsed lists the values a typed key cannot read.
func unparsed(values []string, dataType string) []string {
	var bad []string
	if dataType == TypeString {
		return nil
	}
	for _, v := range values {
		if !prepareColumns(Value{String: v}, dataType).parsedAs(dataType) {
			bad = append(bad, v)
		}
	}
	return bad
}

// importRecord writes one row and returns the contact it created or changed, or uuid.Nil for a skipped duplicate.
func importRecord(ctx context.Context, tx *Repository, workspaceID uuid.UUID, columns []string, record map[string]string, actual map[string]Key, byEmail map[string]existingContact, userIDOwners map[string][]uuid.UUID, action string) (uuid.UUID, error) {
	row := map[string]string{}
	var order []string
	for _, col := range columns {
		k, ok := actual[strings.ToLower(col)]
		if !ok {
			return uuid.Nil, apperr.InvalidInput(apperr.CodeValidation, fmt.Sprintf("Attribute key %q not found in attribute key map", col))
		}
		if _, seen := row[k.Key]; !seen {
			order = append(order, k.Key)
		}
		row[k.Key] = record[col]
	}
	if row[keyEmail] == "" {
		return uuid.Nil, apperr.InvalidInput(apperr.CodeValidation, "Email is required for all contacts")
	}
	existing, found := byEmail[row[keyEmail]]
	if found && action == DuplicateSkip {
		return uuid.Nil, nil
	}
	if found {
		// resolveUserIdConflict: a userId another contact holds gives way to this contact's own, or to none.
		if slices.ContainsFunc(userIDOwners[row[keyUserID]], func(owner uuid.UUID) bool { return owner != existing.id }) {
			delete(row, keyUserID)
			order = slices.DeleteFunc(order, func(k string) bool { return k == keyUserID })
			if existing.userID != "" {
				row[keyUserID] = existing.userID
				order = append(order, keyUserID)
			}
		}
	}
	values := make([]keyedColumns, 0, len(order))
	for _, name := range order {
		k := actual[strings.ToLower(name)]
		values = append(values, keyedColumns{keyID: k.ID, columns: prepareCSVColumns(row[name], k.DataType)})
	}
	if !found {
		id, err := tx.insertContact(ctx, workspaceID)
		if err != nil {
			return uuid.Nil, err
		}
		return id, tx.upsertAttributes(ctx, id, values)
	}
	if action == DuplicateOverwrite {
		if err := tx.deleteAllAttributes(ctx, existing.id); err != nil {
			return uuid.Nil, err
		}
	}
	if err := tx.upsertAttributes(ctx, existing.id, values); err != nil {
		return uuid.Nil, err
	}
	return existing.id, tx.touchContact(ctx, existing.id)
}
