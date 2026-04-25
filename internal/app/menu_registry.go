package app

import (
	"context"
	"time"
)

type menuNodeRenderer func(a *App, sessionKey string) (map[string]any, bool)

var menuNodeRenderers = map[string]menuNodeRenderer{
	"menu.root": func(a *App, sessionKey string) (map[string]any, bool) {
		return renderCommandMenuCard(a, sessionKey), true
	},
	"menu.tools": func(a *App, sessionKey string) (map[string]any, bool) {
		return renderToolsMenuCard(a, sessionKey), true
	},
	"menu.review": func(a *App, sessionKey string) (map[string]any, bool) {
		return newReviewFormService(a).renderReviewMenuCard(sessionKey), true
	},
	"menu.skills": func(a *App, sessionKey string) (map[string]any, bool) {
		card, err := newSkillsService(a).renderSkillsCard(sessionKey, false)
		if err != nil {
			return nil, false
		}
		return card, true
	},
	"menu.group.model": func(a *App, sessionKey string) (map[string]any, bool) {
		return newBackendConfigurationService(a).renderModelMenuCard(sessionKey), true
	},
	"menu.group.system": func(a *App, sessionKey string) (map[string]any, bool) {
		return renderSystemMenuCard(a, sessionKey), true
	},
	"menu.group.backend": func(a *App, sessionKey string) (map[string]any, bool) {
		return renderBackendMenuCard(a, sessionKey), true
	},
	"menu.codex_upgrade": func(a *App, sessionKey string) (map[string]any, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		view, err := newBackendUpgradeService(a).loadCodexUpgradeView(ctx, false)
		if err != nil {
			return nil, false
		}
		return newUpgradeRenderService(a).renderCodexUpgradeStatusCard(sessionKey, view, false), true
	},
	"menu.claude_upgrade": func(a *App, sessionKey string) (map[string]any, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		view, err := newBackendUpgradeService(a).loadClaudeUpgradeView(ctx, false)
		if err != nil {
			return nil, false
		}
		return newUpgradeRenderService(a).renderClaudeUpgradeStatusCard(sessionKey, view, false), true
	},
	"menu.thread": func(a *App, sessionKey string) (map[string]any, bool) {
		card, err := conversationBackend(a).renderThreadsCard(sessionKey, false)
		if err != nil {
			return nil, false
		}
		return card, true
	},
	"menu.workspace": func(a *App, sessionKey string) (map[string]any, bool) {
		return newWorkspaceConfigService(a).renderWorkspaceMenuCard(sessionKey), true
	},
	"menu.debug.logs": func(a *App, sessionKey string) (map[string]any, bool) {
		return newDebugService(a).renderDebugLogsCard(sessionKey), true
	},
}
