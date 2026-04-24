package app

import (
	"strings"

	"feidex/internal/config"
	"feidex/internal/pathdisplay"
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

func renderWorkspaceDisplayPath(path, workspaceCwd string) string {
	return pathdisplay.RenderWorkspaceDisplayPath(path, workspaceCwd)
}

func splitPathLineReference(path string) (base, suffix string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	base = trimLineReferenceSuffix(path)
	return base, strings.TrimPrefix(path, base)
}
