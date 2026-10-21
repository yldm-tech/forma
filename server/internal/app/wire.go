//go:build wireinject

package app

import (
	"context"
	"log/slog"

	"github.com/google/wire"

	"github.com/yldm-tech/forma/server/internal/health"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/database"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
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
	wire.Struct(new(Platform), "*"),
)

// InitializeServer builds `forma serve`. The returned cleanup closes the database pool and the Valkey client.
func InitializeServer(ctx context.Context, cfg config.Config, log *slog.Logger) (*httpserver.Server, func(), error) {
	wire.Build(
		database.Open,
		platformSet,
		health.NewDatabasePinger,
		health.NewHandler,
		webui.New,
		NewRouter,
		httpserver.New,
	)
	return nil, nil, nil
}

// InitializeWorker builds `forma worker`. The returned cleanup closes the job queue's connection pool.
func InitializeWorker(ctx context.Context, cfg config.Config, log *slog.Logger) (*jobs.Worker, func(), error) {
	wire.Build(
		jobs.OpenPool,
		jobs.NewRegistry,
		jobs.NewWorker,
	)
	return nil, nil, nil
}
