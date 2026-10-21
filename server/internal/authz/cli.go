package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/database"
	"github.com/yldm-tech/forma/server/internal/platform/logger"
)

// CLIUsage documents `forma authz`.
const CLIUsage = `usage: forma authz <command>

commands:
  schema check
      compare SpiceDB's schema with authzed/schema.zed; exit 2 on drift
  schema apply [--expected-current-digest=sha256:<hex>]
      write the schema; a SpiceDB that already holds a different one needs its current digest (from schema check)
  backfill [--apply] [--scope=all | --organization-id=<id> | --workspace-id=<id>] [--after-organization-id=<id>]
           [--prune --confirm-prune --expected-endpoint=<host:port>] [--max-prune=<1-500>]
      rebuild SpiceDB's relationships from PostgreSQL; a dry run (the default) only reports. Exit 0 reconciled, 2 drifted, 1 failed
  outbox status | drain [--max-batches=<1-1000>] | replay
      inspect, deliver, or hand dead letters back to the projection outbox; status exits 2 when it is unhealthy

Every command prints one JSON object on stdout; logs go to stderr.
`

type cliCommand struct {
	group, action string
	digest        string
	backfill      BackfillRequest
	endpoint      string
	maxBatches    int
}

var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func parseCLI(args []string) (cliCommand, bool) {
	if len(args) == 0 {
		return cliCommand{}, false
	}
	switch args[0] {
	case "schema":
		return parseSchema(args[1:])
	case "backfill":
		return parseBackfill(args[1:])
	case "outbox":
		return parseOutbox(args[1:])
	}
	return cliCommand{}, false
}

func parseSchema(args []string) (cliCommand, bool) {
	cmd := cliCommand{group: "schema"}
	switch {
	case len(args) == 1 && (args[0] == "check" || args[0] == "apply"):
		cmd.action = args[0]
	case len(args) == 2 && args[0] == "apply" && strings.HasPrefix(args[1], "--expected-current-digest="):
		cmd.action, cmd.digest = "apply", strings.TrimPrefix(args[1], "--expected-current-digest=")
	case len(args) == 3 && args[0] == "apply" && args[1] == "--expected-current-digest":
		cmd.action, cmd.digest = "apply", args[2]
	default:
		return cmd, false
	}
	return cmd, cmd.digest == "" || digestPattern.MatchString(cmd.digest)
}

func parseOutbox(args []string) (cliCommand, bool) {
	cmd := cliCommand{group: "outbox", maxBatches: DefaultDrainBatches}
	if len(args) == 0 {
		return cmd, false
	}
	cmd.action = args[0]
	switch {
	case (cmd.action == "status" || cmd.action == "replay" || cmd.action == "drain") && len(args) == 1:
		return cmd, true
	case cmd.action == "drain" && len(args) == 2 && strings.HasPrefix(args[1], "--max-batches="):
		n, ok := boundedInt(strings.TrimPrefix(args[1], "--max-batches="), 1000)
		cmd.maxBatches = n
		return cmd, ok
	}
	return cmd, false
}

// boundedInt accepts 1..limit written without sign or leading zeros.
func boundedInt(s string, limit int) (int, bool) {
	if s == "" || len(s) > 7 || s[0] == '0' || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 1 && n <= limit
}

// parseBackfill accepts only known flags, each at most once. Pruning needs --apply, --confirm-prune, --expected-endpoint and an explicit scope; the scopes are mutually exclusive; --after-organization-id resumes a full sweep only.
func parseBackfill(args []string) (cliCommand, bool) {
	cmd := cliCommand{group: "backfill", backfill: BackfillRequest{MaxPrune: MaxPrune, Scope: BackfillScope{Kind: ScopeAll}}}
	values := map[string]string{}
	var confirmed bool
	for _, arg := range args {
		switch arg {
		case "--apply":
			cmd.backfill.Apply = true
			continue
		case "--prune":
			cmd.backfill.Prune = true
			continue
		case "--confirm-prune":
			confirmed = true
			continue
		}
		name, value, ok := strings.Cut(arg, "=")
		known := []string{"--after-organization-id", "--expected-endpoint", "--max-prune", "--organization-id", "--scope", "--workspace-id"}
		if !ok || !slices.Contains(known, name) {
			return cmd, false
		}
		if _, repeated := values[name]; repeated {
			return cmd, false
		}
		values[name] = value
	}
	scope, hasScope := values["--scope"]
	org, hasOrg := values["--organization-id"]
	workspace, hasWorkspace := values["--workspace-id"]
	after, hasAfter := values["--after-organization-id"]
	if hasScope && scope != ScopeAll {
		return cmd, false
	}
	if countTrue(hasScope, hasOrg, hasWorkspace) > 1 || (hasAfter && (hasOrg || hasWorkspace)) {
		return cmd, false
	}
	for _, id := range []struct {
		set   bool
		value string
	}{{hasOrg, org}, {hasWorkspace, workspace}, {hasAfter, after}} {
		if id.set && uuid.Validate(id.value) != nil {
			return cmd, false
		}
	}
	switch {
	case hasOrg:
		cmd.backfill.Scope = BackfillScope{Kind: ScopeOrganization, OrganizationID: canonicalID(org)}
	case hasWorkspace:
		cmd.backfill.Scope = BackfillScope{Kind: ScopeWorkspace, WorkspaceID: canonicalID(workspace)}
	case hasAfter:
		cmd.backfill.Scope.AfterOrganizationID = canonicalID(after)
	}
	if raw, ok := values["--max-prune"]; ok {
		n, valid := boundedInt(raw, MaxPrune)
		if !valid {
			return cmd, false
		}
		cmd.backfill.MaxPrune = n
	}
	endpoint, hasEndpoint := values["--expected-endpoint"]
	cmd.endpoint = endpoint
	if !cmd.backfill.Prune {
		return cmd, !confirmed
	}
	return cmd, cmd.backfill.Apply && confirmed && hasEndpoint && endpoint != "" && countTrue(hasScope, hasOrg, hasWorkspace) == 1
}

func countTrue(flags ...bool) int {
	n := 0
	for _, f := range flags {
		if f {
			n++
		}
	}
	return n
}

type cliFailure struct {
	Code      Code   `json:"code"`
	Retryable bool   `json:"retryable"`
	Status    string `json:"status"`
}

func failureOf(err error) cliFailure {
	e := normalize(err, "cli")
	return cliFailure{Code: e.Code, Retryable: e.Retryable, Status: StatusFailed}
}

// RunCLI runs `forma authz` and returns the exit code: 0 clean, 2 drift or an unhealthy outbox, 1 failed or misused.
func RunCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	write := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(stdout, "%s\n", b)
	}
	cmd, ok := parseCLI(args)
	if !ok {
		_, _ = fmt.Fprint(stderr, CLIUsage)
		write(cliFailure{Code: CodeInvalidRequest, Status: StatusFailed})
		return 1
	}
	cfg, err := config.LoadForAuthz()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "forma authz: %v\n", err)
		return 1
	}
	log := logger.New(cfg, stderr)

	needsSpiceDB := cmd.group != "outbox" || cmd.action == "drain"
	if needsSpiceDB && !cfg.Authz.Enabled {
		write(failureOf(newError(CodeDisabled, "cli", false, 0)))
		return 1
	}
	if cmd.group == "backfill" && cmd.endpoint != "" && cmd.endpoint != cfg.Authz.Endpoint {
		// The operator named a different SpiceDB than the configured one: refuse rather than guess.
		write(failureOf(newError(CodeFailedPrecondition, "backfill_cli", false, 0)))
		return 1
	}

	var client *Client
	if needsSpiceDB {
		if client, err = NewClient(cfg.Authz, BulkProfile, log); err != nil {
			write(failureOf(err))
			return 1
		}
		defer func() { _ = client.Close() }()
	}
	if cmd.group == "schema" {
		return runSchemaCLI(ctx, cmd, client, write)
	}

	db, closeDB, err := database.Open(ctx, cfg, log)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "forma authz: %v\n", err)
		return 1
	}
	defer closeDB()
	if cmd.group == "backfill" {
		res := NewBackfill(db, client, log).Run(ctx, cmd.backfill)
		write(res)
		return backfillExitCode(res.Status)
	}
	return runOutboxCLI(ctx, cmd, db, client, log, write)
}

func runSchemaCLI(ctx context.Context, cmd cliCommand, client *Client, write func(any)) int {
	if cmd.action == "check" {
		state, err := CheckSchema(ctx, client)
		if err != nil {
			write(failureOf(err))
			return 1
		}
		write(state)
		if state.Status != SchemaMatched {
			return 2
		}
		return 0
	}
	state, err := ApplySchema(ctx, client, cmd.digest)
	if err != nil {
		write(failureOf(err))
		return 1
	}
	write(state)
	return 0
}

func backfillExitCode(status string) int {
	switch status {
	case StatusReconciled:
		return 0
	case StatusDrifted:
		return 2
	default:
		return 1
	}
}

func runOutboxCLI(ctx context.Context, cmd cliCommand, db *gorm.DB, client *Client, log *slog.Logger, write func(any)) int {
	switch cmd.action {
	case "status":
		s, err := outboxStatus(ctx, db)
		if err != nil {
			write(failureOf(err))
			return 1
		}
		health := "healthy"
		switch {
		case s.DeadLettered > 0 || s.RevocationsPastCritical > 0:
			health = "critical"
		case s.RevocationsPastWarning > 0:
			health = "warning"
		}
		write(struct {
			OutboxStatus
			Status string `json:"status"`
		}{s, health})
		if health != "healthy" {
			return 2
		}
		return 0
	case "replay":
		n, err := replayDeadLetters(ctx, db)
		if err != nil {
			write(failureOf(err))
			return 1
		}
		write(map[string]any{"replayed": n, "status": "replayed"})
		return 0
	default:
		res, err := NewDelivery(db, client, log).Drain(ctx, cmd.maxBatches)
		if err != nil {
			write(failureOf(err))
			return 1
		}
		write(res)
		if res.Status != "drained" {
			return 2
		}
		return 0
	}
}
