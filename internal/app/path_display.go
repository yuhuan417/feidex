package app

import (
	"path/filepath"
	"strings"

	"feidex/internal/config"
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
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	base, suffix := splitPathLineReference(path)
	base = strings.TrimSpace(base)
	if base == "" {
		return path
	}
	if !filepath.IsAbs(base) {
		return filepath.Clean(base) + suffix
	}
	workspaceCwd = strings.TrimSpace(workspaceCwd)
	if workspaceCwd != "" && pathWithinWorkspace(base, workspaceCwd) {
		if rel, err := filepath.Rel(workspaceCwd, base); err == nil && strings.TrimSpace(rel) != "" {
			return filepath.Clean(rel) + suffix
		}
	}
	return filepath.Clean(base) + suffix
}

func splitPathLineReference(path string) (base, suffix string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	base = trimLineReferenceSuffix(path)
	return base, strings.TrimPrefix(path, base)
}
