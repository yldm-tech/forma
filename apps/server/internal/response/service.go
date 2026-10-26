package response

import (
	"log/slog"

	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/contact"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// Service holds the response rules. The client API's steps take the caller's word only for what forma2 did (the workspace the route resolved); the management operations authorize every call.
type Service struct {
	db      *gorm.DB
	jobs    *jobs.Enqueuer
	cfg     config.Config
	cipher  *secret.Cipher
	authz   *authz.Authorizer
	limiter *ratelimit.Limiter
	audit   *audit.Emitter
	log     *slog.Logger
	// segments evaluates the segments a response filter names.
	segments *contact.Evaluator
}

func NewService(cfg config.Config, db *gorm.DB, enqueuer *jobs.Enqueuer, cipher *secret.Cipher, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter, log *slog.Logger) *Service {
	return &Service{db: db, jobs: enqueuer, cfg: cfg, cipher: cipher, authz: authorizer, limiter: limiter, audit: emitter, log: log, segments: contact.NewEvaluator(db, log)}
}
