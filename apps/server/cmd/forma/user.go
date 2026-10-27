package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/platform/logger"
)

const userUsage = `usage: forma user <command> <email>

commands:
  activate    turn the account with the email back on: it signs in again and keeps the organizations that have not suspended it
  deactivate  turn the account off on the whole instance: it signs in nowhere, its sessions stop working on their next request, and its MCP clients' OAuth grants are revoked

An organization's owners and managers suspend and restore a member's access to their own organization; turning an account off or on is the instance operator's alone, and this is the operator's tool for it.
`

// errUserUsage is a malformed `forma user` command line.
var errUserUsage = errors.New("expected `forma user activate <email>` or `forma user deactivate <email>`")

// parseUserArgs reads `forma user <activate|deactivate> <email>` into the switch's new value and the email.
func parseUserArgs(args []string) (active bool, email string, err error) {
	if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
		return false, "", errUserUsage
	}
	switch args[0] {
	case "activate":
		active = true
	case "deactivate":
	default:
		return false, "", errUserUsage
	}
	return active, strings.TrimSpace(args[1]), nil
}

// runUser is `forma user activate|deactivate <email>`: the instance operator's switch on an account (users.is_active), which no API writes. The users trigger projects the change to SpiceDB as any other write of the column does.
func runUser(ctx context.Context, args []string, stdout io.Writer) error {
	active, email, err := parseUserArgs(args)
	if err != nil {
		return err
	}
	cfg, err := config.LoadForMigrate()
	if err != nil {
		return err
	}
	cfg.DatabaseMaxOpenConns = 2
	db, closeDB, err := database.Open(ctx, cfg, logger.New(cfg, os.Stderr))
	if err != nil {
		return err
	}
	defer closeDB()
	return setAccountActive(ctx, db, stdout, email, active)
}

// setAccountActive turns the account with the email on or off and reports what it did; an account already in that state is left alone. Turned off, the account's OAuth grants are revoked in the same transaction. Turned on, it names the organizations that still suspend the account, which their owners and managers restore.
func setAccountActive(ctx context.Context, db *gorm.DB, stdout io.Writer, email string, active bool) error {
	var rows []struct {
		ID       string
		Email    string
		IsActive bool
	}
	if err := db.WithContext(ctx).Raw(`SELECT id::text, email, is_active FROM users WHERE lower(email) = lower(?)`, email).Scan(&rows).Error; err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("no user has the email %q", email)
	}
	u := rows[0]
	state := map[bool]string{true: "active", false: "deactivated"}[active]
	if u.IsActive == active {
		_, err := fmt.Fprintf(stdout, "%s is already %s\n", u.Email, state)
		return err
	}
	var grants int64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE users SET is_active = ?, updated_at = now() WHERE id = ?`, active, u.ID).Error; err != nil {
			return fmt.Errorf("update user: %w", err)
		}
		if active {
			return nil
		}
		// The OAuth grants go with the switch: an inactive user's tokens are refused anyway, but a refresh token left standing would work again the day the account is turned back on.
		id, err := uuid.Parse(u.ID)
		if err != nil {
			return fmt.Errorf("parse user id: %w", err)
		}
		grants, err = auth.RevokeOAuthGrants(ctx, tx, id)
		return err
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "%s is now %s\n", u.Email, state); err != nil {
		return err
	}
	if !active {
		if grants > 0 {
			_, err := fmt.Fprintf(stdout, "Revoked %d OAuth refresh token(s): its MCP clients must be authorized again.\n", grants)
			return err
		}
		return nil
	}
	var suspending []string
	if err := db.WithContext(ctx).Raw(`SELECT o.name FROM memberships m JOIN organizations o ON o.id = m.organization_id WHERE m.user_id = ? AND m.suspended_at IS NOT NULL ORDER BY o.name`, u.ID).Scan(&suspending).Error; err != nil {
		return fmt.Errorf("list suspensions: %w", err)
	}
	if len(suspending) > 0 {
		_, err := fmt.Fprintf(stdout, "Still suspended in %s: an owner or manager of each restores the access on its members page.\n", strings.Join(suspending, ", "))
		return err
	}
	return nil
}
