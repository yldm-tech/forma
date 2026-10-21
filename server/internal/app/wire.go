//go:build wireinject

package app

import (
	"context"
	"log/slog"

	"github.com/google/wire"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/health"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/database"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/platform/valkey"
	"github.com/yldm-tech/forma/server/internal/webui"
)

// platformSet is the shared infrastructure of `forma serve`, bundled into Platform for the feature modules.
var platformSet = wire.NewSet(
	valkey.Open,
	ratelimit.New,
	mailer.New,
	audit.New,
	httpapi.NewClientIPResolver,
	secret.NewCipher,
	wire.Struct(new(Platform), "*"),
)

// authSet is the credential module: sessions, sign-up, sign-in, two-factor authentication and the account flows under /api/app/auth.
var authSet = wire.NewSet(
	auth.NewRepository,
	auth.NewSessions,
	auth.NewService,
	auth.NewTwoFactor,
	auth.NewHandler,
)

// InitializeServer builds `forma serve`. The returned cleanup closes the database pool, the Valkey client and the SpiceDB connection.
func InitializeServer(ctx context.Context, cfg config.Config, log *slog.Logger) (*httpserver.Server, func(), error) {
	wire.Build(
		database.Open,
		authz.OpenSpiceDB,
		authz.NewAuthorizer,
		platformSet,
		authSet,
		health.NewDatabasePinger,
		health.NewHandler,
		webui.New,
		NewRouter,
		httpserver.New,
	)
	return nil, nil, nil
}

// InitializeWorker builds `forma worker`. The returned cleanup closes the job queue's pool, the database pool and the SpiceDB connection.
func InitializeWorker(ctx context.Context, cfg config.Config, log *slog.Logger) (*jobs.Worker, func(), error) {
	wire.Build(
		jobs.OpenPool,
		database.Open,
		authz.OpenSpiceDB,
		authz.NewDelivery,
		authz.NewAudit,
		NewJobRegistry,
		jobs.NewWorker,
	)
	return nil, nil, nil
}
