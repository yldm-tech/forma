package authz

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func event(targetType string, revocation bool) outboxEvent {
	e := outboxEvent{ID: uuid.New(), TargetType: targetType, PrimaryID: uuid.NewString(), IsRevocation: revocation, CreatedAt: time.Now()}
	if pairTargets[targetType] {
		e.SecondaryID = uuid.NewString()
	}
	return e
}

type fakeRun struct {
	calls [][]outboxEvent
	fail  func(group deliveryGroup, events []outboxEvent) error
}

func (f *fakeRun) run(_ context.Context, group deliveryGroup, events []outboxEvent) error {
	f.calls = append(f.calls, events)
	if f.fail == nil {
		return nil
	}
	return f.fail(group, events)
}

func fakeDelivery(f *fakeRun) *Delivery {
	return &Delivery{log: discardLog(), run: f.run}
}

func ids(events ...outboxEvent) []uuid.UUID { return eventIDs(events) }

func sameIDs(a, b []uuid.UUID) bool {
	key := func(id uuid.UUID) string { return id.String() }
	as, bs := make([]string, 0, len(a)), make([]string, 0, len(b))
	for _, id := range a {
		as = append(as, key(id))
	}
	for _, id := range b {
		bs = append(bs, key(id))
	}
	slices.Sort(as)
	slices.Sort(bs)
	return slices.Equal(as, bs)
}

func TestDeliveryGroupsEveryTargetTypeIntoItsReconciler(t *testing.T) {
	events := []outboxEvent{
		event(targetAPIKeyWorkspace, false), event(targetMembership, false), event(targetWorkspace, true),
		event(targetUser, false), event(targetOrganization, true), event(targetTeam, false),
		event(targetTeamMembership, false), event(targetWorkspaceTeam, false), event(targetAPIKey, false),
	}
	var groups []deliveryGroup
	f := &fakeRun{fail: func(g deliveryGroup, _ []outboxEvent) error { groups = append(groups, g); return nil }}
	out := fakeDelivery(f).deliverAll(context.Background(), events)
	if want := []deliveryGroup{groupMembership, groupOrganization, groupUser, groupTeamWorkspace, groupAPIKey}; !slices.Equal(groups, want) {
		t.Fatalf("groups = %v, want %v", groups, want)
	}
	if len(f.calls[3]) != 4 || len(f.calls[4]) != 2 || !sameIDs(out.delivered, ids(events...)) || len(out.failures) != 0 {
		t.Fatalf("calls %v, outcome %+v", f.calls, out)
	}
}

func TestDeliveryKeepsGoingWhenAFailureIsLocalToOneGroup(t *testing.T) {
	membership, apiKey := event(targetMembership, false), event(targetAPIKey, false)
	f := &fakeRun{fail: func(g deliveryGroup, _ []outboxEvent) error {
		if g == groupMembership {
			return newError(CodeUnauthenticated, "write_relationships", false, 1)
		}
		return nil
	}}
	out := fakeDelivery(f).deliverAll(context.Background(), []outboxEvent{membership, apiKey})
	if !sameIDs(out.delivered, ids(apiKey)) || out.halt != "" || len(out.failures) != 1 {
		t.Fatalf("outcome %+v", out)
	}
	// A lone event is not blamed for a failure that describes the instance.
	if f := out.failures[0]; f.attributable || f.retryable || f.code != CodeUnauthenticated {
		t.Fatalf("failure %+v", f)
	}
}

func TestDeliveryHaltsOnATransientFaultAndReleasesTheRestUnblamed(t *testing.T) {
	membership, org, apiKey := event(targetMembership, true), event(targetOrganization, true), event(targetAPIKey, false)
	f := &fakeRun{fail: func(g deliveryGroup, _ []outboxEvent) error {
		if g == groupMembership {
			return newError(CodeUnavailable, "write_relationships", true, 3)
		}
		return nil
	}}
	out := fakeDelivery(f).deliverAll(context.Background(), []outboxEvent{membership, org, apiKey})
	if len(f.calls) != 1 || out.halt != CodeUnavailable || len(out.delivered) != 0 {
		t.Fatalf("calls %d, outcome %+v", len(f.calls), out)
	}
	merged := mergeFailures(out.failures)
	if len(merged) != 1 || !merged[0].retryable || merged[0].attributable || !sameIDs(merged[0].ids, ids(membership, org, apiKey)) {
		t.Fatalf("failures %+v", merged)
	}
}

func TestDeliveryTreatsAnUnstableSnapshotAsTransient(t *testing.T) {
	f := &fakeRun{fail: func(deliveryGroup, []outboxEvent) error { return errUnstable() }}
	out := fakeDelivery(f).deliverAll(context.Background(), []outboxEvent{event(targetTeam, false)})
	if len(out.failures) != 1 || !out.failures[0].retryable || out.failures[0].attributable || out.halt != codeProjectionUnstable {
		t.Fatalf("outcome %+v", out)
	}
}

func TestDeliveryTreatsAnUntypedFailureAsTransientDeliveryFailure(t *testing.T) {
	f := &fakeRun{fail: func(deliveryGroup, []outboxEvent) error { return errors.New("read users: connection reset") }}
	out := fakeDelivery(f).deliverAll(context.Background(), []outboxEvent{event(targetUser, false)})
	if len(out.failures) != 1 || out.failures[0].code != codeProjectionDeliveryFailed || !out.failures[0].retryable {
		t.Fatalf("outcome %+v", out)
	}
}

func TestDeliveryIsolatesTheEventAPerEventFailureIsAttributableTo(t *testing.T) {
	events := []outboxEvent{event(targetAPIKey, false), event(targetAPIKey, false), event(targetAPIKey, false), event(targetAPIKey, false), event(targetAPIKey, false)}
	poison := events[3]
	f := &fakeRun{fail: func(_ deliveryGroup, batch []outboxEvent) error {
		if slices.ContainsFunc(batch, func(e outboxEvent) bool { return e.ID == poison.ID }) {
			return newError(CodeInvalidRequest, "write_relationships", false, 1)
		}
		return nil
	}}
	out := fakeDelivery(f).deliverAll(context.Background(), events)
	if !sameIDs(out.delivered, ids(events[0], events[1], events[2], events[4])) || len(out.failures) != 1 {
		t.Fatalf("outcome %+v", out)
	}
	if f := out.failures[0]; !f.attributable || f.retryable || !sameIDs(f.ids, ids(poison)) {
		t.Fatalf("failure %+v", f)
	}
}

func TestDeliveryDoesNotSplitAGroupForAnInstanceFailure(t *testing.T) {
	events := []outboxEvent{event(targetTeam, false), event(targetWorkspace, false), event(targetTeamMembership, false)}
	f := &fakeRun{fail: func(deliveryGroup, []outboxEvent) error {
		return newError(CodeInternal, "write_relationships", false, 1)
	}}
	out := fakeDelivery(f).deliverAll(context.Background(), events)
	if len(f.calls) != 1 || len(out.failures) != 1 || out.failures[0].attributable || !sameIDs(out.failures[0].ids, ids(events...)) {
		t.Fatalf("calls %d, outcome %+v", len(f.calls), out)
	}
}

func TestDeliveryStopsSplittingAtTheFirstTransientFault(t *testing.T) {
	events := []outboxEvent{event(targetAPIKey, false), event(targetAPIKey, false), event(targetAPIKey, false), event(targetAPIKey, false)}
	f := &fakeRun{fail: func(_ deliveryGroup, batch []outboxEvent) error {
		if len(batch) == len(events) {
			return newError(CodeInvalidRequest, "write_relationships", false, 1)
		}
		return newError(CodeTimeout, "write_relationships", true, 3)
	}}
	out := fakeDelivery(f).deliverAll(context.Background(), events)
	merged := mergeFailures(out.failures)
	if out.halt != CodeTimeout || len(f.calls) != 2 || len(merged) != 1 || !sameIDs(merged[0].ids, ids(events...)) || merged[0].attributable {
		t.Fatalf("calls %d, outcome %+v", len(f.calls), out)
	}
}

func TestMergeFailuresFoldsEqualVerdicts(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	merged := mergeFailures([]failure{
		{code: CodeUnavailable, retryable: true, ids: []uuid.UUID{a}},
		{code: CodeInvalidRequest, attributable: true, ids: []uuid.UUID{b}},
		{code: CodeUnavailable, retryable: true, ids: []uuid.UUID{c}},
	})
	if len(merged) != 2 || !sameIDs(merged[0].ids, []uuid.UUID{a, c}) || !sameIDs(merged[1].ids, []uuid.UUID{b}) {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestOutboxEventValidity(t *testing.T) {
	id := uuid.NewString()
	cases := []struct {
		e    outboxEvent
		want bool
	}{
		{outboxEvent{TargetType: targetUser, PrimaryID: id}, true},
		{outboxEvent{TargetType: targetMembership, PrimaryID: id, SecondaryID: id}, true},
		{outboxEvent{TargetType: targetMembership, PrimaryID: id}, false},
		{outboxEvent{TargetType: targetUser, PrimaryID: id, SecondaryID: id}, false},
		{outboxEvent{TargetType: targetUser, PrimaryID: "not-a-uuid"}, false},
		{outboxEvent{TargetType: "dashboard", PrimaryID: id}, false},
	}
	for _, tc := range cases {
		if got := tc.e.valid(); got != tc.want {
			t.Errorf("%+v valid = %v, want %v", tc.e, got, tc.want)
		}
	}
}
