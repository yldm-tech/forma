package contact

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
)

// exportBatchSize is how many contacts the export reads at a time.
const exportBatchSize = 500

// ContentTypeCSV is the type of the contacts export.
const ContentTypeCSV = "text/csv;charset=utf-8"

// Export writes the workspace's contacts that a search matches (the contacts list's search; an empty one matches all of them), newest first, to w as CSV, and returns the file's name. The header row is the workspace's attribute keys, oldest first, so the file goes back in through the CSV import; each row is a contact's values as the list shows them, empty where it has none. A value a spreadsheet would read as a formula gets a leading apostrophe, as in the response export; a number (-5) is not one and is written as it is, so it imports again. The import takes that apostrophe off again (undefangCell) and leaves a key alone where a cell is empty, so a re-imported file gives the contacts the values they had. The import needs an email on every row, though: a file with a contact that has no email (one the SDK created from a userId alone) is refused as a whole, as any file with an empty email cell is. The contacts are read in batches, newest first by keyset, so nothing is held beyond one batch. Whoever may list the workspace's contacts may export them; the export is audited.
func (s *Service) Export(ctx context.Context, userID, workspaceID uuid.UUID, search string, w io.Writer) (name string, err error) {
	ev := audit.Event{Action: audit.ActionExported, TargetType: audit.TargetContact}
	defer func() { s.record(ctx, ev, err) }()

	org, err := s.organizationOf(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	ev.OrganizationID = org.String()
	if err := s.read(ctx, userID, workspaceID); err != nil {
		return "", err
	}
	keys, err := s.repo.keys(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	buf := bufio.NewWriter(w)
	// The byte order mark Excel needs to read the file as UTF-8; the import skips it.
	if _, err := buf.WriteString("\ufeff"); err != nil {
		return "", fmt.Errorf("write contacts export: %w", err)
	}
	out := csv.NewWriter(buf)
	header := make([]string, len(keys))
	for i, k := range keys {
		header[i] = defangCell(k.Key)
	}
	if err := out.Write(header); err != nil {
		return "", fmt.Errorf("write contacts export: %w", err)
	}
	count := 0
	var after *contactRow
	record := make([]string, len(keys))
	for {
		page, err := s.repo.exportPage(ctx, workspaceID, jsTrim(search), after)
		if err != nil {
			return "", err
		}
		for _, c := range page {
			for i, k := range keys {
				record[i] = defangCell(c.Attributes[k.Key])
			}
			if err := out.Write(record); err != nil {
				return "", fmt.Errorf("write contacts export: %w", err)
			}
		}
		count += len(page)
		if len(page) < exportBatchSize {
			break
		}
		last := page[len(page)-1]
		after = &contactRow{ID: last.ID, CreatedAt: last.CreatedAt}
	}
	out.Flush()
	if err := out.Error(); err != nil {
		return "", fmt.Errorf("write contacts export: %w", err)
	}
	if err := buf.Flush(); err != nil {
		return "", fmt.Errorf("write contacts export: %w", err)
	}
	ev.NewObject = map[string]any{"workspaceId": workspaceID.String(), "contactCount": count, "searched": search != ""}
	return exportFileName(s.now()), nil
}

// exportFileName is contacts-<UTC time>-utc.csv, the shape of the response export's name.
func exportFileName(now time.Time) string {
	return "contacts-" + now.UTC().Format("2006-01-02-15-04-05") + "-utc.csv"
}

// defangCell gives a cell a spreadsheet would evaluate as a formula (one starting with =, +, -, @, a tab or a carriage return) a leading apostrophe, as the response export does. A number such as -5 or +1 starts like one but is read as the number it is, so it is left alone.
func defangCell(s string) string {
	if s == "" || !strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return s
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return "'" + s
	}
	return s
}

// undefangCell is the inverse of defangCell, which the CSV import applies to every cell: an apostrophe is taken off where defangCell would have put it, before a text that starts with =, +, -, @, a tab or a carriage return and is not a number. Any other apostrophe ('-5, 'abc) is kept. A value that itself starts with such an apostrophe cannot be told from a defanged one and loses it.
func undefangCell(s string) string {
	if len(s) < 2 || s[0] != '\'' {
		return s
	}
	if rest := s[1:]; defangCell(rest) == s {
		return rest
	}
	return s
}

// exportPage is the batch of the contacts a search matches (searchedContacts) that comes after a contact in the export's order, newest first, with their values.
func (r *Repository) exportPage(ctx context.Context, workspaceID uuid.UUID, search string, after *contactRow) ([]Contact, error) {
	query, args := searchedContacts(workspaceID, search)
	if after != nil {
		query += ` AND (c.created_at, c.id) < (?, ?)`
		args = append(args, after.CreatedAt, after.ID)
	}
	query += ` ORDER BY c.created_at DESC, c.id DESC LIMIT ?`
	args = append(args, exportBatchSize)
	var rows []contactRow
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("export contacts: %w", err)
	}
	return r.withAttributes(ctx, rows)
}
