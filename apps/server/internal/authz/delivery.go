package authz

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Delivery batches. The periodic job drains at most deliveryJobBatches per tick; the operator command defaults to DefaultDrainBatches.
const (
	deliveryJobBatches  = 10
	DefaultDrainBatches = 100
	// maxSplitDepth: 2^8 exceeds the claim batch, so a split always reaches single events first.
	maxSplitDepth = 8
)

// perEventCodes are the failures one event can cause. Only they justify splitting a group to find the culprit, and only they, on a single event, count toward dead-lettering; every other code describes the instance.
var perEventCodes = map[Code]bool{codeProjectionInvalidSource: true, CodeInvalidRequest: true}

// deliveryGroup is one reconciler call per tick, and the attribution boundary: a failure is charged to the events in flight with it and nothing else.
type deliveryGroup int

const (
	groupMembership deliveryGroup = iota
	groupOrganization
	groupUser
	groupTeamWorkspace
	groupAPIKey
)

var groupOf = map[string]deliveryGroup{
	targetMembership:      groupMembership,
	targetOrganization:    groupOrganization,
	targetUser:            groupUser,
	targetTeam:            groupTeamWorkspace,
	targetTeamMembership:  groupTeamWorkspace,
	targetWorkspace:       groupTeamWorkspace,
	targetWorkspaceTeam:   groupTeamWorkspace,
	targetAPIKey:          groupAPIKey,
	targetAPIKeyWorkspace: groupAPIKey,
}

// Delivery projects claimed outbox events into SpiceDB.
type Delivery struct {
	db  *gorm.DB
	log *slog.Logger
	// run projects one group's events; it reads PostgreSQL's current state for every target the events name.
	run func(ctx context.Context, group deliveryGroup, events []outboxEvent) error
}

// NewDelivery returns nil when AuthZed is disabled; the delivery job then does nothing, and the outbox waits.
func NewDelivery(db *gorm.DB, client *Client, log *slog.Logger) *Delivery {
	if client == nil {
		return nil
	}
	p := newProjector(db, client, log)
	return &Delivery{db: db, log: log, run: func(ctx context.Context, group deliveryGroup, events []outboxEvent) error {
		return p.deliver(ctx, group, events)
	}}
}

func (p *projector) deliver(ctx context.Context, group deliveryGroup, events []outboxEvent) error {
	switch group {
	case groupMembership:
		targets := make([]membershipTarget, 0, len(events))
		for _, e := range events {
			targets = append(targets, membershipTarget{e.PrimaryID, e.SecondaryID})
		}
		return p.reconcileMembershipsChunked(ctx, targets)
	case groupOrganization:
		return p.deliverOrganizations(ctx, primaryIDs(events))
	case groupUser:
		return p.deliverUsers(ctx, primaryIDs(events))
	case groupTeamWorkspace:
		var t teamWorkspaceTargets
		for _, e := range events {
			switch e.TargetType {
			case targetTeam:
				t.TeamIDs = append(t.TeamIDs, e.PrimaryID)
			case targetTeamMembership:
				t.TeamMemberships = append(t.TeamMemberships, teamMembershipTarget{e.PrimaryID, e.SecondaryID})
			case targetWorkspace:
				t.WorkspaceIDs = append(t.WorkspaceIDs, e.PrimaryID)
			case targetWorkspaceTeam:
				t.WorkspaceTeams = append(t.WorkspaceTeams, workspaceTeamTarget{e.PrimaryID, e.SecondaryID})
			}
		}
		return p.reconcileTeamWorkspaceChunked(ctx, t)
	case groupAPIKey:
		var t apiKeyTargets
		for _, e := range events {
			if e.TargetType == targetAPIKey {
				t.APIKeyIDs = append(t.APIKeyIDs, e.PrimaryID)
			} else {
				t.Grants = append(t.Grants, apiKeyWorkspaceTarget{e.PrimaryID, e.SecondaryID})
			}
		}
		return p.reconcileAPIKeysChunked(ctx, t)
	}
	return fmt.Errorf("unknown delivery group %d", group)
}

func primaryIDs(events []outboxEvent) []string {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.PrimaryID)
	}
	return uniqueIDs(ids)
}

// deliverOrganizations: organizations are only inserted and deleted, so a row that still exists needs nothing and one that is gone loses its relationships.
func (p *projector) deliverOrganizations(ctx context.Context, ids []string) error {
	var existing []string
	if err := p.db.WithContext(ctx).Raw(`SELECT id::text FROM organizations WHERE id = ANY(?::uuid[])`, idArray(ids)).Scan(&existing).Error; err != nil {
		return fmt.Errorf("read organizations: %w", err)
	}
	return p.forEachBounded(ctx, without(ids, existing), p.deleteOrganization)
}

// deliverUsers reads every named user once. A missing user is treated exactly as an inactive one: its relationships go everywhere; an active user has all its memberships and team roles reconciled.
func (p *projector) deliverUsers(ctx context.Context, ids []string) error {
	var active []string
	if err := p.db.WithContext(ctx).Raw(`SELECT id::text FROM users WHERE id = ANY(?::uuid[]) AND is_active`, idArray(ids)).Scan(&active).Error; err != nil {
		return fmt.Errorf("read users: %w", err)
	}
	if err := p.forEachBounded(ctx, without(ids, active), p.deleteUser); err != nil {
		return err
	}
	if len(active) == 0 {
		return nil
	}
	var memberships []membershipTarget
	if err := p.db.WithContext(ctx).Raw(`SELECT organization_id::text AS organization_id, user_id::text AS user_id FROM memberships WHERE user_id = ANY(?::uuid[]) ORDER BY 1, 2`, idArray(active)).Scan(&memberships).Error; err != nil {
		return fmt.Errorf("read user memberships: %w", err)
	}
	if err := p.reconcileMembershipsChunked(ctx, memberships); err != nil {
		return err
	}
	var teamMemberships []teamMembershipTarget
	if err := p.db.WithContext(ctx).Raw(`SELECT team_id::text AS team_id, user_id::text AS user_id FROM team_members WHERE user_id = ANY(?::uuid[]) ORDER BY 1, 2`, idArray(active)).Scan(&teamMemberships).Error; err != nil {
		return fmt.Errorf("read user team memberships: %w", err)
	}
	return p.reconcileTeamWorkspaceChunked(ctx, teamWorkspaceTargets{TeamMemberships: teamMemberships})
}

func without(ids, remove []string) []string {
	drop := map[string]bool{}
	for _, id := range remove {
		drop[id] = true
	}
	var out []string
	for _, id := range ids {
		if !drop[id] {
			out = append(out, id)
		}
	}
	return out
}

// forEachBounded runs per-subject deletes ten at a time, stopping at the first window with a failure.
func (p *projector) forEachBounded(ctx context.Context, ids []string, run func(ctx context.Context, id string) error) error {
	return inWindows(ids, func(id string) error { return run(ctx, id) })
}

// groupOutcome is how one attempt at a group ended.
type groupOutcome struct {
	failed    bool
	code      Code
	retryable bool
}

func (d *Delivery) runGroup(ctx context.Context, group deliveryGroup, events []outboxEvent) groupOutcome {
	err := d.run(ctx, group, events)
	if err == nil {
		return groupOutcome{}
	}
	e, ok := AsError(err)
	if !ok {
		// The reconcilers report their own failures as *Error; anything else is one of the PostgreSQL reads around them, transient rather than any event's fault.
		return groupOutcome{failed: true, code: codeProjectionDeliveryFailed, retryable: true}
	}
	// A source row that kept moving will settle: a retry, not a poison, and likeliest on exactly the hot rows the outbox carries.
	return groupOutcome{failed: true, code: e.Code, retryable: e.Retryable || e.Code == codeProjectionUnstable}
}

type failure struct {
	// attributable: the attempt covered this event alone and the code is one an event can cause.
	attributable bool
	code         Code
	retryable    bool
	ids          []uuid.UUID
}

type deliveryOutcome struct {
	delivered []uuid.UUID
	failures  []failure
	// halt is set when delivery hit a transient fault: the rest of the tick is released untried rather than spending retry budget against an instance known to be unreachable.
	halt Code
}

func eventIDs(events []outboxEvent) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	return ids
}

// deliverGroup delivers one group, halving it to isolate the culprit when the failure could be one event's, so a poison event is alone by the time it is charged a permanent failure.
func (d *Delivery) deliverGroup(ctx context.Context, group deliveryGroup, events []outboxEvent, depth int) deliveryOutcome {
	o := d.runGroup(ctx, group, events)
	if !o.failed {
		return deliveryOutcome{delivered: eventIDs(events)}
	}
	splittable := len(events) > 1 && !o.retryable && perEventCodes[o.code] && depth < maxSplitDepth
	if !splittable {
		out := deliveryOutcome{failures: []failure{{attributable: len(events) == 1 && perEventCodes[o.code], code: o.code, retryable: o.retryable, ids: eventIDs(events)}}}
		if o.retryable {
			out.halt = o.code
		}
		return out
	}
	middle := (len(events) + 1) / 2
	left := d.deliverGroup(ctx, group, events[:middle], depth+1)
	if left.halt != "" {
		left.failures = append(left.failures, failure{code: left.halt, retryable: true, ids: eventIDs(events[middle:])})
		return left
	}
	right := d.deliverGroup(ctx, group, events[middle:], depth+1)
	return deliveryOutcome{
		delivered: append(left.delivered, right.delivered...),
		failures:  append(left.failures, right.failures...),
		halt:      right.halt,
	}
}

// deliverAll delivers the groups in order (memberships, organizations, users, teams and workspaces, API keys) and stops at the first transient fault.
func (d *Delivery) deliverAll(ctx context.Context, events []outboxEvent) deliveryOutcome {
	grouped := map[deliveryGroup][]outboxEvent{}
	for _, e := range events {
		g := groupOf[e.TargetType]
		grouped[g] = append(grouped[g], e)
	}
	var out deliveryOutcome
	order := []deliveryGroup{groupMembership, groupOrganization, groupUser, groupTeamWorkspace, groupAPIKey}
	for i, g := range order {
		if len(grouped[g]) == 0 {
			continue
		}
		o := d.deliverGroup(ctx, g, grouped[g], 0)
		out.delivered = append(out.delivered, o.delivered...)
		out.failures = append(out.failures, o.failures...)
		if o.halt != "" {
			var untried []uuid.UUID
			for _, rest := range order[i+1:] {
				untried = append(untried, eventIDs(grouped[rest])...)
			}
			if len(untried) > 0 {
				out.failures = append(out.failures, failure{code: o.halt, retryable: true, ids: untried})
			}
			out.halt = o.halt
			return out
		}
	}
	return out
}

// mergeFailures folds failures with the same verdict so each verdict is one release statement.
func mergeFailures(failures []failure) []failure {
	var merged []failure
	for _, f := range failures {
		i := slices.IndexFunc(merged, func(m failure) bool {
			return m.code == f.code && m.attributable == f.attributable && m.retryable == f.retryable
		})
		if i < 0 {
			merged = append(merged, failure{attributable: f.attributable, code: f.code, retryable: f.retryable, ids: slices.Clone(f.ids)})
			continue
		}
		merged[i].ids = append(merged[i].ids, f.ids...)
	}
	return merged
}

// BatchResult counts one claimed batch.
type BatchResult struct {
	Claimed      int `json:"claimed"`
	Delivered    int `json:"delivered"`
	Failed       int `json:"failed"`
	DeadLettered int `json:"deadLettered"`
}

// processBatch claims, delivers and releases one batch under owner.
func (d *Delivery) processBatch(ctx context.Context, owner string) (BatchResult, error) {
	events, err := claimOutbox(ctx, d.db, owner, outboxBatchSize)
	if err != nil || len(events) == 0 {
		return BatchResult{}, err
	}
	started := time.Now()
	out := d.deliverAll(ctx, events)
	return d.settle(ctx, owner, events, out, time.Since(started))
}

// settle completes the delivered events of a claimed batch and releases the rest with their backoff. It runs under its own context so that a delivery bounded by a deadline that has already passed can still settle its events rather than leave them leased.
func (d *Delivery) settle(ctx context.Context, owner string, events []outboxEvent, out deliveryOutcome, duration time.Duration) (BatchResult, error) {
	if err := markDelivered(ctx, d.db, owner, out.delivered); err != nil {
		return BatchResult{}, err
	}
	deliveredAt := time.Now()
	for _, e := range events {
		if e.IsRevocation && slices.Contains(out.delivered, e.ID) {
			recordRevocationDelivery(ctx, deliveredAt.Sub(e.CreatedAt))
		}
	}
	if len(out.delivered) > 0 {
		recordOutboxDelivery(ctx, len(out.delivered), duration, "delivered")
	}
	res := BatchResult{Claimed: len(events), Delivered: len(out.delivered)}
	codes := map[Code]bool{}
	for _, f := range mergeFailures(out.failures) {
		res.Failed += len(f.ids)
		codes[f.code] = true
		n, err := markFailed(ctx, d.db, owner, f.ids, f.code, f.attributable, f.retryable)
		if err != nil {
			return BatchResult{}, err
		}
		res.DeadLettered += n
	}
	if res.Failed > 0 {
		sorted := make([]string, 0, len(codes))
		for c := range codes {
			sorted = append(sorted, string(c))
		}
		slices.Sort(sorted)
		// Counts and stable codes only, never an event, target or tenant identifier.
		d.log.WarnContext(ctx, "AuthZed projection outbox delivery failed", "component", "authz", "count", res.Failed, "dead_lettered", res.DeadLettered, "error_codes", sorted, "operation", "projection_outbox_delivery", "status", "failed")
		recordOutboxDelivery(ctx, res.Failed, duration, "failed")
	}
	return res, nil
}

// syncDeliveryTimeout bounds the claim and the SpiceDB delivery a request waits for before it answers; past it the events fall back to the outbox. syncSettleTimeout bounds the bookkeeping that follows, on a budget of its own so that a delivery that used all of syncDeliveryTimeout can still release its events. A request therefore waits at most their sum, however long the outbox backlog and however slow SpiceDB.
const (
	syncDeliveryTimeout = 3 * time.Second
	syncSettleTimeout   = time.Second
)

// DeliverAPIKey projects one API key's pending events, the key and its workspace grants, before the request that wrote them answers. /api/v3 and MCP authorize against SpiceDB while v1 and v2 read PostgreSQL, so without this a new key works on v1 and is refused on v3 until the worker's next tick, up to deliveryInterval later. A failure is not the caller's: the events are released to the outbox exactly as a failed batch's are, the worker delivers them, and the failure is logged by its stable code. Nil, with AuthZed disabled, does nothing.
func (d *Delivery) DeliverAPIKey(ctx context.Context, id uuid.UUID) {
	if d == nil {
		return
	}
	d.deliverAggregate(ctx, aggregate{{targetTypes: []string{targetAPIKey, targetAPIKeyWorkspace}, ids: []string{id.String()}}})
}

// The methods below are DeliverAPIKey for the writes that change people's access: they are called once the write has committed, so a revocation reaches SpiceDB before the request answers instead of waiting for the worker. An undelivered revocation older than revocationMaxAge stops every authorization check in the deployment, so leaving one to a worker whose queue is backed up turns one membership change into a 500 for every organization. A failure is handled as DeliverAPIKey's is: the events stay in the outbox for the worker and the request still succeeds. What they promise is that the relationships are written before the answer, not that the next check sees them: with AUTHZED_CONSISTENCY=minimize_latency SpiceDB answers from a revision it may have cached for up to its quantization interval (5 s by default), so a new grant can still be refused for those seconds, where it waited for the worker's backlog before.

// DeliverMemberships projects the pending changes to one user's access: their organization and team memberships, in every organization (a role change, a removal, leaving, the teams they were given or lost), and their own row (a user just created).
func (d *Delivery) DeliverMemberships(ctx context.Context, userID uuid.UUID) {
	if d == nil {
		return
	}
	d.deliverAggregate(ctx, usersAggregate(userID))
}

// DeliverTeam projects the pending changes to one team: the team itself (created, deleted), its members, and the workspace grants that name it.
func (d *Delivery) DeliverTeam(ctx context.Context, teamID uuid.UUID) {
	if d == nil {
		return
	}
	id := []string{teamID.String()}
	d.deliverAggregate(ctx, aggregate{{targetTypes: []string{targetTeam, targetTeamMembership}, ids: id}, {targetTypes: []string{targetWorkspaceTeam}, ids: id, bySecond: true}})
}

// DeliverWorkspace projects the pending changes to one workspace: the workspace itself (created, deleted), the team grants on it, and the API key grants on it.
func (d *Delivery) DeliverWorkspace(ctx context.Context, workspaceID uuid.UUID) {
	if d == nil {
		return
	}
	id := []string{workspaceID.String()}
	d.deliverAggregate(ctx, aggregate{{targetTypes: []string{targetWorkspace, targetWorkspaceTeam}, ids: id}, {targetTypes: []string{targetAPIKeyWorkspace}, ids: id, bySecond: true}})
}

// usersAggregate is the users' own rows and every organization and team membership that names them.
func usersAggregate(userIDs ...uuid.UUID) aggregate {
	ids := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		ids = append(ids, id.String())
	}
	return aggregate{{targetTypes: []string{targetUser}, ids: ids}, {targetTypes: []string{targetMembership, targetTeamMembership}, ids: ids, bySecond: true}}
}

// Organizations names whole organizations for DeliverOrganizations: the organizations and the teams, workspaces and API keys in them. The outbox does not say which organization an event belongs to, so these are read while the rows exist; a deletion reads them before it deletes, in its own transaction or before it.
type Organizations struct {
	ids, teams, workspaces, apiKeys []string
}

// ReadOrganizations reads the teams, workspaces and API keys of the organizations. db may be a transaction.
func ReadOrganizations(ctx context.Context, db *gorm.DB, organizationIDs ...uuid.UUID) (Organizations, error) {
	var o Organizations
	if len(organizationIDs) == 0 {
		return o, nil
	}
	for _, id := range organizationIDs {
		o.ids = append(o.ids, id.String())
	}
	for _, read := range []struct {
		table string
		into  *[]string
	}{{"teams", &o.teams}, {"workspaces", &o.workspaces}, {"api_keys", &o.apiKeys}} {
		// table is one of the constants above.
		if err := db.WithContext(ctx).Raw(`SELECT id::text FROM `+read.table+` WHERE organization_id IN ?`, organizationIDs).Scan(read.into).Error; err != nil {
			return Organizations{}, fmt.Errorf("read the organizations' %s: %w", read.table, err)
		}
	}
	return o, nil
}

// DeliverOrganizations projects the pending changes to whole organizations, created or deleted: each organization's row and memberships, its teams with their members, its workspaces with their team grants, and its API keys with their workspace grants; and, for users, what DeliverMemberships projects. Deleting an organization is the largest revocation the app makes, a row for every member, team, workspace and key, all of which would otherwise wait for the worker.
func (d *Delivery) DeliverOrganizations(ctx context.Context, o Organizations, users ...uuid.UUID) {
	if d == nil {
		return
	}
	a := aggregate{
		{targetTypes: []string{targetOrganization, targetMembership}, ids: o.ids},
		{targetTypes: []string{targetTeam, targetTeamMembership}, ids: o.teams},
		{targetTypes: []string{targetWorkspace, targetWorkspaceTeam}, ids: o.workspaces},
		{targetTypes: []string{targetAPIKey, targetAPIKeyWorkspace}, ids: o.apiKeys},
	}
	d.deliverAggregate(ctx, append(a, usersAggregate(users...)...))
}

func (d *Delivery) deliverAggregate(ctx context.Context, a aggregate) {
	// Detached from the request, so a client that goes away mid-request cannot leave the claimed events leased for a whole outboxLease; bounded instead by the deadlines below, so the request never waits on the outbox backlog (the claim filters the whole pending set) or on a stalled SpiceDB.
	ctx = context.WithoutCancel(ctx)
	deliverCtx, cancel := context.WithTimeout(ctx, syncDeliveryTimeout)
	defer cancel()
	// One settle deadline for every batch, syncSettleTimeout past the delivery's: the bookkeeping of the last batch still has its own second, and the whole call stays within the sum however many batches it takes.
	deadline, _ := deliverCtx.Deadline()
	settleCtx, cancelSettle := context.WithDeadline(ctx, deadline.Add(syncSettleTimeout))
	defer cancelSettle()
	owner := uuid.NewString()
	var err error
	// An aggregate can hold more than one claim: an organization's deletion is a revocation for every member. Batches continue until a claim comes back short or the delivery deadline passes; what is left goes to the worker.
	for {
		var events []outboxEvent
		events, err = claimAggregate(deliverCtx, d.db, owner, a)
		if err != nil || len(events) == 0 {
			break
		}
		started := time.Now()
		out := d.deliverAll(deliverCtx, events)
		var res BatchResult
		// A failed batch would fail the next one the same way; its events are back in the outbox for the worker.
		if res, err = d.settle(settleCtx, owner, events, out, time.Since(started)); err != nil || res.Failed > 0 || len(events) < outboxBatchSize || deliverCtx.Err() != nil {
			break
		}
	}
	if err != nil {
		// Only PostgreSQL fails here, the claim or the bookkeeping, and its text can carry identifiers, so only the stable code is logged. A claim cut off by its deadline leased nothing, and whatever the bookkeeping left unsettled is claimable again once its lease expires; either way the worker delivers the events.
		d.log.WarnContext(ctx, "AuthZed synchronous projection failed", "component", "authz", "operation", "projection_sync_delivery", "status", "failed", "error_code", codeProjectionDeliveryFailed)
	}
}

// DrainResult totals a drain and reports what is left.
type DrainResult struct {
	BatchResult
	Remaining int64  `json:"remaining"`
	Status    string `json:"status"`
}

// Drain processes up to maxBatches batches under one lease owner. It stops when a batch claims nothing or delivers nothing: delivered rows are complete and failed ones back off, so every productive batch shrinks the claimable set.
func (d *Delivery) Drain(ctx context.Context, maxBatches int) (DrainResult, error) {
	owner := uuid.NewString()
	var total DrainResult
	for range maxBatches {
		r, err := d.processBatch(ctx, owner)
		if err != nil {
			return total, err
		}
		total.Claimed += r.Claimed
		total.Delivered += r.Delivered
		total.Failed += r.Failed
		total.DeadLettered += r.DeadLettered
		if r.Claimed == 0 || r.Delivered == 0 {
			break
		}
	}
	status, err := outboxStatus(ctx, d.db)
	if err != nil {
		return total, err
	}
	recordOutboxStatus(ctx, status)
	total.Remaining = status.Pending
	total.Status = "partial"
	if status.Pending == 0 {
		total.Status = "drained"
	}
	return total, nil
}
