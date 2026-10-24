package authz

import (
	"testing"
)

func TestParseBackfill(t *testing.T) {
	org := "0190a6e2-7c1e-7a8b-9c3d-4e5f60718293"
	valid := []struct {
		args []string
		want BackfillRequest
	}{
		{nil, BackfillRequest{MaxPrune: MaxPrune, Scope: BackfillScope{Kind: ScopeAll}}},
		{[]string{"--apply", "--scope=all"}, BackfillRequest{Apply: true, MaxPrune: MaxPrune, Scope: BackfillScope{Kind: ScopeAll}}},
		{[]string{"--organization-id=" + org}, BackfillRequest{MaxPrune: MaxPrune, Scope: BackfillScope{Kind: ScopeOrganization, OrganizationID: org}}},
		{[]string{"--workspace-id=" + org, "--max-prune=7"}, BackfillRequest{MaxPrune: 7, Scope: BackfillScope{Kind: ScopeWorkspace, WorkspaceID: org}}},
		{[]string{"--after-organization-id=" + org}, BackfillRequest{MaxPrune: MaxPrune, Scope: BackfillScope{Kind: ScopeAll, AfterOrganizationID: org}}},
		{[]string{"--apply", "--prune", "--confirm-prune", "--expected-endpoint=spicedb:50051", "--scope=all"}, BackfillRequest{Apply: true, Prune: true, MaxPrune: MaxPrune, Scope: BackfillScope{Kind: ScopeAll}}},
	}
	for _, tc := range valid {
		cmd, ok := parseCLI(append([]string{"backfill"}, tc.args...))
		if !ok || cmd.backfill != tc.want {
			t.Errorf("parse %v = %+v, %v; want %+v", tc.args, cmd.backfill, ok, tc.want)
		}
	}
	invalid := [][]string{
		{"--scope=organization"},
		{"--scope=all", "--organization-id=" + org},
		{"--organization-id=" + org, "--workspace-id=" + org},
		{"--organization-id=" + org, "--after-organization-id=" + org},
		{"--organization-id=clh7v5w8x0000abcdefghijk"},
		{"--organization-id=" + org, "--organization-id=" + org},
		{"--max-prune=0"},
		{"--max-prune=501"},
		{"--max-prune=07"},
		{"--unknown"},
		{"--confirm-prune"},
		{"--prune", "--confirm-prune", "--expected-endpoint=x:1", "--scope=all"},
		{"--apply", "--prune", "--expected-endpoint=x:1", "--scope=all"},
		{"--apply", "--prune", "--confirm-prune", "--scope=all"},
		{"--apply", "--prune", "--confirm-prune", "--expected-endpoint=x:1"},
	}
	for _, args := range invalid {
		if _, ok := parseCLI(append([]string{"backfill"}, args...)); ok {
			t.Errorf("parse %v accepted", args)
		}
	}
}

func TestParseSchemaAndOutbox(t *testing.T) {
	digest := "sha256:" + "ab0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"[:64]
	valid := []struct {
		args          []string
		group, action string
	}{
		{[]string{"schema", "check"}, "schema", "check"},
		{[]string{"schema", "apply"}, "schema", "apply"},
		{[]string{"schema", "apply", "--expected-current-digest", digest}, "schema", "apply"},
		{[]string{"schema", "apply", "--expected-current-digest=" + digest}, "schema", "apply"},
		{[]string{"outbox", "status"}, "outbox", "status"},
		{[]string{"outbox", "replay"}, "outbox", "replay"},
		{[]string{"outbox", "drain"}, "outbox", "drain"},
		{[]string{"outbox", "drain", "--max-batches=1000"}, "outbox", "drain"},
	}
	for _, tc := range valid {
		cmd, ok := parseCLI(tc.args)
		if !ok || cmd.group != tc.group || cmd.action != tc.action {
			t.Errorf("parse %v = %+v, %v", tc.args, cmd, ok)
		}
	}
	if cmd, _ := parseCLI([]string{"outbox", "drain"}); cmd.maxBatches != DefaultDrainBatches {
		t.Errorf("default max batches = %d", cmd.maxBatches)
	}
	for _, args := range [][]string{
		nil, {"schema"}, {"schema", "check", "--x"}, {"schema", "apply", "--expected-current-digest", "sha256:short"},
		{"outbox"}, {"outbox", "status", "--x"}, {"outbox", "drain", "--max-batches=1001"}, {"outbox", "drain", "--max-batches=0"}, {"other"},
	} {
		if _, ok := parseCLI(args); ok {
			t.Errorf("parse %v accepted", args)
		}
	}
}
