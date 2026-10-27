package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// Read-only results links (the survey_result_shares table): a survey's summary, under a filter frozen when the link was created, published to people without an account. Whoever may publish the survey lists, creates, changes and revokes its links; anyone holding a link's key reads the summary and the trend through it, rate limited per client address, until the link expires or is revoked.
//
// A link never shows who answered: contacts, their attributes and response ids are dropped from every card. Cards that list individual answers (revealableTypes, the values typed into an Other option, and hidden fields) show only their counts unless the link reveals them, and uploaded files are never shown.
//
// The survey is the authority on what a link reveals. A link stores each card it reveals as the survey offered it when someone chose it (its id, kind and type), and a stored card counts only while the survey still offers one exactly like it: a card removed from the survey, or replaced by a different card under the same id, is dropped from the link the next time the link is listed, read or saved, and is never revealed meanwhile.

// maxResultShares bounds the links one survey keeps, so the list stays one a person can review.
const maxResultShares = 50

// maxResultShareNameLength is the longest name of a link, in characters, as the table's CHECK bounds it.
const maxResultShareNameLength = 100

// CodeResultShareNotFound is the error a key answers when it names no link, or one that expired or was revoked: the three are not told apart.
const CodeResultShareNotFound = "result_share_not_found"

// CodeResultShareSegmentNotFound is the error a new link answers when its filter names a segment the survey's workspace does not have: a segment since deleted, another workspace's, or no segment id at all. Such a segment holds nobody, so the link would silently show no responses (or all of them).
const CodeResultShareSegmentNotFound = "result_share_segment_not_found"

// shareKeyPattern is the shape of a key secret.NewToken issues; anything else names no link and is refused before the database is asked.
var shareKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// Kinds of the cards a link can reveal: an element listing individual answers, a choice element's Other values, and a hidden field.
const (
	RevealKindAnswers     = "answers"
	RevealKindOtherValues = "otherValues"
	RevealKindHiddenField = "hiddenField"
)

// revealableTypes are the element types whose cards list individual answers, which a link shows only when it reveals them. File uploads are not among them: their files are never shown through a link.
var revealableTypes = []string{surveydoc.ElementOpenText, surveydoc.ElementDate, surveydoc.ElementAddress, surveydoc.ElementContactInfo}

// ResultShareCreator is who created a link, while their account exists.
type ResultShareCreator struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ResultShare is a survey's read-only results link as the people who manage it see it, the key included so they can copy the link again.
type ResultShare struct {
	ID                 uuid.UUID           `json:"id"`
	CreatedAt          time.Time           `json:"createdAt"`
	UpdatedAt          time.Time           `json:"updatedAt"`
	SurveyID           uuid.UUID           `json:"surveyId"`
	Key                string              `json:"key"`
	Name               string              `json:"name"`
	FilterCriteria     json.RawMessage     `json:"filterCriteria"`
	RevealedElementIDs []string            `json:"revealedElementIds"`
	ExpiresAt          *time.Time          `json:"expiresAt"`
	Creator            *ResultShareCreator `json:"creator"`
	// MissingSegmentIDs are the segments the frozen filter names that no longer exist, sorted: the link counts nobody as in them, so it no longer shows the responses it was created for.
	MissingSegmentIDs []string `json:"missingSegmentIds"`
	// revealed are the cards the link reveals, as stored: the survey may since have dropped some (keepOffered).
	revealed []Revealable
}

// Revealable is a card of the survey that lists individual answers, which a link hides unless it reveals the card's id.
type Revealable struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Type is the element's type; empty for a hidden field.
	Type string `json:"type,omitempty"`
}

// ResultShares is a survey's links, newest first, with the cards a link can reveal.
type ResultShares struct {
	Shares     []ResultShare `json:"shares"`
	Revealable []Revealable  `json:"revealable"`
}

// ResultShareInput is a new link: its name, the filter it freezes, the cards it reveals and when it expires.
type ResultShareInput struct {
	Name               string
	FilterCriteria     *response.Criteria
	RevealedElementIDs []string
	ExpiresAt          *time.Time
}

// ResultShareUpdate is what a link's managers may change after creating it; the filter stays frozen.
type ResultShareUpdate struct {
	Name               string
	RevealedElementIDs []string
	ExpiresAt          *time.Time
}

// SharedSurvey is what a link shows of the survey: its name and type, and the parts of its document the summary cards read for headlines, choices and recall. Nothing about how it is distributed or who manages it.
type SharedSurvey struct {
	ID           uuid.UUID       `json:"id"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Blocks       json.RawMessage `json:"blocks"`
	Endings      json.RawMessage `json:"endings"`
	HiddenFields json.RawMessage `json:"hiddenFields"`
	Variables    json.RawMessage `json:"variables"`
}

// SharedResultsLink is what a link says about itself to its readers.
type SharedResultsLink struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expiresAt"`
	// Filtered is set when the link shows only the responses matching a filter.
	Filtered bool `json:"filtered"`
	// SegmentMissing is set when the filter names a segment that no longer exists, so the responses shown are not the ones the link was created for.
	SegmentMissing bool `json:"segmentMissing"`
}

// SharedResults is the summary a link shows. WithheldIDs are the cards, by element or hidden field id, whose individual answers the link does not show: their entries keep their counts and have no samples.
type SharedResults struct {
	Link        SharedResultsLink `json:"link"`
	Survey      SharedSurvey      `json:"survey"`
	Summary     *Summary          `json:"summary"`
	WithheldIDs []string          `json:"withheldIds"`
}

// resultShareRow is a survey_result_shares row with its creator.
type resultShareRow struct {
	ID             uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
	SurveyID       uuid.UUID
	ShareKey       string
	Name           string
	FilterCriteria []byte
	Revealed       []byte
	ExpiresAt      *time.Time
	CreatorID      *uuid.UUID
	CreatorName    *string
}

const selectResultShare = `SELECT s.id, s.created_at, s.updated_at, s.survey_id, s.share_key, s.name, s.filter_criteria,
	s.revealed, s.expires_at, u.id AS creator_id, u.name AS creator_name
	FROM survey_result_shares s LEFT JOIN users u ON u.id = s.created_by `

func (r resultShareRow) share() (ResultShare, error) {
	out := ResultShare{ID: r.ID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, SurveyID: r.SurveyID, Key: r.ShareKey, Name: r.Name,
		FilterCriteria: r.FilterCriteria, ExpiresAt: r.ExpiresAt, MissingSegmentIDs: []string{}}
	if err := json.Unmarshal(r.Revealed, &out.revealed); err != nil {
		return out, fmt.Errorf("decode result share %s revealed cards: %w", r.ID, err)
	}
	out.RevealedElementIDs = revealedIDs(out.revealed)
	if r.CreatorID != nil {
		out.Creator = &ResultShareCreator{ID: *r.CreatorID}
		if r.CreatorName != nil {
			out.Creator.Name = *r.CreatorName
		}
	}
	return out, nil
}

func (s *Service) resultShares(ctx context.Context, where string, args ...any) ([]ResultShare, error) {
	var rows []resultShareRow
	if err := s.db.WithContext(ctx).Raw(selectResultShare+where, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load result shares: %w", err)
	}
	out := make([]ResultShare, 0, len(rows))
	for _, r := range rows {
		share, err := r.share()
		if err != nil {
			return nil, err
		}
		out = append(out, share)
	}
	return out, nil
}

// revealedIDs are the ids of the cards, each once, in their order.
func revealedIDs(cards []Revealable) []string {
	out := []string{}
	for _, c := range cards {
		if !slices.Contains(out, c.ID) {
			out = append(out, c.ID)
		}
	}
	return out
}

// reveals tells whether cards include the card with id of the kind given.
func reveals(cards []Revealable, id, kind string) bool {
	return slices.ContainsFunc(cards, func(c Revealable) bool { return c.ID == id && c.Kind == kind })
}

// keepOffered drops from the link the cards the survey no longer offers as they were revealed (same id, kind and type) and reports whether it dropped any.
func (sh *ResultShare) keepOffered(sv *surveyDoc) bool {
	offered := sv.revealable()
	kept := []Revealable{}
	for _, c := range sh.revealed {
		if slices.Contains(offered, c) {
			kept = append(kept, c)
		}
	}
	changed := len(kept) != len(sh.revealed)
	sh.revealed, sh.RevealedElementIDs = kept, revealedIDs(kept)
	return changed
}

// pruneReveals is keepOffered, with the cards dropped removed from the stored link too, so a card added back to the survey later under the same id is not revealed without anyone choosing it. The write applies only while the link still reveals what was read, so it never undoes a concurrent change.
func (s *Service) pruneReveals(ctx context.Context, sh *ResultShare, sv *surveyDoc) error {
	stored, err := json.Marshal(sh.revealed)
	if err != nil {
		return fmt.Errorf("encode result share revealed cards: %w", err)
	}
	if !sh.keepOffered(sv) {
		return nil
	}
	kept, err := json.Marshal(sh.revealed)
	if err != nil {
		return fmt.Errorf("encode result share revealed cards: %w", err)
	}
	if err := s.db.WithContext(ctx).Exec(`UPDATE survey_result_shares SET revealed = ?::jsonb WHERE id = ? AND revealed = ?::jsonb`,
		string(kept), sh.ID, string(stored)).Error; err != nil {
		return fmt.Errorf("prune result share revealed cards: %w", err)
	}
	return nil
}

// revealable lists the survey's cards that show individual answers, in the summary's order: elements first, then hidden fields.
func (sv *surveyDoc) revealable() []Revealable {
	out := []Revealable{}
	for _, el := range sv.Elements {
		switch {
		case slices.Contains(revealableTypes, el.Type):
			out = append(out, Revealable{ID: el.ID, Kind: RevealKindAnswers, Type: el.Type})
		case el.Type == surveydoc.ElementMultipleChoiceSingle || el.Type == surveydoc.ElementMultipleChoiceMulti:
			if slices.ContainsFunc(el.Choices, func(c surveydoc.Choice) bool { return c.ID == "other" }) {
				out = append(out, Revealable{ID: el.ID, Kind: RevealKindOtherValues, Type: el.Type})
			}
		}
	}
	for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceIngested) {
		out = append(out, Revealable{ID: f.StorageKey, Kind: RevealKindHiddenField})
	}
	return out
}

// publishableSurvey authorizes managing the survey's results links, which is publishing it (workspace write), and loads it. An unknown survey is the same 403 as a foreign one.
func (s *Service) publishableSurvey(ctx context.Context, userID, surveyID uuid.UUID) (*surveyDoc, error) {
	return s.readableSurvey(ctx, userID, surveyID, authz.SurveyPublish)
}

// ResultShares lists the survey's results links, newest first, for those who may publish it.
func (s *Service) ResultShares(ctx context.Context, userID, surveyID uuid.UUID) (*ResultShares, error) {
	sv, err := s.publishableSurvey(ctx, userID, surveyID)
	if err != nil {
		return nil, err
	}
	shares, err := s.resultShares(ctx, `WHERE s.survey_id = ? ORDER BY s.created_at DESC, s.id DESC`, surveyID)
	if err != nil {
		return nil, err
	}
	for i := range shares {
		if err := s.pruneReveals(ctx, &shares[i], sv); err != nil {
			return nil, err
		}
		if err := s.markMissingSegments(ctx, &shares[i], sv); err != nil {
			return nil, err
		}
	}
	return &ResultShares{Shares: shares, Revealable: sv.revealable()}, nil
}

// missingSegments lists, sorted, the segments criteria name that hold nobody because they do not exist the way the filter evaluates them (response.segmentConditions): no segment of the survey's workspace has the id, or one it nests is gone.
func (s *Service) missingSegments(ctx context.Context, sv *surveyDoc, f *response.Criteria) ([]string, error) {
	missing := []string{}
	if f == nil {
		return missing, nil
	}
	for key := range f.Segments {
		found := false
		if id, ok := rowid.Parse(key); ok {
			var err error
			if _, _, found, err = s.segments.SegmentMembers(ctx, sv.WorkspaceID, id); err != nil {
				return nil, fmt.Errorf("evaluate segment %s: %w", id, err)
			}
		}
		if !found {
			missing = append(missing, key)
		}
	}
	slices.Sort(missing)
	return missing, nil
}

// frozenCriteria decodes the filter a link stores.
func (sh *ResultShare) frozenCriteria() (*response.Criteria, error) {
	var filter response.Criteria
	if err := json.Unmarshal(sh.FilterCriteria, &filter); err != nil {
		return nil, fmt.Errorf("decode result share %s filter: %w", sh.ID, err)
	}
	return &filter, nil
}

// markMissingSegments sets the link's MissingSegmentIDs from its frozen filter. A link whose segment was deleted is kept and marked rather than deleted: its managers decide what to do with it.
func (s *Service) markMissingSegments(ctx context.Context, sh *ResultShare, sv *surveyDoc) error {
	filter, err := sh.frozenCriteria()
	if err != nil {
		return err
	}
	sh.MissingSegmentIDs, err = s.missingSegments(ctx, sv, filter)
	return err
}

// checkResultShareFields trims the name and checks the name, the revealed ids and the expiry against the survey, as the field errors a binding failure reports. It returns the cards the ids reveal, in the survey's order.
//
// before is the link as it was, nil for a new one. An id among the cards it revealed stays revealed only for the very card it was granted for, and one the survey no longer offers is dropped rather than refused: the people editing the link cannot see it to take it off. Any other id must name a card the survey offers. Its expiry may be kept as it was even once it has passed, so an expired link can still be renamed.
func checkResultShareFields(sv *surveyDoc, name string, revealed []string, before *ResultShare, expiresAt *time.Time, now time.Time) (string, []Revealable, error) {
	name = strings.TrimSpace(name)
	var fields []apperr.FieldError
	if len([]rune(name)) > maxResultShareNameLength {
		fields = append(fields, apperr.FieldError{Field: "name", Rule: "max", Param: fmt.Sprint(maxResultShareNameLength)})
	}
	var stored []Revealable
	keptExpiry := false
	if before != nil {
		stored = before.revealed
		keptExpiry = expiresAt != nil && before.ExpiresAt != nil && expiresAt.Equal(*before.ExpiresAt)
	}
	if expiresAt != nil && !expiresAt.After(now) && !keptExpiry {
		fields = append(fields, apperr.FieldError{Field: "expiresAt", Rule: "future"})
	}
	storedIDs := revealedIDs(stored)
	cards := []Revealable{}
	for _, r := range sv.revealable() {
		if slices.Contains(revealed, r.ID) && (!slices.Contains(storedIDs, r.ID) || slices.Contains(stored, r)) {
			cards = append(cards, r)
		}
	}
	for _, id := range revealed {
		if !slices.Contains(storedIDs, id) && !slices.ContainsFunc(cards, func(c Revealable) bool { return c.ID == id }) {
			fields = append(fields, apperr.FieldError{Field: "revealedElementIds", Rule: "oneof", Param: id})
			break
		}
	}
	if len(fields) > 0 {
		return "", nil, apperr.Validation(fields)
	}
	return name, cards, nil
}

// CreateResultShare creates a results link of the survey under the filter given, which the link keeps as it is: the filter is checked against the survey now, the way the summary checks it.
func (s *Service) CreateResultShare(ctx context.Context, userID, surveyID uuid.UUID, in ResultShareInput) (share *ResultShare, err error) {
	var organizationID string
	defer func() {
		e := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetResultShare, ActorType: audit.ActorUser, Status: audit.StatusSuccess, OrganizationID: organizationID}
		if err != nil {
			e.Status = audit.StatusFailure
		} else {
			e.TargetID = share.ID.String()
			e.NewObject = map[string]any{"surveyId": surveyID, "name": share.Name, "revealedElementIds": share.RevealedElementIDs, "expiresAt": share.ExpiresAt, "filtered": hasCriteria(in.FilterCriteria)}
		}
		s.audit.Log(ctx, audit.FromRequest(ctx, e))
	}()
	sv, err := s.publishableSurvey(ctx, userID, surveyID)
	if err != nil {
		return nil, err
	}
	organizationID = sv.OrganizationID.String()
	name, revealed, err := checkResultShareFields(sv, in.Name, in.RevealedElementIDs, nil, in.ExpiresAt, time.Now())
	if err != nil {
		return nil, err
	}
	if _, err := response.Where(ctx, s.segments, sv.Survey, in.FilterCriteria); err != nil {
		return nil, err
	}
	missing, err := s.missingSegments(ctx, sv, in.FilterCriteria)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return nil, apperr.InvalidInput(CodeResultShareSegmentNotFound, "The filter uses a segment that does not exist in this workspace: "+strings.Join(missing, ", ")+". Remove it from the filter and create the link again.")
	}
	criteria, err := compactCriteria(in.FilterCriteria)
	if err != nil {
		return nil, err
	}
	revealedJSON, err := json.Marshal(revealed)
	if err != nil {
		return nil, fmt.Errorf("encode result share revealed cards: %w", err)
	}
	key, err := secret.NewToken()
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The survey row is locked so two creations cannot both pass the bound.
		if err := tx.Exec(`SELECT 1 FROM surveys WHERE id = ? FOR UPDATE`, surveyID).Error; err != nil {
			return fmt.Errorf("lock survey: %w", err)
		}
		var count int
		if err := tx.Raw(`SELECT count(*) FROM survey_result_shares WHERE survey_id = ?`, surveyID).Row().Scan(&count); err != nil {
			return fmt.Errorf("count result shares: %w", err)
		}
		if count >= maxResultShares {
			return apperr.OperationNotAllowed(fmt.Sprintf("A survey can have at most %d results links. Revoke one you no longer need first.", maxResultShares))
		}
		return tx.Raw(`INSERT INTO survey_result_shares (survey_id, created_by, share_key, name, filter_criteria, revealed, expires_at)
			VALUES (?, ?, ?, ?, ?::jsonb, ?::jsonb, ?) RETURNING id`,
			surveyID, userID, key, name, string(criteria), string(revealedJSON), in.ExpiresAt).Row().Scan(&id)
	})
	if err != nil {
		var appErr *apperr.Error
		if errors.As(err, &appErr) {
			return nil, err
		}
		return nil, fmt.Errorf("insert result share: %w", err)
	}
	found, err := s.resultShares(ctx, `WHERE s.id = ?`, id)
	if err != nil {
		return nil, err
	}
	found[0].keepOffered(sv)
	return &found[0], nil
}

// compactCriteria is the criteria as the link stores them: the members that are set, so {} for no filter.
func compactCriteria(f *response.Criteria) ([]byte, error) {
	members := map[string]json.RawMessage{}
	if f != nil {
		raw, err := json.Marshal(f)
		if err != nil {
			return nil, fmt.Errorf("encode result share filter: %w", err)
		}
		if err := json.Unmarshal(raw, &members); err != nil {
			return nil, fmt.Errorf("decode result share filter: %w", err)
		}
		for k, v := range members {
			if string(v) == "null" {
				delete(members, k)
			}
		}
	}
	out, err := json.Marshal(members)
	if err != nil {
		return nil, fmt.Errorf("encode result share filter: %w", err)
	}
	return out, nil
}

// hasCriteria tells whether the criteria filter anything: any member set, an empty list or map included, since the response module reads it as a condition.
func hasCriteria(f *response.Criteria) bool {
	raw, err := compactCriteria(f)
	return err != nil || string(raw) != "{}"
}

// existingResultShare is the survey's link with id, or a 404 when the survey has no such link.
func (s *Service) existingResultShare(ctx context.Context, surveyID, shareID uuid.UUID) (*ResultShare, error) {
	found, err := s.resultShares(ctx, `WHERE s.id = ? AND s.survey_id = ?`, shareID, surveyID)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, apperr.NotFound("Results link", shareID.String())
	}
	return &found[0], nil
}

// UpdateResultShare changes a link's name, revealed cards and expiry. Its key and filter stay as they are. Cards it revealed that the survey no longer offers are dropped.
func (s *Service) UpdateResultShare(ctx context.Context, userID, surveyID, shareID uuid.UUID, in ResultShareUpdate) (share *ResultShare, err error) {
	var organizationID string
	var before *ResultShare
	defer func() {
		e := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetResultShare, TargetID: shareID.String(), ActorType: audit.ActorUser, Status: audit.StatusSuccess, OrganizationID: organizationID}
		if err != nil {
			e.Status = audit.StatusFailure
		} else {
			e.OldObject = auditedShare(before)
			e.NewObject = auditedShare(share)
		}
		s.audit.Log(ctx, audit.FromRequest(ctx, e))
	}()
	sv, err := s.publishableSurvey(ctx, userID, surveyID)
	if err != nil {
		return nil, err
	}
	organizationID = sv.OrganizationID.String()
	if before, err = s.existingResultShare(ctx, surveyID, shareID); err != nil {
		return nil, err
	}
	name, revealed, err := checkResultShareFields(sv, in.Name, in.RevealedElementIDs, before, in.ExpiresAt, time.Now())
	if err != nil {
		return nil, err
	}
	revealedJSON, err := json.Marshal(revealed)
	if err != nil {
		return nil, fmt.Errorf("encode result share revealed cards: %w", err)
	}
	if err := s.db.WithContext(ctx).Exec(`UPDATE survey_result_shares SET name = ?, revealed = ?::jsonb, expires_at = ?, updated_at = now()
		WHERE id = ? AND survey_id = ?`, name, string(revealedJSON), in.ExpiresAt, shareID, surveyID).Error; err != nil {
		return nil, fmt.Errorf("update result share: %w", err)
	}
	if share, err = s.existingResultShare(ctx, surveyID, shareID); err != nil {
		return nil, err
	}
	share.keepOffered(sv)
	if err := s.markMissingSegments(ctx, share, sv); err != nil {
		return nil, err
	}
	return share, nil
}

// auditedShare is what the audit log records of a link: everything but its key, which is a credential.
func auditedShare(share *ResultShare) any {
	if share == nil {
		return nil
	}
	return map[string]any{"surveyId": share.SurveyID, "name": share.Name, "revealedElementIds": share.RevealedElementIDs, "expiresAt": share.ExpiresAt}
}

// DeleteResultShare revokes a link: it is deleted, and its key stops working at once.
func (s *Service) DeleteResultShare(ctx context.Context, userID, surveyID, shareID uuid.UUID) (err error) {
	var organizationID string
	var before *ResultShare
	defer func() {
		e := audit.Event{Action: audit.ActionDeleted, TargetType: audit.TargetResultShare, TargetID: shareID.String(), ActorType: audit.ActorUser, Status: audit.StatusSuccess, OrganizationID: organizationID}
		if err != nil {
			e.Status = audit.StatusFailure
		} else {
			e.OldObject = auditedShare(before)
		}
		s.audit.Log(ctx, audit.FromRequest(ctx, e))
	}()
	sv, err := s.publishableSurvey(ctx, userID, surveyID)
	if err != nil {
		return err
	}
	organizationID = sv.OrganizationID.String()
	if before, err = s.existingResultShare(ctx, surveyID, shareID); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Exec(`DELETE FROM survey_result_shares WHERE id = ? AND survey_id = ?`, shareID, surveyID).Error; err != nil {
		return fmt.Errorf("delete result share: %w", err)
	}
	return nil
}

// resultShareNotFound is the answer to a key that names no live link.
func resultShareNotFound() *apperr.Error {
	return &apperr.Error{Status: 404, Code: CodeResultShareNotFound, Message: "This results link does not exist, has expired or was revoked."}
}

// liveResultShare is the link a key names while it has not expired, with its survey and frozen filter, revealing only the cards the survey still offers.
func (s *Service) liveResultShare(ctx context.Context, key string) (*ResultShare, *surveyDoc, *response.Criteria, error) {
	if !shareKeyPattern.MatchString(key) {
		return nil, nil, nil, resultShareNotFound()
	}
	found, err := s.resultShares(ctx, `WHERE s.share_key = ? AND (s.expires_at IS NULL OR s.expires_at > now())`, key)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(found) == 0 {
		return nil, nil, nil, resultShareNotFound()
	}
	share := &found[0]
	sv, err := loadSurveyDoc(ctx, s.db, share.SurveyID)
	if err != nil {
		return nil, nil, nil, err
	}
	if sv == nil {
		return nil, nil, nil, resultShareNotFound()
	}
	if err := s.pruneReveals(ctx, share, sv); err != nil {
		return nil, nil, nil, err
	}
	filter, err := share.frozenCriteria()
	if err != nil {
		return nil, nil, nil, err
	}
	return share, sv, filter, nil
}

// SharedResults is the summary a link shows, computed now under its frozen filter, with what identifies respondents removed and the cards it does not reveal withheld.
func (s *Service) SharedResults(ctx context.Context, key string) (*SharedResults, error) {
	share, sv, filter, err := s.liveResultShare(ctx, key)
	if err != nil {
		return nil, err
	}
	summary, err := s.summary(ctx, sv, filter)
	if err != nil {
		return nil, err
	}
	withheld := redactSummary(summary, share.revealed)
	missing, err := s.missingSegments(ctx, sv, filter)
	if err != nil {
		return nil, err
	}
	shared, err := loadSharedSurvey(ctx, s.db, sv.ID)
	if err != nil {
		return nil, err
	}
	return &SharedResults{
		Link:        SharedResultsLink{Name: share.Name, ExpiresAt: share.ExpiresAt, Filtered: hasCriteria(filter), SegmentMissing: len(missing) > 0},
		Survey:      *shared,
		Summary:     summary,
		WithheldIDs: withheld,
	}, nil
}

// SharedTrends is the trend a link shows: the survey's responses over time under its frozen filter, by the interval asked for ("" lets the server pick one). It counts and scores, so nothing in it identifies a respondent.
func (s *Service) SharedTrends(ctx context.Context, key, interval string) (*Trends, error) {
	_, sv, filter, err := s.liveResultShare(ctx, key)
	if err != nil {
		return nil, err
	}
	return s.trends(ctx, sv, filter, interval)
}

// loadSharedSurvey reads the parts of the survey document a link shows.
func loadSharedSurvey(ctx context.Context, db *gorm.DB, id uuid.UUID) (*SharedSurvey, error) {
	var rows []struct {
		Name         string
		Type         string
		Blocks       []byte
		Endings      []byte
		HiddenFields []byte
		Variables    []byte
	}
	if err := db.WithContext(ctx).Raw(`SELECT name, type, blocks, endings, hidden_fields, variables FROM surveys WHERE id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load shared survey: %w", err)
	}
	if len(rows) == 0 {
		return nil, resultShareNotFound()
	}
	r := rows[0]
	orJSON := func(raw []byte, empty string) json.RawMessage {
		if len(raw) == 0 || string(raw) == "null" {
			return json.RawMessage(empty)
		}
		return raw
	}
	return &SharedSurvey{ID: id, Name: r.Name, Type: r.Type, Blocks: orJSON(r.Blocks, "[]"), Endings: orJSON(r.Endings, "[]"),
		HiddenFields: orJSON(r.HiddenFields, `{"enabled":false}`), Variables: orJSON(r.Variables, "[]")}, nil
}

// elementID reads the id of a summary entry's element.
func elementID(raw json.RawMessage) string {
	var el struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &el)
	return el.ID
}

// anonymous is a sample with what identifies its respondent removed: the response id, the contact and its attributes.
func anonymous(samples []Sample) []Sample {
	out := make([]Sample, len(samples))
	for i, sample := range samples {
		out[i] = Sample{UpdatedAt: sample.UpdatedAt, Value: sample.Value}
	}
	return out
}

// redactSummary makes a summary fit for a results link, in place: no entry names a contact or a response, and the cards listing individual answers that revealed does not include, by id and kind, keep their counts without the answers. It returns the ids of the cards withheld that way, in the summary's order.
func redactSummary(summary *Summary, revealed []Revealable) []string {
	withheld := []string{}
	for i, entry := range summary.Summary {
		switch e := entry.(type) {
		case SamplesSummary:
			id := elementID(e.Element)
			if reveals(revealed, id, RevealKindAnswers) {
				e.Samples = anonymous(e.Samples)
			} else {
				e.Samples = []Sample{}
				withheld = append(withheld, id)
			}
			summary.Summary[i] = e
		case FileUploadSummary:
			e.Files = []Sample{}
			withheld = append(withheld, elementID(e.Element))
			summary.Summary[i] = e
		case MultipleChoiceSummary:
			show := reveals(revealed, elementID(e.Element), RevealKindOtherValues)
			choices := make([]ChoiceCount, len(e.Choices))
			for j, c := range e.Choices {
				if c.Others != nil {
					if show {
						others := make([]Other, len(*c.Others))
						for k, o := range *c.Others {
							others[k] = Other{Value: o.Value}
						}
						c.Others = &others
					} else {
						c.Others, c.OthersBreakdown = nil, nil
					}
				}
				choices[j] = c
			}
			e.Choices = choices
			summary.Summary[i] = e
		case HiddenFieldSummary:
			if reveals(revealed, e.ID, RevealKindHiddenField) {
				e.Samples = anonymous(e.Samples)
			} else {
				e.Samples = []Sample{}
				e.Breakdown = Breakdown{Values: []ValueCount{}}
				withheld = append(withheld, e.ID)
			}
			summary.Summary[i] = e
		}
	}
	return withheld
}
