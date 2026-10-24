package storage

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// RegisterJobs adds the module's background work to `forma worker`: the cleanup of the files deleted responses and reset surveys pointed at.
func RegisterJobs(reg *jobs.Registry, files *Service, workspaces *workspace.Repository, log *slog.Logger) {
	jobs.Add(reg, &deleteResponseFilesWorker{files: files, workspaces: workspaces, log: log})
}

// deleteResponseFilesWorker is forma2's deleteResponseFileUrls. The URLs come out of response data, which respondents write, and a URL names the storage id its object lives under, so each URL's id is resolved again and a file stored outside the job's workspace is left alone: a URL planted under a key that only later became a file upload element was never checked against the survey's workspace. Files that cannot be deleted fail the job, which River retries; a URL that is not a stored file's, or names another workspace, is logged and skipped.
type deleteResponseFilesWorker struct {
	river.WorkerDefaults[response.DeleteFilesArgs]
	files      *Service
	workspaces *workspace.Repository
	log        *slog.Logger
}

func (w *deleteResponseFilesWorker) Work(ctx context.Context, job *river.Job[response.DeleteFilesArgs]) error {
	// Nothing can have been stored without a bucket.
	if !w.files.Configured() {
		return nil
	}
	owners := map[string]*uuid.UUID{}
	var failed []error
	for _, fileURL := range job.Args.FileURLs {
		f, ok := parseFileURL(fileURL)
		if !ok {
			w.log.ErrorContext(ctx, "Skipping a response file whose URL is not a stored file's", "workspace_id", job.Args.WorkspaceID)
			continue
		}
		owner, seen := owners[f.storageID]
		if !seen {
			ws, err := w.workspaces.ResolveClientID(ctx, f.storageID)
			if err != nil {
				return err
			}
			if ws != nil {
				owner = &ws.ID
			}
			owners[f.storageID] = owner
		}
		if owner == nil || *owner != job.Args.WorkspaceID {
			w.log.ErrorContext(ctx, "Refusing to delete a response file stored outside the survey's workspace", "workspace_id", job.Args.WorkspaceID, "storage_id", f.storageID)
			continue
		}
		// The URL carries the name percent-encoded; the object is stored under the decoded name.
		name, err := url.PathUnescape(f.fileName)
		if err != nil {
			w.log.ErrorContext(ctx, "Skipping a response file whose name is not percent-encoded", "workspace_id", job.Args.WorkspaceID, "storage_id", f.storageID)
			continue
		}
		if err := w.files.Delete(ctx, name, job.Args.WorkspaceID.String(), f.storageID, f.access); err != nil {
			if CodeOf(err) == CodeInvalidInput {
				w.log.ErrorContext(ctx, "Skipping a response file whose name leaves its directory", "workspace_id", job.Args.WorkspaceID, "storage_id", f.storageID)
				continue
			}
			failed = append(failed, err)
		}
	}
	return errors.Join(failed...)
}

// storedFile is a stored file's URL taken apart: the id it is stored under, its access type and its percent-encoded name, directories included.
type storedFile struct {
	storageID string
	access    AccessType
	fileName  string
}

// ParseFileURL is forma2's parseStorageFileUrl for other modules: the storage id a stored file's URL names, its access type and its percent-encoded name with its directories; ok is false for a URL that is not a stored file's.
func ParseFileURL(fileURL string) (storageID string, access AccessType, fileName string, ok bool) {
	f, ok := parseFileURL(fileURL)
	return f.storageID, f.access, f.fileName, ok
}

// parseFileURL is forma2's parseStorageFileUrl: a relative /storage/ URL or the path of an absolute one, without its query or fragment, split into the storage id, the access type and the rest.
func parseFileURL(fileURL string) (storedFile, bool) {
	path := fileURL
	if !strings.HasPrefix(fileURL, "/storage/") {
		u, err := url.Parse(fileURL)
		if err != nil || u.Scheme == "" {
			return storedFile{}, false
		}
		path = u.EscapedPath()
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if !strings.HasPrefix(path, "/storage/") {
		return storedFile{}, false
	}
	var segments []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	if len(segments) < 4 {
		return storedFile{}, false
	}
	access := AccessType(segments[2])
	if access != AccessPrivate && access != AccessPublic {
		return storedFile{}, false
	}
	return storedFile{storageID: segments[1], access: access, fileName: strings.Join(segments[3:], "/")}, true
}
