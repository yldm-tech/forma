//go:build wireinject

package app

import (
	"context"
	"log/slog"

	"github.com/google/wire"

	"github.com/yldm-tech/forma/server/internal/health"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/database"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/webui"
)

// InitializeServer builds `forma serve`. The returned cleanup closes the database pool.
func InitializeServer(ctx context.Context, cfg config.Config, log *slog.Logger) (*httpserver.Server, func(), error) {
	wire.Build(
		database.Open,
		health.NewDatabasePinger,
		health.NewHandler,
		webui.New,
		NewRouter,
		httpserver.New,
	)
	return nil, nil, nil
}
