package app

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
	"menu.group.model": func(a *App, sessionKey string) (map[string]any, bool) {
		return a.renderModelMenuCard(sessionKey), true
	},
	"menu.group.system": func(a *App, sessionKey string) (map[string]any, bool) {
		return a.renderSystemMenuCard(sessionKey), true
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
