package ai_test

import (
	"log/slog"

	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/ai"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/response"
)

// The example responses screen quotas through the real response service; only ScreenQuotas is called, which needs nothing but the transaction and the logger.
func init() {
	ai.NewQuotaScreener = func(db *gorm.DB, log *slog.Logger) ai.QuotaScreener {
		return response.NewService(config.Config{}, db, nil, nil, nil, nil, nil, log)
	}
}
