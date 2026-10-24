package analysis

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/apps/server/internal/storage"
	"github.com/yldm-tech/forma/apps/server/internal/storage/s3test"
	"github.com/yldm-tech/forma/apps/server/internal/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// The naming rules of forma2's attachment-zip-paths.ts.
func TestAttachmentZipPaths(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Front/Back":                  "Front-Back",
		"  Q3. Upload a photo  ":      "Q3. Upload a photo",
		`a:b*c?d"e<f>g|h`:             "abcdefgh",
		"...":                         "unnamed",
		"":                            "unnamed",
		"con":                         "_con",
		"LPT1":                        "_LPT1",
		"tab\tand\nnewline":           "tabandnewline",
		"many   spaces":               "many spaces",
		strings.Repeat("a", 70) + ".": strings.Repeat("a", 60),
		strings.Repeat("😀", 31):       strings.Repeat("😀", 30),
	} {
		if got := sanitizeSegment(in); got != want {
			t.Errorf("sanitizeSegment(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"photo.jpg":                       "photo.jpg",
		".env":                            "env",
		"report.final.pdf":                "report.final.pdf",
		"x.p-d_f":                         "x.pdf",
		"...jpg":                          "file.jpg",
		"nul.txt":                         "_nul.txt",
		"a.":                              "a",
		"":                                "file",
		strings.Repeat("b", 80) + ".jpeg": strings.Repeat("b", 60) + ".jpeg",
		"name." + strings.Repeat("x", 30): "name." + strings.Repeat("x", 20),
	} {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	used := map[string]bool{}
	id := uuid.MustParse("0192f5c8-0000-7000-8000-000000000001")
	at := time.Date(2026, 1, 5, 23, 30, 5, 0, time.UTC)
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	first := attachmentZipPath(id, at, 2, "Upload / photo", "photo.jpg", tokyo, used)
	second := attachmentZipPath(id, at, 2, "Upload / photo", "photo.jpg", tokyo, used)
	third := attachmentZipPath(id, at, 2, "Upload / photo", "photo.jpg", tokyo, used)
	if first != "2026-01-06T08-30-05_"+id.String()+"/2_Upload - photo/photo.jpg" || !strings.HasSuffix(second, "/photo (2).jpg") || !strings.HasSuffix(third, "/photo (3).jpg") {
		t.Errorf("paths = %s, %s, %s", first, second, third)
	}
	for in, want := range map[string]string{
		"/storage/ws/private/My%20Report--fid--0192f5c8-0000-7000-8000-000000000001.pdf": "My Report.pdf",
		"https://app.example.com/storage/ws/private/a--fid--x.png?sig=1":                 "a.png",
		"/storage/ws/private/plain.txt":                                                  "plain.txt",
		"/storage/ws/private/noext--fid--0192f5c8":                                       "noext",
		"/storage/ws/private/bad%--fid--x.pdf":                                           "",
		"not a url":                                                                      "",
	} {
		if got := originalFileName(in); got != want {
			t.Errorf("originalFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

// forma2's manifest.test.ts: the columns in order, ISO UTC timestamps, an empty size for a file never read, and formula triggers defanged.
func TestAttachmentManifest(t *testing.T) {
	t.Parallel()
	size := int64(12)
	id := uuid.MustParse("0192f5c8-0000-7000-8000-000000000001")
	at := time.Date(2026, 1, 5, 10, 0, 0, 123e6, time.FixedZone("x", 3600))
	var buf bytes.Buffer
	if err := writeManifest(&buf, []AttachmentEntry{
		{ZipPath: "a/b.pdf", ResponseID: id, ResponseCreatedAt: at, ElementID: "q1", ElementLabel: "=SUM(A1)", OriginalFileName: "b.pdf", Status: AttachmentOK, Bytes: &size},
		{ResponseID: id, ResponseCreatedAt: at, ElementID: "q1", ElementLabel: "Upload", OriginalFileName: "+x", Status: AttachmentForeignWorkspace},
	}); err != nil {
		t.Fatal(err)
	}
	want := `"filePath","responseId","responseCreatedAt","elementId","elementLabel","originalFileName","bytes","status"` + "\n" +
		`"a/b.pdf","` + id.String() + `","2026-01-05T09:00:00.123Z","q1","'=SUM(A1)","b.pdf",12,"ok"` + "\n" +
		`"","` + id.String() + `","2026-01-05T09:00:00.123Z","q1","Upload","'+x","","skipped_foreign_workspace"`
	if buf.String() != want {
		t.Fatalf("manifest =\n%s\nwant\n%s", buf.String(), want)
	}
}

// sessionFromHeader reads the test user as the session middleware would, for routes that authenticate per route.
type sessionFromHeader struct{}

func (sessionFromHeader) Require() gin.HandlerFunc { return headerSessions{}.Require() }

func (sessionFromHeader) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		if id, err := uuid.Parse(c.GetHeader(testUserHeader)); err == nil {
			httpapi.SetActor(c, httpapi.Actor{UserID: id})
		}
		c.Next()
	}
}

type attachmentsEnv struct {
	*env
	s3     *minio.Client
	bucket string
	logs   *strings.Builder
	router *gin.Engine
}

func newAttachmentsEnv(t *testing.T) *attachmentsEnv {
	e := newEnv(t)
	s3cfg := s3test.Config(t)
	cfg := e.cfg
	cfg.S3 = s3cfg
	cfg.RateLimitingDisabled = false
	a := &attachmentsEnv{env: e, s3: s3test.Client(t), bucket: s3cfg.BucketName, logs: &strings.Builder{}}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	bucket, err := storage.NewBucket(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	frozen := time.Now()
	limiter := ratelimit.New(cfg, valkeytest.Client(t), log).WithClock(func() time.Time { return frozen })
	exporter := NewAttachmentExporter(e.db, storage.NewService(bucket), workspace.NewRepository(e.db, log), log)
	middleware := v3api.NewMiddleware(cfg, apikey.NewAuthenticator(e.db, log), limiter)
	h := NewAttachmentsHandler(e.svc, exporter, middleware, v3api.NewAuditor(cfg, audit.New(cfg, log)), limiter, log)
	h.now = func() time.Time { return time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC) }
	a.router = httpserver.NewEngine(cfg, log)
	h.Register(a.router.Group("/api", v3api.ErrorHandler(log), authz.DecisionMemo(), sessionFromHeader{}.Optional()))
	return a
}

func (a *attachmentsEnv) put(key, body string) {
	a.t.Helper()
	if _, err := a.s3.PutObject(a.t.Context(), a.bucket, key, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
		a.t.Fatal(err)
	}
}

func (a *attachmentsEnv) get(user uuid.UUID, path string) *httptest.ResponseRecorder {
	a.t.Helper()
	a.authz.Deliver(a.t)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set(testUserHeader, user.String())
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec
}

const attachmentBlocks = `[{"id":"b1","name":"B","elements":[
	{"id":"q1","type":"openText","headline":{"default":"Name"},"required":false,"inputType":"text"},
	{"id":"files","type":"fileUpload","headline":{"default":"<p>Upload / your #recall:q1/fallback:x# files</p>"},"required":false},
	{"id":"more","type":"fileUpload","headline":{"default":""},"required":false}
]}]`

func TestAttachmentExport(t *testing.T) {
	a := newAttachmentsEnv(t)
	owner, outsider := a.user(), a.user()
	tz := "Asia/Tokyo"
	_, ws := a.organization(owner, &tz)
	otherOwner := a.user()
	_, foreignWS := a.organization(otherOwner, nil)
	sv := a.survey(ws, surveyOpts{blocks: attachmentBlocks})
	empty := a.survey(ws, surveyOpts{blocks: attachmentBlocks, name: "Empty"})
	base := "/api/surveys/" + sv.String() + "/attachments"

	a.put(ws.String()+"/private/surveys/s/report--fid--1.pdf", "pdf-bytes")
	a.put(ws.String()+"/private/surveys/s/photo--fid--2.jpg", "jpg")
	at := time.Date(2026, 1, 5, 23, 30, 5, 0, time.UTC)
	r1 := a.response(sv, row{at: at, finished: true, data: `{"files":["/storage/` + ws.String() + `/private/surveys/s/report--fid--1.pdf","/storage/` + foreignWS.String() + `/private/x--fid--3.pdf","https://evil.example/nope.pdf"],"more":["/storage/` + ws.String() + `/private/surveys/s/photo--fid--2.jpg","/storage/` + ws.String() + `/private/surveys/s/gone--fid--4.png"]}`})
	a.response(sv, row{at: at.Add(-time.Hour), finished: false, data: `{"q1":"no files","files":"/storage/` + ws.String() + `/private/single.pdf"}`})

	if rec := a.get(outsider, base+"?dryRun=true"); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider = %d %s", rec.Code, rec.Body)
	}
	rec := a.get(owner, base+"?dryRun=true")
	var counts struct{ Data AttachmentCounts }
	if err := json.Unmarshal(rec.Body.Bytes(), &counts); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("dry run = %d %s", rec.Code, rec.Body)
	}
	if counts.Data != (AttachmentCounts{FileCount: 3, ResponseCount: 1, ExceedsMaxFiles: false, MaxFiles: 5000}) {
		t.Fatalf("counts = %+v", counts.Data)
	}
	if rec := a.get(owner, "/api/surveys/"+empty.String()+"/attachments"); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "attachment_export_empty") {
		t.Fatalf("empty = %d %s", rec.Code, rec.Body)
	}
	if rec := a.get(owner, base+`?filters={"finished":true`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not valid JSON") || !strings.Contains(rec.Body.String(), `"name":"filters"`) {
		t.Fatalf("bad filter JSON = %d %s", rec.Code, rec.Body)
	}
	if rec := a.get(owner, base+"?dryRun=yes"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad dryRun = %d", rec.Code)
	}
	if rec := a.get(owner, base+"?"+url.Values{"filters": {`{"finished":false}`}, "dryRun": {"true"}}.Encode()); !strings.Contains(rec.Body.String(), `"fileCount":0`) {
		t.Fatalf("filtered dry run = %s", rec.Body)
	}

	rec = a.get(owner, base)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" || rec.Header().Get("Cache-Control") != "no-store" ||
		rec.Header().Get("Content-Disposition") != `attachment; filename="`+sv.String()+`-attachments-2026-03-04.zip"` {
		t.Fatalf("download = %d %v", rec.Code, rec.Header())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	folder := "2026-01-06T08-30-05_" + r1.String()
	files := map[string]string{}
	for _, f := range zr.File {
		if f.Method != zip.Store {
			t.Errorf("%s is compressed", f.Name)
		}
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		files[f.Name] = string(b)
	}
	if files[folder+"/2_Upload - your @Name files/report.pdf"] != "pdf-bytes" || files[folder+"/3_more/photo.jpg"] != "jpg" {
		t.Fatalf("archive = %v", keys(files))
	}
	if _, truncated := files["_TRUNCATED.txt"]; truncated || len(files) != 3 {
		t.Fatalf("archive = %v", keys(files))
	}
	rows, err := csv.NewReader(strings.NewReader(files["manifest.csv"])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, r := range rows[1:] {
		statuses[r[5]] = r[7] + ":" + r[6]
	}
	want := map[string]string{"report.pdf": "ok:9", "x.pdf": "skipped_foreign_workspace:", "nope.pdf": "skipped_invalid_url:", "photo.jpg": "ok:3", "gone.png": "missing_in_storage:"}
	for name, status := range want {
		if statuses[name] != status {
			t.Errorf("manifest %s = %q, want %q (%v)", name, statuses[name], status, rows)
		}
	}

	// Three downloads per ten minutes; the pre-flight is not charged.
	for _, path := range []string{base, base + "?dryRun=true", base} {
		if rec := a.get(owner, path); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d", path, rec.Code)
		}
	}
	rec = a.get(owner, base)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), "Too many attachment exports") {
		t.Fatalf("fourth download = %d %s", rec.Code, rec.Body)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
