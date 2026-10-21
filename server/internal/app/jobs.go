package app

import (
	"log/slog"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// NewJobRegistry is every job kind and schedule `forma worker` runs: the platform's own, then each feature module's.
func NewJobRegistry(log *slog.Logger, delivery *authz.Delivery, audit *authz.Audit) *jobs.Registry {
	reg := jobs.NewRegistry(log)
	authz.RegisterJobs(reg, delivery, audit)
	return reg
}
