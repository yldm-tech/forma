package app

import (
	"context"
	"log/slog"

	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// newSSO is single sign-on as `forma serve` runs it: like forma2 at start-up, it loads the SAML connection from the saml-connection directory in the working directory before serving.
func newSSO(ctx context.Context, cfg config.Config, svc *auth.Service, log *slog.Logger) *auth.SSO {
	sso := auth.NewSSO(cfg, svc, log)
	sso.PreloadSAMLConnection(ctx, auth.SAMLConnectionDir)
	return sso
}
