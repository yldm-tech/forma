package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// workspaceItem is forma2's TV3WorkspaceListItem: never the whole workspace.
type workspaceItem struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	OrganizationID uuid.UUID `json:"organizationId"`
}

// listWorkspaces is forma2's listV3Workspaces, which had no HTTP route: the workspaces the caller may read, a user's within the organizations they belong to and a key's within its own organization, by name, then id, unpaginated.
func (h *Handler) listWorkspaces(ctx context.Context, p *principal) *sdk.CallToolResult {
	items, err := h.workspaces(ctx, p)
	if err != nil {
		return h.failed(ctx, p, err)
	}
	body, err := json.Marshal(map[string]any{"data": items, "meta": map[string]any{"nextCursor": nil, "totalCount": len(items)}})
	if err != nil {
		return h.failed(ctx, p, err)
	}
	return toolResult(http.StatusOK, http.Header{}, body, p.requestID)
}

func (h *Handler) workspaces(ctx context.Context, p *principal) ([]workspaceItem, error) {
	ids, err := h.authz.LookupWorkspaceIDs(ctx, p.actor, false)
	if err != nil {
		return nil, err
	}
	items := []workspaceItem{}
	if len(ids) == 0 {
		return items, nil
	}
	query := h.db.WithContext(ctx).Table("workspaces").Select("id, name, organization_id").Where("id IN ?", ids)
	if p.apiKey != nil {
		query = query.Where("organization_id = ?", p.apiKey.OrganizationID)
	} else {
		query = query.Where("organization_id IN (SELECT organization_id FROM memberships WHERE user_id = ?)", p.userID)
	}
	if err := query.Scan(&items).Error; err != nil {
		return nil, fmt.Errorf("load workspaces: %w", err)
	}
	collator := collate.New(language.Und)
	slices.SortFunc(items, func(a, b workspaceItem) int {
		if c := collator.CompareString(a.Name, b.Name); c != 0 {
			return c
		}
		return slices.Compare([]byte(a.ID.String()), []byte(b.ID.String()))
	})
	return slices.CompactFunc(items, func(a, b workspaceItem) bool { return a.ID == b.ID }), nil
}
