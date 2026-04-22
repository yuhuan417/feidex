package app

import (
	"context"
	"time"
)

type menuNodeRenderer func(a *App, sessionKey string) (map[string]any, bool)

var menuNodeRenderers = map[string]menuNodeRenderer{
	"menu.root": func(a *App, sessionKey string) (map[string]any, bool) {
		return a.renderCommandMenuCard(sessionKey), true
	},
	"menu.tools": func(a *App, sessionKey string) (map[string]any, bool) {
		return a.renderToolsMenuCard(sessionKey), true
	},
	"menu.review": func(a *App, sessionKey string) (map[string]any, bool) {
		return a.renderReviewMenuCard(sessionKey), true
	},
	"menu.skills": func(a *App, sessionKey string) (map[string]any, bool) {
		card, err := a.renderSkillsCard(sessionKey, false)
		if err != nil {
			return nil, false
		}
		return card, true
	},
	"menu.group.model": func(a *App, sessionKey string) (map[string]any, bool) {
		return a.renderModelMenuCard(sessionKey), true
	},
	"menu.group.system": func(a *App, sessionKey string) (map[string]any, bool) {
		return a.renderSystemMenuCard(sessionKey), true
	},
	"menu.group.backend": func(a *App, sessionKey string) (map[string]any, bool) {
		return a.renderBackendMenuCard(sessionKey), true
	},
	"menu.codex_upgrade": func(a *App, sessionKey string) (map[string]any, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		view, err := a.loadCodexUpgradeView(ctx, false)
		if err != nil {
			return nil, false
		}
		return a.renderCodexUpgradeStatusCard(sessionKey, view, false), true
	},
	"menu.claude_upgrade": func(a *App, sessionKey string) (map[string]any, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		view, err := a.loadClaudeUpgradeView(ctx, false)
		if err != nil {
			return nil, false
		}
		return a.renderClaudeUpgradeStatusCard(sessionKey, view, false), true
	},
	"menu.thread": func(a *App, sessionKey string) (map[string]any, bool) {
		card, err := a.renderThreadsCard(sessionKey, false)
		if err != nil {
			return nil, false
		}
		return card, true
	},
	"menu.workspace": func(a *App, sessionKey string) (map[string]any, bool) {
		return a.renderWorkspaceMenuCard(sessionKey), true
	},
	"menu.debug.logs": func(a *App, sessionKey string) (map[string]any, bool) {
		return a.renderDebugLogsCard(sessionKey), true
	},
}
