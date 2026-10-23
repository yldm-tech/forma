package survey

import (
	"context"
	"encoding/json"
	"slices"
	"unicode/utf16"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
)

// The survey module's product events, forma2's (apps/web/app/api/v3/surveys, apps/web/modules/survey/editor). They describe a survey's shape (counts, types, ids), never its text.

// captureCreated is forma2's survey_created, sent for a signed-in user creating from the editor, which says how it started: blank, template, xm-template or ai.
func (s *Service) captureCreated(ctx context.Context, caller Caller, sv *Survey, createdFrom string) {
	user := caller.userID()
	if user == nil || createdFrom == "" || !analytics.Enabled() {
		return
	}
	org := idString(s.organizationOf(ctx, sv.WorkspaceID))
	analytics.Capture(user.String(), "survey_created", analytics.Properties{
		"survey_id":       sv.ID.String(),
		"survey_type":     sv.Type,
		"organization_id": org,
		"workspace_id":    sv.WorkspaceID.String(),
		"question_count":  len(questionIDs(sv.Blocks)),
		"created_from":    createdFrom,
	}, analytics.Groups{OrganizationID: org, WorkspaceID: sv.WorkspaceID.String()})
}

// captureEditorSave is forma2's events of an editor save: what the save added (hidden fields, block logic, variables, languages, follow-ups), then survey_published for a draft that went live and survey_updated for any survey that is not a draft.
func captureEditorSave(caller Caller, organizationID *uuid.UUID, old, updated *Survey) {
	user := caller.userID()
	if user == nil || old == nil || !analytics.Enabled() {
		return
	}
	userID, org, ws := user.String(), idString(organizationID), updated.WorkspaceID.String()
	groups := analytics.Groups{OrganizationID: org, WorkspaceID: ws}
	base := func(extra analytics.Properties) analytics.Properties {
		p := analytics.Properties{"organization_id": org, "workspace_id": ws, "survey_id": updated.ID.String()}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}

	oldFields, newFields := hiddenFieldIDs(old.HiddenFields), hiddenFieldIDs(updated.HiddenFields)
	if slices.ContainsFunc(newFields, func(id string) bool { return !slices.Contains(oldFields, id) }) {
		analytics.Capture(userID, "hidden_field_added", base(analytics.Properties{"field_count": len(newFields)}), groups)
	}

	oldLogic := map[string]int{}
	for _, b := range blockLogic(old.Blocks) {
		oldLogic[b.id] = b.rules
	}
	for _, b := range blockLogic(updated.Blocks) {
		if b.rules > oldLogic[b.id] {
			analytics.Capture(userID, "conditional_logic_added", base(analytics.Properties{"question_id": b.id}), groups)
		}
	}

	oldVariables := map[string]bool{}
	for _, v := range variables(old.Variables) {
		oldVariables[v.ID] = true
	}
	for _, v := range variables(updated.Variables) {
		if !oldVariables[v.ID] {
			analytics.Capture(userID, "variable_created", base(analytics.Properties{"variable_type": v.Type}), groups)
		}
	}

	oldCodes := map[string]bool{}
	for _, l := range old.Languages {
		oldCodes[l.Language.Code] = true
	}
	count := len(old.Languages)
	// The first language added to a single-language survey makes it multilingual; the rest are additions.
	enabling := count <= 1
	for _, l := range updated.Languages {
		if oldCodes[l.Language.Code] {
			continue
		}
		event := "survey_language_added"
		if enabling {
			event, enabling = "survey_language_enabled", false
		}
		analytics.Capture(userID, event, base(analytics.Properties{"language_code": l.Language.Code, "existing_language_count": count}), groups)
		count++
	}

	oldFollowUps := map[string]bool{}
	for _, f := range old.FollowUps {
		oldFollowUps[f.ID.String()] = true
	}
	for _, f := range updated.FollowUps {
		if !oldFollowUps[f.ID.String()] {
			analytics.Capture(userID, "follow_up_added", base(analytics.Properties{"follow_up_id": f.ID.String()}), groups)
		}
	}

	if updated.Status == StatusDraft {
		return
	}
	props := analytics.Properties{
		"survey_id":       updated.ID.String(),
		"survey_type":     updated.Type,
		"question_count":  len(questionIDs(updated.Blocks)),
		"organization_id": org,
		"workspace_id":    ws,
		"has_targeting":   updated.Segment != nil && !updated.Segment.IsPrivate,
		"language_count":  len(updated.Languages),
	}
	if old.Status == StatusDraft && updated.Status == StatusInProgress {
		analytics.Capture(userID, "survey_published", props, groups)
	}
	analytics.Capture(userID, "survey_updated", props, groups)
}

func idString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func hiddenFieldIDs(raw json.RawMessage) []string {
	var h struct {
		FieldIDs []string `json:"fieldIds"`
	}
	_ = json.Unmarshal(raw, &h)
	return h.FieldIDs
}

type blockRules struct {
	id    string
	rules int
}

// blockLogic is each block's logic rules, its fallback counting as one.
func blockLogic(raw json.RawMessage) []blockRules {
	var blocks []struct {
		ID            string            `json:"id"`
		Logic         []json.RawMessage `json:"logic"`
		LogicFallback *string           `json:"logicFallback"`
	}
	_ = json.Unmarshal(raw, &blocks)
	out := make([]blockRules, len(blocks))
	for i, b := range blocks {
		out[i] = blockRules{id: b.ID, rules: len(b.Logic)}
		if b.LogicFallback != nil && *b.LogicFallback != "" {
			out[i].rules++
		}
	}
	return out
}

type variable struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func variables(raw json.RawMessage) []variable {
	var vs []variable
	_ = json.Unmarshal(raw, &vs)
	return vs
}

// questionIDs are the ids of every element of every block, forma2's getElementsFromBlocks.
func questionIDs(raw json.RawMessage) []string {
	var blocks []struct {
		Elements []struct {
			ID string `json:"id"`
		} `json:"elements"`
	}
	_ = json.Unmarshal(raw, &blocks)
	var ids []string
	for _, b := range blocks {
		for _, e := range b.Elements {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

// captureGenerated is forma2's ai_survey_generated for a signed-in user: how long the prompt was, never what it said.
func captureGenerated(caller Caller, organizationID uuid.UUID, in *GenerateInput, streamed bool) {
	user := caller.userID()
	if user == nil {
		return
	}
	props := analytics.Properties{"prompt_length": len(utf16.Encode([]rune(in.Prompt)))}
	if streamed {
		props["streamed"] = true
	}
	analytics.Capture(user.String(), "ai_survey_generated", props, analytics.Groups{OrganizationID: organizationID.String(), WorkspaceID: in.WorkspaceID.String()})
}
