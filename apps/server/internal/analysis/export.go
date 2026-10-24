package analysis

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// Export formats.
const (
	FormatCSV  = "csv"
	FormatXLSX = "xlsx"
)

// Content types of the exported files, as forma2's download set them.
const (
	ContentTypeCSV  = "text/csv;charset=utf-8"
	ContentTypeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

// exportBatchSize is how many responses one read of the export takes, forma2's batchSize.
const exportBatchSize = 3000

// exportBasicsCovered is forma2's EXPORT_BASICS_COVERED_RESERVED_NAMES: reserved fields the fixed columns already carry.
var exportBasicsCovered = map[string]bool{"responseId": true, "surveyId": true, "finished": true, "startedAt": true}

// reservedExportEntries is forma2's getReservedExportEntries: the reserved catalog minus what the fixed columns carry, what the survey's own names shadow, and the IP address unless the survey captures it. The anonymize switch removes no column: anonymized responses export empty cells.
func reservedExportEntries(sv *surveyDoc) []embeddeddata.ReservedField {
	var out []embeddeddata.ReservedField
	for _, e := range embeddeddata.DropShadowed(embeddeddata.ReservedCatalog, embeddeddata.ShadowingNames(sv.Fields, sv.elementIDs())) {
		if exportBasicsCovered[e.Name] || (e.Name == "ipAddress" && !sv.IsCaptureIPEnabled) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// recallPattern is forma2's RECALL_TOKEN_PATTERN.
var recallPattern = regexp.MustCompile(`#recall:([A-Za-z0-9_-]+)/fallback:([^#]*)#`)

// defaultHeadlines is forma2's replaceHeadlineRecall(survey, "default") read back as each element's default headline: every recall token becomes "@" and the label of what it recalls (an ingested field's name, an element's headline as text, a computed field's name or a reserved field's title, else the token's id), with tokens nested in a label shown as "___". forma2 rewrote the headlines one element after another on the survey it was reading, so an element recalling an earlier one sees that element's rewritten headline.
func defaultHeadlines(sv *surveyDoc) []string {
	headlines := make([]string, len(sv.Elements))
	current := map[string]string{}
	for i := len(sv.Elements) - 1; i >= 0; i-- {
		current[sv.Elements[i].ID] = sv.Elements[i].Headline.Default()
	}
	label := func(id string) string {
		for _, f := range sv.Fields {
			if f.StorageKey == id && f.Source == embeddeddata.SourceIngested {
				return f.Name
			}
		}
		if h, ok := current[id]; ok {
			return surveydoc.TextContent(h)
		}
		for _, f := range sv.Fields {
			if f.StorageKey == id && f.Source == embeddeddata.SourceComputed {
				return f.Name
			}
		}
		for _, e := range embeddeddata.ReservedCatalog {
			if e.Name == id {
				return embeddeddata.TitleCase(e.Name)
			}
		}
		return ""
	}
	first := map[string]bool{}
	for i, el := range sv.Elements {
		text := el.Headline.Default()
		if strings.Contains(text, "#recall:") {
			text = recallPattern.ReplaceAllStringFunc(text, func(token string) string {
				id := recallPattern.FindStringSubmatch(token)[1]
				l := label(id)
				if l == "" {
					l = id
				}
				return "@" + recallPattern.ReplaceAllLiteralString(l, "___")
			})
		}
		headlines[i] = text
		if !first[el.ID] {
			first[el.ID] = true
			current[el.ID] = text
		}
	}
	return headlines
}

// exportColumns is forma2's extractSurveyDetails: the header groups of the export.
type exportColumns struct {
	reserved   []embeddeddata.ReservedField
	elements   [][]string
	variables  []embeddeddata.Linked
	hidden     []string
	attributes []string
}

func newExportColumns(sv *surveyDoc, attributes []string) exportColumns {
	cols := exportColumns{reserved: reservedExportEntries(sv), variables: embeddeddata.OfSource(sv.Fields, embeddeddata.SourceComputed), attributes: attributes}
	for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceIngested) {
		cols.hidden = append(cols.hidden, f.StorageKey)
	}
	for i, h := range defaultHeadlines(sv) {
		el := sv.Elements[i]
		headline := fmt.Sprintf("%d. %s", i+1, surveydoc.TextContent(h))
		switch el.Type {
		case surveydoc.ElementMatrix:
			labels := make([]string, len(el.Rows))
			for j, row := range el.Rows {
				labels[j] = headline + " - " + surveydoc.TextContent(row.Label.Default())
			}
			cols.elements = append(cols.elements, labels)
		case surveydoc.ElementMultipleChoiceMulti, surveydoc.ElementMultipleChoiceSingle, surveydoc.ElementRanking:
			cols.elements = append(cols.elements, []string{headline, headline + " - Option ID"})
		default:
			cols.elements = append(cols.elements, []string{headline})
		}
	}
	return cols
}

// headers is forma2's export header row. Quotas are always a column: forma2's quota licence check always answered yes.
func (c exportColumns) headers(sv *surveyDoc) []string {
	h := []string{"No.", "Response ID", "Timestamp", "Finished", "Quotas", "Survey ID", "Forma ID (internal)", "User ID", "Tags"}
	for _, e := range c.reserved {
		h = append(h, embeddeddata.TitleCase(e.Name))
	}
	for _, labels := range c.elements {
		h = append(h, labels...)
	}
	for _, v := range c.variables {
		h = append(h, v.Name)
	}
	h = append(h, c.hidden...)
	for _, a := range c.attributes {
		h = append(h, "person."+a)
	}
	if sv.IsVerifyEmailEnabled {
		h = append(h, "Verified Email")
	}
	return h
}

// storageURL matches a stored file's relative URL, forma2's STORAGE_URL_PATTERN.
var storageURL = regexp.MustCompile(`^/storage/[^/]+/(public|private)/.+`)

// resolveStorageURLs is forma2's resolveStorageUrlsInObject: every relative storage URL in a value made absolute, a public file's against the public domain and a private one's against the app.
func (s *Service) resolveStorageURLs(v any) any {
	switch x := v.(type) {
	case string:
		if !storageURL.MatchString(x) {
			return x
		}
		base := s.cfg.WebAppURL
		if !strings.Contains(x, "/private/") && s.cfg.PublicURL != nil {
			base = s.cfg.PublicURL
		}
		if base == nil {
			return x
		}
		return strings.TrimRight(base.String(), "/") + x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = s.resolveStorageURLs(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = s.resolveStorageURLs(e)
		}
		return out
	}
	return v
}

// processResponseData is forma2's processResponseData, an answer as export text: a list joined by "; " without its empty entries, an object as "key: value" lines without its empty values.
func processResponseData(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return jsvalue.FormatNumber(x)
	case []any:
		parts := []string{}
		for _, e := range x {
			if e == nil || e == "" {
				continue
			}
			parts = append(parts, jsvalue.String(e))
		}
		return strings.Join(parts, "; ")
	case map[string]any:
		lines := []string{}
		for _, k := range jsonbKeys(x) {
			if x[k] == "" {
				continue
			}
			lines = append(lines, k+": "+jsvalue.String(x[k]))
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

// joinJS is Array.prototype.join: null entries as empty strings.
func joinJS(list []any, sep string) string {
	parts := make([]string, len(list))
	for i, e := range list {
		if e != nil {
			parts[i] = jsvalue.String(e)
		}
	}
	return strings.Join(parts, sep)
}

// choiceIDs is forma2's extractChoiceIdsFromResponse: the ids of the choices an answer names, matched by the label in the response's language or any other, "other" for a label no choice has. The empty string a multi-select keeps for an open Other box is skipped.
func choiceIDs(el *element, answer any, language string) []string {
	if !truthy(answer) {
		return nil
	}
	find := func(v any) string {
		s, isString := v.(string)
		for _, c := range el.Choices {
			if !isString {
				break
			}
			matched := false
			if l, ok := c.Label[language]; ok && l == s {
				matched = true
			}
			for _, l := range c.Label {
				if l == s {
					matched = true
				}
			}
			if matched {
				if c.ID == "" {
					return "other"
				}
				return c.ID
			}
		}
		return "other"
	}
	switch x := answer.(type) {
	case []any:
		var out []string
		for _, v := range x {
			if v != "" {
				out = append(out, find(v))
			}
		}
		return out
	case string:
		return []string{find(x)}
	}
	return nil
}

// exportRow is forma2's getResponsesJson for one response: cells by header, as the JavaScript object it was. A later write to a header another group also has replaces the earlier one; a header without a cell is an undefined value, an empty cell.
func (s *Service) exportRow(sv *surveyDoc, cols exportColumns, n int, r *response.WithQuotas, zone string) (map[string]any, error) {
	row := map[string]any{}
	var data, variables, ttc, meta, attributes map[string]any
	for _, part := range []struct {
		raw json.RawMessage
		dst *map[string]any
	}{{r.Data, &data}, {r.Variables, &variables}, {r.TTC, &ttc}, {r.Meta, &meta}, {r.ContactAttributes, &attributes}} {
		if len(part.raw) > 0 {
			if err := json.Unmarshal(part.raw, part.dst); err != nil {
				return nil, fmt.Errorf("decode response %s: %w", r.ID, err)
			}
		}
	}

	row["No."] = float64(n)
	row["Response ID"] = r.ID.String()
	row["Timestamp"] = FormatTimestamp(r.CreatedAt, zone)
	row["Finished"] = "No"
	if r.Finished {
		row["Finished"] = "Yes"
	}
	row["Survey ID"] = r.SurveyID.String()
	row["Forma ID (internal)"], row["User ID"] = "", ""
	if r.Contact != nil {
		row["Forma ID (internal)"] = r.Contact.ID.String()
		if r.Contact.UserID != nil {
			row["User ID"] = *r.Contact.UserID
		}
	}
	tags := make([]string, len(r.Tags))
	for i, t := range r.Tags {
		tags[i] = t.Name
	}
	row["Tags"] = strings.Join(tags, ", ")
	quotas := make([]string, len(r.Quotas))
	for i, q := range r.Quotas {
		quotas[i] = q.Name
	}
	row["Quotas"] = strings.Join(quotas, ", ")

	projected := embeddeddata.Project(cols.reserved, embeddeddata.ValueResponse{ID: r.ID.String(), SurveyID: r.SurveyID.String(), CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt, Finished: r.Finished, Language: r.Language, Data: data, Variables: variables, TTC: ttc, Meta: meta})
	for _, e := range cols.reserved {
		v, ok := projected[e.Name]
		if !ok {
			v = ""
		}
		row[embeddeddata.TitleCase(e.Name)] = v
	}

	resolved, _ := s.resolveStorageURLs(data).(map[string]any)
	language := "default"
	if r.Language != nil && *r.Language != "" {
		language = *r.Language
	}
	for i, labels := range cols.elements {
		el := &sv.Elements[i]
		answer := member(resolved, el.ID)
		switch el.Type {
		case surveydoc.ElementMatrix:
			if !truthy(answer) {
				continue
			}
			cells, _ := answer.(map[string]any)
			for j, label := range labels {
				row[label] = ""
				if def := el.Rows[j].Label.Default(); def != "" {
					if v, ok := cells[def]; ok {
						row[label] = v
					}
				}
			}
		case surveydoc.ElementMultipleChoiceMulti, surveydoc.ElementMultipleChoiceSingle, surveydoc.ElementRanking:
			row[labels[0]] = processResponseData(answer)
			row[labels[1]] = strings.Join(choiceIDs(el, answer, language), ", ")
		default:
			row[labels[0]] = processResponseData(answer)
		}
	}

	for _, f := range cols.variables {
		if v, ok := variables[f.StorageKey]; ok {
			row[f.Name] = v
		} else {
			delete(row, f.Name)
		}
	}
	for _, a := range cols.attributes {
		v := member(attributes, a)
		if !truthy(v) {
			v = ""
		}
		row["person."+a] = v
	}
	for _, key := range cols.hidden {
		switch v := member(resolved, key).(type) {
		case []any:
			row[key] = joinJS(v, "; ")
		case float64:
			row[key] = v
		default:
			row[key] = processResponseData(v)
		}
	}
	if sv.IsVerifyEmailEnabled {
		row["Verified Email"] = processResponseData(member(resolved, "verifiedEmail"))
	}
	return row, nil
}

// formulaTrigger matches a cell a spreadsheet would evaluate as a formula.
var formulaTrigger = regexp.MustCompile(`^[=+\-@\t\r]`)

// defang is forma2's sanitizeFormulaInjection: a text cell or header a spreadsheet would read as a formula gets a leading apostrophe. Numbers are left alone.
func defang(v any) any {
	if s, ok := v.(string); ok && formulaTrigger.MatchString(s) {
		return "'" + s
	}
	return v
}

// sheetWriter writes the export's rows.
type sheetWriter interface {
	row(cells []any) error
	close() error
}

// csvWriter is forma2's convertToCsv (json2csv's defaults: every header and text quoted, numbers and booleans bare, an undefined or null cell empty, rows joined by "\n") behind the byte order mark the download added.
type csvWriter struct {
	w    *bufio.Writer
	rows int
}

func newCSVWriter(w io.Writer) *csvWriter {
	b := bufio.NewWriter(w)
	_, _ = b.WriteString("\ufeff")
	return &csvWriter{w: b}
}

func csvQuote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func (c *csvWriter) row(cells []any) error {
	if c.rows > 0 {
		_ = c.w.WriteByte('\n')
	}
	c.rows++
	for i, v := range cells {
		if i > 0 {
			_ = c.w.WriteByte(',')
		}
		switch x := v.(type) {
		case nil:
		case string:
			_, _ = c.w.WriteString(csvQuote(x))
		case float64:
			_, _ = c.w.WriteString(jsvalue.FormatNumber(x))
		case bool:
			_, _ = c.w.WriteString(strconv.FormatBool(x))
		default:
			b, err := json.Marshal(x)
			if err != nil {
				return fmt.Errorf("encode export cell: %w", err)
			}
			_, _ = c.w.WriteString(csvQuote(string(b)))
		}
	}
	return nil
}

func (c *csvWriter) close() error { return c.w.Flush() }

// xlsxWriter is forma2's convertToXlsxBuffer: one sheet, "Sheet1", a number cell for a number, a text cell for text, no cell for an undefined or null value.
type xlsxWriter struct {
	f   *excelize.File
	sw  *excelize.StreamWriter
	out io.Writer
	n   int
}

func newXLSXWriter(w io.Writer) (*xlsxWriter, error) {
	f := excelize.NewFile()
	sw, err := f.NewStreamWriter("Sheet1")
	if err != nil {
		return nil, fmt.Errorf("start xlsx sheet: %w", err)
	}
	return &xlsxWriter{f: f, sw: sw, out: w}, nil
}

func (x *xlsxWriter) row(cells []any) error {
	x.n++
	values := make([]any, len(cells))
	for i, v := range cells {
		switch c := v.(type) {
		case nil, string, float64, bool:
			values[i] = c
		default:
			b, err := json.Marshal(c)
			if err != nil {
				return fmt.Errorf("encode export cell: %w", err)
			}
			values[i] = string(b)
		}
	}
	cell, err := excelize.CoordinatesToCellName(1, x.n)
	if err != nil {
		return err
	}
	if err := x.sw.SetRow(cell, values); err != nil {
		return fmt.Errorf("write xlsx row: %w", err)
	}
	return nil
}

func (x *xlsxWriter) close() error {
	defer func() { _ = x.f.Close() }()
	if err := x.sw.Flush(); err != nil {
		return fmt.Errorf("finish xlsx sheet: %w", err)
	}
	if _, err := x.f.WriteTo(x.out); err != nil {
		return fmt.Errorf("write xlsx: %w", err)
	}
	return nil
}

// unsafeFileNameRun is what forma2's sanitizeString replaces: every run of characters other than letters, digits, "-", "." and "_" becomes "_", cut to 255.
var unsafeFileNameRun = regexp.MustCompile(`[^0-9a-zA-Z\-._]+`)

// exportFileName is forma2's getResponsesFileName: export-<name>-<UTC time>-utc.<format>, lower case.
func exportFileName(surveyName, format string, now time.Time) string {
	name := unsafeFileNameRun.ReplaceAllString(surveyName, "_")
	if len(name) > 255 {
		name = name[:255]
	}
	return strings.ToLower(fmt.Sprintf("export-%s-%s-utc.%s", name, now.UTC().Format("2006-01-02-15-04-05"), format))
}

// Export is forma2's getResponsesDownloadUrlAction: the survey's responses matching the filter, newest first, as a CSV or XLSX file written to w, and the file's name. The responses are read in batches inside one read-only snapshot, so the columns (the contact attributes the exported responses hold) and the rows agree; nothing is held in memory beyond one batch.
func (s *Service) Export(ctx context.Context, userID, surveyID uuid.UUID, format string, filter *response.Criteria, w io.Writer) (string, error) {
	sv, err := s.readableSurvey(ctx, userID, surveyID, authz.SurveyResponseExport)
	if err != nil {
		return "", err
	}
	clause, err := response.Where(sv.Survey, filter)
	if err != nil {
		return "", err
	}
	zone := "UTC"
	if sv.DisplayTimeZone != nil {
		zone = *sv.DisplayTimeZone
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		attributes, err := exportedAttributes(ctx, tx, surveyID, clause)
		if err != nil {
			return err
		}
		cols := newExportColumns(sv, attributes)
		headers := cols.headers(sv)
		var out sheetWriter
		if format == FormatXLSX {
			if out, err = newXLSXWriter(w); err != nil {
				return err
			}
		} else {
			out = newCSVWriter(w)
		}
		cells := make([]any, len(headers))
		for i, h := range headers {
			cells[i] = defang(h)
		}
		if err := out.row(cells); err != nil {
			return err
		}
		n := 0
		var cursor *response.Cursor
		for {
			batch, err := response.Page(ctx, tx, surveyID, clause, cursor, exportBatchSize, 0)
			if err != nil {
				return err
			}
			for i := range batch {
				n++
				row, err := s.exportRow(sv, cols, n, &batch[i], zone)
				if err != nil {
					return err
				}
				for j, h := range headers {
					cells[j] = defang(row[h])
				}
				if err := out.row(cells); err != nil {
					return err
				}
			}
			if len(batch) < exportBatchSize {
				break
			}
			last := batch[len(batch)-1]
			cursor = &response.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
		}
		return out.close()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return "", err
	}
	org, ws := sv.OrganizationID.String(), sv.WorkspaceID.String()
	analytics.Capture(userID.String(), "responses_exported", analytics.Properties{
		"survey_id":       surveyID.String(),
		"format":          format,
		"filter_applied":  filter != nil && !reflect.DeepEqual(*filter, response.Criteria{}),
		"organization_id": org,
		"workspace_id":    ws,
	}, analytics.Groups{OrganizationID: org, WorkspaceID: ws})
	return exportFileName(sv.Name, format, time.Now()), nil
}

// exportedAttributes are the contact attribute keys the exported responses hold, in the order forma2 met them: response by response in the export's order, each response's keys in the order its snapshot enumerates them.
func exportedAttributes(ctx context.Context, tx *gorm.DB, surveyID uuid.UUID, clause response.Clause) ([]string, error) {
	var keys []string
	seen := map[string]bool{}
	var cursor *response.Cursor
	for {
		args := append([]any{surveyID}, clause.Args...)
		where := "r.survey_id = ? AND " + clause.SQL
		if cursor != nil {
			where += " AND (r.created_at < ? OR (r.created_at = ? AND r.id < ?))"
			args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
		}
		var rows []struct {
			ID                uuid.UUID
			CreatedAt         time.Time
			ContactAttributes []byte
		}
		if err := tx.WithContext(ctx).Raw(`SELECT r.id, r.created_at, r.contact_attributes FROM responses r WHERE `+where+
			` ORDER BY r.created_at DESC, r.id DESC LIMIT ?`, append(args, exportBatchSize)...).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("read exported contact attributes: %w", err)
		}
		for _, r := range rows {
			var attributes map[string]json.RawMessage
			if len(r.ContactAttributes) > 0 {
				if err := json.Unmarshal(r.ContactAttributes, &attributes); err != nil {
					return nil, fmt.Errorf("decode contact attributes of response %s: %w", r.ID, err)
				}
			}
			for _, k := range jsonbKeys(attributes) {
				if !seen[k] {
					seen[k] = true
					keys = append(keys, k)
				}
			}
		}
		if len(rows) < exportBatchSize {
			return keys, nil
		}
		last := rows[len(rows)-1]
		cursor = &response.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}
