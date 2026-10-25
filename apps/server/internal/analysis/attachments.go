package analysis

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/storage"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// Limits of an attachment export, forma2's: above MaxAttachmentFiles the export is refused (narrowing the filter is the remedy), and the archive stops before crossing MaxAttachmentBytes, saying so in _TRUNCATED.txt. Object sizes are not in the database, so the byte cap is enforced while streaming.
const (
	MaxAttachmentFiles = 5000
	MaxAttachmentBytes = 5 * 1024 * 1024 * 1024

	attachmentBatchSize = 500
	truncationNotePath  = "_TRUNCATED.txt"
	manifestPath        = "manifest.csv"
)

// Statuses of an attachment in the manifest, forma2's TAttachmentStatus.
const (
	AttachmentOK               = "ok"
	AttachmentForeignWorkspace = "skipped_foreign_workspace"
	AttachmentInvalidURL       = "skipped_invalid_url"
	AttachmentMissing          = "missing_in_storage"
	AttachmentUnavailable      = "unavailable_in_storage"
	AttachmentTruncated        = "skipped_export_truncated"
)

// AttachmentEntry is one file a response's file upload answer names, forma2's TAttachmentEntry.
type AttachmentEntry struct {
	ZipPath           string
	ResponseID        uuid.UUID
	ResponseCreatedAt time.Time
	ElementID         string
	ElementLabel      string
	OriginalFileName  string
	Status            string
	// Bytes is filled in by the stream once storage reports the object's size.
	Bytes *int64
	// storage is where the object lives, set only for an ok entry.
	storage *storedObject
}

type storedObject struct {
	storageID string
	access    storage.AccessType
	fileName  string
}

// AttachmentCollection is forma2's CollectResponseAttachmentsResult.
type AttachmentCollection struct {
	Entries         []AttachmentEntry
	FileCount       int
	ResponseCount   int
	ExceedsMaxFiles bool
}

// AttachmentExporter collects a survey's response attachments and streams them as a ZIP, forma2's ENG-1256 export.
type AttachmentExporter struct {
	db         *gorm.DB
	files      *storage.Service
	workspaces *workspace.Repository
	log        *slog.Logger
}

func NewAttachmentExporter(db *gorm.DB, files *storage.Service, workspaces *workspace.Repository, log *slog.Logger) *AttachmentExporter {
	return &AttachmentExporter{db: db, files: files, workspaces: workspaces, log: log}
}

// labelledElement is a file upload element's place in the survey (1-based, so folders sort in survey order) and its label.
type labelledElement struct {
	id    string
	index int
	label string
}

// fileUploadLabels is forma2's buildFileUploadElementLabels: each file upload element with its position among all elements and its default headline as text, recall tokens shown as labels, or its id when that is empty. The recipe is the CSV export's, so a folder and its column read the same.
func fileUploadLabels(sv *surveyDoc) []labelledElement {
	var out []labelledElement
	for i, h := range defaultHeadlines(sv) {
		el := sv.Elements[i]
		if el.Type != surveydoc.ElementFileUpload {
			continue
		}
		label := surveydoc.TextContent(h)
		if label == "" {
			label = el.ID
		}
		out = append(out, labelledElement{id: el.ID, index: i + 1, label: label})
	}
	return out
}

// Collect is forma2's collectResponseAttachments: the file upload answers of the survey's responses matching filter, newest first, one entry per URL. Every URL's storage id is resolved again, since response data comes from respondents: a file stored outside the survey's workspace, or a URL that is not a stored file's, is reported in the manifest and never read. It stops after one file past maxFiles, so the caller can tell a full export from one over the cap. It reads no storage object.
func (x *AttachmentExporter) Collect(ctx context.Context, sv *surveyDoc, filter response.Clause, timeZone string) (*AttachmentCollection, error) {
	out := &AttachmentCollection{Entries: []AttachmentEntry{}}
	labels := fileUploadLabels(sv)
	if len(labels) == 0 {
		return out, nil
	}
	zone, err := time.LoadLocation(timeZone)
	if err != nil {
		zone = time.UTC
	}
	owners := map[string]*uuid.UUID{}
	usedPaths := map[string]bool{}
	var cursor *response.Cursor
	for {
		batch, err := x.responses(ctx, sv.ID, filter, cursor)
		if err != nil {
			return nil, err
		}
		for _, r := range batch {
			entries, files, capped, err := x.collectFromResponse(ctx, sv, r, labels, owners, usedPaths, MaxAttachmentFiles-out.FileCount, zone)
			if err != nil {
				return nil, err
			}
			out.Entries = append(out.Entries, entries...)
			out.FileCount += files
			if files > 0 {
				out.ResponseCount++
			}
			if capped {
				out.ExceedsMaxFiles = true
				return out, nil
			}
		}
		if len(batch) < attachmentBatchSize {
			return out, nil
		}
		last := batch[len(batch)-1]
		cursor = &response.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

type attachmentResponse struct {
	ID        uuid.UUID
	CreatedAt time.Time
	Data      []byte
}

// responses is one batch of the matching responses, only the columns the export needs.
func (x *AttachmentExporter) responses(ctx context.Context, surveyID uuid.UUID, filter response.Clause, cursor *response.Cursor) ([]attachmentResponse, error) {
	query := `SELECT r.id, r.created_at, r.data FROM responses r WHERE r.survey_id = ?`
	args := []any{surveyID}
	if filter.SQL != "" {
		query += " AND " + filter.SQL
		args = append(args, filter.Args...)
	}
	if cursor != nil {
		query += ` AND (r.created_at < ? OR (r.created_at = ? AND r.id < ?))`
		args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	query += ` ORDER BY r.created_at DESC, r.id DESC LIMIT ?`
	args = append(args, attachmentBatchSize)
	var rows []attachmentResponse
	if err := x.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list attachment responses: %w", err)
	}
	return rows, nil
}

func (x *AttachmentExporter) collectFromResponse(ctx context.Context, sv *surveyDoc, r attachmentResponse, labels []labelledElement, owners map[string]*uuid.UUID, usedPaths map[string]bool, remaining int, zone *time.Location) ([]AttachmentEntry, int, bool, error) {
	var data map[string]any
	if len(r.Data) > 0 {
		if err := json.Unmarshal(r.Data, &data); err != nil {
			return nil, 0, false, fmt.Errorf("decode response %s data: %w", r.ID, err)
		}
	}
	var entries []AttachmentEntry
	files := 0
	for _, el := range labels {
		urls, _ := data[el.id].([]any)
		for _, u := range urls {
			fileURL, ok := u.(string)
			if !ok {
				continue
			}
			entry := AttachmentEntry{ResponseID: r.ID, ResponseCreatedAt: r.CreatedAt, ElementID: el.id, ElementLabel: el.label, OriginalFileName: originalFileName(fileURL)}
			obj, status, err := x.resolve(ctx, sv, r.ID, el.id, fileURL, owners)
			if err != nil {
				return nil, 0, false, err
			}
			if obj == nil {
				entry.Status = status
				entries = append(entries, entry)
				continue
			}
			if files >= remaining {
				return entries, files, true, nil
			}
			entry.ZipPath = attachmentZipPath(r.ID, r.CreatedAt, el.index, el.label, entry.OriginalFileName, zone, usedPaths)
			entry.Status = AttachmentOK
			entry.storage = obj
			entries = append(entries, entry)
			files++
		}
	}
	return entries, files, false, nil
}

// resolve is forma2's resolveAttachmentStorage: the object a URL names when its storage id resolves to the survey's workspace and its name decodes, otherwise why it is skipped.
func (x *AttachmentExporter) resolve(ctx context.Context, sv *surveyDoc, responseID uuid.UUID, elementID, fileURL string, owners map[string]*uuid.UUID) (*storedObject, string, error) {
	storageID, access, encoded, ok := storage.ParseFileURL(fileURL)
	if !ok {
		x.log.WarnContext(ctx, "Skipping an attachment with an unparseable storage URL", "response_id", responseID, "element_id", elementID)
		return nil, AttachmentInvalidURL, nil
	}
	owner, seen := owners[storageID]
	if !seen {
		ws, err := x.workspaces.ResolveClientID(ctx, storageID)
		if err != nil {
			return nil, "", err
		}
		if ws != nil {
			owner = &ws.ID
		}
		owners[storageID] = owner
	}
	if owner == nil || *owner != sv.WorkspaceID {
		x.log.ErrorContext(ctx, "Refusing to export a response file stored outside the survey's workspace", "response_id", responseID, "element_id", elementID, "storage_id", storageID, "survey_workspace_id", sv.WorkspaceID)
		return nil, AttachmentForeignWorkspace, nil
	}
	name, ok := unescape(encoded)
	if !ok {
		x.log.WarnContext(ctx, "Skipping an attachment whose storage URL has malformed percent-encoding", "response_id", responseID, "element_id", elementID)
		return nil, AttachmentInvalidURL, nil
	}
	return &storedObject{storageID: storageID, access: access, fileName: name}, "", nil
}

// unescape is decodeURIComponent: the object is stored under the decoded name, the URL carries it percent-encoded. A malformed escape, which a respondent can write, is reported rather than failing the export.
func unescape(s string) (string, bool) {
	decoded, err := url.PathUnescape(s)
	return decoded, err == nil
}

// originalFileName is forma2's getOriginalFileNameFromUrl: the URL's last segment without its query, the name the uploader gave it (the --fid--<uuid> suffix removed, the extension kept), percent-decoded; "" when that fails.
func originalFileName(fileURL string) string {
	var last string
	if strings.HasPrefix(fileURL, "/storage/") {
		last = fileURL[strings.LastIndex(fileURL, "/")+1:]
	} else {
		u, err := url.Parse(fileURL)
		if err != nil || u.Scheme == "" {
			return ""
		}
		p := u.EscapedPath()
		last = p[strings.LastIndex(p, "/")+1:]
	}
	if i := strings.IndexAny(last, "?#"); i >= 0 {
		last = last[:i]
	}
	name, fid, hasFid := strings.Cut(last, "--fid--")
	if !hasFid || fid == "" {
		if name == "" {
			return ""
		}
		decoded, err := url.PathUnescape(name)
		if err != nil {
			return ""
		}
		return decoded
	}
	if dot := strings.LastIndex(last, "."); dot > strings.Index(last, "--fid--") {
		name += "." + last[dot+1:]
	}
	decoded, err := url.PathUnescape(name)
	if err != nil {
		return ""
	}
	return decoded
}

// Path rules of forma2's attachment-zip-paths.ts.
const (
	maxSegmentLength   = 60
	maxExtensionLength = 20
	fallbackSegment    = "unnamed"
	fallbackFileName   = "file"
)

var (
	unsafeCharacters = regexp.MustCompile(`[\x00-\x1f\x7f:*?"<>|]`)
	notAlphanumeric  = regexp.MustCompile(`[^A-Za-z0-9]`)
)

// isJSSpace is JavaScript's \s.
func isJSSpace(r rune) bool {
	return unicode.IsSpace(r) || r == '\uFEFF' || r == '\u00A0' || r == '\u1680' || (r >= '\u2000' && r <= '\u200A') || r == '\u2028' || r == '\u2029' || r == '\u202F' || r == '\u205F' || r == '\u3000'
}

// collapseSpace replaces every run of JavaScript white space with one space.
func collapseSpace(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if isJSSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// stripUnsafe is forma2's stripUnsafeCharacters: NFC, path separators as dashes, control and reserved characters removed, white space collapsed, trimmed.
func stripUnsafe(raw string) string {
	s := norm.NFC.String(raw)
	s = strings.NewReplacer(`\`, "-", "/", "-").Replace(s)
	s = unsafeCharacters.ReplaceAllString(s, "")
	return strings.TrimFunc(collapseSpace(s), isJSSpace)
}

// trimEdges removes dots and white space from both ends: Windows trims them on extraction, which would merge two names.
func trimEdges(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r == '.' || isJSSpace(r) })
}

// sliceUTF16 is String.prototype.slice(0, n): the first n UTF-16 code units, a split surrogate pair dropped.
func sliceUTF16(s string, n int) string {
	units := 0
	for i, r := range s {
		units += utf16.RuneLen(r)
		if units > n {
			return s[:i]
		}
	}
	return s
}

var reservedWindowsNames = func() map[string]bool {
	m := map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}
	for i := 1; i <= 9; i++ {
		m["com"+strconv.Itoa(i)] = true
		m["lpt"+strconv.Itoa(i)] = true
	}
	return m
}()

func escapeReserved(s string) string {
	if reservedWindowsNames[strings.ToLower(s)] {
		return "_" + s
	}
	return s
}

// sanitizeSegment is forma2's sanitizeZipPathSegment, for a folder named after a headline.
func sanitizeSegment(raw string) string {
	stripped := trimEdges(stripUnsafe(raw))
	if stripped == "" {
		return fallbackSegment
	}
	truncated := trimEdges(sliceUTF16(stripped, maxSegmentLength))
	if truncated == "" {
		return fallbackSegment
	}
	return escapeReserved(truncated)
}

// splitExtension splits at the last dot; a leading dot is part of the name.
func splitExtension(name string) (base, ext string) {
	dot := strings.LastIndex(name, ".")
	if dot <= 0 {
		return name, ""
	}
	return name[:dot], name[dot+1:]
}

// sanitizeFileName is forma2's sanitizeZipFileName: the base sanitized and cut, the extension reduced to letters and digits, so the file still opens.
func sanitizeFileName(raw string) string {
	base, ext := splitExtension(norm.NFC.String(raw))
	safeBase := trimEdges(sliceUTF16(trimEdges(stripUnsafe(base)), maxSegmentLength))
	safeExt := sliceUTF16(notAlphanumeric.ReplaceAllString(ext, ""), maxExtensionLength)
	if safeBase == "" {
		if safeExt != "" {
			return fallbackFileName + "." + safeExt
		}
		return fallbackFileName
	}
	if safeExt != "" {
		return escapeReserved(safeBase) + "." + safeExt
	}
	return escapeReserved(safeBase)
}

// attachmentZipPath is forma2's buildAttachmentZipPath: <created at in the zone, YYYY-MM-DDTHH-mm-ss>_<response id>/<element position>_<label>/<file name>, a name already used in the archive suffixed " (n)" before its extension.
func attachmentZipPath(responseID uuid.UUID, createdAt time.Time, index int, label, fileName string, zone *time.Location, used map[string]bool) string {
	dir := createdAt.In(zone).Format("2006-01-02T15-04-05") + "_" + responseID.String() + "/" + strconv.Itoa(index) + "_" + sanitizeSegment(label)
	name := sanitizeFileName(fileName)
	candidate := dir + "/" + name
	for attempt := 2; used[candidate]; attempt++ {
		base, ext := splitExtension(name)
		suffixed := base + " (" + strconv.Itoa(attempt) + ")"
		if ext != "" {
			suffixed += "." + ext
		}
		candidate = dir + "/" + suffixed
	}
	used[candidate] = true
	return candidate
}

// AttachmentArchiveName is forma2's buildAttachmentArchiveFileName.
func AttachmentArchiveName(surveyID uuid.UUID, now time.Time) string {
	return surveyID.String() + "-attachments-" + now.UTC().Format("2006-01-02") + ".zip"
}

// StreamZip is forma2's streamAttachmentsAsZip: every ok entry read from storage and stored uncompressed (photos and documents do not compress), one at a time; an object storage cannot produce is recorded in the manifest as missing or unavailable; the archive stops before it would exceed the byte cap, marking the rest truncated and adding _TRUNCATED.txt; manifest.csv lists every entry last. The response is already committed when this runs, so a storage failure never becomes an error status; an error returned here is a broken connection or archive.
func (x *AttachmentExporter) StreamZip(ctx context.Context, entries []AttachmentEntry, w io.Writer, now time.Time) error {
	zw := zip.NewWriter(w)
	manifest := make([]AttachmentEntry, 0, len(entries))
	var total int64
	appended := 0
	truncatedFrom := -1
	for i, entry := range entries {
		if entry.Status != AttachmentOK || entry.storage == nil {
			manifest = append(manifest, entry)
			continue
		}
		obj, err := x.files.Open(ctx, entry.storage.fileName, entry.storage.storageID, entry.storage.storageID, entry.storage.access)
		if err != nil {
			entry.Status = AttachmentUnavailable
			if storage.CodeOf(err) == storage.CodeFileNotFound {
				entry.Status = AttachmentMissing
			}
			x.log.WarnContext(ctx, "Attachment export: storage could not produce a file", "zip_path", entry.ZipPath, "err", err)
			manifest = append(manifest, entry)
			continue
		}
		if total+obj.ContentLength > MaxAttachmentBytes {
			_ = obj.Body.Close()
			truncatedFrom = i
			break
		}
		f, err := zw.CreateHeader(&zip.FileHeader{Name: entry.ZipPath, Method: zip.Store, Modified: now})
		if err == nil {
			_, err = io.Copy(f, obj.Body)
		}
		_ = obj.Body.Close()
		if err != nil {
			return fmt.Errorf("write attachment %s: %w", entry.ZipPath, err)
		}
		total += obj.ContentLength
		appended++
		size := obj.ContentLength
		entry.Bytes = &size
		manifest = append(manifest, entry)
	}
	if truncatedFrom >= 0 {
		for _, dropped := range entries[truncatedFrom:] {
			if dropped.Status == AttachmentOK {
				dropped.Status = AttachmentTruncated
			}
			manifest = append(manifest, dropped)
		}
		note := fmt.Sprintf("This archive was truncated after %d files because the export reached the %d byte limit.\nNarrow the response filter and download again to get the rest.\n", appended, int64(MaxAttachmentBytes))
		if err := writeZipFile(zw, truncationNotePath, now, []byte(note)); err != nil {
			return err
		}
	}
	var csv strings.Builder
	if err := writeManifest(&csv, manifest); err != nil {
		return err
	}
	if err := writeZipFile(zw, manifestPath, now, []byte(csv.String())); err != nil {
		return err
	}
	return zw.Close()
}

func writeZipFile(zw *zip.Writer, name string, now time.Time, body []byte) error {
	f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: now})
	if err == nil {
		_, err = f.Write(body)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// manifestFields are forma2's MANIFEST_FIELDS.
var manifestFields = []string{"filePath", "responseId", "responseCreatedAt", "elementId", "elementLabel", "originalFileName", "bytes", "status"}

// writeManifest is forma2's buildAttachmentManifestCsv through convertToCsv: every column quoted text with formula triggers defanged, the size a bare number or an empty text, timestamps in UTC.
func writeManifest(w io.Writer, entries []AttachmentEntry) error {
	out := &csvWriter{w: bufio.NewWriter(w)}
	header := make([]any, len(manifestFields))
	for i, f := range manifestFields {
		header[i] = f
	}
	if err := out.row(header); err != nil {
		return err
	}
	for _, e := range entries {
		var size any = ""
		if e.Bytes != nil {
			size = float64(*e.Bytes)
		}
		cells := []any{e.ZipPath, e.ResponseID.String(), jsvalue.ISOString(e.ResponseCreatedAt), e.ElementID, e.ElementLabel, e.OriginalFileName, size, e.Status}
		if err := out.row(cells); err != nil {
			return err
		}
	}
	return out.close()
}
