package app

import (
	"strings"

	"feidex/internal/config"
)

func workspaceCwd(cfg *config.Config, workspaceID string) string {
	if cfg == nil {
		return ""
	}
	if ws := config.FindWorkspace(cfg, workspaceID); ws != nil {
		return strings.TrimSpace(ws.Cwd)
	}
	return ""
}
