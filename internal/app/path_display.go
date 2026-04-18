package app

import (
	"strings"

	"feidex/internal/config"
	"feidex/internal/pathdisplay"
)

func (a *App) workspaceCwd(workspaceID string) string {
	if a == nil {
		return ""
	}
	if ws := config.FindWorkspace(a.cfg, workspaceID); ws != nil {
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
