package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/caarlos0/env/v11"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/platform/logger"
	"github.com/yldm-tech/forma/apps/server/internal/seed"
)

// defaultFixturesPath is where `forma seed --contract` writes the contract tests' id map when run from the repository root, the directory apps/docs/api-v3-reference/contract-tests/hooks.py reads it from.
const defaultFixturesPath = "apps/docs/api-v3-reference/contract-tests/fixtures.json"

// seedEnv is what `forma seed` reads besides the database: forma2's ALLOW_SEED switch and SEED_API_KEY.
type seedEnv struct {
	AllowSeed  string `env:"ALLOW_SEED"`
	SeedAPIKey string `env:"SEED_API_KEY"`
}

// seedFlags are the command line of `forma seed`.
type seedFlags struct {
	clear    bool
	contract bool
	out      string
}

func parseSeedFlags(args []string) (seedFlags, error) {
	var f seedFlags
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&f.clear, "clear", false, "")
	flags.BoolVar(&f.contract, "contract", false, "")
	flags.StringVar(&f.out, "out", defaultFixturesPath, "")
	if err := flags.Parse(args); err != nil {
		return seedFlags{}, err
	}
	if flags.NArg() > 0 {
		return seedFlags{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if f.out == "" {
		return seedFlags{}, errors.New("--out needs a path")
	}
	return f, nil
}

// seedAllowed reads ALLOW_SEED as the 1/0 switch every boolean variable is. Seeding writes known passwords and, with SEED_API_KEY, an API key, so it runs only when asked for explicitly: unlike forma2, which refused only when NODE_ENV was production, there is no environment the binary could tell apart.
func seedAllowed(v string) (bool, error) {
	switch v {
	case "1":
		return true, nil
	case "", "0":
		return false, nil
	default:
		return false, fmt.Errorf("ALLOW_SEED must be 1 or 0, got %q", v)
	}
}

// runSeed is `forma seed [--clear] [--contract [--out path]]`: forma2's db:seed, db:seed:clear and db:seed:contract.
func runSeed(ctx context.Context, args []string) error {
	f, err := parseSeedFlags(args)
	if err != nil {
		return err
	}
	e, err := env.ParseAs[seedEnv]()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	allowed, err := seedAllowed(e.AllowSeed)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !allowed {
		return errors.New("refusing to seed: the seed writes demo users with a published password, so set ALLOW_SEED=1 to confirm this database is disposable")
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

	fixtures, err := seed.Run(ctx, db, seed.Options{Clear: f.clear, APIKeySecret: e.SeedAPIKey, Contract: f.contract}, log)
	if err != nil {
		return err
	}
	if fixtures != nil {
		b, err := json.MarshalIndent(fixtures, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(f.out, append(b, '\n'), 0o600); err != nil {
			return fmt.Errorf("write contract fixtures: %w", err)
		}
		log.InfoContext(ctx, "wrote the contract fixtures", "path", f.out)
	}

	fmt.Printf("Seeded. Sign in with any of these, password %q:\n", seed.Password)
	for _, u := range seed.Users {
		fmt.Printf("  %-8s %s\n", u.Role, u.Email)
	}
	if e.SeedAPIKey != "" {
		fmt.Println("The seeded API key is fbk_ followed by SEED_API_KEY.")
	}
	return nil
}
