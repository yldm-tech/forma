package survey

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

func TestDecodeCursor(t *testing.T) {
	id := uuid.New()
	date := encodeCursor(dateOrNameCursor{cursorVersion, SortUpdatedAt, "2026-01-02T03:04:05.123456Z", id.String()})
	c, err := DecodeCursor(*date, SortUpdatedAt)
	if err != nil || c.ID != id || c.At.Nanosecond() != 123456000 {
		t.Fatalf("DecodeCursor = %+v, %v", c, err)
	}
	reason := func(err error) string {
		e, _ := apperr.As(err)
		if e == nil {
			return ""
		}
		return e.Message
	}
	if _, err := DecodeCursor(*date, SortName); reason(err) != "The cursor does not match the requested sort order." {
		t.Errorf("sort mismatch: %v", err)
	}
	for _, bad := range []string{"%%%", base64.RawURLEncoding.EncodeToString([]byte(`{"version":2,"sortBy":"name","value":"a","id":"` + id.String() + `"}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"version":1,"sortBy":"createdAt","value":"yesterday","id":"` + id.String() + `"}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"version":1,"sortBy":"relevance","bucket":"later","updatedAt":"2026-01-02T03:04:05Z","id":"` + id.String() + `"}`)),
	} {
		if _, err := DecodeCursor(bad, SortName); reason(err) != "The cursor is invalid." {
			t.Errorf("DecodeCursor(%q) = %v", bad, err)
		}
	}
}

func TestParseListQuery(t *testing.T) {
	ws := uuid.New()
	parse := func(raw string) (*ListQuery, string) {
		q, _ := url.ParseQuery(raw)
		out, p := ParseListQuery(q)
		if p != nil {
			return nil, p.InvalidParams[0].Name + ": " + p.InvalidParams[0].Reason
		}
		return out, ""
	}
	q, msg := parse("workspaceId=" + ws.String() + "&filter[status][in]=draft,inProgress&filter[status][in]=draft&filter[status][in]=archived&filter[type][in]=app&filter[name][contains]=%20Feed%20&includeTotalCount=false&sortBy=relevance&limit=5")
	if msg != "" || q.WorkspaceID != ws || q.Limit != 5 || q.IncludeTotalCount || q.SortBy != SortRelevance || q.Filter.Name != "Feed" ||
		len(q.Filter.Statuses) != 2 || !q.Filter.IncludeArchived || len(q.Filter.Types) != 1 {
		t.Fatalf("parsed %+v %q", q, msg)
	}
	cases := map[string]string{
		"workspaceId=" + ws.String() + "&page=2": "page: Unsupported query parameter. Use only workspaceId, limit, cursor, includeTotalCount, filter[name][contains], filter[status][in], filter[type][in], sortBy.",
		"":                                       "workspaceId: Invalid input: expected string, received null",
		"workspaceId=" + ws.String() + "&limit=251":            "limit: Too big: expected number to be <=250",
		"workspaceId=" + ws.String() + "&limit=0":              "limit: Too small: expected number to be >=1",
		"workspaceId=" + ws.String() + "&filter[type][in]=web": `filter[type][in].0: Invalid option: expected one of "link"|"app"`,
		"workspaceId=" + ws.String() + "&sortBy=oldest":        `sortBy: Invalid option: expected one of "createdAt"|"updatedAt"|"name"|"relevance"`,
		"workspaceId=" + ws.String() + "&cursor=abc":           "cursor: The cursor is invalid.",
	}
	for raw, want := range cases {
		if _, msg := parse(raw); msg != want {
			t.Errorf("%q: %q, want %q", raw, msg, want)
		}
	}
}

func TestListPaging(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	names := []string{"delta", "alpha", "charlie", "bravo", "echo"}
	statuses := []string{StatusDraft, StatusInProgress, StatusCompleted, StatusInProgress, StatusPaused}
	for i, n := range names {
		e.exec(`INSERT INTO surveys (workspace_id, name, status, type, created_at, updated_at) VALUES (?, ?, ?, 'link', ?, ?)`, ws, n, statuses[i], base.Add(time.Duration(i)*time.Hour), base.Add(time.Duration(i)*time.Minute))
	}
	e.exec(`INSERT INTO surveys (workspace_id, name, type) VALUES (?, 'zulu app', 'app')`, ws)

	walk := func(query string) []string {
		var out []string
		cursor := ""
		for {
			path := surveysPath("?workspaceId=" + ws.String() + "&limit=2" + query)
			if cursor != "" {
				path += "&cursor=" + url.QueryEscape(cursor)
			}
			var page struct {
				Data []map[string]any `json:"data"`
				Meta map[string]any   `json:"meta"`
			}
			e.ok(e.v3(owner, http.MethodGet, path, nil), http.StatusOK, &page)
			for _, s := range page.Data {
				out = append(out, s["name"].(string))
			}
			next, _ := page.Meta["nextCursor"].(string)
			if next == "" {
				return out
			}
			cursor = next
		}
	}
	expect := func(query string, want ...string) {
		t.Helper()
		if got := walk(query); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s: %v, want %v", query, got, want)
		}
	}
	expect("&sortBy=name&filter[type][in]=link", "alpha", "bravo", "charlie", "delta", "echo")
	expect("&sortBy=createdAt&filter[type][in]=link", "echo", "bravo", "charlie", "alpha", "delta")
	expect("&filter[type][in]=link", "echo", "bravo", "charlie", "alpha", "delta")
	expect("&sortBy=relevance&filter[type][in]=link", "bravo", "alpha", "echo", "charlie", "delta")
	expect("&sortBy=name&filter[name][contains]=LT", "delta")
	expect("&sortBy=name&filter[status][in]=inProgress,paused", "alpha", "bravo", "echo")
	expect("&sortBy=name&filter[type][in]=app", "zulu app")

	p := e.problem(e.v3(owner, http.MethodGet, surveysPath("?workspaceId="+ws.String()+"&sortBy=name&cursor="+url.QueryEscape(*encodeCursor(relevanceCursor{cursorVersion, SortRelevance, bucketOther, "2026-01-01T00:00:00Z", uuid.NewString()}))), nil), http.StatusBadRequest, "Invalid query parameters")
	if p.param(t, "cursor").Reason != "The cursor does not match the requested sort order." {
		t.Fatalf("%+v", p)
	}
}
