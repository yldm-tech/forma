package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/org"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/platform/logger"
)

const organizationUsage = `usage: forma organization create --owner <email> --name <name>

commands:
  create  give the user with the email a new organization they own, with its default workspace, as creating one in the app does

On a self-hosted instance only the owner of an organization may create another one in the app, so someone who owns none (an owner whose last organization was deleted, or a user the operator wants to give a tenant of their own) gets one from the operator here. The new rows reach the authorization projection through the worker, which must be running. Forma Cloud lets anyone create organizations in the app and refuses this command.
`

// errOrganizationUsage is a malformed `forma organization` command line.
var errOrganizationUsage = errors.New("expected `forma organization create --owner <email> --name <name>`")

// parseOrganizationArgs reads `forma organization create --owner <email> --name <name>`, the flags in either order and as --flag value or --flag=value.
func parseOrganizationArgs(args []string) (owner, name string, err error) {
	if len(args) < 1 || args[0] != "create" {
		return "", "", errOrganizationUsage
	}
	values := map[string]*string{"--owner": &owner, "--name": &name}
	seen := map[string]bool{}
	for rest := args[1:]; len(rest) > 0; {
		flag, value, inline := strings.Cut(rest[0], "=")
		dst, ok := values[flag]
		if !ok || seen[flag] {
			return "", "", errOrganizationUsage
		}
		if !inline {
			if len(rest) < 2 {
				return "", "", errOrganizationUsage
			}
			value, rest = rest[1], rest[1:]
		}
		rest = rest[1:]
		seen[flag] = true
		*dst = strings.TrimSpace(value)
	}
	if owner == "" || name == "" {
		return "", "", errOrganizationUsage
	}
	return owner, name, nil
}

// runOrganization is `forma organization create`: the operator's way to give a user an organization that the app's rule (org.Service.CanCreate) would not let them create.
func runOrganization(ctx context.Context, args []string, stdout io.Writer) error {
	owner, name, err := parseOrganizationArgs(args)
	if err != nil {
		return err
	}
	// The migration loader reads only the database settings, so the one switch this command depends on is read here, as the full configuration parses it.
	if v := strings.TrimSpace(os.Getenv("IS_FORMA_CLOUD")); v != "" {
		cloud, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("IS_FORMA_CLOUD: %w", err)
		}
		if cloud {
			return errors.New("on Forma Cloud anyone signed in creates organizations in the app, which also sets up their billing")
		}
	}
	cfg, err := config.LoadForMigrate()
	if err != nil {
		return err
	}
	cfg.DatabaseMaxOpenConns = 2
	log := logger.New(cfg, os.Stderr)
	db, closeDB, err := database.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer closeDB()
	return createOrganization(ctx, db, log, stdout, owner, name)
}

// createOrganization creates the organization for the active user with the email and reports it.
func createOrganization(ctx context.Context, db *gorm.DB, log *slog.Logger, stdout io.Writer, email, name string) error {
	name = strings.TrimSpace(name)
	var rows []struct {
		ID       uuid.UUID
		Email    string
		IsActive bool
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, email, is_active FROM users WHERE lower(email) = lower(?)`, email).Scan(&rows).Error; err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("no user has the email %q", email)
	}
	u := rows[0]
	if !u.IsActive {
		return fmt.Errorf("%s is deactivated: turn the account on with `forma user activate %s` first", u.Email, u.Email)
	}
	orgID, workspaceID, err := org.CreateForOwner(ctx, db, log, u.ID, name)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Created organization %s (%s) owned by %s, with workspace %s\n", name, orgID, u.Email, workspaceID)
	return err
}
