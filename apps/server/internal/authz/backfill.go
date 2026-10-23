package authz

import (
	"context"
	"log/slog"
	"sync"

	"gorm.io/gorm"
)

// Reconciliation limits.
const (
	// MaxPrune is the most orphaned records one run may prune. A larger count is a symptom (wrong endpoint, wrong database, a restore in progress), so a unit over it prunes nothing and the run reports instead.
	MaxPrune = 500
	// maxObservedPerUnit bounds the relationships one unit's observation holds in memory.
	maxObservedPerUnit = 20_000
	// maxTrackedOrphanRefs bounds the sweep's de-duplication set; past it the count may double-count and the run says so.
	maxTrackedOrphanRefs = 50_000
	// maxReportedEntries caps each reported list; the counters keep the true totals.
	maxReportedEntries = 100
)

// BackfillScope is the unit list of a run: every organization (resumable after an id), one organization, or one workspace.
type BackfillScope struct {
	Kind                string
	AfterOrganizationID string
	OrganizationID      string
	WorkspaceID         string
}

const (
	ScopeAll          = "all"
	ScopeOrganization = "organization"
	ScopeWorkspace    = "workspace"
)

// BackfillRequest configures one run. A dry run observes and reports; Apply writes what PostgreSQL says. Prune (only with Apply) additionally reconciles records observed only in SpiceDB, which removes their relationships.
type BackfillRequest struct {
	Apply    bool
	Prune    bool
	MaxPrune int
	Scope    BackfillScope
}

// BackfillCounters are the run's totals.
type BackfillCounters struct {
	Failed                int `json:"failed"`
	Ignored               int `json:"ignored"`
	Invalid               int `json:"invalid"`
	MismatchedParents     int `json:"mismatchedParents"`
	MismatchedPermissions int `json:"mismatchedPermissions"`
	Missing               int `json:"missing"`
	Orphaned              int `json:"orphaned"`
	Pruned                int `json:"pruned"`
	Reconciled            int `json:"reconciled"`
	Scanned               int `json:"scanned"`
	Skipped               int `json:"skipped"`
	Unmanaged             int `json:"unmanaged"`
}

// BackfillFailure is one unit that could not be observed or reconciled; OrganizationID is empty when no tenant can be named.
type BackfillFailure struct {
	Attempts       int    `json:"attempts"`
	Code           Code   `json:"code"`
	OrganizationID string `json:"organizationId"`
	Retryable      bool   `json:"retryable"`
}

// Backfill statuses: failed if any unit failed, drifted if anything is left unaccounted for, reconciled otherwise.
const (
	StatusReconciled = "reconciled"
	StatusDrifted    = "drifted"
	StatusFailed     = "failed"
)

// BackfillResult is the run's report.
type BackfillResult struct {
	// CompletedAtSnapshot is a SpiceDB revision read after every write of an applying run, or null.
	CompletedAtSnapshot   *string              `json:"completedAtSnapshot"`
	Counters              BackfillCounters     `json:"counters"`
	Failures              []BackfillFailure    `json:"failures"`
	LastOrganizationID    *string              `json:"lastOrganizationId"`
	MismatchedParents     []ParentEdge         `json:"mismatchedParents"`
	MismatchedPermissions []PermissionMismatch `json:"mismatchedPermissions"`
	Mode                  string               `json:"mode"`
	OrphanScope           string               `json:"orphanScope"`
	Orphans               []SourceRef          `json:"orphans"`
	Scope                 string               `json:"scope"`
	Status                string               `json:"status"`
	Truncated             bool                 `json:"truncated"`
	Unmanaged             []RelationshipRef    `json:"unmanaged"`
}

// backfillApply is the only way the engine mutates: a dry run gets no-ops, so it is inert by construction.
type backfillApply struct {
	memberships   func(context.Context, []membershipTarget) error
	teamWorkspace func(context.Context, teamWorkspaceTargets) error
	apiKeys       func(context.Context, apiKeyTargets) error
}

func noopApply() backfillApply {
	return backfillApply{
		memberships:   func(context.Context, []membershipTarget) error { return nil },
		teamWorkspace: func(context.Context, teamWorkspaceTargets) error { return nil },
		apiKeys:       func(context.Context, apiKeyTargets) error { return nil },
	}
}

func projectorApply(p *projector) backfillApply {
	return backfillApply{memberships: p.reconcileMembershipsChunked, teamWorkspace: p.reconcileTeamWorkspaceChunked, apiKeys: p.reconcileAPIKeysChunked}
}

// Backfill audits and repairs SpiceDB against PostgreSQL, one organization at a time.
type Backfill struct {
	source    backfillSource
	client    *Client
	projector *projector
}

func NewBackfill(db *gorm.DB, client *Client, log *slog.Logger) *Backfill {
	return &Backfill{source: backfillSource{db: db}, client: client, projector: newProjector(db, client, log)}
}

type runContext struct {
	ctx     context.Context
	b       *Backfill
	apply   backfillApply
	dryRun  bool
	pruning bool
	// maxPrune is clamped to 1..MaxPrune.
	maxPrune int
	// ownsOrphans: a narrow scope counts its own orphans; a full scope leaves that to the streamed sweep, which sees strictly more.
	ownsOrphans bool
	s           *BackfillResult
}

// Run executes one reconciliation run. It never stops at a failed unit: each failure is recorded and the run moves on, so one tenant's outage cannot abort the sweep.
func (b *Backfill) Run(ctx context.Context, req BackfillRequest) BackfillResult {
	res := BackfillResult{Mode: "dry_run", Scope: req.Scope.Kind, OrphanScope: "known_resources"}
	if req.Apply {
		res.Mode = "apply"
	}
	if req.Scope.Kind == ScopeAll {
		res.OrphanScope = "all"
	}
	rc := &runContext{
		ctx:         ctx,
		b:           b,
		apply:       noopApply(),
		dryRun:      !req.Apply,
		pruning:     req.Prune && req.Apply,
		maxPrune:    max(1, min(req.MaxPrune, MaxPrune)),
		ownsOrphans: req.Scope.Kind != ScopeAll,
		s:           &res,
	}
	if req.Apply {
		rc.apply = projectorApply(b.projector)
	}

	switch req.Scope.Kind {
	case ScopeWorkspace:
		rc.processWorkspace(req.Scope.WorkspaceID)
	case ScopeOrganization:
		exists, err := b.source.organizationExists(ctx, req.Scope.OrganizationID)
		switch {
		case err != nil:
			rc.recordFailure("", err)
		case !exists:
			rc.recordFailure("", newError(CodeNotFound, "backfill_scope", false, 0))
		default:
			rc.processOrganization(req.Scope.OrganizationID)
		}
	default:
		// The sweep runs even when enumeration broke off: it is the only thing that finds a resource no organization reaches.
		rc.enumerateOrganizations(req.Scope.AfterOrganizationID)
		if err := rc.sweepOrphans(); err != nil {
			res.Truncated = true
			rc.recordFailure("", err)
		}
	}

	if req.Apply && res.Counters.Failed == 0 {
		// A revision read after every write, usable as a freshness floor; null rather than one that might predate the writes.
		if page, err := b.client.ReadRelationships(ctx, RelationshipFilter{ResourceType: typeOrganization}, 1, ""); err == nil && page.Revision != "" {
			res.CompletedAtSnapshot = &page.Revision
		}
	}
	res.Status = runStatus(res)
	res.Failures = nonNil(res.Failures)
	res.MismatchedParents = nonNil(res.MismatchedParents)
	res.MismatchedPermissions = nonNil(res.MismatchedPermissions)
	res.Orphans = nonNil(res.Orphans)
	res.Unmanaged = nonNil(res.Unmanaged)
	return res
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// runStatus: a failure outranks drift, and every category of unrepaired state counts as drift, including the ones this tool deliberately leaves alone (invalid grants, unmanaged relationships), because a clean exit must mean nothing is unaccounted for.
func runStatus(r BackfillResult) string {
	c := r.Counters
	if c.Failed > 0 {
		return StatusFailed
	}
	if c.Orphaned > c.Pruned || c.Missing > 0 || c.MismatchedParents > 0 || c.MismatchedPermissions > 0 || c.Invalid > 0 || c.Unmanaged > 0 || r.Truncated {
		return StatusDrifted
	}
	return StatusReconciled
}

func pushCapped[T any](list *[]T, items ...T) {
	room := maxReportedEntries - len(*list)
	if room <= 0 {
		return
	}
	*list = append(*list, items[:min(room, len(items))]...)
}

func (rc *runContext) recordFailure(organizationID string, err error) {
	e := normalize(err, "backfill")
	rc.s.Counters.Failed++
	pushCapped(&rc.s.Failures, BackfillFailure{Attempts: e.Attempts, Code: e.Code, OrganizationID: organizationID, Retryable: e.Retryable})
}

// recordSummary tallies what an observation implies and verifies the organizations its resources claim.
func (rc *runContext) recordSummary(o observation) error {
	rc.s.Counters.Ignored += o.ignored
	rc.s.Counters.Unmanaged += len(o.unmanaged)
	pushCapped(&rc.s.Unmanaged, o.unmanaged...)
	mismatched, err := rc.b.source.mismatchedParentEdges(rc.ctx, o.parentEdges)
	if err != nil {
		return err
	}
	rc.s.Counters.MismatchedParents += len(mismatched)
	pushCapped(&rc.s.MismatchedParents, mismatched...)
	return nil
}

// pruneDecision: overBudget is distinct from an empty list, because "nothing to prune" and "too much to prune safely" lead to different decisions.
type pruneDecision struct {
	overBudget bool
	refs       []SourceRef
}

func (rc *runContext) recordScopedOrphans(o observation) (pruneDecision, error) {
	missing, err := rc.b.source.missingSourceRefs(rc.ctx, o.sourceRefs)
	if err != nil {
		return pruneDecision{}, err
	}
	rc.s.Counters.Orphaned += len(missing)
	pushCapped(&rc.s.Orphans, missing...)
	if len(missing) > rc.maxPrune {
		rc.s.Counters.Skipped++
		return pruneDecision{overBudget: true}, nil
	}
	if !rc.pruning {
		return pruneDecision{}, nil
	}
	return pruneDecision{refs: missing}, nil
}

func (rc *runContext) recordDryRunDrift(expected []Relationship, expectedRefs []SourceRef, o observation) {
	rc.s.Counters.Missing += len(unprojectedRefs(expectedRefs, o.sourceRefs))
	m := mismatchedPermissions(expected, o.managed)
	rc.s.Counters.MismatchedPermissions += len(m)
	pushCapped(&rc.s.MismatchedPermissions, m...)
}

// readAll reads every relationship matching filter at one revision, or fails: a partial observation taken for a complete one would make live access look orphaned.
func (rc *runContext) readAll(filter RelationshipFilter) ([]Relationship, error) {
	var all []Relationship
	err := rc.forEachPage(filter, func(page []Relationship) error {
		if len(all)+len(page) > maxObservedPerUnit {
			return newError(CodeLimitExceeded, "read_all_relationships", false, 0)
		}
		all = append(all, page...)
		return nil
	})
	return all, err
}

// forEachPage streams every page of a read. The cursor must advance and every page must report the revision of the first: a torn read is aborted rather than silently mixing two views.
func (rc *runContext) forEachPage(filter RelationshipFilter, onPage func([]Relationship) error) error {
	cursor, revision := "", ""
	for {
		page, err := rc.b.client.ReadRelationships(rc.ctx, filter, maxRelationshipReads, cursor)
		if err != nil {
			return err
		}
		if revision != "" && page.Revision != "" && page.Revision != revision {
			return newError(CodeAborted, "for_each_relationship_page", true, 0)
		}
		if cursor != "" && page.Cursor == cursor {
			return newError(CodeInternal, "for_each_relationship_page", false, 0)
		}
		if page.Revision != "" {
			revision = page.Revision
		}
		if len(page.Relationships) > 0 {
			if err := onPage(page.Relationships); err != nil {
				return err
			}
		}
		if page.Cursor == "" {
			return nil
		}
		cursor = page.Cursor
	}
}

func organizationSourceRefs(src organizationSource) []SourceRef {
	var refs []SourceRef
	for _, m := range src.memberships {
		refs = append(refs, SourceRef{Kind: refMembership, OrganizationID: m.OrganizationID, UserID: m.UserID})
	}
	for _, id := range src.teamIDs {
		refs = append(refs, SourceRef{Kind: refTeam, TeamID: id})
	}
	for _, m := range src.teamMemberships {
		refs = append(refs, SourceRef{Kind: refTeamMembership, TeamID: m.TeamID, UserID: m.UserID})
	}
	for _, id := range src.workspaceIDs {
		refs = append(refs, SourceRef{Kind: refWorkspace, WorkspaceID: id})
	}
	for _, g := range src.workspaceTeams {
		refs = append(refs, SourceRef{Kind: refWorkspaceTeamGrant, TeamID: g.TeamID, WorkspaceID: g.WorkspaceID})
	}
	for _, id := range src.apiKeyIDs {
		refs = append(refs, SourceRef{Kind: refAPIKey, APIKeyID: id})
	}
	for _, g := range src.apiKeyGrants {
		refs = append(refs, SourceRef{Kind: refAPIKeyWorkspaceGrant, APIKeyID: g.APIKeyID, WorkspaceID: g.WorkspaceID})
	}
	return refs
}

// reconcileTargets are the lists the three reconcilers take between them.
type reconcileTargets struct {
	memberships   []membershipTarget
	teamWorkspace teamWorkspaceTargets
	apiKeys       apiKeyTargets
}

// add turns refs into targets. A record PostgreSQL does not hold becomes a target, never a delete instruction: the reconciler re-reads PostgreSQL, so a record recreated meanwhile is written rather than removed.
func (t *reconcileTargets) add(refs []SourceRef) {
	for _, r := range refs {
		switch r.Kind {
		case refAPIKey:
			t.apiKeys.APIKeyIDs = append(t.apiKeys.APIKeyIDs, r.APIKeyID)
		case refAPIKeyWorkspaceGrant:
			t.apiKeys.Grants = append(t.apiKeys.Grants, apiKeyWorkspaceTarget{r.APIKeyID, r.WorkspaceID})
		case refMembership:
			t.memberships = append(t.memberships, membershipTarget{r.OrganizationID, r.UserID})
		case refTeam:
			t.teamWorkspace.TeamIDs = append(t.teamWorkspace.TeamIDs, r.TeamID)
		case refTeamMembership:
			t.teamWorkspace.TeamMemberships = append(t.teamWorkspace.TeamMemberships, teamMembershipTarget{r.TeamID, r.UserID})
		case refWorkspace:
			t.teamWorkspace.WorkspaceIDs = append(t.teamWorkspace.WorkspaceIDs, r.WorkspaceID)
		case refWorkspaceTeamGrant:
			t.teamWorkspace.WorkspaceTeams = append(t.teamWorkspace.WorkspaceTeams, workspaceTeamTarget{r.WorkspaceID, r.TeamID})
		}
	}
}

// reconcile runs the three reconcilers and stops at the first failure: continuing would spend more retry budget against an instance already known to be failing.
func (rc *runContext) reconcile(t reconcileTargets) error {
	if err := rc.apply.memberships(rc.ctx, t.memberships); err != nil {
		return err
	}
	if err := rc.apply.teamWorkspace(rc.ctx, t.teamWorkspace); err != nil {
		return err
	}
	return rc.apply.apiKeys(rc.ctx, t.apiKeys)
}

// observeOrganization reads the relationships on the organization's own resources, ten reads at a time. Resources whose rows are gone are unreachable from here, which is why a narrow scope reports orphanScope known_resources.
func (rc *runContext) observeOrganization(organizationID string, src organizationSource) (pruneDecision, error) {
	filters := []RelationshipFilter{{ResourceType: typeOrganization, ResourceID: organizationID}}
	for _, id := range src.teamIDs {
		filters = append(filters, RelationshipFilter{ResourceType: typeTeam, ResourceID: id})
	}
	for _, id := range src.workspaceIDs {
		filters = append(filters, RelationshipFilter{ResourceType: typeWorkspace, ResourceID: id})
	}
	for _, id := range src.apiKeyIDs {
		filters = append(filters, RelationshipFilter{ResourceType: typeAPIKey, ResourceID: id})
	}
	var (
		mu       sync.Mutex
		observed []Relationship
	)
	if err := inWindows(filters, func(f RelationshipFilter) error {
		rels, err := rc.readAll(f)
		mu.Lock()
		observed = append(observed, rels...)
		mu.Unlock()
		return err
	}); err != nil {
		return pruneDecision{}, err
	}
	o := summarize(observed)
	if rc.dryRun {
		rc.recordDryRunDrift(src.expected, organizationSourceRefs(src), o)
	}
	if !rc.ownsOrphans {
		return pruneDecision{}, nil
	}
	if err := rc.recordSummary(o); err != nil {
		return pruneDecision{}, err
	}
	return rc.recordScopedOrphans(o)
}

func (rc *runContext) processOrganization(organizationID string) {
	rc.s.Counters.Scanned++
	rc.s.LastOrganizationID = &organizationID

	src, err := rc.b.source.organization(rc.ctx, organizationID)
	if err != nil {
		rc.recordFailure(organizationID, err)
		return
	}
	rc.s.Counters.Invalid += len(src.invalidTeam) + len(src.invalidAPIKey)

	// A full applying sweep skips the observation: its writes converge the missing direction anyway.
	var decision pruneDecision
	if rc.ownsOrphans || rc.dryRun {
		if decision, err = rc.observeOrganization(organizationID, src); err != nil {
			rc.s.Truncated = true
			rc.recordFailure(organizationID, err)
			return
		}
	}
	if rc.dryRun {
		return
	}
	t := reconcileTargets{
		memberships:   src.memberships,
		teamWorkspace: teamWorkspaceTargets{TeamIDs: src.teamIDs, TeamMemberships: src.teamMemberships, WorkspaceIDs: src.workspaceIDs, WorkspaceTeams: src.workspaceTeams},
		apiKeys:       apiKeyTargets{APIKeyIDs: src.apiKeyIDs, Grants: src.apiKeyGrants},
	}
	t.add(decision.refs)
	if err := rc.reconcile(t); err != nil {
		rc.recordFailure(organizationID, err)
		return
	}
	// Counted after the reconcile, so a failed one never reports relationships as pruned that are still there.
	rc.s.Counters.Pruned += len(decision.refs)
	rc.s.Counters.Reconciled++
}

func (rc *runContext) enumerateOrganizations(after string) {
	cursor := after
	for {
		ids, err := rc.b.source.organizationIDPage(rc.ctx, cursor)
		if err != nil {
			// Recorded rather than returned, so the report survives with lastOrganizationId to resume from.
			rc.s.Truncated = true
			rc.recordFailure("", err)
			return
		}
		if len(ids) == 0 {
			return
		}
		// Sequential: lastOrganizationId is the resume cursor, so out-of-order processing could skip a failed organization on resume.
		for _, id := range ids {
			rc.processOrganization(id)
		}
		cursor = ids[len(ids)-1]
	}
}

// sweepOrphans streams every managed resource type to find records PostgreSQL no longer holds at all. It counts the whole sweep before deleting anything, and deletes only if the confirmed total fits the budget: enforcing the cap page by page would delete what fit and halt on the page that did not, which is exactly what the cap exists to prevent.
func (rc *runContext) sweepOrphans() error {
	seen := map[string]bool{}
	var prunable []SourceRef
	orphans := 0
	for _, resourceType := range managedResourceTypes {
		err := rc.forEachPage(RelationshipFilter{ResourceType: resourceType}, func(page []Relationship) error {
			o := summarize(page)
			if err := rc.recordSummary(o); err != nil {
				return err
			}
			missing, err := rc.b.source.missingSourceRefs(rc.ctx, o.sourceRefs)
			if err != nil {
				return err
			}
			var fresh []SourceRef
			for _, r := range missing {
				k := r.key()
				if seen[k] {
					continue
				}
				if len(seen) >= maxTrackedOrphanRefs {
					rc.s.Truncated = true
				} else {
					seen[k] = true
				}
				fresh = append(fresh, r)
			}
			rc.s.Counters.Orphaned += len(fresh)
			pushCapped(&rc.s.Orphans, fresh...)
			orphans += len(fresh)
			prunable = append(prunable, fresh[:min(len(fresh), max(0, rc.maxPrune-len(prunable)))]...)
			return nil
		})
		if err != nil {
			return err
		}
	}
	if !rc.pruning || orphans == 0 {
		return nil
	}
	if orphans > rc.maxPrune {
		rc.s.Counters.Skipped++
		return nil
	}
	var t reconcileTargets
	t.add(prunable)
	if err := rc.reconcile(t); err != nil {
		rc.recordFailure("", err)
		return nil
	}
	rc.s.Counters.Pruned += len(prunable)
	return nil
}

func workspaceSourceRefs(src workspaceSource, workspaceID string) []SourceRef {
	if !src.exists {
		return nil
	}
	var refs []SourceRef
	for _, g := range src.workspaceTeams {
		refs = append(refs, SourceRef{Kind: refWorkspaceTeamGrant, TeamID: g.TeamID, WorkspaceID: g.WorkspaceID})
	}
	for _, g := range src.apiKeyGrants {
		refs = append(refs, SourceRef{Kind: refAPIKeyWorkspaceGrant, APIKeyID: g.APIKeyID, WorkspaceID: g.WorkspaceID})
	}
	return append(refs, SourceRef{Kind: refWorkspace, WorkspaceID: workspaceID})
}

// withinWorkspace withholds prune targets whose principal (the team or key a grant names) has no row: reconciling those deletes subject-wide, reaching other tenants on the strength of one orphan here. They stay counted, so the run reports drifted and asks for a wider scope.
func (rc *runContext) withinWorkspace(refs []SourceRef) ([]SourceRef, error) {
	principal := func(r SourceRef) (SourceRef, bool) {
		switch r.Kind {
		case refWorkspaceTeamGrant:
			return SourceRef{Kind: refTeam, TeamID: r.TeamID}, true
		case refAPIKeyWorkspaceGrant:
			return SourceRef{Kind: refAPIKey, APIKeyID: r.APIKeyID}, true
		}
		return SourceRef{}, false
	}
	var principals []SourceRef
	for _, r := range refs {
		if p, ok := principal(r); ok {
			principals = append(principals, p)
		}
	}
	if len(principals) == 0 {
		return refs, nil
	}
	missing, err := rc.b.source.missingSourceRefs(rc.ctx, principals)
	if err != nil {
		return nil, err
	}
	gone := map[string]bool{}
	for _, m := range missing {
		gone[m.key()] = true
	}
	var kept []SourceRef
	for _, r := range refs {
		if p, ok := principal(r); ok && gone[p.key()] {
			continue
		}
		kept = append(kept, r)
	}
	return kept, nil
}

// processWorkspace reconciles one workspace's grants. The API keys holding grants on it are reconciled in full, which is convergent; deletion is held to the workspace by withinWorkspace.
func (rc *runContext) processWorkspace(workspaceID string) {
	rc.s.Counters.Scanned++
	src, err := rc.b.source.workspace(rc.ctx, workspaceID)
	if err != nil {
		rc.recordFailure("", err)
		return
	}
	organizationID := src.organizationID
	rc.s.Counters.Invalid += len(src.invalidTeam) + len(src.invalidAPIKey)

	decision, err := func() (pruneDecision, error) {
		observed, err := rc.readAll(RelationshipFilter{ResourceType: typeWorkspace, ResourceID: workspaceID})
		if err != nil {
			return pruneDecision{}, err
		}
		o := summarize(observed)
		if err := rc.recordSummary(o); err != nil {
			return pruneDecision{}, err
		}
		if rc.dryRun {
			rc.recordDryRunDrift(src.expected, workspaceSourceRefs(src, workspaceID), o)
		}
		d, err := rc.recordScopedOrphans(o)
		if err != nil {
			return d, err
		}
		d.refs, err = rc.withinWorkspace(d.refs)
		return d, err
	}()
	if err != nil {
		rc.s.Truncated = true
		rc.recordFailure(organizationID, err)
		return
	}
	if rc.dryRun {
		return
	}
	// Naming a workspace whose row is gone removes every relationship on it, which is a prune: it needs the same permission and budget as any other.
	var workspaceIDs []string
	if src.exists || (rc.pruning && !decision.overBudget) {
		workspaceIDs = []string{workspaceID}
	}
	t := reconcileTargets{
		teamWorkspace: teamWorkspaceTargets{WorkspaceIDs: workspaceIDs, WorkspaceTeams: src.workspaceTeams},
		apiKeys:       apiKeyTargets{Grants: src.apiKeyGrants},
	}
	t.add(decision.refs)
	if err := rc.reconcile(t); err != nil {
		rc.recordFailure(organizationID, err)
		return
	}
	rc.s.Counters.Pruned += len(decision.refs)
	rc.s.Counters.Reconciled++
}
