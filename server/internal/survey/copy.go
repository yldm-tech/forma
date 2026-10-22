package survey

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// defaultRecord is a new survey's columns before anything is set: the database defaults forma2's Prisma create fell back to.
func defaultRecord() record {
	add := "add"
	return record{
		Type: TypeLink, Status: StatusDraft,
		WelcomeCard: json.RawMessage(`{"enabled": false}`), Blocks: json.RawMessage(`[]`), Endings: json.RawMessage(`[]`),
		HiddenFields: json.RawMessage(`{"enabled": false}`), Variables: json.RawMessage(`[]`), Metadata: json.RawMessage(`{}`),
		SingleUse: json.RawMessage(`{"enabled": false, "isEncrypted": true}`), Recaptcha: json.RawMessage(`{"enabled": false, "threshold": 0.1}`),
		DisplayOption: "displayOnce", CustomHeadScriptsMode: &add,
	}
}

// userLocale is the dashboard language of a user, en-US when unknown.
func (r *Repository) userLocale(ctx context.Context, userID uuid.UUID) (string, error) {
	var locales []string
	if err := r.db.WithContext(ctx).Raw(`SELECT locale FROM users WHERE id = ?`, userID).Scan(&locales).Error; err != nil {
		return "", fmt.Errorf("load user locale: %w", err)
	}
	if len(locales) == 0 {
		return defaultLocale, nil
	}
	return locales[0], nil
}

// Copy is forma2's copySurveyToOtherWorkspaceAction; duplicating is copying into the survey's own workspace. The caller needs workspace.write on both workspaces, which must be in one organization. The copy is a draft named "<name> (copy)" that keeps the document, languages, triggers, segment, follow-ups and quotas, and leaves everything else (PIN, slug, schedule, display settings beyond the basics) at its defaults.
func (s *Service) Copy(ctx context.Context, caller Caller, surveyID, targetWorkspaceID uuid.UUID) (*Survey, error) {
	source, err := s.authorized(ctx, caller, surveyID, authz.SurveyWrite)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, caller.Actor, authz.WorkspaceWrite, authz.Workspace(targetWorkspaceID)); err != nil {
		return nil, err
	}
	sourceOrg, err := s.repo.organizationOf(ctx, source.WorkspaceID)
	if err != nil {
		return nil, err
	}
	targetOrg, err := s.repo.organizationOf(ctx, targetWorkspaceID)
	if err != nil {
		return nil, err
	}
	if targetOrg == nil {
		return nil, apperr.NotFound("Workspace", targetWorkspaceID.String())
	}
	if sourceOrg == nil || *sourceOrg != *targetOrg {
		return nil, apperr.OperationNotAllowed("Source and target workspaces must be in the same organization")
	}
	if err := s.stateMutation(ctx, targetWorkspaceID); err != nil {
		return nil, err
	}
	copied, err := s.copy(ctx, caller, source, targetWorkspaceID, *targetOrg)
	id := ""
	if copied != nil {
		id = copied.ID.String()
	}
	s.record(ctx, caller, audit.ActionCopiedToOtherWorkspace, audit.TargetSurvey, id, targetOrg, err, nil, copied)
	return copied, err
}

func (s *Service) copy(ctx context.Context, caller Caller, source *Survey, targetWorkspaceID, organizationID uuid.UUID) (*Survey, error) {
	locale := defaultLocale
	if user := caller.userID(); user != nil {
		l, err := s.repo.userLocale(ctx, *user)
		if err != nil {
			return nil, err
		}
		locale = l
	}
	var blocks []surveydoc.Block
	if err := json.Unmarshal(source.Blocks, &blocks); err == nil {
		if m := surveydoc.MediaIssue(blocks); m != "" {
			return nil, apperr.InvalidInput(apperr.CodeBadRequest, m)
		}
	}
	quotas, err := s.repo.quotas(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	sameWorkspace := source.WorkspaceID == targetWorkspaceID
	newID := uuid.Must(uuid.NewV7())

	rec := defaultRecord()
	rec.Name = source.Name + " " + s.catalog.copySuffix(locale, 1)
	rec.Type, rec.Status, rec.CreatedBy = source.Type, StatusDraft, caller.userID()
	rec.WelcomeCard, rec.Blocks, rec.Endings, rec.Variables, rec.HiddenFields = source.WelcomeCard, source.Blocks, source.Endings, source.Variables, source.HiddenFields
	rec.SurveyClosedMessage, rec.SingleUse, rec.WorkspaceOverwrites, rec.Styling = source.SurveyClosedMessage, source.SingleUse, source.WorkspaceOverwrites, source.Styling
	rec.DisplayOption, rec.RecontactDays, rec.DisplayLimit = source.DisplayOption, source.RecontactDays, source.DisplayLimit

	err = s.repo.transaction(ctx, func(tx *Repository) error {
		if err := tx.insert(ctx, newID, targetWorkspaceID, rec); err != nil {
			return err
		}
		if err := tx.copyLanguages(ctx, newID, targetWorkspaceID, source.Languages); err != nil {
			return err
		}
		triggerIDs, err := tx.copyTriggers(ctx, source, targetWorkspaceID, sameWorkspace, func(n int) string { return s.catalog.copySuffix(locale, n) })
		if err != nil {
			return err
		}
		if err := tx.setTriggers(ctx, newID, triggerIDs); err != nil {
			return err
		}
		if err := tx.copySegment(ctx, newID, source.Segment, targetWorkspaceID, sameWorkspace, s.now()); err != nil {
			return err
		}
		for _, f := range source.FollowUps {
			if err := tx.applyFollowUps(ctx, newID, []followUpWrite{{ID: uuid.Must(uuid.NewV7()), Name: f.Name, Trigger: f.Trigger, Action: f.Action}}); err != nil {
				return err
			}
		}
		for _, q := range quotas {
			if _, err := tx.insertQuota(ctx, newID, quotaWrite{Name: q.Name, Limit: q.Limit, Logic: q.Logic, Action: q.Action, EndingCardID: q.EndingCardID, CountPartialSubmissions: q.CountPartialSubmissions}); err != nil {
				return err
			}
		}
		doc, err := declared(source.Variables, source.HiddenFields)
		if err != nil {
			return err
		}
		if err := tx.reconcileEmbeddedData(ctx, newID, targetWorkspaceID, embeddedPatch{Variables: doc.Variables, HiddenFields: doc.HiddenFields}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	created, err := s.repo.byID(ctx, newID)
	if err != nil {
		return nil, err
	}
	if rec.CreatedBy != nil {
		if err := s.subscribeCreator(ctx, newID, *rec.CreatedBy, organizationID); err != nil {
			return nil, err
		}
	}
	return created, nil
}

// copyLanguages links the copy to the target workspace's languages of the same codes, creating the ones it lacks with the source's alias.
func (r *Repository) copyLanguages(ctx context.Context, surveyID, workspaceID uuid.UUID, languages []SurveyLanguage) error {
	links := make([]languageLink, 0, len(languages))
	for _, l := range languages {
		var id uuid.UUID
		err := r.db.WithContext(ctx).Raw(`INSERT INTO languages (workspace_id, code, alias) VALUES (?, ?, ?)
			ON CONFLICT (workspace_id, code) DO UPDATE SET code = EXCLUDED.code RETURNING id`, workspaceID, l.Language.Code, l.Language.Alias).Row().Scan(&id)
		if err != nil {
			return fmt.Errorf("copy survey language: %w", err)
		}
		links = append(links, languageLink{LanguageID: id, Default: l.Default, Enabled: l.Enabled})
	}
	return r.setLanguages(ctx, surveyID, links)
}

// copyTriggers resolves the source's triggers in the target workspace, forma2's rules: the same workspace keeps the same action classes; elsewhere a code action is matched by key and a no-code action by its configuration, and an unmatched one is created, under "<name> (copy)", "(copy 2)", ... when its name is taken.
func (r *Repository) copyTriggers(ctx context.Context, source *Survey, targetWorkspaceID uuid.UUID, sameWorkspace bool, suffix func(int) string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(source.Triggers))
	if sameWorkspace {
		for _, t := range source.Triggers {
			ids = append(ids, t.ActionClass.ID)
		}
		return ids, nil
	}
	existing, err := r.actionClasses(ctx, targetWorkspaceID)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, a := range existing {
		names[a.Name] = true
	}
	for _, t := range source.Triggers {
		ac := t.ActionClass
		if id, ok := matchingActionClass(existing, ac); ok {
			ids = append(ids, id)
			continue
		}
		name := ac.Name
		conflict := names[ac.Name]
		if conflict {
			n := 1
			name = ac.Name + " " + suffix(n)
			for names[name] {
				n++
				name = ac.Name + " " + suffix(n)
			}
		}
		var id uuid.UUID
		switch {
		case ac.Type == "code":
			err = r.db.WithContext(ctx).Raw(`INSERT INTO action_classes (workspace_id, name, description, type, key) VALUES (?, ?, ?, 'code', ?)
				ON CONFLICT (workspace_id, key) DO UPDATE SET key = EXCLUDED.key RETURNING id`, targetWorkspaceID, name, ac.Description, ac.Key).Row().Scan(&id)
		case conflict:
			err = r.db.WithContext(ctx).Raw(`INSERT INTO action_classes (workspace_id, name, description, type, no_code_config) VALUES (?, ?, ?, 'noCode', ?::jsonb) RETURNING id`,
				targetWorkspaceID, name, ac.Description, jsonParam(ac.NoCodeConfig)).Row().Scan(&id)
		default:
			err = r.db.WithContext(ctx).Raw(`INSERT INTO action_classes (workspace_id, name, description, type, no_code_config) VALUES (?, ?, ?, 'noCode', ?::jsonb)
				ON CONFLICT (workspace_id, name) DO UPDATE SET name = EXCLUDED.name RETURNING id`, targetWorkspaceID, name, ac.Description, jsonParam(ac.NoCodeConfig)).Row().Scan(&id)
		}
		if err != nil {
			return nil, fmt.Errorf("copy trigger action class: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// matchingActionClass is the target action class a trigger reuses: the same key for a code action, an identical configuration for a no-code one.
func matchingActionClass(existing []ActionClass, ac ActionClass) (uuid.UUID, bool) {
	for _, e := range existing {
		switch ac.Type {
		case "code":
			if ac.Key != nil && e.Key != nil && *e.Key == *ac.Key {
				return e.ID, true
			}
		case "noCode":
			if sameJSON(e.NoCodeConfig, ac.NoCodeConfig) {
				return e.ID, true
			}
		}
	}
	return uuid.Nil, false
}

// copySegment gives the copy forma2's segment: a private segment becomes a new private one for the copy, a public one is shared within the workspace and recreated (renamed on a clash) in another.
func (r *Repository) copySegment(ctx context.Context, surveyID uuid.UUID, seg *Segment, workspaceID uuid.UUID, sameWorkspace bool, now time.Time) error {
	if seg == nil {
		return nil
	}
	var id uuid.UUID
	var err error
	switch {
	case seg.IsPrivate:
		id, err = r.createSegment(ctx, workspaceID, surveyID.String(), nil, true, seg.Filters)
	case sameWorkspace:
		id = seg.ID
	default:
		title := seg.Title
		var taken bool
		if taken, err = r.segmentTitleExists(ctx, workspaceID, title); err != nil {
			return err
		}
		if taken {
			title = seg.Title + "-" + strconv.FormatInt(now.UnixMilli(), 10)
		}
		id, err = r.createSegment(ctx, workspaceID, title, nil, false, seg.Filters)
	}
	if err != nil {
		return err
	}
	return r.setSurveySegment(ctx, surveyID, &id)
}
