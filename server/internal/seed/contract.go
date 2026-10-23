package seed

import (
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

// Fixtures is the id map docs/api-v3-reference/contract-tests/hooks.py reads. Read is keyed by parameter name and applies to every operation without a more specific entry; Operations is keyed by operationId and wins over it. An identifier in neither keeps the value Schemathesis generated and gets the documented 403.
type Fixtures struct {
	WorkspaceID uuid.UUID                    `json:"workspaceId"`
	Read        map[string]uuid.UUID         `json:"read"`
	Operations  map[string]OperationFixtures `json:"operations"`
}

// OperationFixtures are one operation's identifiers by location.
type OperationFixtures struct {
	Path map[string]uuid.UUID `json:"path"`
}

// contractIDs are forma2's CONTRACT_IDS. Every mutating operation gets its own disposable resource: Schemathesis runs one case per operation in no fixed order, so a DELETE aimed at the survey a GET reads would make what the GET sees depend on the order.
var contractIDs = struct {
	surveyRead, surveyPatch, surveyDelete, surveyArchive, surveyRestore               string
	workflowPatch, workflowDelete, workflowDuplicate, workflowEnable, workflowDisable string
	workflowArchive, workflowUnarchive, workflowTest, actionClassRead                 string
}{
	surveyRead: "clctsurveyread0000000001", surveyPatch: "clctsurveypatch000000001", surveyDelete: "clctsurveydelete00000001",
	surveyArchive: "clctsurveyarchive0000001", surveyRestore: "clctsurveyrestore0000001",
	workflowPatch: "clctworkflowpatch0000001", workflowDelete: "clctworkflowdelete000001", workflowDuplicate: "clctworkflowduplicate001",
	workflowEnable: "clctworkflowenable000001", workflowDisable: "clctworkflowdisable00001", workflowArchive: "clctworkflowarchive00001",
	workflowUnarchive: "clctworkflowunarchive001", workflowTest: "clctworkflowtest00000001", actionClassRead: "clctactionclassread00001",
}

// readSurveyLanguages are the languages the `lang` examples of GET /api/v3/surveys/{surveyId} ask for. A language the survey does not configure answers a documented 400, so without them the operation only ever exercises its error path and the survey resource, the largest schema in the contract, is never validated.
var readSurveyLanguages = []string{"en-US", "de-DE", "pt-PT", "zh-Hans", "zh-Hans-CN"}

// contract is forma2's seed-contract-fixtures.ts: the disposable surveys, workflows and action class the contract tests read and mutate, and their id map.
func (s *seeder) contract() (*Fixtures, error) {
	id := legacyid.ID
	question := []map[string]any{{"id": "fixture", "type": "openText", "headline": text("Contract fixture question"), "required": false, "inputType": "text", "charLimit": map[string]any{"enabled": false}}}
	surveys := []struct {
		key, name string
		languages []string
		archived  bool
	}{
		{key: contractIDs.surveyRead, name: "Contract fixture — read", languages: readSurveyLanguages},
		{key: contractIDs.surveyPatch, name: "Contract fixture — patch"},
		{key: contractIDs.surveyDelete, name: "Contract fixture — delete"},
		{key: contractIDs.surveyArchive, name: "Contract fixture — archive"},
		// Restore only has something to do on an archived survey.
		{key: contractIDs.surveyRestore, name: "Contract fixture — restore", archived: true},
	}
	for _, sv := range surveys {
		if err := s.survey(id(sv.key), sv.key, sv.name, "inProgress", question, sv.languages); err != nil {
			return nil, err
		}
		if sv.archived {
			if err := s.exec("archived survey", `UPDATE surveys SET archived_at = now() WHERE id = ?`, id(sv.key)); err != nil {
				return nil, err
			}
		}
	}

	workflows := []struct{ key, name, status string }{
		{contractIDs.workflowPatch, "Contract fixture — patch", "draft"},
		{contractIDs.workflowDelete, "Contract fixture — delete", "draft"},
		{contractIDs.workflowDuplicate, "Contract fixture — duplicate", "draft"},
		// enable only takes a draft or disabled workflow; disable and archive need a live one.
		{contractIDs.workflowEnable, "Contract fixture — enable", "draft"},
		{contractIDs.workflowDisable, "Contract fixture — disable", "enabled"},
		{contractIDs.workflowArchive, "Contract fixture — archive", "enabled"},
		{contractIDs.workflowUnarchive, "Contract fixture — unarchive", "archived"},
		// test is a dry run, but it resolves the trigger and the recipient allowlist for real.
		{contractIDs.workflowTest, "Contract fixture — test", "draft"},
	}
	for _, w := range workflows {
		// The recipient is a workspace member, the seeded owner: enable and test refuse any other address (forma2's recipient allowlist), and would answer their documented error instead of the success body these fixtures exist for.
		def := workflowDefinition(w.key, KitchenSinkSurveyID, Users[0].Email, "Contract fixture", "Contract fixture body.")
		if err := s.workflow(id(w.key), w.name, "Disposable fixture for the v3 API contract tests.", w.status, def); err != nil {
			return nil, err
		}
	}

	// An empty list satisfies a list schema without validating an item, so every list endpoint gets at least one row.
	if err := s.exec("action class", `INSERT INTO action_classes (id, workspace_id, name, description, type, key) VALUES (?, ?, 'Contract fixture — action class', 'Disposable fixture for the v3 API contract tests.', 'code', 'contract-fixture-action')
		ON CONFLICT (id) DO NOTHING`, id(contractIDs.actionClassRead), WorkspaceID); err != nil {
		return nil, err
	}
	// No tag fixtures: every /api/v3/tags operation is session-only, so the API key is refused with the documented 401 before a handler looks for a row.

	path := func(name, key string) OperationFixtures {
		return OperationFixtures{Path: map[string]uuid.UUID{name: id(key)}}
	}
	return &Fixtures{
		WorkspaceID: WorkspaceID,
		Read: map[string]uuid.UUID{
			"surveyId":   id(contractIDs.surveyRead),
			"workflowId": FollowUpWorkflowID,
			"runId":      legacyid.ID("clseedwfrunfup01"),
		},
		Operations: map[string]OperationFixtures{
			"patchSurveyV3":       path("surveyId", contractIDs.surveyPatch),
			"deleteSurveyV3":      path("surveyId", contractIDs.surveyDelete),
			"archiveSurveyV3":     path("surveyId", contractIDs.surveyArchive),
			"restoreSurveyV3":     path("surveyId", contractIDs.surveyRestore),
			"patchWorkflowV3":     path("workflowId", contractIDs.workflowPatch),
			"deleteWorkflowV3":    path("workflowId", contractIDs.workflowDelete),
			"duplicateWorkflowV3": path("workflowId", contractIDs.workflowDuplicate),
			"enableWorkflowV3":    path("workflowId", contractIDs.workflowEnable),
			"disableWorkflowV3":   path("workflowId", contractIDs.workflowDisable),
			"archiveWorkflowV3":   path("workflowId", contractIDs.workflowArchive),
			"unarchiveWorkflowV3": path("workflowId", contractIDs.workflowUnarchive),
			"testWorkflowV3":      path("workflowId", contractIDs.workflowTest),
		},
	}, nil
}
