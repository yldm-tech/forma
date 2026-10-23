package contact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
)

// Segment membership is forma2's segmentFilterToPrismaQuery (apps/web/modules/contacts/segments/lib/filter/prisma-query.ts) as SQL: the filter tree becomes a condition on a contact row `c`, evaluated by PostgreSQL. The client API asks it which of a workspace's segments a contact is in.

// Devices a client reports; a device filter compares against it.
const (
	DevicePhone   = "phone"
	DeviceDesktop = "desktop"
)

// numericText is forma2's NUMBER_PATTERN_SQL: text a number key stored without a parsed number that arithmetic filters still compare as a number.
const numericText = `^-?[0-9]+(\.[0-9]+)?$`

// attributeText is an attribute row's value as text, the column forma2 always kept and its string operators compared: the text as stored, a number as PostgreSQL prints it, a date as Date#toISOString prints it.
const attributeText = `COALESCE(ca.value_text, ca.value_number::text, to_char(ca.value_date AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'))`

// condition is a SQL boolean over the contact row `c` with its arguments. The zero value constrains nothing, Prisma's `{}`, which matches every contact and is dropped from the group around it.
type condition struct {
	sql  string
	args []any
}

func (c condition) empty() bool { return c.sql == "" }

// matchNone is forma2's impossible id condition.
var matchNone = condition{sql: "FALSE"}

func not(c condition) condition {
	return condition{sql: "NOT (" + c.sql + ")", args: c.args}
}

func join(op string, parts []condition) condition {
	if len(parts) == 1 {
		return parts[0]
	}
	sqls := make([]string, len(parts))
	var args []any
	for i, p := range parts {
		sqls[i] = "(" + p.sql + ")"
		args = append(args, p.args...)
	}
	return condition{sql: strings.Join(sqls, " "+op+" "), args: args}
}

// segmentNotFoundError is a referenced segment that does not exist; forma2's getSegment threw, failing the whole query.
type segmentNotFoundError struct{ id string }

func (e *segmentNotFoundError) Error() string { return "segment " + e.id + " not found" }

// notFound converts a missing nested segment into forma2's ResourceNotFoundError.
func notFound(err error) error {
	var nf *segmentNotFoundError
	if errors.As(err, &nf) {
		return apperr.NotFound("segment", nf.id)
	}
	return err
}

// builder turns one tree into a condition. Its clock is read once, so every relative window of a query agrees.
type builder struct {
	db          *gorm.DB
	log         *slog.Logger
	workspaceID uuid.UUID
	device      string
	now         time.Time
}

// tree is forma2's processFilters: AND binds tighter than OR, so consecutive "and" nodes form a group, "or" starts the next, and the tree matches when any group does. A node that constrains nothing is dropped from its group, a group left empty is dropped, and a tree with no groups left constrains nothing.
func (b *builder) tree(ctx context.Context, filters Filters, path []string) (condition, error) {
	var groups [][]condition
	var current []condition
	started := false
	for i, node := range filters {
		var c condition
		var err error
		if node.Leaf != nil {
			c, err = b.leaf(ctx, node.Leaf, path)
		} else {
			c, err = b.tree(ctx, node.Group, path)
		}
		if err != nil {
			return condition{}, err
		}
		if !started || (i > 0 && node.Connector != nil && *node.Connector == connectorOr) {
			if started {
				groups = append(groups, current)
			}
			current, started = nil, true
		}
		if !c.empty() {
			current = append(current, c)
		}
	}
	if started {
		groups = append(groups, current)
	}
	var clauses []condition
	for _, g := range groups {
		if len(g) > 0 {
			clauses = append(clauses, join("AND", g))
		}
	}
	if len(clauses) == 0 {
		return condition{}, nil
	}
	return join("OR", clauses), nil
}

func (b *builder) leaf(ctx context.Context, f *Filter, path []string) (condition, error) {
	switch f.Root.Type {
	case filterRootAttribute:
		return b.attribute(f.Root.ContactAttributeKey, f.Qualifier.Operator, decodeValue(f.Value))
	case filterRootPerson:
		// Only userId identifies a person; any other identifier constrains nothing.
		if f.Root.PersonIdentifier != personIdentifierUserID {
			return condition{}, nil
		}
		return b.attribute(personIdentifierUserID, f.Qualifier.Operator, decodeValue(f.Value))
	case filterRootDevice:
		return b.deviceFilter(f), nil
	case filterRootSegment:
		return b.segment(ctx, f, path)
	case filterRootSurveyInteraction:
		return b.interaction(f)
	}
	return condition{}, nil
}

// hasAttribute is "the contact has a row for key" plus extra conditions on that row.
func hasAttribute(key, extra string, args ...any) condition {
	sql := `EXISTS (SELECT 1 FROM contact_attributes ca JOIN contact_attribute_keys k ON k.id = ca.attribute_key_id WHERE ca.contact_id = c.id AND k.key = ?`
	if extra != "" {
		sql += " AND " + extra
	}
	return condition{sql: sql + ")", args: append([]any{key}, args...)}
}

var sqlComparison = map[string]string{"lessThan": "<", "lessEqual": "<=", "greaterThan": ">", "greaterEqual": ">="}

func (b *builder) attribute(key, op string, v filterValue) (condition, error) {
	switch {
	case op == operatorIsSet:
		return hasAttribute(key, ""), nil
	case op == operatorIsNotSet:
		return not(hasAttribute(key, "")), nil
	case slices.Contains(dateOperators, op):
		return b.dateAttribute(key, op, v)
	case slices.Contains(arithmeticOperators, op):
		// Validation guarantees a number; a comparison with anything else is JavaScript's comparison with NaN, which nothing satisfies.
		if v.num == nil {
			return matchNone, nil
		}
		n := *v.num
		cmp := sqlComparison[op]
		return hasAttribute(key, fmt.Sprintf(`(ca.value_number %[1]s ? OR (k.data_type = 'number' AND ca.value_number IS NULL AND ca.value_text ~ ? AND ca.value_text::double precision %[1]s ?))`, cmp), n, numericText, n), nil
	}
	s := stringOperand(v)
	switch op {
	case operatorEquals:
		return hasAttribute(key, "lower("+attributeText+") = lower(?)", s), nil
	case operatorNotEquals:
		return hasAttribute(key, "lower("+attributeText+") <> lower(?)", s), nil
	case "contains":
		return hasAttribute(key, attributeText+" ILIKE ?", "%"+escapeLike(s)+"%"), nil
	case "doesNotContain":
		return hasAttribute(key, attributeText+" NOT ILIKE ?", "%"+escapeLike(s)+"%"), nil
	case "startsWith":
		return hasAttribute(key, attributeText+" ILIKE ?", escapeLike(s)+"%"), nil
	case "endsWith":
		return hasAttribute(key, attributeText+" ILIKE ?", "%"+escapeLike(s)), nil
	}
	return hasAttribute(key, attributeText+" = ?", s), nil
}

// stringOperand is the value a string operator compares: a string as is, a number as String(n), anything else as JSON.stringify gives it.
func stringOperand(v filterValue) string {
	switch {
	case v.str != nil:
		return *v.str
	case v.num != nil:
		return jsNumberString(*v.num)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, v.raw); err != nil {
		return string(v.raw)
	}
	return compact.String()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// dateAttribute matches a date key's parsed date, or, for a row whose text never parsed, that text compared as an ISO string.
func (b *builder) dateAttribute(key, op string, v filterValue) (condition, error) {
	var bounds []struct {
		cmp string
		at  time.Time
	}
	add := func(cmp string, at time.Time) {
		bounds = append(bounds, struct {
			cmp string
			at  time.Time
		}{cmp, at})
	}
	parse := func(s string) (time.Time, error) {
		t, ok := jsDate(s)
		if !ok {
			return time.Time{}, fmt.Errorf("invalid date %q in segment filter", s)
		}
		return t, nil
	}
	switch op {
	case operatorIsOlderThan, operatorIsNewerThan:
		if r, ok := v.relative(); ok {
			threshold := subtractTimeUnit(b.now, r.Amount, r.Unit)
			if op == operatorIsOlderThan {
				add("<", threshold)
			} else {
				add(">=", threshold)
			}
		}
	case operatorIsBefore, operatorIsAfter, operatorIsSameDay:
		if v.str != nil {
			t, err := parse(*v.str)
			if err != nil {
				return condition{}, err
			}
			switch op {
			case operatorIsBefore:
				add("<", t)
			case operatorIsAfter:
				add(">", t)
			default:
				day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
				add(">=", day)
				add("<=", day.Add(24*time.Hour-time.Millisecond))
			}
		}
	case operatorIsBetween:
		if p, ok := v.pair(); ok {
			start, err := parse(p[0])
			if err != nil {
				return condition{}, err
			}
			end, err := parse(p[1])
			if err != nil {
				return condition{}, err
			}
			add(">=", start)
			add("<=", end)
		}
	}
	if len(bounds) == 0 {
		return hasAttribute(key, "k.data_type = 'date'"), nil
	}
	var onDate, onText []string
	var dateArgs, textArgs []any
	for _, bd := range bounds {
		onDate = append(onDate, "ca.value_date "+bd.cmp+" ?")
		dateArgs = append(dateArgs, bd.at)
		onText = append(onText, `ca.value_text COLLATE "C" `+bd.cmp+" ?")
		textArgs = append(textArgs, jsISOString(bd.at))
	}
	extra := "k.data_type = 'date' AND ((" + strings.Join(onDate, " AND ") + ") OR (ca.value_date IS NULL AND " + strings.Join(onText, " AND ") + "))"
	return hasAttribute(key, extra, append(dateArgs, textArgs...)...), nil
}

// subtractTimeUnit is forma2's subtractTimeUnit: JavaScript's setDate, setMonth and setFullYear on the field minus amount, truncated to an integer, with overflow rolling into the neighbouring month or year.
func subtractTimeUnit(t time.Time, amount float64, unit string) time.Time {
	t = t.UTC()
	switch unit {
	case unitDays:
		return t.AddDate(0, 0, int(math.Trunc(float64(t.Day())-amount))-t.Day())
	case unitWeeks:
		return t.AddDate(0, 0, int(math.Trunc(float64(t.Day())-amount*7))-t.Day())
	case unitMonths:
		m := int(t.Month()) - 1
		return t.AddDate(0, int(math.Trunc(float64(m)-amount))-m, 0)
	case unitYears:
		return t.AddDate(int(math.Trunc(float64(t.Year())-amount))-t.Year(), 0, 0)
	}
	return t
}

// deviceFilter decides a device filter now: the device is a property of the request, not of the contact. Without a device the filter constrains nothing.
func (b *builder) deviceFilter(f *Filter) condition {
	if b.device == "" {
		return condition{}
	}
	v := decodeValue(f.Value)
	same := v.str != nil && *v.str == b.device
	matches := (f.Qualifier.Operator == operatorEquals && same) || (f.Qualifier.Operator == operatorNotEquals && !same)
	if matches {
		return condition{}
	}
	return matchNone
}

// segment inlines a nested segment's tree. A reference back into the path being evaluated, or to another workspace's segment, constrains nothing; a missing segment fails the query.
func (b *builder) segment(ctx context.Context, f *Filter, path []string) (condition, error) {
	id := f.Root.SegmentID
	if slices.Contains(path, id) {
		b.log.ErrorContext(ctx, "Circular reference detected in segment filter", "segmentId", id)
		return condition{}, nil
	}
	nested, workspaceID, err := loadSegmentFilters(ctx, b.db, id)
	if err != nil {
		return condition{}, err
	}
	if workspaceID != b.workspaceID {
		b.log.ErrorContext(ctx, "Refusing to resolve a segment filter referencing another workspace's segment", "segmentId", id, "segmentWorkspaceId", workspaceID.String(), "workspaceId", b.workspaceID.String())
		return condition{}, nil
	}
	inner, err := b.tree(ctx, nested, append(slices.Clone(path), id))
	if err != nil {
		return condition{}, err
	}
	switch f.Qualifier.Operator {
	case operatorUserIsIn:
		return inner, nil
	case operatorUserIsNotIn:
		if inner.empty() {
			return matchNone, nil
		}
		return not(inner), nil
	}
	return condition{}, nil
}

// loadSegmentFilters reads a segment's stored tree by id alone, as forma2's getSegment did; the caller decides what a foreign workspace means.
func loadSegmentFilters(ctx context.Context, db *gorm.DB, id string) (Filters, uuid.UUID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, uuid.Nil, &segmentNotFoundError{id}
	}
	var row struct {
		WorkspaceID uuid.UUID
		Filters     []byte
	}
	res := db.WithContext(ctx).Raw(`SELECT workspace_id, filters FROM segments WHERE id = ?`, parsed).Scan(&row)
	if res.Error != nil {
		return nil, uuid.Nil, fmt.Errorf("load segment %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, uuid.Nil, &segmentNotFoundError{id}
	}
	var tree Filters
	if err := json.Unmarshal(row.Filters, &tree); err != nil {
		return nil, uuid.Nil, fmt.Errorf("decode segment %s filters: %w", id, err)
	}
	return tree, row.WorkspaceID, nil
}

// Survey-interaction operators: which relation they look at, whether only finished responses count, and whether they assert the absence of a match (forma2's SURVEY_INTERACTION_SEMANTICS).
type interactionSemantics struct {
	displays        bool
	requireFinished bool
	negate          bool
}

var interactionOperators = map[string]interactionSemantics{
	"haveSeen":                {displays: true},
	"haveNotSeen":             {displays: true, negate: true},
	"haveStartedRespondingTo": {},
	"haveCompleted":           {requireFinished: true},
	"haveNotCompleted":        {requireFinished: true, negate: true},
}

func (b *builder) interaction(f *Filter) (condition, error) {
	var v interactionValue
	if err := json.Unmarshal(f.Value, &v); err != nil {
		return condition{}, fmt.Errorf("decode survey interaction filter: %w", err)
	}
	sem := interactionOperators[f.Qualifier.Operator]
	since := subtractTimeUnit(b.now, float64(v.Within.Amount), v.Within.Unit)
	table, alias := "responses", "r"
	if sem.displays {
		table, alias = "displays", "d"
	}
	sql := fmt.Sprintf(`EXISTS (SELECT 1 FROM %[1]s %[2]s WHERE %[2]s.contact_id = c.id AND %[2]s.created_at >= ?`, table, alias)
	args := []any{since}
	if v.SurveyScope == surveyInteractionScopeSpecific {
		ids := v.SurveyIDs
		if len(ids) == 0 {
			ids = []string{""}
		}
		sql += fmt.Sprintf(` AND %s.survey_id::text IN ?`, alias)
		args = append(args, ids)
	}
	if sem.requireFinished {
		sql += fmt.Sprintf(` AND %s.finished`, alias)
	}
	c := condition{sql: sql + ")", args: args}
	if sem.negate {
		return not(c), nil
	}
	return c, nil
}

// Interactions is a contact's survey history: every display and response, unbounded in time, so the in-memory evaluation agrees with the database.
type Interactions struct {
	Displays  []DisplayRow
	Responses []ResponseRow
}

type DisplayRow struct {
	SurveyID  uuid.UUID
	CreatedAt time.Time
}

type ResponseRow struct {
	SurveyID  uuid.UUID
	CreatedAt time.Time
	Finished  bool
}

// resultPair is one node's outcome with the connector before it.
type resultPair struct {
	result    bool
	connector *string
}

// combine is forma2's combineFilterResults, the in-memory counterpart of tree: AND before OR.
func combine(pairs []resultPair) bool {
	if len(pairs) == 0 {
		return false
	}
	current := pairs[0].result
	var groups []bool
	for _, p := range pairs[1:] {
		if p.connector != nil && *p.connector == connectorOr {
			groups = append(groups, current)
			current = p.result
			continue
		}
		current = current && p.result
	}
	return slices.Contains(append(groups, current), true)
}

// evaluateInteraction is forma2's evaluateSurveyInteractionFilterInMemory: the same window, scope and finished rules as the SQL.
func evaluateInteraction(f *Filter, data Interactions, now time.Time) (bool, error) {
	var v interactionValue
	if err := json.Unmarshal(f.Value, &v); err != nil {
		return false, fmt.Errorf("decode survey interaction filter: %w", err)
	}
	sem := interactionOperators[f.Qualifier.Operator]
	since := subtractTimeUnit(now, float64(v.Within.Amount), v.Within.Unit)
	inScope := func(id uuid.UUID) bool {
		return v.SurveyScope != surveyInteractionScopeSpecific || slices.Contains(v.SurveyIDs, id.String())
	}
	match := false
	if sem.displays {
		for _, d := range data.Displays {
			if !d.CreatedAt.Before(since) && inScope(d.SurveyID) {
				match = true
				break
			}
		}
	} else {
		for _, r := range data.Responses {
			if !r.CreatedAt.Before(since) && inScope(r.SurveyID) && (!sem.requireFinished || r.Finished) {
				match = true
				break
			}
		}
	}
	return match != sem.negate, nil
}

// evaluateInteractionsOnly is forma2's tryEvaluateSurveyInteractionSegmentInMemory: a tree made only of survey-interaction filters is answered from the contact's loaded history without a query. ok is false when any node needs the database, or the tree is empty.
func evaluateInteractionsOnly(tree Filters, data Interactions, now time.Time) (result, ok bool, err error) {
	if len(tree) == 0 {
		return false, false, nil
	}
	pairs := make([]resultPair, 0, len(tree))
	for _, node := range tree {
		var r bool
		switch {
		case node.Group != nil:
			nested, nestedOK, err := evaluateInteractionsOnly(node.Group, data, now)
			if err != nil || !nestedOK {
				return false, false, err
			}
			r = nested
		case node.Leaf != nil && node.Leaf.Root.Type == filterRootSurveyInteraction:
			r, err = evaluateInteraction(node.Leaf, data, now)
			if err != nil {
				return false, false, err
			}
		default:
			return false, false, nil
		}
		pairs = append(pairs, resultPair{result: r, connector: node.Connector})
	}
	return combine(pairs), true, nil
}

// Evaluator decides segment membership for the client API, which asks it on every identify of a contact.
type Evaluator struct {
	db  *gorm.DB
	log *slog.Logger
	now func() time.Time
}

func NewEvaluator(db *gorm.DB, log *slog.Logger) *Evaluator {
	return &Evaluator{db: db, log: log, now: time.Now}
}

// SegmentIDsForContact is forma2's getPersonSegmentIds: the workspace's segments the contact is in. A segment without filters always matches; one made only of survey-interaction filters is answered from interactions; every other one is a query. A segment whose condition cannot be built is logged and skipped, so one bad segment does not hide the rest.
func (e *Evaluator) SegmentIDsForContact(ctx context.Context, workspaceID, contactID uuid.UUID, device string, interactions Interactions) ([]uuid.UUID, error) {
	var rows []struct {
		ID      uuid.UUID
		Filters []byte
	}
	if err := e.db.WithContext(ctx).Raw(`SELECT id, filters FROM segments WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list segments: %w", err)
	}
	now := e.now()
	matched := []uuid.UUID{}
	type check struct {
		id   uuid.UUID
		cond condition
	}
	var checks []check
	for _, row := range rows {
		var tree Filters
		if err := json.Unmarshal(row.Filters, &tree); err != nil {
			e.log.WarnContext(ctx, "Failed to read segment filters, skipping", "segmentId", row.ID.String(), "workspaceId", workspaceID.String(), "err", err)
			continue
		}
		if len(tree) == 0 {
			matched = append(matched, row.ID)
			continue
		}
		inMemory, ok, err := evaluateInteractionsOnly(tree, interactions, now)
		if err != nil {
			e.log.WarnContext(ctx, "Failed to evaluate segment in memory, falling back to DB query", "segmentId", row.ID.String(), "workspaceId", workspaceID.String(), "err", err)
		}
		if ok && err == nil {
			if inMemory {
				matched = append(matched, row.ID)
			}
			continue
		}
		b := &builder{db: e.db, log: e.log, workspaceID: workspaceID, device: device, now: now}
		cond, err := b.tree(ctx, tree, []string{row.ID.String()})
		if err != nil {
			e.log.WarnContext(ctx, "Failed to build query for segment, skipping", "segmentId", row.ID.String(), "workspaceId", workspaceID.String(), "err", err)
			continue
		}
		checks = append(checks, check{row.ID, cond})
	}
	if len(checks) == 0 {
		return matched, nil
	}
	err := e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, ch := range checks {
			in, err := isMember(ctx, tx, workspaceID, contactID, ch.cond)
			if err != nil {
				return err
			}
			if in {
				matched = append(matched, ch.id)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matched, nil
}

// IsMember reports whether a contact is in one segment, evaluated as the client API evaluates it.
func (e *Evaluator) IsMember(ctx context.Context, segmentID, contactID uuid.UUID, device string) (bool, error) {
	tree, workspaceID, err := loadSegmentFilters(ctx, e.db, segmentID.String())
	if err != nil {
		return false, notFound(err)
	}
	b := &builder{db: e.db, log: e.log, workspaceID: workspaceID, device: device, now: e.now()}
	cond, err := b.tree(ctx, tree, []string{segmentID.String()})
	if err != nil {
		return false, notFound(err)
	}
	return isMember(ctx, e.db, workspaceID, contactID, cond)
}

func isMember(ctx context.Context, db *gorm.DB, workspaceID, contactID uuid.UUID, cond condition) (bool, error) {
	sql := `SELECT EXISTS (SELECT 1 FROM contacts c WHERE c.id = ? AND c.workspace_id = ?`
	args := []any{contactID, workspaceID}
	if !cond.empty() {
		sql += " AND (" + cond.sql + ")"
		args = append(args, cond.args...)
	}
	var in bool
	if err := db.WithContext(ctx).Raw(sql+")", args...).Scan(&in).Error; err != nil {
		return false, fmt.Errorf("evaluate segment: %w", err)
	}
	return in, nil
}

// SegmentContacts is forma2's getContactsInSegment query: the contacts of the segment's workspace its filters match (evaluated without a device, so a device filter constrains nothing), newest first, limit of them after offset, and how many match in all.
func (e *Evaluator) SegmentContacts(ctx context.Context, segmentID uuid.UUID, limit, offset int) ([]uuid.UUID, int64, error) {
	where, args, err := e.segmentContactsWhere(ctx, segmentID)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := e.db.WithContext(ctx).Raw(`SELECT count(*)`+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count segment contacts: %w", err)
	}
	ids := []uuid.UUID{}
	if err := e.db.WithContext(ctx).Raw(`SELECT c.id`+where+` ORDER BY c.created_at DESC, c.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...).Scan(&ids).Error; err != nil {
		return nil, 0, fmt.Errorf("list segment contacts: %w", err)
	}
	return ids, total, nil
}

// AllSegmentContacts is SegmentContacts without paging, as forma2's share modal read the segment it generated personal links for: every matching contact, newest first.
func (e *Evaluator) AllSegmentContacts(ctx context.Context, segmentID uuid.UUID) ([]uuid.UUID, error) {
	where, args, err := e.segmentContactsWhere(ctx, segmentID)
	if err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	if err := e.db.WithContext(ctx).Raw(`SELECT c.id`+where+` ORDER BY c.created_at DESC, c.id DESC`, args...).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("list segment contacts: %w", err)
	}
	return ids, nil
}

// segmentContactsWhere is the FROM and WHERE clauses selecting the contacts a segment matches, with their arguments; a missing segment, or a missing segment it nests, is forma2's ResourceNotFoundError.
func (e *Evaluator) segmentContactsWhere(ctx context.Context, segmentID uuid.UUID) (string, []any, error) {
	tree, workspaceID, err := loadSegmentFilters(ctx, e.db, segmentID.String())
	if err != nil {
		return "", nil, notFound(err)
	}
	b := &builder{db: e.db, log: e.log, workspaceID: workspaceID, now: e.now()}
	cond, err := b.tree(ctx, tree, []string{segmentID.String()})
	if err != nil {
		return "", nil, notFound(err)
	}
	where := ` FROM contacts c WHERE c.workspace_id = ?`
	args := []any{workspaceID}
	if !cond.empty() {
		where += " AND (" + cond.sql + ")"
		args = append(args, cond.args...)
	}
	return where, args, nil
}
