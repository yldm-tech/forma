package mcp

import (
	"bytes"
	"io"
	"log/slog"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/survey"
	"github.com/yldm-tech/forma/apps/server/internal/workflow"
)

// The operations engine is built during wiring, before the HTTP server's engine; gin must already be in release mode then, or its debug route listing lands in the JSON log stream. Not parallel: gin's mode and writer are process-wide.
func TestOperationsEngineIsBuiltInReleaseMode(t *testing.T) {
	mode, writer := gin.Mode(), gin.DefaultWriter
	t.Cleanup(func() {
		gin.SetMode(mode)
		gin.DefaultWriter = writer
	})
	var out bytes.Buffer
	gin.DefaultWriter = &out
	gin.SetMode(gin.DebugMode)

	newOperations(&survey.Handler{}, &workflow.Handler{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if gin.Mode() != gin.ReleaseMode {
		t.Errorf("gin mode = %q, want release", gin.Mode())
	}
	if out.Len() != 0 {
		t.Errorf("gin wrote %q, want nothing", out.String())
	}
}
