package app

type menuNodeRenderer func(a *App, sessionKey string) (map[string]any, bool)

func menuNodeRenderers() map[string]menuNodeRenderer {
	return menuNodeRenderersRegistry()
}
